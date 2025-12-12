package githubrelease

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_extractToDir_pathTraversal(t *testing.T) {
	// securejoin sanitizes traversal paths rather than rejecting them,
	// so we verify the file ends up inside the extraction directory
	// rather than escaping to the actual path
	tests := []struct {
		name            string
		archivePath     string // path within the archive
		expectedInside  bool   // should file end up inside destDir?
		expectedSubpath string // where it should end up (relative to destDir)
	}{
		{
			name:            "path traversal with ../ is sanitized",
			archivePath:     "../../etc/passwd",
			expectedInside:  true,
			expectedSubpath: "etc/passwd",
		},
		{
			name:            "path traversal with subdir/../../../ is sanitized",
			archivePath:     "subdir/../../../etc/passwd",
			expectedInside:  true,
			expectedSubpath: "etc/passwd",
		},
		{
			name:            "absolute path is sanitized",
			archivePath:     "/etc/passwd",
			expectedInside:  true,
			expectedSubpath: "etc/passwd",
		},
		{
			name:            "normal path works",
			archivePath:     "normal/path/file.txt",
			expectedInside:  true,
			expectedSubpath: "normal/path/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Create an archive with the test path
			archivePath := createMaliciousArchive(t, dir, tt.archivePath)

			// Extract - should succeed (securejoin sanitizes, doesn't reject)
			err := extractToDir(context.Background(), archivePath, dir)
			require.NoError(t, err)

			// Verify the file ended up inside the extraction directory
			expectedPath := filepath.Join(dir, tt.expectedSubpath)
			_, err = os.Stat(expectedPath)
			assert.NoError(t, err, "file should exist at sanitized path: %s", expectedPath)

			// Verify no file escaped outside the extraction directory
			// Walk parent directories to ensure nothing leaked
			parentDir := filepath.Dir(dir)
			err = filepath.Walk(parentDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // ignore errors walking
				}
				if !info.IsDir() && !strings.HasPrefix(path, dir) {
					t.Errorf("file escaped extraction directory: %s", path)
				}
				return nil
			})
			assert.NoError(t, err)
		})
	}
}

func Test_extractToDir_symlinkTraversal(t *testing.T) {
	tests := []struct {
		name       string
		linkTarget string
	}{
		{
			name:       "relative path traversal",
			linkTarget: "../../../etc/passwd",
		},
		{
			name:       "absolute path",
			linkTarget: "/etc/passwd",
		},
		{
			name:       "complex traversal",
			linkTarget: "subdir/../../../../../../etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Create an archive with a symlink pointing outside the extraction directory
			archivePath := createArchiveWithSymlink(t, dir, "malicious_link", tt.linkTarget)

			// Attempt extraction - should fail because symlink target would escape
			err := extractToDir(context.Background(), archivePath, dir)
			require.Error(t, err)
			// The error could be about escaping or validation failure (if target doesn't exist)
			assert.True(t, strings.Contains(err.Error(), "symlink") || strings.Contains(err.Error(), "validation"),
				"error should mention symlink issue: %v", err)

			// Verify no symlink was left behind
			linkPath := filepath.Join(dir, "malicious_link")
			_, err = os.Lstat(linkPath)
			assert.True(t, os.IsNotExist(err), "malicious symlink should not exist")
		})
	}
}

func Test_extractToDir_symlinkInsideDir(t *testing.T) {
	dir := t.TempDir()

	// First create a file to link to
	targetFile := filepath.Join(dir, "target.txt")
	err := os.WriteFile(targetFile, []byte("target content"), 0644)
	require.NoError(t, err)

	// Create an archive with a symlink pointing inside the extraction directory
	archivePath := createArchiveWithSymlink(t, dir, "valid_link", "target.txt")

	// Extraction should succeed - symlink stays inside
	err = extractToDir(context.Background(), archivePath, dir)
	require.NoError(t, err)

	// Verify symlink was created
	linkPath := filepath.Join(dir, "valid_link")
	_, err = os.Lstat(linkPath)
	require.NoError(t, err, "symlink should exist")

	// Verify symlink can be followed and reads correct content
	content, err := os.ReadFile(linkPath)
	require.NoError(t, err)
	assert.Equal(t, "target content", string(content))

	// Verify symlink resolves inside destDir (defense in depth check)
	realPath, err := filepath.EvalSymlinks(linkPath)
	require.NoError(t, err)
	// Resolve dir to canonical path too (handles macOS /var -> /private/var)
	canonicalDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(realPath, canonicalDir),
		"symlink should resolve inside extraction directory, got: %s (expected prefix: %s)", realPath, canonicalDir)
}

func createMaliciousArchive(t *testing.T, dir, maliciousPath string) string {
	archivePath := filepath.Join(dir, "malicious.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Create a file with a malicious path
	content := []byte("malicious content")
	header := &tar.Header{
		Name: maliciousPath,
		Size: int64(len(content)),
		Mode: 0644,
	}
	require.NoError(t, tarWriter.WriteHeader(header))
	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	return archivePath
}

func createArchiveWithSymlink(t *testing.T, dir, linkName, linkTarget string) string {
	archivePath := filepath.Join(dir, "symlink_attack.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Create a symlink entry
	header := &tar.Header{
		Name:     linkName,
		Typeflag: tar.TypeSymlink,
		Linkname: linkTarget,
		Mode:     0777,
	}
	require.NoError(t, tarWriter.WriteHeader(header))

	return archivePath
}
