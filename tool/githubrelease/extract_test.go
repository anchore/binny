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
	// securejoin sanitizes symlink targets the same way it sanitizes file paths.
	// A symlink to "../../../etc/passwd" becomes a symlink to "destDir/etc/passwd".
	// This is safe because the symlink points inside destDir, not outside.
	tests := []struct {
		name               string
		linkTarget         string
		expectedTargetPath string // relative to destDir
	}{
		{
			name:               "relative path traversal is sanitized",
			linkTarget:         "../../../etc/passwd",
			expectedTargetPath: "etc/passwd",
		},
		{
			name:               "absolute path is sanitized",
			linkTarget:         "/etc/passwd",
			expectedTargetPath: "etc/passwd",
		},
		{
			name:               "complex traversal is sanitized",
			linkTarget:         "subdir/../../../../../../etc/passwd",
			expectedTargetPath: "etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Create an archive with a symlink that has a traversal path
			archivePath := createArchiveWithSymlink(t, dir, "sanitized_link", tt.linkTarget)

			// Extraction should succeed - securejoin sanitizes the target
			err := extractToDir(context.Background(), archivePath, dir)
			require.NoError(t, err)

			// Verify symlink was created
			linkPath := filepath.Join(dir, "sanitized_link")
			_, err = os.Lstat(linkPath)
			require.NoError(t, err, "symlink should exist")

			// Read where the symlink points
			target, err := os.Readlink(linkPath)
			require.NoError(t, err)

			// The symlink should point to a sanitized path inside destDir
			// (it will be a relative path to destDir/etc/passwd)
			// Use filepath.FromSlash to handle Windows path separators
			expectedTarget := filepath.FromSlash(tt.expectedTargetPath)
			assert.Equal(t, expectedTarget, target,
				"symlink target should be sanitized to safe path inside destDir")
		})
	}
}

func Test_extractToDir_symlinkInsideDir(t *testing.T) {
	dir := t.TempDir()

	// Create an archive with BOTH a symlink and its target file
	// This tests the realistic case where symlink and target are both in the archive
	archivePath := createArchiveWithSymlinkAndTarget(t, dir)

	// Extraction should succeed - symlink stays inside
	err := extractToDir(context.Background(), archivePath, dir)
	require.NoError(t, err)

	// Verify symlink was created
	linkPath := filepath.Join(dir, "valid_link")
	_, err = os.Lstat(linkPath)
	require.NoError(t, err, "symlink should exist")

	// Verify symlink can be followed and reads correct content
	content, err := os.ReadFile(linkPath)
	require.NoError(t, err)
	assert.Equal(t, "target content", string(content))
}

func Test_extractToDir_symlinkBeforeTarget(t *testing.T) {
	// This tests the edge case where a symlink appears in the archive BEFORE its target.
	// The symlink is valid (points inside destDir) but the target doesn't exist yet
	// when the symlink is created ("dangling symlink").
	dir := t.TempDir()

	// Create archive where symlink comes BEFORE the target file
	archivePath := createArchiveWithSymlinkBeforeTarget(t, dir)

	// Extraction should succeed - even though symlink is temporarily dangling
	err := extractToDir(context.Background(), archivePath, dir)
	require.NoError(t, err)

	// Verify both symlink and target exist
	linkPath := filepath.Join(dir, "link_first")
	targetPath := filepath.Join(dir, "target_second.txt")

	_, err = os.Lstat(linkPath)
	require.NoError(t, err, "symlink should exist")

	_, err = os.Stat(targetPath)
	require.NoError(t, err, "target file should exist")

	// Verify symlink can be followed
	content, err := os.ReadFile(linkPath)
	require.NoError(t, err)
	assert.Equal(t, "target content", string(content))
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

// createArchiveWithSymlinkAndTarget creates an archive with a target file FIRST, then a symlink to it.
// This is the "easy" case where the target exists when the symlink is validated.
func createArchiveWithSymlinkAndTarget(t *testing.T, dir string) string {
	archivePath := filepath.Join(dir, "symlink_with_target.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// First: create the target file
	content := []byte("target content")
	fileHeader := &tar.Header{
		Name: "target.txt",
		Size: int64(len(content)),
		Mode: 0644,
	}
	require.NoError(t, tarWriter.WriteHeader(fileHeader))
	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	// Second: create a symlink pointing to the target
	linkHeader := &tar.Header{
		Name:     "valid_link",
		Typeflag: tar.TypeSymlink,
		Linkname: "target.txt",
		Mode:     0777,
	}
	require.NoError(t, tarWriter.WriteHeader(linkHeader))

	return archivePath
}

// createArchiveWithSymlinkBeforeTarget creates an archive with a symlink FIRST, then its target.
// This exposes the "dangling symlink" edge case where the symlink is created before its target exists.
func createArchiveWithSymlinkBeforeTarget(t *testing.T, dir string) string {
	archivePath := filepath.Join(dir, "symlink_before_target.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// First: create the symlink (target doesn't exist yet!)
	linkHeader := &tar.Header{
		Name:     "link_first",
		Typeflag: tar.TypeSymlink,
		Linkname: "target_second.txt",
		Mode:     0777,
	}
	require.NoError(t, tarWriter.WriteHeader(linkHeader))

	// Second: create the target file
	content := []byte("target content")
	fileHeader := &tar.Header{
		Name: "target_second.txt",
		Size: int64(len(content)),
		Mode: 0644,
	}
	require.NoError(t, tarWriter.WriteHeader(fileHeader))
	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	return archivePath
}
