package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

// statusTool is a binny.Tool for status tests, which never install anything
type statusTool struct{}

func (statusTool) Name() string { return "tsc" }

func (statusTool) Fingerprint() string { return "aaaa" }

func (statusTool) InstallTo(context.Context, string, string) (string, error) {
	return "", errors.New("not installable")
}

func (statusTool) ResolveVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func (statusTool) UpdateVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func newStatusStore(t *testing.T) *binny.Store {
	t.Helper()
	store, err := binny.NewStore(t.TempDir())
	require.NoError(t, err)
	return store
}

func newStatusCache(t *testing.T) *binny.Cache {
	t.Helper()
	cache, err := binny.NewCache(t.TempDir())
	require.NoError(t, err)
	return cache
}

func storeEntry(t *testing.T, store *binny.Store) binny.StoreEntry {
	t.Helper()
	entries := store.GetByName("tsc")
	require.Len(t, entries, 1)
	return entries[0]
}

func Test_installedStatus_layout(t *testing.T) {
	store := newStatusStore(t)
	bin := filepath.Join(t.TempDir(), "tsc")
	require.NoError(t, os.WriteFile(bin, []byte("v1"), 0o755))
	require.NoError(t, store.AddTool("tsc", "v1", bin))
	entry := storeEntry(t, store)

	version, _, err := installedStatus(store, nil, statusTool{}, entry)
	require.NoError(t, err)
	assert.Equal(t, "v1", version, "a copy is installed while the cache is off")

	version, _, err = installedStatus(store, newStatusCache(t), statusTool{}, entry)
	require.NoError(t, err)
	assert.Empty(t, version, "a copy is not installed while the cache is on, since check fails it and install replaces it")
}

func Test_installedStatus_danglingLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	store := newStatusStore(t)
	cache := newStatusCache(t)

	staged, cleanup, err := cache.Stage("tsc")
	defer cleanup()
	require.NoError(t, err)
	bin := filepath.Join(staged, "tsc")
	require.NoError(t, os.WriteFile(bin, []byte("v1"), 0o755))
	cached, err := cache.Add("tsc", "v1", statusTool{}.Fingerprint(), staged, bin, false)
	require.NoError(t, err)
	require.NoError(t, store.LinkCacheEntry(*cached))

	version, _, err := installedStatus(store, cache, statusTool{}, storeEntry(t, store))
	require.NoError(t, err)
	assert.Equal(t, "v1", version)

	// what `binny cache delete` leaves behind is not installed, rather than an error row
	require.NoError(t, cache.Delete(binny.CacheRef{Name: "tsc"}))
	version, _, err = installedStatus(store, cache, statusTool{}, storeEntry(t, store))
	require.NoError(t, err)
	assert.Empty(t, version)
}
