package command

import (
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"github.com/anchore/binny/cmd/binny/cli/internal/yamlpatch"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/clio"
	"github.com/anchore/go-logger"
)

type UpdateConfig struct {
	Config      string `json:"config" yaml:"config" mapstructure:"config"`
	StopOnError bool   `json:"stopOnError" yaml:"stopOnError" mapstructure:"stopOnError"`
	option.Core `json:"" yaml:",inline" mapstructure:",squash"`
}

func Update(app clio.Application) *cobra.Command {
	cfg := &UpdateConfig{
		StopOnError: false,
		Core:        option.DefaultCore(),
	}

	var names []string

	return app.SetupCommand(&cobra.Command{
		Use:   "update",
		Short: "Update pinned tool version configuration with latest available versions (that are still within any provided constraints)",
		Args:  cobra.ArbitraryArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			names = args
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(*cfg, names)
		},
	}, cfg)
}

func runUpdate(cfg UpdateConfig, names []string) error {
	newCfg, err := getUpdatedConfig(cfg, names)
	if err != nil {
		return err
	}
	if newCfg == nil {
		return fmt.Errorf("no command config found")
	}

	return yamlpatch.Write(cfg.Config, updateLockYamlPatcher{cfg: *newCfg})
}

var _ yamlpatch.Patcher = (*updateLockYamlPatcher)(nil)

type updateLockYamlPatcher struct {
	cfg option.Core
}

func (p updateLockYamlPatcher) PatchYaml(node *yaml.Node) error {
	toolsNode := yamlpatch.FindToolsSequenceNode(node)
	for _, toolCfg := range p.cfg.Tools {
		toolVersionWantNode := yamlpatch.FindToolVersionWantNode(toolsNode, toolCfg.Name)
		toolVersionWantNode.Value = toolCfg.Version.Want
	}
	return nil
}

// nolint: funlen,gocognit
func getUpdatedConfig(cfg UpdateConfig, names []string) (*option.Core, error) {
	var (
		errs                 error
		newCfgs              []option.Tool
		failedTools          []string
		alreadyUpToDateTools []string
	)

	names, ogCfgs := selectNamesAndConfigs(cfg.Core, names)

	prog, stage := trackUpdateLockCmd(names)

	defer func() {
		if errs != nil {
			prog.SetError(errs)
		} else {
			if len(alreadyUpToDateTools) == len(ogCfgs) {
				stage.Set("versions already up to date")
			}
			prog.SetCompleted()
		}
	}()

	g := errgroup.Group{}
	g.SetLimit(3)
	lock := sync.Mutex{}

	for i := range ogCfgs {
		toolCfg := ogCfgs[i]

		g.Go(func() (err error) {
			var newVersion *string

			ogVersion := toolCfg.Version.Want
			tProg, tState := trackToolUpdateVersion(toolCfg.Name, ogVersion)

			defer func() {
				if err != nil {
					tProg.SetError(err)
				} else {
					if newVersion != nil {
						tState.updated.Set(*newVersion)
					}
					tProg.SetCompleted()
				}
			}()

			tProg.Increment()
			newVersion, err = getUpdatedToolVersion(toolCfg)

			lock.Lock()

			if err != nil {
				failedTools = append(failedTools, toolCfg.Name)
				errs = multierror.Append(errs, err)
				if cfg.StopOnError {
					return err
				}
			}

			if newVersion != nil {
				if *newVersion == ogVersion {
					newVersion = nil
				} else {
					toolCfg.Version.Want = *newVersion
				}
			} else {
				alreadyUpToDateTools = append(alreadyUpToDateTools, toolCfg.Name)
			}

			newCfgs = append(newCfgs, toolCfg)

			lock.Unlock()

			prog.Increment()
			if cfg.StopOnError && err != nil {
				return err
			}
			return nil
		})
	}

	// note: we can ignore the error here because we are tracking the error through the multierror object
	g.Wait() // nolint: errcheck

	newCfg := cfg
	newCfg.Tools = newCfgs

	return &newCfg.Core, errs
}

func selectNamesAndConfigs(cfg option.Core, names []string) ([]string, []option.Tool) {
	nameSet := strset.New(names...)
	if len(names) == 0 {
		nameSet.Add(cfg.Tools.Names()...)
	}

	var ogCfgs []option.Tool

	// always order the tools in the same order as the original config (not what the user might have passed in)
	names = nil
	for i := range cfg.Tools {
		toolCfg := cfg.Tools[i]

		if !nameSet.Has(toolCfg.Name) {
			continue
		}

		ogCfgs = append(ogCfgs, toolCfg)
		names = append(names, toolCfg.Name)
	}
	return names, ogCfgs
}

func trackUpdateLockCmd(toolNames []string) (*progress.Manual, *progress.AtomicStage) {
	prog := progress.NewManual(int64(len(toolNames)))
	stage := progress.NewAtomicStage("")

	bus.Publish(partybus.Event{
		Type:   event.CLIUpdateCmdStarted,
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

func trackToolUpdateVersion(toolName, version string) (*progress.Manual, *toolVersionUpdate) {
	prog := progress.NewManual(-1)
	state := &toolVersionUpdate{
		name:     toolName,
		original: version,
		updated:  progress.NewAtomicStage(""),
	}

	bus.Publish(partybus.Event{
		Type:   event.ToolUpdateVersionStartedEvent,
		Source: state,
		Value:  prog,
	})

	return prog, state
}

var _ event.ToolUpdate = (*toolVersionUpdate)(nil)

type toolVersionUpdate struct {
	name     string
	original string
	updated  *progress.AtomicStage
}

func (t *toolVersionUpdate) Name() string {
	return t.name
}

func (t *toolVersionUpdate) Version() string {
	return t.original
}

func (t *toolVersionUpdate) Updated() string {
	return t.updated.Stage()
}

func getUpdatedToolVersion(toolCfg option.Tool) (*string, error) {
	t, intent, err := toolCfg.ToTool()
	if err != nil {
		return nil, err
	}

	newVersion, err := t.UpdateVersion(intent.Want, intent.Constraint)
	if err != nil {
		return nil, fmt.Errorf("unable to update version for tool %q: %w", toolCfg.Name, err)
	}

	if newVersion == toolCfg.Version.Want {
		fields := logger.Fields{
			"tool":    toolCfg.Name,
			"version": toolCfg.Version.Want,
		}
		if toolCfg.Version.Constraint != "" {
			fields["constraint"] = fmt.Sprintf("%q", toolCfg.Version.Constraint)
		}
		log.WithFields(fields).Debug("tool version pin is up to date")

		return nil, nil
	}

	fields := logger.Fields{
		"tool":    toolCfg.Name,
		"version": fmt.Sprintf("%s ➔ %s", toolCfg.Version.Want, newVersion),
	}
	if toolCfg.Version.Constraint != "" {
		fields["constraint"] = fmt.Sprintf("%q", toolCfg.Version.Constraint)
	}

	log.WithFields(fields).Info("updated tool version pin")

	return &newVersion, nil
}
