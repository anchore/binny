package command

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool"
	"github.com/anchore/clio"
)

type CheckConfig struct {
	Config           string `json:"config" yaml:"config" mapstructure:"config"`
	option.AppConfig `json:"" yaml:",inline" mapstructure:",squash"`
}

func Check(app clio.Application) *cobra.Command {
	cfg := &CheckConfig{
		AppConfig: option.DefaultAppConfig(),
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

func runCheck(cmdCfg CheckConfig, names []string) error {
	if len(names) == 0 {
		names = cmdCfg.Tools.Names()
	}

	toolOpts, err := cmdCfg.Tools.GetAllOptions(names)
	if err != nil {
		return err
	}

	if len(toolOpts) == 0 {
		// TODO: should this be an error?
		log.Warn("no tools to verify")
		return nil
	}

	// get the current store state
	store, err := binny.NewStore(cmdCfg.Store.Root)
	if err != nil {
		return err
	}

	var errs error
	var failedTools []string
	for _, opt := range toolOpts {
		t, intent, err := opt.ToTool()
		if err != nil {
			failedTools = append(failedTools, opt.Name)
			errs = multierror.Append(errs, fmt.Errorf("failed to check tool %q: %w", opt.Name, err))
			continue
		}

		resolvedVersion, err := tool.ResolveVersion(t, *intent)
		if err != nil {
			failedTools = append(failedTools, t.Name())
			errs = multierror.Append(errs, fmt.Errorf("failed to check tool %q: %w", t.Name(), err))
			continue
		}

		// otherwise continue to install the tool
		err = tool.Check(t.Name(), resolvedVersion, store, tool.VerifyConfig{
			VerifyXXH64Digest:  true,
			VerifySHA256Digest: cmdCfg.VerifySHA256Digest,
		})
		if err != nil {
			failedTools = append(failedTools, t.Name())
			errs = multierror.Append(errs, fmt.Errorf("failed to check tool %q: %w", t.Name(), err))
			continue
		}

		log.WithFields("tool", t.Name(), "version", resolvedVersion).Debug("installation verified")
	}

	if errs != nil {
		log.WithFields("tools", failedTools).Error("verification failed")
		return errs
	}

	log.Info("all tools verified")

	return nil
}
