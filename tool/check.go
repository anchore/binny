package tool

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
)

type VerifyConfig struct {
	VerifyXXH64Digest  bool
	VerifySHA256Digest bool
}

// Check returns an error when the tool is not installed in the store at the resolved version, in the layout the cache
// setting asks for, with matching digests. A nil cache means the cache is disabled.
func Check(store *binny.Store, cache *binny.Cache, t binny.Tool, resolvedVersion string, verifyConfig VerifyConfig) error {
	toolName := t.Name()
	entry, err := store.Get(toolName, resolvedVersion)
	if err != nil {
		return err
	}

	if entry == nil {
		return fmt.Errorf("tool %q not installed", toolName)
	}

	linked, err := checkCacheLayout(store, cache, t, entry, resolvedVersion)
	if err != nil {
		return err
	}

	// keeping the archive files changes what is installed, so an install with the other layout doesn't count. A link
	// to the current cache entry already matches, since keep-archive-files is part of the fingerprint.
	if keep := keepsArchiveFiles(t); !linked && keep != (entry.DirInRoot != "") {
		return fmt.Errorf("tool %q is installed with keep-archive-files=%t", toolName, !keep)
	}

	if err := entry.Verify(verifyConfig.VerifyXXH64Digest, verifyConfig.VerifySHA256Digest); err != nil {
		return fmt.Errorf("failed to validate tool %q: %w", toolName, err)
	}

	return nil
}

func keepsArchiveFiles(t binny.Tool) bool {
	k, ok := t.(binny.ArchiveFileKeeper)
	return ok && k.KeepArchiveFiles()
}

// checkCacheLayout reports whether the store entry is a link to the tool's current cache entry. An entry whose layout
// does not match the cache setting is an error, so the next install converts it: a copy while the cache is used, a
// link into the cache while it is not, or a link to another fingerprint's entry of the same name and version.
func checkCacheLayout(store *binny.Store, cache *binny.Cache, t binny.Tool, entry *binny.StoreEntry, resolvedVersion string) (bool, error) {
	linkedOut, err := entry.IsCacheLink()
	if err != nil {
		return false, fmt.Errorf("failed to validate tool %q: %w", t.Name(), err)
	}

	fingerprint, useCache := cacheFingerprint(cache, t, resolvedVersion)
	if !useCache {
		if linkedOut {
			return false, fmt.Errorf("tool %q is linked into the cache, which is not used for it", t.Name())
		}
		return false, nil
	}

	if !linkedOut {
		// where symlinks are refused, a copy of the current entry is what the install leaves. The probe makes and
		// removes a temp dir in the store root, so even `binny check` and `binny list` write there once, but only for
		// a copy that matches the cache entry.
		if entry.DirInRoot == "" && isCopyOfCacheEntry(cache, entry, resolvedVersion, fingerprint) && !store.LinksWork() {
			return false, nil
		}
		return false, fmt.Errorf("tool %q is installed in the store, not linked into the cache", t.Name())
	}

	target, err := os.Readlink(entry.Path())
	if err != nil {
		return false, err
	}

	// the install writes the absolute entry path, so this is a lexical comparison without resolving symlinks. One that
	// climbs back out of the entry with .. is not local to it, and neither is the entry dir itself.
	entryDir := cache.EntryDir(t.Name(), resolvedVersion, fingerprint)
	if rel, err := filepath.Rel(entryDir, target); err != nil || !filepath.IsLocal(rel) {
		return false, fmt.Errorf("tool %q is linked to %q, not to the cache entry for its current config %q", t.Name(), target, entryDir)
	}

	return true, nil
}

// isCopyOfCacheEntry reports whether the store entry recorded the same xxh64 as the cache entry at the fingerprint. It
// compares recorded digests only: Check hashes the store file right after.
func isCopyOfCacheEntry(cache *binny.Cache, entry *binny.StoreEntry, resolvedVersion, fingerprint string) bool {
	cached, err := cache.Get(entry.Name, resolvedVersion, fingerprint)
	if err != nil || cached == nil {
		return false
	}
	want, ok := cached.Digests[internal.XXH64Algorithm]
	return ok && entry.Digests[internal.XXH64Algorithm] == want
}
