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

	toolOpts, err := cfg.Tools.GetAllOptions(names)
	if err != nil {
		return err
	}

	if len(toolOpts) == 0 {
		// TODO: should this be an error?
		log.Warn("no tools to install")
		return nil
	}

	// get the current store state
	store, err := binny.NewStore(cfg.Store.Root)
	if err != nil {
		return err
	}

	var errs error
	var failedTools []string
	for _, opt := range toolOpts {
		t, intent, err := opt.ToTool()
		if err != nil {
			failedTools = append(failedTools, opt.Name)
			errs = multierror.Append(errs, fmt.Errorf("failed to install tool %q: %w", opt.Name, err))
			continue
		}

		// otherwise continue to install the tool
		if err := tool.Install(t, *intent, store, cfg.VerifyDigest); err != nil {
			failedTools = append(failedTools, t.Name())
			errs = multierror.Append(errs, fmt.Errorf("failed to install tool %q: %w", t.Name(), err))
			continue
		}
	}

	if errs != nil {
		log.WithFields("tools", failedTools).Error("installation failed")
		return errs
	}

	log.Info("all tools installed")

	return nil
}
