package command

import (
	"bytes"
	"fmt"

	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"

	"github.com/anchore/binny/cmd/binny/cli/internal/yamlpatch"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
)

func toMap(s any) (map[string]any, error) {
	var m map[string]any
	err := mapstructure.Decode(s, &m)
	if err != nil {
		return nil, fmt.Errorf("unable to create map from struct: %w", err)
	}

	for k, v := range m {
		switch vv := v.(type) {
		case string:
			if vv == "" {
				delete(m, k)
			}
		case []string:
			if len(vv) == 0 {
				delete(m, k)
			}
		default:
			if vv == nil {
				delete(m, k)
			}
		}
	}

	return m, nil
}

var _ yamlpatch.Patcher = (*yamlToolAppender)(nil)

type yamlToolAppender struct {
	toolCfg option.Tool
}

func (p yamlToolAppender) PatchYaml(node *yaml.Node) error {
	patchNode, err := yamlpatch.GetYamlNode(p.toolCfg)
	if err != nil {
		return fmt.Errorf("unable to create new tool yaml config: %w", err)
	}

	toolsNode := yamlpatch.FindToolsSequenceNode(node)

	toolsNode.Content = append(toolsNode.Content, patchNode.Content[0])

	return nil
}

func reportNewConfiguration(cfg option.Tool) {
	var buff bytes.Buffer
	enc := yaml.NewEncoder(&buff)
	enc.SetIndent(2)

	if err := enc.Encode(&cfg); err != nil {
		log.WithFields("error", err).Warn("unable to encode new tool configuration")
	} else {
		bus.Report(buff.String())
	}

	bus.Notify(fmt.Sprintf("Added tool configuration for %q", cfg.Name))
}
