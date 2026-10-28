package option

import (
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
)

func TestDefaultCache(t *testing.T) {
	c := DefaultCache()
	assert.False(t, c.Enabled)
	assert.Equal(t, filepath.Join(xdg.CacheHome, "binny"), c.Root)
}
