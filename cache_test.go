package binny

import (
	"encoding/json"
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
	staged, err := os.MkdirTemp(cache.StagingRoot(), "binny-install-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(staged) })

	bin := filepath.Join(staged, "bin")
	require.NoError(t, os.WriteFile(bin, []byte(name+version+fingerprint), 0o644))
	entry, err := cache.Add(name, version, fingerprint, staged, bin, false)
	require.NoError(t, err)
	return entry
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	cache, err := NewCache(t.TempDir())
	require.NoError(t, err)
	return cache
}

// writeEntryFile replaces an entry's metadata with the given value.
func writeEntryFile(t *testing.T, entry *CacheEntry, metadata any) {
	t.Helper()
	contents, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(entry.Dir, cacheEntryFile), contents, 0o644))
}

// addForeignContentForTest puts things under the cache root that binny did not create, as a misconfigured root has.
func addForeignContentForTest(t *testing.T, cache *Cache) []string {
	t.Helper()
	paths := []string{
		filepath.Join(cache.Root(), "notes.txt"),
		filepath.Join(cache.Root(), "Documents", "report.txt"),
		// the depth of an entry, but without an entry file
		filepath.Join(cache.Root(), "binny", "v1", "src", "main.go"),
		// a version dir name that does not round trip through path escaping
		filepath.Join(cache.Root(), "binny", "v1%zz", "aaaa", cacheEntryFile),
		filepath.Join(cache.StagingRoot(), "someone-elses-file"),
	}
	for _, p := range paths {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("keep me"), 0o644))
	}
	return paths
}

func listed(t *testing.T, cache *Cache) []string {
	t.Helper()
	entries, err := cache.List()
	require.NoError(t, err)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name+"@"+e.Version+"/"+e.Fingerprint)
	}
	return got
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
		cache := newTestCache(t)

		addCacheEntryForTest(t, cache, "tsc", "v1", "bbbb")
		addCacheEntryForTest(t, cache, "binny", "v2", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "v1", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "v10", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "cli/v1", "aaaa")
		addForeignContentForTest(t, cache)
		// a staging dir left by an install that was killed
		require.NoError(t, os.MkdirAll(filepath.Join(cache.StagingRoot(), "binny-install-tsc-1", "tsc"), 0o755))

		entries, err := cache.List()
		require.NoError(t, err)

		var got []string
		for _, e := range entries {
			got = append(got, e.Name+"@"+e.Version+"/"+e.Fingerprint)
			assert.Equal(t, cache.EntryDir(e.Name, e.Version, e.Fingerprint), e.Dir)
			// listed entries carry their metadata, so the binary path is the binary
			assert.FileExists(t, e.BinPath())
			assert.NotEmpty(t, e.Digests)
			size, err := e.Size()
			require.NoError(t, err)
			assert.Positive(t, size)
		}
		// semver versions by precedence, then anything else as a string
		assert.Equal(t, []string{"binny@v1/aaaa", "binny@v2/aaaa", "binny@v10/aaaa", "binny@cli/v1/aaaa", "tsc@v1/bbbb"}, got)
	})

	t.Run("skips an unreadable dir binny did not create", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix permissions")
		}
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		cache := newTestCache(t)
		addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
		unreadable := filepath.Join(cache.Root(), "Private")
		require.NoError(t, os.Mkdir(unreadable, 0o000))
		t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

		assert.Equal(t, []string{"tsc@v1/aaaa"}, listed(t, cache))
		require.NoError(t, cache.DeleteAll())
		assert.DirExists(t, unreadable)
	})

	t.Run("skips a corrupt entry", func(t *testing.T) {
		cache := newTestCache(t)
		addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
		corrupt := addCacheEntryForTest(t, cache, "tsc", "v2", "aaaa")
		require.NoError(t, os.WriteFile(filepath.Join(corrupt.Dir, cacheEntryFile), []byte("{"), 0o644))

		assert.Equal(t, []string{"tsc@v1/aaaa"}, listed(t, cache))
	})
}

func TestCache_Get(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		got, err := newTestCache(t).Get("tsc", "v1", "aaaa")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("matches what add returned", func(t *testing.T) {
		cache := newTestCache(t)
		added := addCacheEntryForTest(t, cache, "tsc", "cli/v1", "aaaa")

		got, err := cache.Get("tsc", "cli/v1", "aaaa")
		require.NoError(t, err)
		assert.Equal(t, added, got)
	})

	t.Run("malformed entry file", func(t *testing.T) {
		cache := newTestCache(t)
		entry := addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
		require.NoError(t, os.WriteFile(filepath.Join(entry.Dir, cacheEntryFile), []byte("{"), 0o644))

		_, err := cache.Get("tsc", "v1", "aaaa")
		require.ErrorContains(t, err, "corrupt")
		require.ErrorContains(t, err, "binny cache delete tsc@v1")
	})

	t.Run("binary path escapes the entry", func(t *testing.T) {
		cache := newTestCache(t)
		entry := addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
		for _, escape := range []string{"../../../../../../usr/bin/env", "/usr/bin/env", "", ".", "bin/.."} {
			writeEntryFile(t, entry, cacheEntryMetadata{PathInDir: escape, Digests: entry.Digests})

			_, err := cache.Get("tsc", "v1", "aaaa")
			require.ErrorContains(t, err, "not within the entry", escape)
		}
	})

	t.Run("no sha256 digest", func(t *testing.T) {
		cache := newTestCache(t)
		entry := addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
		writeEntryFile(t, entry, cacheEntryMetadata{PathInDir: entry.PathInDir, Digests: map[string]string{internal.XXH64Algorithm: entry.Digests[internal.XXH64Algorithm]}})

		_, err := cache.Get("tsc", "v1", "aaaa")
		require.ErrorContains(t, err, "corrupt")
		require.ErrorContains(t, err, "sha256")
	})

	t.Run("dir without an entry file", func(t *testing.T) {
		cache := newTestCache(t)
		require.NoError(t, os.MkdirAll(cache.EntryDir("tsc", "v1", "aaaa"), 0o755))

		_, err := cache.Get("tsc", "v1", "aaaa")
		require.ErrorContains(t, err, "binny did not create it")
	})

	t.Run("root writable by other users", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix permissions")
		}
		for _, mode := range []os.FileMode{0o777, 0o775} {
			cache := newTestCache(t)
			addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
			require.NoError(t, os.Chmod(cache.Root(), mode))

			_, err := cache.Get("tsc", "v1", "aaaa")
			require.ErrorContains(t, err, "writable by other users", mode)

			// and a later binny run refuses it up front, before even a link into it is checked
			_, err = NewCache(cache.Root())
			require.ErrorContains(t, err, "writable by other users", mode)
		}
	})
}

func TestNewCache(t *testing.T) {
	t.Run("empty root", func(t *testing.T) {
		_, err := NewCache("")
		require.ErrorContains(t, err, "cache.root")
	})

	t.Run("symlinked root is resolved", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("requires symlinks")
		}
		target := newTestCache(t).Root()
		link := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink(target, link))

		cache, err := NewCache(link)
		require.NoError(t, err)
		// so the links binny makes do not pass through a link someone could point elsewhere later
		assert.Equal(t, target, cache.Root())
	})

	t.Run("missing root that becomes a link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("requires symlinks")
		}
		root := filepath.Join(t.TempDir(), "missing")
		cache, err := NewCache(root)
		require.NoError(t, err)

		elsewhere := t.TempDir()
		require.NoError(t, os.Symlink(elsewhere, root))

		_, cleanup, err := cache.Stage("tsc")
		defer cleanup()
		require.ErrorContains(t, err, "now resolves")
		assert.NoDirExists(t, filepath.Join(elsewhere, cacheStagingDirName))
	})
}

func TestCache_rejectsEscapingKeys(t *testing.T) {
	cache := newTestCache(t)
	for _, key := range [][3]string{
		{"", "v1", "aaaa"},
		{"..", "v1", "aaaa"},
		{".tmp", "v1", "aaaa"},
		{"a/b", "v1", "aaaa"},
		{`a\b`, "v1", "aaaa"},
		{"tsc", "..", "aaaa"},
		{"tsc", ".", "aaaa"},
		{"tsc", "", "aaaa"},
		{"tsc", "v1", ".."},
		{"tsc", "v1", "a/b"},
	} {
		_, err := cache.Get(key[0], key[1], key[2])
		require.ErrorContains(t, err, "invalid cache entry key", key)

		_, err = cache.Add(key[0], key[1], key[2], filepath.Join(cache.StagingRoot(), "binny-x"), filepath.Join(cache.StagingRoot(), "binny-x", "bin"), false)
		require.ErrorContains(t, err, "invalid cache entry key", key)
	}
}

func TestCache_Add_rejectsStagingOutsideStagingRoot(t *testing.T) {
	cache := newTestCache(t)
	for _, staged := range []string{t.TempDir(), cache.StagingRoot(), cache.Root()} {
		_, err := cache.Add("tsc", "v1", "aaaa", staged, filepath.Join(staged, "bin"), false)
		require.ErrorContains(t, err, "not within the cache staging root", staged)
	}
}

func TestCacheEntry_Verify(t *testing.T) {
	t.Run("intact", func(t *testing.T) {
		entry := addCacheEntryForTest(t, newTestCache(t), "tsc", "v1", "aaaa")
		require.NoError(t, entry.Verify())

		// read-only, so writing through a store's link cannot change what every store shares
		fi, err := os.Stat(entry.BinPath())
		require.NoError(t, err)
		assert.Zero(t, fi.Mode().Perm()&0o222)
	})

	t.Run("tampered binary", func(t *testing.T) {
		entry := addCacheEntryForTest(t, newTestCache(t), "tsc", "v1", "aaaa")
		require.NoError(t, os.Chmod(entry.BinPath(), 0o755))
		require.NoError(t, os.WriteFile(entry.BinPath(), []byte("tampered"), 0o755))
		require.ErrorContains(t, entry.Verify(), "binny cache delete tsc@v1")
	})

	t.Run("missing binary", func(t *testing.T) {
		entry := addCacheEntryForTest(t, newTestCache(t), "tsc", "v1", "aaaa")
		require.NoError(t, os.Remove(entry.BinPath()))
		require.ErrorContains(t, entry.Verify(), "corrupt")
	})

	t.Run("algorithm this binny does not know", func(t *testing.T) {
		entry := addCacheEntryForTest(t, newTestCache(t), "tsc", "v1", "aaaa")
		entry.Digests["blake3"] = "made-by-a-later-binny"
		require.NoError(t, entry.Verify())
	})

	t.Run("algorithm this binny computes but the entry lacks", func(t *testing.T) {
		entry := addCacheEntryForTest(t, newTestCache(t), "tsc", "v1", "aaaa")
		delete(entry.Digests, internal.XXH64Algorithm)
		require.NoError(t, entry.Verify())
	})

	t.Run("no algorithm in common", func(t *testing.T) {
		entry := addCacheEntryForTest(t, newTestCache(t), "tsc", "v1", "aaaa")
		entry.Digests = map[string]string{"blake3": "made-by-a-later-binny"}
		require.ErrorContains(t, entry.Verify(), "can be checked")
	})
}

func TestCache_Delete(t *testing.T) {
	setup := func(t *testing.T) *Cache {
		t.Helper()
		cache := newTestCache(t)
		addCacheEntryForTest(t, cache, "binny", "v1", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "v1", "bbbb")
		addCacheEntryForTest(t, cache, "binny", "v2", "aaaa")
		addCacheEntryForTest(t, cache, "binny", "cli/v3", "aaaa")
		addCacheEntryForTest(t, cache, "tsc", "v1", "aaaa")
		return cache
	}

	t.Run("by name", func(t *testing.T) {
		cache := setup(t)
		require.NoError(t, cache.Delete(CacheRef{Name: "tsc"}))
		assert.Equal(t, []string{"binny@v1/aaaa", "binny@v1/bbbb", "binny@v2/aaaa", "binny@cli/v3/aaaa"}, listed(t, cache))
		assert.NoDirExists(t, filepath.Join(cache.Root(), "tsc"))

		// the entry was moved into a staging dir to be removed, and that dir is gone too
		leftover, err := filepath.Glob(filepath.Join(cache.StagingRoot(), cacheStagingPrefix+"delete-*"))
		require.NoError(t, err)
		assert.Empty(t, leftover)
	})

	t.Run("by name and version", func(t *testing.T) {
		cache := setup(t)
		require.NoError(t, cache.Delete(CacheRef{Name: "binny", Version: "v1"}, CacheRef{Name: "binny", Version: "cli/v3"}))
		assert.Equal(t, []string{"binny@v2/aaaa", "tsc@v1/aaaa"}, listed(t, cache))

		// removing the last version of a name removes the name too
		require.NoError(t, cache.Delete(CacheRef{Name: "binny", Version: "v2"}))
		assert.NoDirExists(t, filepath.Join(cache.Root(), "binny"))
	})

	t.Run("corrupt entry", func(t *testing.T) {
		cache := setup(t)
		corrupt := filepath.Join(cache.EntryDir("tsc", "v1", "aaaa"), cacheEntryFile)
		require.NoError(t, os.WriteFile(corrupt, []byte("{"), 0o644))

		require.NoError(t, cache.Delete(CacheRef{Name: "tsc", Version: "v1"}))
		assert.NoDirExists(t, filepath.Join(cache.Root(), "tsc"))
	})

	t.Run("no match", func(t *testing.T) {
		cache := setup(t)
		for _, missing := range []CacheRef{{Name: "grype"}, {Name: "binny", Version: "v3"}, {Name: ".."}} {
			err := cache.Delete(CacheRef{Name: "tsc"}, missing)
			require.ErrorContains(t, err, missing.String())
		}
		assert.Len(t, listed(t, cache), 5, "nothing is removed when any ref matches nothing")
	})

	t.Run("leaves what binny did not create", func(t *testing.T) {
		cache := setup(t)
		foreign := addForeignContentForTest(t, cache)

		err := cache.Delete(CacheRef{Name: "Documents"})
		require.ErrorContains(t, err, "Documents")

		require.NoError(t, cache.Delete(CacheRef{Name: "binny"}))
		for _, p := range foreign {
			assert.FileExists(t, p)
		}
	})

	t.Run("all", func(t *testing.T) {
		cache := setup(t)
		require.NoError(t, os.MkdirAll(filepath.Join(cache.StagingRoot(), "binny-install-tsc-1"), 0o755))
		require.NoError(t, cache.DeleteAll())
		des, err := os.ReadDir(cache.Root())
		require.NoError(t, err)
		assert.Empty(t, des)
	})

	t.Run("all leaves what binny did not create", func(t *testing.T) {
		cache := setup(t)
		foreign := addForeignContentForTest(t, cache)
		require.NoError(t, os.MkdirAll(filepath.Join(cache.StagingRoot(), "binny-install-tsc-1"), 0o755))

		require.NoError(t, cache.DeleteAll())
		assert.Empty(t, listed(t, cache))
		assert.NoDirExists(t, filepath.Join(cache.StagingRoot(), "binny-install-tsc-1"))
		for _, p := range foreign {
			assert.FileExists(t, p)
		}
	})

	t.Run("all leaves a symlinked staging root alone", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("requires symlinks")
		}
		cache := newTestCache(t)
		elsewhere := t.TempDir()
		victim := filepath.Join(elsewhere, "binny-worktree")
		require.NoError(t, os.Mkdir(victim, 0o755))
		require.NoError(t, os.Symlink(elsewhere, cache.StagingRoot()))

		require.ErrorContains(t, cache.DeleteAll(), "not a dir")
		assert.DirExists(t, victim)
	})

	t.Run("all on a missing root", func(t *testing.T) {
		cache, err := NewCache(filepath.Join(t.TempDir(), "missing"))
		require.NoError(t, err)
		require.NoError(t, cache.DeleteAll())
	})
}
