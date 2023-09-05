package option

import (
	"github.com/anchore/clio"
)

type Check struct {
	VerifyDigest bool `json:"verify-digest" yaml:"verify-digest" mapstructure:"verify-digest"`
}

func (o *Check) AddFlags(flags clio.FlagSet) {
	flags.BoolVarP(&o.VerifyDigest, "verify-digest", "d", "Verifying the digest of already installed tools")
}
