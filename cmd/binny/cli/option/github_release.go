package option

import "github.com/anchore/clio"

type GithubRelease struct {
	KeepArchiveFiles bool `json:"keep-archive-files" yaml:"keep-archive-files" mapstructure:"keep-archive-files"`
}

func (o *GithubRelease) AddFlags(flags clio.FlagSet) {
	flags.BoolVarP(&o.KeepArchiveFiles, "keep-archive-files", "", "Keep the whole extracted archive and link to the binary inside it (for tools that read files next to their own binary)")
}
