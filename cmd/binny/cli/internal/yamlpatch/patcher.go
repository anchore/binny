package yamlpatch

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
	"gopkg.in/yaml.v3"
)

type Patcher interface {
	PatchYaml(node *yaml.Node) error
}

func Write(path string, patcher Patcher) error {
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

	if err := patcher.PatchYaml(doc); err != nil {
		return fmt.Errorf("unabl to patch yaml: %w", err)
	}

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
