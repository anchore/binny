package gobuild

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/binny/tool/goproxy"
)

func TestFindGitRoot(t *testing.T) {
	// this test runs from within the binny repo (tool/gobuild directory)
	// so the git root is "../.." relative to the test working directory

	t.Run("finds git root from current directory", func(t *testing.T) {
		root, err := findGitRoot(".")
		require.NoError(t, err)
		// verify the returned path contains a .git directory
		require.DirExists(t, filepath.Join(root, ".git"))
	})

	t.Run("finds git root from subdirectory path", func(t *testing.T) {
		root, err := findGitRoot("./some/fake/path")
		require.NoError(t, err)
		// verify the returned path contains a .git directory
		require.DirExists(t, filepath.Join(root, ".git"))
	})

	t.Run("returns relative path for relative input", func(t *testing.T) {
		root, err := findGitRoot(".")
		require.NoError(t, err)
		// should return a relative path since input was relative
		require.False(t, filepath.IsAbs(root), "expected relative path but got: %s", root)
	})
}

func TestIsInstallMethod(t *testing.T) {
	tests := []struct {
		name   string
		method string
		want   bool
	}{
		{
			name:   "go-build",
			method: "go-build",
			want:   true,
		},
		{
			name:   "gobuild",
			method: "gobuild",
			want:   true,
		},
		{
			name:   "go build with space",
			method: "go build",
			want:   true,
		},
		{
			name:   "case insensitive",
			method: "Go-Build",
			want:   true,
		},
		{
			name:   "go-install is different",
			method: "go-install",
			want:   false,
		},
		{
			name:   "empty string",
			method: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsInstallMethod(tt.method)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultVersionResolverConfig(t *testing.T) {
	tests := []struct {
		name         string
		params       any
		wantMethod   string
		wantParamsFn func(t *testing.T, params any)
		wantErr      require.ErrorAssertionFunc
	}{
		{
			name: "local module uses git resolver with git root",
			params: InstallerParameters{
				Module: ".",
			},
			wantMethod: git.ResolveMethod,
			wantParamsFn: func(t *testing.T, params any) {
				gitParams, ok := params.(git.VersionResolutionParameters)
				require.True(t, ok)
				// findGitRoot should find the git root containing .git directory
				require.DirExists(t, filepath.Join(gitParams.Path, ".git"))
			},
		},
		{
			name: "relative path module finds git root",
			params: InstallerParameters{
				Module: "./cmd/tool",
			},
			wantMethod: git.ResolveMethod,
			wantParamsFn: func(t *testing.T, params any) {
				gitParams, ok := params.(git.VersionResolutionParameters)
				require.True(t, ok)
				// findGitRoot traverses up and finds the git root containing .git directory
				require.DirExists(t, filepath.Join(gitParams.Path, ".git"))
			},
		},
		{
			name: "github module uses github-release resolver",
			params: InstallerParameters{
				Module: "github.com/owner/repo",
			},
			wantMethod: githubrelease.ResolveMethod,
			wantParamsFn: func(t *testing.T, params any) {
				ghParams, ok := params.(githubrelease.VersionResolutionParameters)
				require.True(t, ok)
				require.Equal(t, "owner/repo", ghParams.Repo)
			},
		},
		{
			name: "github module with subpackage uses github-release resolver",
			params: InstallerParameters{
				Module: "github.com/anchore/syft/cmd/syft",
			},
			wantMethod: githubrelease.ResolveMethod,
			wantParamsFn: func(t *testing.T, params any) {
				ghParams, ok := params.(githubrelease.VersionResolutionParameters)
				require.True(t, ok)
				require.Equal(t, "anchore/syft", ghParams.Repo)
			},
		},
		{
			name: "non-github module uses goproxy resolver",
			params: InstallerParameters{
				Module: "gitlab.com/owner/repo",
			},
			wantMethod: goproxy.ResolveMethod,
			wantParamsFn: func(t *testing.T, params any) {
				gpParams, ok := params.(goproxy.VersionResolutionParameters)
				require.True(t, ok)
				require.Equal(t, "gitlab.com/owner/repo", gpParams.Module)
			},
		},
		{
			name:    "invalid params type",
			params:  "invalid",
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			method, params, err := DefaultVersionResolverConfig(tt.params)
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.wantMethod, method)
			if tt.wantParamsFn != nil {
				tt.wantParamsFn(t, params)
			}
		})
	}
}
