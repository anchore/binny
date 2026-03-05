package internal

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// TemplateSlice renders templates in each element of a slice with the given version.
func TemplateSlice(in []string, version string) ([]string, error) {
	ret := make([]string, len(in))
	for i, arg := range in {
		rendered, err := TemplateString(arg, version)
		if err != nil {
			return nil, err
		}
		ret[i] = rendered
	}
	return ret, nil
}

// TemplateFlags joins ldflags and renders them as a template with the given version.
func TemplateFlags(ldFlags []string, version string) (string, error) {
	flags := strings.Join(ldFlags, " ")
	return TemplateString(flags, version)
}

// TemplateString renders a single string as a template with the given version.
func TemplateString(in string, version string) (string, error) {
	tmpl, err := template.New("template").Funcs(sprig.FuncMap()).Parse(in)
	if err != nil {
		return "", err
	}

	buf := bytes.Buffer{}
	err = tmpl.Execute(&buf, map[string]string{
		"Version": version,
	})
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
