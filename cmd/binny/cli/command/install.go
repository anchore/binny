package command

import (
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"
	"golang.org/x/sync/errgroup"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool"
	"github.com/anchore/clio"
)

type InstallConfig struct {
	Config       string `json:"config" yaml:"config" mapstructure:"config"`
	StopOnError  bool   `json:"stopOnError" yaml:"stopOnError" mapstructure:"stopOnError"`
	option.Check `json:"" yaml:",inline" mapstructure:",squash"`
	option.Core  `json:"" yaml:",inline" mapstructure:",squash"`
}

func Install(app clio.Application) *cobra.Command {
	cfg := &InstallConfig{
		StopOnError: false,
		Core:        option.DefaultCore(),
	}

	var names []string

	return app.SetupCommand(&cobra.Command{
		Use:   "install",
		Short: "Install tools",
		Args:  cobra.ArbitraryArgs,
		PreRunE: func(_ *cobra.Command, args []string) error {
			names = args
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runInstall(*cfg, names)
		},
	}, cfg)
}

// nolint: funlen
func runInstall(cmdCfg InstallConfig, names []string) error {
	names, toolOpts := selectNamesAndConfigs(cmdCfg.Core, names)

	if len(toolOpts) == 0 {
		bus.Report("no tools to install")
		log.Warn("no tools to install")
		return nil
	}

	// get the current store state
	store, err := binny.NewStore(cmdCfg.Store.Root)
	if err != nil {
		return err
	}

	var (
		errs                  error
		failedTools           []string
		alreadyInstalledTools []string
		alreadyInstalled      bool
	)

	prog, stage := trackInstallCmd(names)

	defer func() {
		if errs != nil {
			prog.SetError(errs)
		} else {
			if alreadyInstalled {
				stage.Set("Already installed")
			} else {
				stage.Set("Installed")
			}
			prog.SetCompleted()
		}
	}()

	g := errgroup.Group{}
	g.SetLimit(3)
	lock := sync.Mutex{}

	for i := range toolOpts {
		opt := toolOpts[i]

		g.Go(func() error {
			err := installTool(store, cmdCfg, opt)
			if err != nil {
				lock.Lock()
				if errors.Is(err, tool.ErrAlreadyInstalled) {
					alreadyInstalledTools = append(alreadyInstalledTools, opt.Name)
				} else {
					failedTools = append(failedTools, opt.Name)
					errs = multierror.Append(errs, err)
				}
				lock.Unlock()
			}
			prog.Increment()
			if cmdCfg.StopOnError && err != nil {
				return err
			}
			return nil
		})
	}

	// note: we can ignore the error here because we are tracking the error through the multierror object
	g.Wait() //nolint: errcheck

	alreadyInstalled = len(alreadyInstalledTools) > 0 && len(alreadyInstalledTools) == len(toolOpts)

	if errs != nil {
		log.WithFields("failed", failedTools).Warn("failed to install all tools")
		return errs
	}

	if alreadyInstalled {
		log.Info("tools already installed")
	} else {
		log.Info("tools installed")
	}

	return nil
}

func trackInstallCmd(toolNames []string) (*progress.Manual, *progress.AtomicStage) {
	prog := progress.NewManual(int64(len(toolNames)))
	stage := progress.NewAtomicStage("Installing")

	bus.Publish(partybus.Event{
		Type:   event.CLIInstallCmdStarted,
		Source: toolNames,
		Value: struct {
			progress.Stager
			progress.Progressable
		}{
			Stager:       stage,
			Progressable: prog,
		},
	})

	return prog, stage
}

func installTool(store *binny.Store, cfg InstallConfig, opt option.Tool) error {
	t, intent, err := opt.ToTool()
	if err != nil {
		return fmt.Errorf("failed to resolve tool config %q: %w", opt.Name, err)
	}

	// otherwise continue to install the tool
	if err := tool.Install(t, *intent, store, tool.VerifyConfig{
		VerifyXXH64Digest:  true,
		VerifySHA256Digest: cfg.VerifySHA256Digest,
	}); err != nil {
		return fmt.Errorf("failed to install tool %q: %w", t.Name(), err)
	}
	return nil
}
