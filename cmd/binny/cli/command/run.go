package command

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/creack/pty"
	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"
	"golang.org/x/term"

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
		PreRunE: func(cmd *cobra.Command, args []string) error {
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

func run(path string, args []string) error {
	c := exec.Command(path, args...)

	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}

	// make sure to close the pty at the end
	defer func() { _ = ptmx.Close() }() // best effort

	// handle pty size
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				log.Printf("error resizing pty: %s", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH                        // initial resize
	defer func() { signal.Stop(ch); close(ch) }() // cleanup signals when done

	// Set stdin in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // best effort

	// copy stdin to the pty and the pty to stdout. The goroutine will keep reading until the next keystroke before returning.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	_, _ = io.Copy(os.Stdout, ptmx)

	return nil
}
