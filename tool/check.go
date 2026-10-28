package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anchore/binny"
)

type VerifyConfig struct {
	VerifyXXH64Digest  bool
	VerifySHA256Digest bool
}

func Check(store *binny.Store, cache *binny.Cache, t binny.Tool, resolvedVersion string, verifyConfig VerifyConfig) error {
	toolName := t.Name()
	entry, err := store.Get(toolName, resolvedVersion)
	if err != nil {
		return err
	}

	if entry == nil {
		return fmt.Errorf("tool %q not installed", toolName)
	}

	linked, err := checkCacheLink(cache, t, entry, resolvedVersion)
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

// checkCacheLink reports whether the store entry is a link to the tool's current cache entry. A link to any other
// entry (another fingerprint of the same name and version) is an error, so the next install relinks it.
func checkCacheLink(cache *binny.Cache, t binny.Tool, entry *binny.StoreEntry, resolvedVersion string) (bool, error) {
	fingerprint, ok := cacheFingerprint(cache, t)
	if !ok {
		return false, nil
	}

	fi, err := os.Lstat(entry.Path())
	if err != nil {
		return false, err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}

	target, err := os.Readlink(entry.Path())
	if err != nil {
		return false, err
	}

	// the install writes the absolute entry path, so this is a string comparison without resolving symlinks
	entryDir := cache.EntryDir(t.Name(), resolvedVersion, fingerprint)
	if !strings.HasPrefix(target, entryDir+string(filepath.Separator)) {
		return false, fmt.Errorf("tool %q is linked to %q, not to the cache entry for its current config %q", t.Name(), target, entryDir)
	}

	return true, nil
}
