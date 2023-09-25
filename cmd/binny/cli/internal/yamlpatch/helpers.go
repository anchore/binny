package yamlpatch

import (
	"gopkg.in/yaml.v3"
)

func FindToolVersionWantNode(toolsNode *yaml.Node, toolName string) *yaml.Node {
	toolNode := FindToolNode(toolsNode, toolName)
	toolVersionNode := findToolVersionNode(toolNode)
	toolVersionWantNode := findToolVersionWantNode(toolVersionNode)
	return toolVersionWantNode
}

func FindToolsSequenceNode(node *yaml.Node) *yaml.Node {
	for idx, v := range node.Content {
		var next *yaml.Node
		if idx+1 < len(node.Content) {
			next = node.Content[idx+1]
		} else {
			break
		}
		if v.Value == "tools" {
			if next.Tag == "!!seq" {
				return next
			}
			if next == nil {
				return node.Content[idx]
			}
		}
	}
	return nil
}

func FindToolNode(toolSequenceNode *yaml.Node, name string) *yaml.Node {
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

func GetYamlNode(s any) (*yaml.Node, error) {
	var n yaml.Node

	by, err := yaml.Marshal(s)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(by, &n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}
