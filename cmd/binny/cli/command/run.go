package command

import (
	"fmt"
	"path/filepath"

	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/clio"
)

type RunConfig struct {
	Config      string `json:"config" yaml:"config" mapstructure:"config"`
	option.Core `json:"" yaml:",inline" mapstructure:",squash"`
}

func Run(app clio.Application) *cobra.Command {
	cfg := &RunConfig{
		Core: option.DefaultCore(),
	}

	var isHelpFlag bool

	return app.SetupCommand(&cobra.Command{
		Use:                "run NAME [flags] [args]",
		Short:              "run a specific tool",
		DisableFlagParsing: true, // pass these as arguments to the tool
		Args:               cobra.ArbitraryArgs,
		PreRunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("no tool name provided")
			}

			name := args[0]

			if name == "--help" || name == "-h" {
				isHelpFlag = true
			}

			// note: this implies that the application configuration needs to be up to date with the tool names
			// installed.
			if !isHelpFlag && !strset.New(cfg.Tools.Names()...).Has(name) {
				return fmt.Errorf("no tool configured with name: %s", name)
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var toolArgs []string
			if len(args) > 1 {
				toolArgs = args[1:]
			}

			if isHelpFlag {
				return cmd.Help()
			}

			return runRunRUN(*cfg, args[0], toolArgs)
		},
	}, cfg)
}

func runRunRUN(cfg RunConfig, name string, args []string) error {
	store, err := binny.NewStore(cfg.Root)
	if err != nil {
		return err
	}

	entries := store.GetByName(name)
	switch len(entries) {
	case 0:
		return fmt.Errorf("no tool installed with name: %s", name)
	case 1:
		// pass
	default:
		return fmt.Errorf("multiple tools installed with name: %s", name)
	}

	entry := entries[0]

	fullPath, err := filepath.Abs(entry.Path())
	if err != nil {
		return fmt.Errorf("unable to resolve path to tool: %w", err)
	}

	return run(fullPath, args)
}
