package option

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/tool/githubrelease"
)

func TestDeriveInstallParameters_GithubRelease(t *testing.T) {
	tests := []struct {
		name      string
		params    map[string]any
		goos      string
		expected  githubrelease.InstallerParameters
		expectErr require.ErrorAssertionFunc
	}{
		{
			name: "valid parameters with binary name",
			params: map[string]any{
				"binary": "mytool",
				"repo":   "owner/repo",
			},
			goos:      "linux",
			expected:  githubrelease.InstallerParameters{Binary: "mytool", Repo: "owner/repo"},
			expectErr: require.NoError,
		},
		{
			name: "missing binary name, should default to tool name on Linux",
			params: map[string]any{
				"repo": "owner/repo",
			},
			goos:      "linux",
			expected:  githubrelease.InstallerParameters{Binary: "mytool", Repo: "owner/repo"},
			expectErr: require.NoError,
		},
		{
			name: "missing binary name, should default to tool name with .exe on Windows",
			params: map[string]any{
				"repo": "owner/repo",
			},
			goos:      "windows",
			expected:  githubrelease.InstallerParameters{Binary: "mytool.exe", Repo: "owner/repo"},
			expectErr: require.NoError,
		},
		{
			name: "bad data shape should return an error",
			params: map[string]any{
				"binary": map[string]string{"bogus": "BogOsiTy"},
			},
			goos:      "linux",
			expectErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := deriveInstallParameters("mytool", "githubrelease", tt.params, tt.goos)
			if tt.expectErr == nil {
				tt.expectErr = require.NoError
			}
			tt.expectErr(t, err)
			if err == nil {
				instParams, ok := result.(githubrelease.InstallerParameters)
				require.True(t, ok)
				require.Equal(t, tt.expected, instParams)
			}
		})
	}
}
