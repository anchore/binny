package option

import (
	"fmt"

	"github.com/anchore/fangs"
)

var _ fangs.FlagAdder = (*Format)(nil)

type Format struct {
	Output           string   `yaml:"output" json:"output" mapstructure:"output"`
	AllowableFormats []string `yaml:"-" json:"-" mapstructure:"-"`
	JQCommand        string   `yaml:"jqCommand" json:"jqCommand" mapstructure:"jqCommand"`
}

// CheckJQ fails when a jq command is given for an output format other than json.
func (o Format) CheckJQ() error {
	if o.JQCommand != "" && o.Output != "json" {
		return fmt.Errorf("--jq can only be used when --output format is 'json'")
	}
	return nil
}

func (o *Format) AddFlags(flags fangs.FlagSet) {
	flags.StringVarP(
		&o.Output,
		"output", "o",
		fmt.Sprintf("output format to report results in (allowable values: %s)", o.AllowableFormats),
	)
	flags.StringVarP(
		&o.JQCommand,
		"jq", "",
		"JQ command to apply to the JSON output",
	)
}
