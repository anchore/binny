package command

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool"
	"github.com/anchore/clio"
)

type CheckConfig struct {
	Config       string `json:"config" yaml:"config" mapstructure:"config"`
	option.Check `json:"" yaml:",inline" mapstructure:",squash"`
	option.Core  `json:"" yaml:",inline" mapstructure:",squash"`
}

func Check(app clio.Application) *cobra.Command {
	cfg := &CheckConfig{
		Core: option.DefaultCore(),
	}

	var names []string

	return app.SetupCommand(&cobra.Command{
		Use:   "check",
		Short: "Verify tool are installed at the configured version",
		Args:  cobra.ArbitraryArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			names = args
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(*cfg, names)
		},
	}, cfg)
}

func runCheck(cmdCfg CheckConfig, names []string) (errs error) {
	names, toolOpts := selectNamesAndConfigs(cmdCfg.Core, names)

	if len(toolOpts) == 0 {
		bus.Report("no tools to verify")
		log.Warn("no tools to verify")
		return nil
	}

	// get the current store state
	store, err := binny.NewStore(cmdCfg.Store.Root)
	if err != nil {
		return err
	}

	monitor := bus.PublishTask(
		event.Title{
			Default:      "Verify installed tools",
			WhileRunning: "Verifying installed tools",
			OnSuccess:    "Verified installed tools",
		},
		"",
		len(toolOpts),
	)

	defer func() {
		if errs != nil {
			monitor.SetError(errs)
		} else {
			monitor.AtomicStage.Set(strings.Join(names, ", "))
			monitor.SetCompleted()
		}
	}()

	var failedTools []string
	for _, opt := range toolOpts {
		monitor.Increment()
		monitor.AtomicStage.Set(opt.Name)

		resolvedVersion, err := checkTool(store, opt, cmdCfg.VerifySHA256Digest)
		if err != nil {
			failedTools = append(failedTools, opt.Name)
			errs = multierror.Append(errs, fmt.Errorf("failed to check tool %q: %w", opt.Name, err))
			continue
		}

		log.WithFields("tool", opt.Name, "version", resolvedVersion).Debug("installation verified")
	}

	if errs != nil {
		log.WithFields("tools", failedTools).Warn("verification failed")
		return errs
	}

	log.Info("all tools verified")

	return nil
}

func checkTool(store *binny.Store, opt option.Tool, verifySha256Digest bool) (string, error) {
	t, intent, err := opt.ToTool()
	if err != nil {
		return "", err
	}

	resolvedVersion, err := tool.ResolveVersion(t, *intent)
	if err != nil {
		return "", err
	}

	// otherwise continue to install the tool
	err = tool.Check(t.Name(), resolvedVersion, store, tool.VerifyConfig{
		VerifyXXH64Digest:  true,
		VerifySHA256Digest: verifySha256Digest,
	})
	if err != nil {
		return resolvedVersion, err
	}

	return resolvedVersion, nil
}
