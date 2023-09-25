package command

import (
	"bytes"
	"fmt"
	"os"

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

	if toolsNode == nil {
		return fmt.Errorf("unable to find tools sequence node")
	}

	toolsNode.Content = append(toolsNode.Content, patchNode.Content[0])

	return nil
}

func updateConfiguration(path string, cfg option.Tool) error {
	if path == "" {
		path = ".binny.yaml"
	}

	// if does not exist, create a new file
	if info, err := os.Stat(path); os.IsNotExist(err) || info != nil && info.Size() == 0 {
		newCfg := struct {
			Tools []option.Tool `yaml:"tools"`
		}{
			Tools: []option.Tool{cfg},
		}
		by, err := yaml.Marshal(&newCfg)
		if err != nil {
			return fmt.Errorf("unable to encode new tool configuration: %w", err)
		}

		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("unable to create config file: %w", err)
		}

		if _, err := fh.Write(by); err != nil {
			return fmt.Errorf("unable to write config: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("unable to stat config file: %w", err)
	} else {
		// otherwise append to the existing file
		if err := yamlpatch.Write(path, yamlToolAppender{toolCfg: cfg}); err != nil {
			return fmt.Errorf("unable to write config: %w", err)
		}
	}

	var buff bytes.Buffer
	enc := yaml.NewEncoder(&buff)
	enc.SetIndent(2)

	if err := enc.Encode(&cfg); err != nil {
		log.WithFields("error", err).Warn("unable to encode new tool configuration")
	} else {
		bus.Report(buff.String())
	}

	bus.Notify(fmt.Sprintf("Added tool configuration for %q", cfg.Name))

	return nil
}
