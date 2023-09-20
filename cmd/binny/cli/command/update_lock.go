package command

import (
	"fmt"

	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/anchore/binny/cmd/binny/cli/internal/yamlpatch"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/clio"
	"github.com/anchore/go-logger"
)

type UpdateConfig struct {
	Config      string `json:"config" yaml:"config" mapstructure:"config"`
	option.Core `json:"" yaml:",inline" mapstructure:",squash"`
}

func UpdateLock(app clio.Application) *cobra.Command {
	cfg := &UpdateConfig{
		Core: option.DefaultCore(),
	}

	var names []string

	return app.SetupCommand(&cobra.Command{
		Use:   "update-lock",
		Short: "Update pinned tool version configuration with latest versions",
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
	newCfg, err := getUpdatedConfig(cfg.Core, names)
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

func getUpdatedConfig(cfg option.Core, names []string) (*option.Core, error) {
	nameSet := strset.New()
	if len(names) == 0 {
		nameSet.Add(cfg.Tools.Names()...)
	}

	var newCfgs []option.Tool

	for _, toolCfg := range cfg.Tools {
		if !nameSet.Has(toolCfg.Name) {
			newCfgs = append(newCfgs, toolCfg)
			continue
		}

		t, intent, err := toolCfg.ToTool()
		if err != nil {
			return nil, err
		}

		newVersion, err := t.UpdateVersion(intent.Want, intent.Constraint)
		if err != nil {
			return nil, err
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
			newCfgs = append(newCfgs, toolCfg)
			continue
		}

		fields := logger.Fields{
			"tool":    toolCfg.Name,
			"version": fmt.Sprintf("%s ➔ %s", toolCfg.Version.Want, newVersion),
		}
		if toolCfg.Version.Constraint != "" {
			fields["constraint"] = fmt.Sprintf("%q", toolCfg.Version.Constraint)
		}

		log.WithFields(fields).Info("updated tool version pin")

		toolCfg.Version.Want = newVersion

		newCfgs = append(newCfgs, toolCfg)
	}

	newCfg := cfg
	newCfg.Tools = newCfgs

	return &newCfg, nil
}
