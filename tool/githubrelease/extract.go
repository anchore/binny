package githubrelease

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	return extractor.Extract(ctx, reader, func(ctx context.Context, f archives.FileInfo) error {
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
			// Resolve the symlink target relative to the symlink's directory
			// Use filepath.Clean to normalize without following symlinks
			var resolvedTarget string
			if filepath.IsAbs(f.LinkTarget) {
				resolvedTarget = filepath.Clean(f.LinkTarget)
			} else {
				resolvedTarget = filepath.Clean(filepath.Join(filepath.Dir(destPath), f.LinkTarget))
			}

			// Ensure the resolved target stays within destDir
			// We need to check the cleaned path, not use securejoin (which would sanitize it)
			if !strings.HasPrefix(resolvedTarget, filepath.Clean(destDir)+string(filepath.Separator)) &&
				resolvedTarget != filepath.Clean(destDir) {
				return fmt.Errorf("symlink escapes extraction directory: %q -> %q", f.NameInArchive, f.LinkTarget)
			}

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
