package binny

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
