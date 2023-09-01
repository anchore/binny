package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/tool"
	"github.com/anchore/clio"
)

type InstallConfig struct {
	Config           string `json:"config" yaml:"config" mapstructure:"config"`
	option.AppConfig `json:"" yaml:",inline" mapstructure:",squash"`
}

func Install(app clio.Application) *cobra.Command {
	cfg := &InstallConfig{
		AppConfig: option.DefaultAppConfig(),
	}

	var names []string

	return app.SetupCommand(&cobra.Command{
		Use:   "install",
		Short: "Install tools",
		Args:  cobra.ArbitraryArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			names = args
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(*cfg, names)
		},
	}, cfg)
}

func runInstall(cfg InstallConfig, names []string) error {
	if len(names) == 0 {
		names = cfg.Tools.Names()
	}

	toolCfgs, err := cfg.Tools.GetAllOptions(names)
	if err != nil {
		return err
	}

	// get the current store state
	store, err := binny.NewStore(cfg.Store.Root)
	if err != nil {
		return err
	}

	for _, opt := range toolCfgs {
		t, intent, err := opt.ToTool()
		if err != nil {
			return err
		}

		// otherwise continue to install the tool
		if err := tool.Install(t, *intent, store); err != nil {
			return fmt.Errorf("failed to install tool %q: %w", t.Name(), err)
		}
	}

	return nil
}
