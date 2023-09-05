package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/chainguard-dev/yam/pkg/yam/formatted"
	"github.com/google/yamlfmt"
	"github.com/google/yamlfmt/engine"
	"github.com/google/yamlfmt/formatters/basic"
	"github.com/scylladb/go-set/strset"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/clio"
	"github.com/anchore/go-logger"
)

type UpdateConfig struct {
	Config           string `json:"config" yaml:"config" mapstructure:"config"`
	option.AppConfig `json:"" yaml:",inline" mapstructure:",squash"`
}

func UpdateLock(app clio.Application) *cobra.Command {
	cfg := &UpdateConfig{
		AppConfig: option.DefaultAppConfig(),
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
	newCfg, err := getUpdatedConfig(cfg.AppConfig, names)
	if err != nil {
		return err
	}

	return writeBackConfig(cfg.Config, *newCfg)
}

func getUpdatedConfig(cfg option.AppConfig, names []string) (*option.AppConfig, error) {
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

func writeBackConfig(path string, cfg option.AppConfig) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}

	contents, err := io.ReadAll(fh)
	if err != nil {
		return err
	}

	if err := fh.Close(); err != nil {
		return err
	}

	var n yaml.Node
	err = yaml.Unmarshal(contents, &n)
	if err != nil {
		return err
	}

	switch len(n.Content) {
	case 0:
		return fmt.Errorf("no documents found in config file")
	case 1:
		// continue
	default:
		return fmt.Errorf("multiple documents found in config file (expected 1)")
	}

	// take the first document
	doc := n.Content[0]

	patchYamlNode(doc, cfg)

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	document := n.HeadComment + "\n" + string(out) + "\n" + n.FootComment + "\n"

	document, err = formatYaml(document)
	if err != nil {
		return err
	}

	fh, err = os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	defer fh.Close()

	_, err = fh.WriteString(document)

	if err != nil {
		return err
	}

	return nil
}

func patchYamlNode(node *yaml.Node, cfg option.AppConfig) {
	toolsNode := findToolsSequenceNode(node)
	for _, toolCfg := range cfg.Tools {
		toolNode := findToolNode(toolsNode, toolCfg.Name)
		toolVersionNode := findToolVersionNode(toolNode)
		toolVersionWantNode := findToolVersionWantNode(toolVersionNode)
		toolVersionWantNode.Value = toolCfg.Version.Want
	}
}

func findToolsSequenceNode(node *yaml.Node) *yaml.Node {
	for idx, v := range node.Content {
		var next *yaml.Node
		if idx+1 < len(node.Content) {
			next = node.Content[idx+1]
		} else {
			break
		}
		if v.Value == "tools" && next.Tag == "!!seq" {
			return next
		}
	}
	return nil
}

func findToolNode(toolSequenceNode *yaml.Node, name string) *yaml.Node {
	for _, v := range toolSequenceNode.Content {
		if v.Tag != "!!map" {
			continue
		}
		var candidateName string
		// each element in the sequence is a map
		for idx, v2 := range v.Content {
			if idx%2 == 0 && v2.Value == "name" {
				candidateName = v.Content[idx+1].Value
				break
			}
		}

		if candidateName == name {
			return v
		}
	}
	return nil
}

func findToolVersionNode(toolNode *yaml.Node) *yaml.Node {
	// each element is the k=v pair in a map
	for idx, v := range toolNode.Content {
		if idx%2 == 0 && v.Value == "version" {
			return toolNode.Content[idx+1]
		}
	}
	return nil
}

func findToolVersionWantNode(toolVersionNode *yaml.Node) *yaml.Node {
	// each element is the k=v pair in a map
	for idx, v := range toolVersionNode.Content {
		if idx%2 == 0 && v.Value == "want" {
			return toolVersionNode.Content[idx+1]
		}
	}
	return nil
}

func formatYaml(contents string) (string, error) {
	registry := yamlfmt.NewFormatterRegistry(&basic.BasicFormatterFactory{})

	factory, err := registry.GetDefaultFactory()
	if err != nil {
		return "", fmt.Errorf("unable to get default YAML formatter factory: %w", err)
	}

	formatter, err := factory.NewFormatter(nil)
	if err != nil {
		return "", fmt.Errorf("unable to create YAML formatter: %w", err)
	}

	breakStyle := yamlfmt.LineBreakStyleLF
	if runtime.GOOS == "windows" {
		breakStyle = yamlfmt.LineBreakStyleCRLF
	}

	lineSepChar, err := breakStyle.Separator()
	if err != nil {
		return "", err
	}

	eng := &engine.ConsecutiveEngine{
		LineSepCharacter: lineSepChar,
		Formatter:        formatter,
		Quiet:            true,
		ContinueOnError:  false,
	}

	out, err := eng.FormatContent([]byte(contents))
	if err != nil {
		return "", fmt.Errorf("unable to format YAML: %w", err)
	}

	var node yaml.Node
	if err = yaml.Unmarshal(out, &node); err != nil {
		return "", fmt.Errorf("unable to unmarshal formatted YAML: %w", err)
	}

	var buf bytes.Buffer
	enc := formatted.NewEncoder(&buf)
	enc, err = enc.SetGapExpressions(".tools")
	if err != nil {
		return "", fmt.Errorf("unable to set gap expressions: %w", err)
	}

	err = enc.Encode(&node)
	if err != nil {
		return "", fmt.Errorf("unable to format YAML: %w", err)
	}

	return buf.String(), nil
}
