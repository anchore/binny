package binny

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/internal"
)

// addCacheEntryForTest puts a single-binary entry in the cache the way an install does.
func addCacheEntryForTest(t *testing.T, cache *Cache, name, version, fingerprint string) *CacheEntry {
	t.Helper()
	require.NoError(t, os.MkdirAll(cache.StagingRoot(), 0o755))
	staged, err := os.MkdirTemp(cache.StagingRoot(), "staged-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(staged) })

	bin := filepath.Join(staged, "bin")
	require.NoError(t, os.WriteFile(bin, []byte(name+version+fingerprint), 0o644))
	entry, err := cache.Add(name, version, fingerprint, staged, bin, false)
	require.NoError(t, err)
	return entry
}

func TestCache_List(t *testing.T) {
	t.Run("missing root", func(t *testing.T) {
		cache, err := NewCache(filepath.Join(t.TempDir(), "missing"))
		require.NoError(t, err)

		entries, err := cache.List()
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("entries", func(t *testing.T) {
		cache, err := NewCache(t.TempDir())
		require.NoError(t, err)

		addCacheEntryForTest(t, cache, "tsc", "v1", "bbbb")
		addCacheEntryForTest(t, cache, "binny", "v2", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "v1", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "cli/v1", "aaaa")
		// a staging dir left by an install that was killed
		require.NoError(t, os.MkdirAll(filepath.Join(cache.StagingRoot(), "binny-install-tsc-1", "tsc"), 0o755))

		entries, err := cache.List()
		require.NoError(t, err)

		var got []string
		for _, e := range entries {
			got = append(got, e.Name+"@"+e.Version+"/"+e.Fingerprint)
			assert.Equal(t, cache.EntryDir(e.Name, e.Version, e.Fingerprint), e.Dir)
			size, err := e.Size()
			require.NoError(t, err)
			assert.Positive(t, size)
		}
		assert.Equal(t, []string{"binny@cli/v1/aaaa", "binny@v1/aaaa", "binny@v2/aaaa", "tsc@v1/bbbb"}, got)
	})
}

func TestCache_linkProbe(t *testing.T) {
	staged := func(t *testing.T, cache *Cache) []os.DirEntry {
		t.Helper()
		des, err := os.ReadDir(cache.StagingRoot())
		require.NoError(t, err)
		return des
	}

	t.Run("links work", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("requires symlinks")
		}
		cache, err := NewCache(t.TempDir())
		require.NoError(t, err)
		assert.True(t, cache.LinksWork())
		assert.Empty(t, staged(t, cache))
	})

	t.Run("links fail", func(t *testing.T) {
		orig := internal.Symlink
		internal.Symlink = func(string, string) error { return errors.New("symlinks refused") }
		t.Cleanup(func() { internal.Symlink = orig })

		cache, err := NewCache(t.TempDir())
		require.NoError(t, err)
		assert.False(t, cache.LinksWork())
		assert.Empty(t, staged(t, cache))
	})
}
