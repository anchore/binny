package githubrelease

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	return extractor.Extract(ctx, reader, func(ctx context.Context, f archives.FileInfo) error {
		// Sanitize path to prevent directory traversal
		if !filepath.IsLocal(f.NameInArchive) {
			return fmt.Errorf("invalid path in archive: %s", f.NameInArchive)
		}

		destPath := filepath.Join(destDir, f.NameInArchive)

		// Handle directories
		if f.IsDir() {
			return os.MkdirAll(destPath, f.Mode())
		}

		// Handle symlinks
		if f.LinkTarget != "" {
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			return os.Symlink(f.LinkTarget, destPath)
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
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
