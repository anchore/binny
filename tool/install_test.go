package tool

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
)

// fakeTool installs an archive-like tree with the binary next to a file it reads relative to itself
type fakeTool struct {
	name string
	keep bool
	// param stands in for the install parameters that make up a real tool's fingerprint
	param string
	// installs counts InstallTo calls when set
	installs *atomic.Int32
	// local acts like a tool built from a local module
	local bool
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) KeepArchiveFiles() bool { return f.keep }

func (f fakeTool) BypassesCache() bool { return f.local }

func (f fakeTool) Fingerprint() string { return fmt.Sprintf("keep-%t-%s", f.keep, f.param) }

func (f fakeTool) InstallTo(_ context.Context, version, destDir string) (string, error) {
	if f.installs != nil {
		f.installs.Add(1)
	}
	lib := filepath.Join(destDir, "package", "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(lib, "lib.d.ts"), []byte("decl"), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(lib, f.name)
	return bin, os.WriteFile(bin, []byte(version), 0o755)
}

func (f fakeTool) ResolveVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func (f fakeTool) UpdateVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func TestInstall_keepArchiveFilesToggle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	store, err := binny.NewStore(t.TempDir())
	require.NoError(t, err)

	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}
	keptDir := filepath.Join(store.Root(), ".binny-dirs", "tsc")

	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc"}, intent, store, nil, cfg))
	assert.NoDirExists(t, keptDir)

	// same version, but now the archive files should be kept, which needs a reinstall
	keeper := fakeTool{name: "tsc", keep: true}
	require.Error(t, Check(store, nil, keeper, "v1", cfg))
	require.NoError(t, Install(context.Background(), keeper, intent, store, nil, cfg))
	assert.FileExists(t, filepath.Join(keptDir, "package", "lib", "lib.d.ts"))
	require.NoError(t, Check(store, nil, keeper, "v1", cfg))
	require.ErrorIs(t, Install(context.Background(), keeper, intent, store, nil, cfg), ErrAlreadyInstalled)

	// and turning it back off reinstalls the plain binary
	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc"}, intent, store, nil, cfg))
	assert.NoDirExists(t, keptDir)
	require.NoError(t, Check(store, nil, fakeTool{name: "tsc"}, "v1", cfg))
}

func TestInstall_keptInstallSurvivesReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	root := t.TempDir()
	store, err := binny.NewStore(root)
	require.NoError(t, err)

	keeper := fakeTool{name: "tsc", keep: true}
	cfg := VerifyConfig{VerifySHA256Digest: true}
	require.NoError(t, Install(context.Background(), keeper, binny.VersionIntent{Want: "v1"}, store, nil, cfg))

	// a later binny run reads the layout back from the state file, so it must not reinstall every time
	reopened, err := binny.NewStore(root)
	require.NoError(t, err)
	require.NoError(t, Check(reopened, nil, keeper, "v1", cfg))
}

func newCacheForTest(t *testing.T) *binny.Cache {
	t.Helper()
	cache, err := binny.NewCache(t.TempDir())
	require.NoError(t, err)
	return cache
}

func newStoreForTest(t *testing.T) *binny.Store {
	t.Helper()
	store, err := binny.NewStore(t.TempDir())
	require.NoError(t, err)
	return store
}

// requireLinkedIntoCache asserts that root/<name> is a symlink to binInEntry inside the tool's cache entry, and that
// it resolves to an executable file. It returns the resolved path.
func requireLinkedIntoCache(t *testing.T, store *binny.Store, cache *binny.Cache, tl fakeTool, version, binInEntry string) string {
	t.Helper()
	path := filepath.Join(store.Root(), tl.name)

	fi, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&os.ModeSymlink, "%q should be a symlink", path)

	target, err := os.Readlink(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cache.EntryDir(tl.name, version, tl.Fingerprint()), binInEntry), target)

	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	info, err := os.Stat(resolved)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	require.NotZero(t, info.Mode()&0o111, "the linked binary should be executable")
	return resolved
}

func TestInstall_cacheReuse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	stores := []*binny.Store{newStoreForTest(t), newStoreForTest(t)}
	for _, store := range stores {
		require.NoError(t, Install(context.Background(), tl, binny.VersionIntent{Want: "v1"}, store, cache, cfg))
	}

	require.EqualValues(t, 1, tl.installs.Load(), "the second store should link to the existing cache entry")
	for _, store := range stores {
		requireLinkedIntoCache(t, store, cache, tl, "v1", "tsc")
	}
}

func TestInstall_cacheFingerprintSeparatesConfigs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	cfg := VerifyConfig{VerifySHA256Digest: true}
	a := fakeTool{name: "tsc", param: "a", installs: &atomic.Int32{}}
	b := fakeTool{name: "tsc", param: "b", installs: a.installs}
	require.NotEqual(t, a.Fingerprint(), b.Fingerprint())

	for _, tl := range []fakeTool{a, b} {
		store := newStoreForTest(t)
		require.NoError(t, Install(context.Background(), tl, binny.VersionIntent{Want: "v1"}, store, cache, cfg))
		requireLinkedIntoCache(t, store, cache, tl, "v1", "tsc")
	}

	assert.EqualValues(t, 2, a.installs.Load())
	entries, err := os.ReadDir(filepath.Join(cache.Root(), "tsc", "v1"))
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestInstall_cacheConcurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	stores := make([]*binny.Store, 8)
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i := range stores {
		stores[i] = newStoreForTest(t)
		// each checkout runs its own binny process, with its own Cache value over the shared root
		own, err := binny.NewCache(cache.Root())
		require.NoError(t, err)
		wg.Go(func() {
			errs[i] = Install(context.Background(), tl, binny.VersionIntent{Want: "v1"}, stores[i], own, cfg)
		})
	}
	wg.Wait()

	for i, store := range stores {
		require.NoError(t, errs[i])
		requireLinkedIntoCache(t, store, cache, tl, "v1", "tsc")
	}

	entries, err := os.ReadDir(filepath.Join(cache.Root(), "tsc", "v1"))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "racing installs should leave a single entry")

	staged, err := os.ReadDir(filepath.Join(cache.Root(), ".tmp"))
	require.NoError(t, err)
	assert.Empty(t, staged, "staging dirs should be cleaned up")
}

func TestInstall_cacheKeepArchiveFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	keeper := fakeTool{name: "tsc", keep: true, installs: &atomic.Int32{}}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), keeper, intent, store, cache, cfg))

	resolved := requireLinkedIntoCache(t, store, cache, keeper, "v1", filepath.Join("package", "lib", "tsc"))
	got, err := os.ReadFile(filepath.Join(filepath.Dir(resolved), "lib.d.ts"))
	require.NoError(t, err)
	assert.Equal(t, "decl", string(got))

	require.ErrorIs(t, Install(context.Background(), keeper, intent, store, cache, cfg), ErrAlreadyInstalled)
	assert.EqualValues(t, 1, keeper.installs.Load())
}

func TestInstall_cacheKeepArchiveFilesToggle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	installs := &atomic.Int32{}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc", installs: installs}, intent, store, cache, cfg))

	// same version, but keeping the archive files changes the fingerprint, so it is another entry
	keeper := fakeTool{name: "tsc", keep: true, installs: installs}
	require.NoError(t, Install(context.Background(), keeper, intent, store, cache, cfg))
	assert.EqualValues(t, 2, installs.Load())

	resolved := requireLinkedIntoCache(t, store, cache, keeper, "v1", filepath.Join("package", "lib", "tsc"))
	assert.FileExists(t, filepath.Join(filepath.Dir(resolved), "lib.d.ts"))
}

func TestInstall_cacheCorruptEntryFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	tl := fakeTool{name: "tsc"}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, newStoreForTest(t), cache, cfg))

	entryDir := filepath.Join(cache.Root(), "tsc", "v1", tl.Fingerprint())
	bin := filepath.Join(entryDir, "tsc")
	require.NoError(t, os.Chmod(bin, 0o755))
	require.NoError(t, os.WriteFile(bin, []byte("tampered"), 0o755))

	err := Install(context.Background(), tl, intent, newStoreForTest(t), cache, cfg)
	require.ErrorContains(t, err, entryDir)
	require.ErrorContains(t, err, "binny cache delete tsc@v1")

	got, err := os.ReadFile(bin)
	require.NoError(t, err)
	assert.Equal(t, "tampered", string(got), "a corrupt entry is left in place")
}

func TestInstall_cacheDisabled(t *testing.T) {
	store := newStoreForTest(t)

	// a disabled cache is nil (option.Cache.Build), so nothing here can touch a cache root
	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc"}, binny.VersionIntent{Want: "v1"}, store, nil, VerifyConfig{}))

	fi, err := os.Lstat(filepath.Join(store.Root(), "tsc"))
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular())
}

func TestInstall_cacheRootNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on unix directory permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o555))
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	cache, err := binny.NewCache(root)
	require.NoError(t, err)

	err = Install(context.Background(), fakeTool{name: "tsc"}, binny.VersionIntent{Want: "v1"}, newStoreForTest(t), cache, VerifyConfig{})
	require.ErrorContains(t, err, cache.Root())
}

func TestInstall_cacheConvertsCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc"}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, store, nil, cfg))

	// enabling the cache turns the copy into a link on the next install
	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))
	requireLinkedIntoCache(t, store, cache, tl, "v1", "tsc")
}

func TestInstall_cacheConvertsKeptDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	keeper := fakeTool{name: "tsc", keep: true}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}
	keptDir := filepath.Join(store.Root(), ".binny-dirs", "tsc")

	require.NoError(t, Install(context.Background(), keeper, intent, store, nil, cfg))
	require.DirExists(t, keptDir)

	// enabling the cache moves the tool into the cache and removes the store's kept dir
	require.NoError(t, Install(context.Background(), keeper, intent, store, cache, cfg))
	resolved := requireLinkedIntoCache(t, store, cache, keeper, "v1", filepath.Join("package", "lib", "tsc"))
	assert.FileExists(t, filepath.Join(filepath.Dir(resolved), "lib.d.ts"))
	assert.NoDirExists(t, keptDir)
	require.ErrorIs(t, Install(context.Background(), keeper, intent, store, cache, cfg), ErrAlreadyInstalled)
}

func TestInstall_cacheVersionWithSlash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}
	intent := binny.VersionIntent{Want: "cli/v2.0.0"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))
	requireLinkedIntoCache(t, store, cache, tl, "cli/v2.0.0", "tsc")
	require.NoError(t, Check(store, cache, tl, "cli/v2.0.0", cfg))
	require.ErrorIs(t, Install(context.Background(), tl, intent, store, cache, cfg), ErrAlreadyInstalled)

	// and a second store reuses the entry
	require.NoError(t, Install(context.Background(), tl, intent, newStoreForTest(t), cache, cfg))
	assert.EqualValues(t, 1, tl.installs.Load())
}

func TestInstall_cacheDisabledConvertsLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc"}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))

	// disabling the cache turns the link into a copy, and leaves the entry for other stores
	require.NoError(t, Install(context.Background(), tl, intent, store, nil, cfg))
	fi, err := os.Lstat(filepath.Join(store.Root(), "tsc"))
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular())
	assert.FileExists(t, filepath.Join(cache.EntryDir("tsc", "v1", tl.Fingerprint()), "tsc"))
}

func TestInstall_cacheRepairsDanglingLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))
	require.NoError(t, os.RemoveAll(cache.EntryDir("tsc", "v1", tl.Fingerprint())))

	require.Error(t, Check(store, cache, tl, "v1", cfg))
	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))
	assert.EqualValues(t, 2, tl.installs.Load())
	requireLinkedIntoCache(t, store, cache, tl, "v1", "tsc")
}

func TestInstall_cacheSkipsLocalModule(t *testing.T) {
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc", local: true}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))

	assert.NoDirExists(t, filepath.Join(cache.Root(), "tsc"))
	fi, err := os.Lstat(filepath.Join(store.Root(), "tsc"))
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular())

	require.ErrorIs(t, Install(context.Background(), tl, intent, store, cache, cfg), ErrAlreadyInstalled)
}

var errLinkRefused = errors.New("symlinks refused")

// refuseLinks makes every symlink binny creates fail, the way windows does without developer mode.
func refuseLinks(t *testing.T) {
	t.Helper()
	orig := internal.Symlink
	internal.Symlink = func(string, string) error { return errLinkRefused }
	t.Cleanup(func() { internal.Symlink = orig })
}

func TestInstall_cacheLinkFailsCopies(t *testing.T) {
	refuseLinks(t)
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}

	require.NoError(t, Install(context.Background(), tl, binny.VersionIntent{Want: "v1"}, store, cache, VerifyConfig{VerifySHA256Digest: true}))
	assert.EqualValues(t, 1, tl.installs.Load())

	path := filepath.Join(store.Root(), "tsc")
	fi, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, fi.Mode().IsRegular())

	entry, err := cache.Get("tsc", "v1", tl.Fingerprint())
	require.NoError(t, err)
	require.NotNil(t, entry)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, entry.Digests["sha256"], fmt.Sprintf("%x", sha256.Sum256(got)))
}

func TestInstall_cacheLinkFailsKeepsCopy(t *testing.T) {
	refuseLinks(t)
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))

	// backdate the copy so a rewrite would show in its mtime
	path := filepath.Join(store.Root(), "tsc")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(path, past, past))

	require.ErrorIs(t, Install(context.Background(), tl, intent, store, cache, cfg), ErrAlreadyInstalled)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, past, fi.ModTime())
	assert.EqualValues(t, 1, tl.installs.Load())
}

func TestInstall_cacheLinkFailsOnlyInStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	tl := fakeTool{name: "tsc", installs: &atomic.Int32{}}
	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}

	// symlink support is per filesystem: the store's refuses them (exFAT, SMB) while the cache's allows them
	orig := internal.Symlink
	internal.Symlink = func(oldname, newname string) error {
		if strings.HasPrefix(newname, store.Root()) {
			return errLinkRefused
		}
		return orig(oldname, newname)
	}
	t.Cleanup(func() { internal.Symlink = orig })

	require.NoError(t, Install(context.Background(), tl, intent, store, cache, cfg))

	// a later binny run accepts the copy instead of copying again
	reopened, err := binny.NewStore(store.Root())
	require.NoError(t, err)
	require.NoError(t, Check(reopened, cache, tl, "v1", cfg))
	require.ErrorIs(t, Install(context.Background(), tl, intent, reopened, cache, cfg), ErrAlreadyInstalled)
}

func TestInstall_cacheLinkFailsKeepArchiveFiles(t *testing.T) {
	refuseLinks(t)
	cache := newCacheForTest(t)

	err := Install(context.Background(), fakeTool{name: "tsc", keep: true}, binny.VersionIntent{Want: "v1"}, newStoreForTest(t), cache, VerifyConfig{})
	require.ErrorIs(t, err, errLinkRefused)
}
