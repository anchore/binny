package option

import (
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCache(t *testing.T) {
	c := DefaultCache()
	assert.False(t, c.Enabled)
	assert.Equal(t, filepath.Join(xdg.CacheHome, "binny"), c.Root)
}

func TestCache_Build(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")

	disabled, err := Cache{Enabled: false, Root: root}.Build()
	require.NoError(t, err)
	assert.Nil(t, disabled, "a disabled cache is nil, so install and check never touch the root")

	enabled, err := Cache{Enabled: true, Root: root}.Build()
	require.NoError(t, err)
	require.NotNil(t, enabled)
	// the root is resolved, and a temp dir can be behind a link (/var on macOS)
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(parent, "cache"), enabled.Root())
}
