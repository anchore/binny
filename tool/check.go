package tool

import (
	"fmt"

	"github.com/anchore/binny"
)

type VerifyConfig struct {
	VerifyXXH64Digest  bool
	VerifySHA256Digest bool
}

func Check(store *binny.Store, t binny.Tool, resolvedVersion string, verifyConfig VerifyConfig) error {
	toolName := t.Name()
	entry, err := store.Get(toolName, resolvedVersion)
	if err != nil {
		return err
	}

	if entry == nil {
		return fmt.Errorf("tool %q not installed", toolName)
	}

	// keeping the archive files changes what is installed, so an install with the other layout doesn't count
	if keep := keepsArchiveFiles(t); keep != (entry.DirInRoot != "") {
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
