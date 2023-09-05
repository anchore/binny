package tool

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/anchore/binny"
)

var ErrMultipleInstallations = fmt.Errorf("too many installations found")

func Check(toolName string, resolvedVersion string, store *binny.Store, verifyDigest bool) error {
	// check if the tool is already installed...
	// if the version matches the desired version, skip
	nameVersionEntries := store.GetByName(toolName, resolvedVersion)

	switch len(nameVersionEntries) {
	case 0:
		nameEntries := store.GetByName(toolName)
		if len(nameEntries) > 0 {
			return fmt.Errorf("tool already installed with different configuration")
		}
		return fmt.Errorf("tool not installed")

	case 1:
		// pass

	default:
		return ErrMultipleInstallations
	}

	entry := nameVersionEntries[0]

	if entry.InstalledVersion != resolvedVersion {
		return fmt.Errorf("tool %q has different version: %s", toolName, entry.InstalledVersion)
	}

	if verifyDigest {
		if err := verifyEntry(entry); err != nil {
			return fmt.Errorf("failed to validate tool %q: %w", toolName, err)
		}
	} else {
		// at least the file must exist
		if _, err := os.Stat(entry.Path()); err != nil {
			return fmt.Errorf("failed to validate tool %q: %w", toolName, err)
		}
	}

	return nil
}

func verifyEntry(entry binny.StoreEntry) error {
	expect := entry.Digest
	actual, err := sha256File(entry.Path())
	if err != nil {
		return fmt.Errorf("failed to calculate sha256 of %q: %w", entry.Path(), err)
	}

	if expect != actual {
		return fmt.Errorf("sha256 mismatch for %q: expected %q, got %q", entry.Path(), expect, actual)
	}

	return nil
}

func sha256File(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer fh.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, fh); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
