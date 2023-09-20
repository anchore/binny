package option

import "github.com/anchore/clio"

type GoInstall struct {
	Module     string `json:"module" yaml:"module" mapstructure:"module"`
	Entrypoint string `json:"entrypoint" yaml:"entrypoint" mapstructure:"entrypoint"`
	LDFlags    string `json:"ld-flags" yaml:"ld-flags" mapstructure:"ld-flags"`
}

func (o *GoInstall) AddFlags(flags clio.FlagSet) {
	flags.StringVarP(&o.Module, "module", "m", "Go module (e.g. github.com/anchore/syft)")
	flags.StringVarP(&o.Entrypoint, "entrypoint", "e", "Entrypoint within the go module (e.g. cmd/syft)")
	flags.StringVarP(&o.LDFlags, "ld-flags", "l", "LD flags to pass to the go install command (e.g. -ldflags \"-X main.version=1.0.0\")")
}
