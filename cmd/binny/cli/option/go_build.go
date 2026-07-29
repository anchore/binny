package option

import "github.com/anchore/clio"

type GoBuild struct {
	Module     string   `json:"module" yaml:"module" mapstructure:"module"`
	Entrypoint string   `json:"entrypoint" yaml:"entrypoint" mapstructure:"entrypoint"`
	LDFlags    string   `json:"ld-flags" yaml:"ld-flags" mapstructure:"ld-flags"`
	Args       []string `json:"args" yaml:"args" mapstructure:"args"`
	Env        []string `json:"env" yaml:"env" mapstructure:"env"`
	Source     string   `json:"source" yaml:"source" mapstructure:"source"`
	RepoURL    string   `json:"repo-url" yaml:"repo-url" mapstructure:"repo-url"`
}

func (o *GoBuild) AddFlags(flags clio.FlagSet) {
	flags.StringVarP(&o.Module, "module", "m", "Go module path (e.g. github.com/anchore/syft)")
	flags.StringVarP(&o.Entrypoint, "entrypoint", "e", "Entrypoint within the go module (e.g. cmd/syft)")
	flags.StringVarP(&o.LDFlags, "ld-flags", "l", "LD flags to pass to the go build command (e.g. -ldflags \"-X main.version=1.0.0\")")
	flags.StringArrayVarP(&o.Args, "args", "a", "Additional arguments to pass to the go build command")
	flags.StringArrayVarP(&o.Env, "env", "", "Environment variables to pass to the go build command")
	flags.StringVarP(&o.Source, "source", "s", "Source mode: 'git' (default) clones the repository, 'goproxy' downloads via go proxy")
	flags.StringVarP(&o.RepoURL, "repo-url", "r", "Explicit git repository URL (auto-derived for github.com modules)")
}
