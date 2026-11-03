package option

import (
	"errors"
	"path/filepath"

	"github.com/adrg/xdg"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal/log"
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

// Build returns the cache to install through, or nil when the cache is disabled. A root that is not safe to use is
// warned about and treated as disabled, so tools install into .tool and links into that root are replaced with copies.
func (c Cache) Build() (*binny.Cache, error) {
	if !c.Enabled {
		return nil, nil
	}
	cache, err := binny.NewCache(c.Root)
	if errors.Is(err, binny.ErrUnsafeCacheRoot) {
		log.WithFields("error", err).Warn("not using the shared cache, installing tools into the store instead")
		return nil, nil
	}
	return cache, err
}
