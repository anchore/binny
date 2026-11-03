package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/tool"
)

// statusTool is a binny.Tool for status tests, installing a binary that holds its version
type statusTool struct{}

func (statusTool) Name() string { return "tsc" }

func (statusTool) Fingerprint() string { return "aaaa" }

func (statusTool) InstallTo(_ context.Context, version, destDir string) (string, error) {
	bin := filepath.Join(destDir, "tsc")
	return bin, os.WriteFile(bin, []byte(version), 0o755)
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
	require.NoError(t, os.WriteFile(bin, []byte("v1.0.0"), 0o755))
	require.NoError(t, store.AddTool("tsc", "v1.0.0", bin))
	entry := storeEntry(t, store)

	version, _, err := installedStatus(store, nil, statusTool{}, entry)
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", version, "a copy is installed while the cache is off")

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
	require.NoError(t, tool.Install(context.Background(), statusTool{}, binny.VersionIntent{Want: "v1.0.0"}, store, cache, tool.VerifyConfig{}))

	version, _, err := installedStatus(store, cache, statusTool{}, storeEntry(t, store))
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", version)

	// what `binny cache delete` leaves behind is not installed, rather than an error row
	require.NoError(t, cache.Delete(binny.CacheRef{Name: "tsc"}))
	version, _, err = installedStatus(store, cache, statusTool{}, storeEntry(t, store))
	require.NoError(t, err)
	assert.Empty(t, version)
}

func Test_getAllStatuses_unconfiguredMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	store := newStatusStore(t)
	cache := newStatusCache(t)

	// a copy deleted by hand
	bin := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.WriteFile(bin, []byte("v1.0.0"), 0o755))
	require.NoError(t, store.AddTool("gone", "v1.0.0", bin))
	require.NoError(t, os.Remove(filepath.Join(store.Root(), "gone")))

	// a link left dangling by `binny cache delete`
	staged, cleanup, err := cache.Stage("tsc")
	defer cleanup()
	require.NoError(t, err)
	bin = filepath.Join(staged, "tsc")
	require.NoError(t, os.WriteFile(bin, []byte("v1.0.0"), 0o755))
	cached, err := cache.Add("tsc", "v1.0.0", statusTool{}.Fingerprint(), staged, bin, false)
	require.NoError(t, err)
	require.NoError(t, store.LinkCacheEntry(*cached))
	require.NoError(t, cache.Delete(binny.CacheRef{Name: "tsc"}))

	statuses := getAllStatuses(context.Background(), ListConfig{}, store, cache, option.ToolOptions{})
	require.Len(t, statuses, 1, "only the missing copy is reported")
	assert.Equal(t, "gone", statuses[0].Name)
	assert.Error(t, statuses[0].Error)
}
