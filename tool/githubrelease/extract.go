package githubrelease

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/mholt/archives"
)

// extractToDir extracts an archive file to the destination directory
func extractToDir(ctx context.Context, archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("unable to open archive: %w", err)
	}
	defer file.Close()

	// Identify the archive format
	format, reader, err := archives.Identify(ctx, archivePath, file)
	if err != nil {
		return fmt.Errorf("unable to identify archive format: %w", err)
	}

	// Check if format supports extraction
	extractor, ok := format.(archives.Extractor)
	if !ok {
		return fmt.Errorf("format %T does not support extraction", format)
	}

	// Extract files
	return extractor.Extract(ctx, reader, func(_ context.Context, f archives.FileInfo) error {
		// SecureJoin resolves the path safely, preventing traversal outside destDir
		destPath, err := securejoin.SecureJoin(destDir, f.NameInArchive)
		if err != nil {
			return fmt.Errorf("invalid path in archive %q: %w", f.NameInArchive, err)
		}

		// Handle directories
		if f.IsDir() {
			return os.MkdirAll(destPath, f.Mode())
		}

		// Handle symlinks
		if f.LinkTarget != "" {
			// Validate symlink target using securejoin to prevent directory traversal.
			// SecureJoin ensures the target path resolves to somewhere inside destDir.
			validatedTarget, err := securejoin.SecureJoin(destDir, f.LinkTarget)
			if err != nil {
				return fmt.Errorf("invalid symlink target %q: %w", f.LinkTarget, err)
			}

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}

			// Calculate relative path from symlink location to validated target
			relTarget, err := filepath.Rel(filepath.Dir(destPath), validatedTarget)
			if err != nil {
				return fmt.Errorf("unable to create relative symlink path: %w", err)
			}

			// Create symlink with the validated relative target
			return os.Symlink(relTarget, destPath)
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		// Extract file
		srcFile, err := f.Open()
		if err != nil {
			return err
		}
		defer srcFile.Close()

		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		return err
	})
}
