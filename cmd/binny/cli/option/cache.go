package option

import (
	"path/filepath"

	"github.com/adrg/xdg"

	"github.com/anchore/binny"
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

// Build returns the cache to install through, or nil when the cache is disabled.
func (c Cache) Build() (*binny.Cache, error) {
	if !c.Enabled {
		return nil, nil
	}
	return binny.NewCache(c.Root)
}
