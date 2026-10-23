package gobuild

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// windows absolute paths are local too (the IsAbs check that fixed the windows CI run); this only runs on windows.
func TestIsLocalModule_windowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path forms")
	}
	for module, want := range map[string]bool{
		`C:\proj`:          true,
		`C:/proj`:          true,
		`\\server\share\x`: true,
		`.\proj`:           true,
		`github.com/x/y`:   false,
		// rooted but drive-less, and drive-relative: not absolute on windows, so classed as remote today
		`\proj`:  false,
		`C:proj`: false,
	} {
		require.Equalf(t, want, IsLocalModule(module), "IsLocalModule(%q)", module)
	}
}

// a local module has to start with ".", "/" (or be absolute): a bare relative path like the README's
// "path/to/module/on/disk" is a remote import path, so the README should show "./path/to/module/on/disk".
func TestIsLocalModule_bareRelativePathIsRemote(t *testing.T) {
	require.False(t, IsLocalModule("path/to/module/on/disk"))
}
