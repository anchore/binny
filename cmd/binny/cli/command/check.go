package command

import (
	"fmt"

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

	// get the current store state
	store, err := binny.NewStore(cmdCfg.Store.Root)
	if err != nil {
		return err
	}

	for _, opt := range toolOpts {
		t, intent, err := opt.ToTool()
		if err != nil {
			return err
		}

		resolvedVersion, err := tool.ResolveVersion(t, *intent)
		if err != nil {
			return fmt.Errorf("failed to resolve version for tool %q: %w", t.Name(), err)
		}

		// otherwise continue to install the tool
		err = tool.Check(t.Name(), resolvedVersion, store)
		if err != nil {
			return fmt.Errorf("failed to install tool %q: %w", t.Name(), err)
		}

		log.WithFields("tool", t.Name(), "version", resolvedVersion).Info("installation verified")
	}

	return nil
}
