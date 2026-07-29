package gobuild

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSourceMode(t *testing.T) {
	tests := []struct {
		name  string
		input SourceMode
		want  SourceMode
	}{
		{
			name:  "git lowercase",
			input: "git",
			want:  SourceModeGit,
		},
		{
			name:  "git uppercase",
			input: "GIT",
			want:  SourceModeGit,
		},
		{
			name:  "goproxy lowercase",
			input: "goproxy",
			want:  SourceModeGoProxy,
		},
		{
			name:  "go-proxy with hyphen",
			input: "go-proxy",
			want:  SourceModeGoProxy,
		},
		{
			name:  "go proxy with space",
			input: "go proxy",
			want:  SourceModeGoProxy,
		},
		{
			name:  "proxy shorthand",
			input: "proxy",
			want:  SourceModeGoProxy,
		},
		{
			name:  "GoProxy mixed case",
			input: "GoProxy",
			want:  SourceModeGoProxy,
		},
		{
			name:  "unknown returns as-is",
			input: "unknown",
			want:  "unknown",
		},
		{
			name:  "empty string returns as-is",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSourceMode(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCopyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission tests not supported on Windows")
	}

	// create a temporary source directory
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// create a regular file with specific permissions
	regularFile := filepath.Join(srcDir, "regular.txt")
	err := os.WriteFile(regularFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	// create an executable file with executable permissions
	execFile := filepath.Join(srcDir, "script.sh")
	err = os.WriteFile(execFile, []byte("#!/bin/bash\necho hello"), 0755)
	require.NoError(t, err)

	// create a subdirectory with specific permissions
	subDir := filepath.Join(srcDir, "subdir")
	err = os.MkdirAll(subDir, 0750)
	require.NoError(t, err)

	// create a file in the subdirectory
	subFile := filepath.Join(subDir, "nested.txt")
	err = os.WriteFile(subFile, []byte("nested content"), 0600)
	require.NoError(t, err)

	// copy the directory
	err = copyDir(srcDir, dstDir)
	require.NoError(t, err)

	// verify regular file was copied with correct permissions
	dstRegularFile := filepath.Join(dstDir, "regular.txt")
	info, err := os.Stat(dstRegularFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0644), info.Mode().Perm())
	content, err := os.ReadFile(dstRegularFile)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(content))

	// verify executable file was copied with correct permissions
	dstExecFile := filepath.Join(dstDir, "script.sh")
	info, err = os.Stat(dstExecFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm())

	// verify subdirectory was created with correct permissions
	dstSubDir := filepath.Join(dstDir, "subdir")
	info, err = os.Stat(dstSubDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, os.FileMode(0750), info.Mode().Perm())

	// verify nested file was copied with correct permissions
	dstSubFile := filepath.Join(dstDir, "subdir", "nested.txt")
	info, err = os.Stat(dstSubFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	content, err = os.ReadFile(dstSubFile)
	require.NoError(t, err)
	require.Equal(t, "nested content", string(content))
}

func TestCopyDir_Symlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests not supported on Windows")
	}

	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// create a regular file
	regularFile := filepath.Join(srcDir, "target.txt")
	err := os.WriteFile(regularFile, []byte("target content"), 0644)
	require.NoError(t, err)

	// create a symlink to the regular file
	symlinkFile := filepath.Join(srcDir, "link.txt")
	err = os.Symlink("target.txt", symlinkFile)
	require.NoError(t, err)

	// create a subdirectory
	subDir := filepath.Join(srcDir, "subdir")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	// create a symlink to the subdirectory
	symlinkDir := filepath.Join(srcDir, "linkdir")
	err = os.Symlink("subdir", symlinkDir)
	require.NoError(t, err)

	// copy the directory
	err = copyDir(srcDir, dstDir)
	require.NoError(t, err)

	// verify the symlink to file was preserved
	dstSymlinkFile := filepath.Join(dstDir, "link.txt")
	linkInfo, err := os.Lstat(dstSymlinkFile)
	require.NoError(t, err)
	require.True(t, linkInfo.Mode()&os.ModeSymlink != 0, "expected symlink")

	target, err := os.Readlink(dstSymlinkFile)
	require.NoError(t, err)
	require.Equal(t, "target.txt", target)

	// verify the symlink to directory was preserved
	dstSymlinkDir := filepath.Join(dstDir, "linkdir")
	linkInfo, err = os.Lstat(dstSymlinkDir)
	require.NoError(t, err)
	require.True(t, linkInfo.Mode()&os.ModeSymlink != 0, "expected symlink")

	target, err = os.Readlink(dstSymlinkDir)
	require.NoError(t, err)
	require.Equal(t, "subdir", target)
}
