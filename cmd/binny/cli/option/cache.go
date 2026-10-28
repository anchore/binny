package option

import (
	"path/filepath"

	"github.com/adrg/xdg"

	"github.com/anchore/binny"
	"github.com/anchore/clio"
)

// Cache configures the per-user cache that stores link their tools to, so each tool is installed once per machine.
type Cache struct {
	Enabled bool   `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Root    string `json:"root" yaml:"root" mapstructure:"root"`
}

func DefaultCache() Cache {
	return Cache{
		Enabled: false,
		Root:    filepath.Join(xdg.CacheHome, "binny"),
	}
}

// Get returns the cache to install through, or nil when the cache is disabled.
func (c Cache) Get() (*binny.Cache, error) {
	if !c.Enabled {
		return nil, nil
	}
	return binny.NewCache(c.Root)
}

type CacheDelete struct {
	All bool `json:"all" yaml:"all" mapstructure:"all"`
}

func (o *CacheDelete) AddFlags(flags clio.FlagSet) {
	flags.BoolVarP(&o.All, "all", "", "Delete every tool in the cache, and any staging dirs left by installs that did not finish")
}
