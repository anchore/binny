package tool

import (
	"fmt"

	"github.com/anchore/binny"
)

type VerifyConfig struct {
	VerifyXXH64Digest  bool
	VerifySHA256Digest bool
}

func Check(store *binny.Store, toolName string, resolvedVersion string, verifyConfig VerifyConfig) error {
	entry, err := store.Get(toolName, resolvedVersion)
	if err != nil {
		return err
	}

	if entry == nil {
		return fmt.Errorf("tool %q not installed", toolName)
	}

	if err := entry.Verify(verifyConfig.VerifyXXH64Digest, verifyConfig.VerifySHA256Digest); err != nil {
		return fmt.Errorf("failed to validate tool %q: %w", toolName, err)
	}

	return nil
}
