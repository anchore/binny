package goinstall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/goproxy"
)

func TestMethods(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		want    bool
	}{
		{
			name:    "valid",
			methods: []string{"go-install", "go", "go install", "goinstall", "golang"},
			want:    true,
		},
		{
			name:    "invalid",
			methods: []string{"made up"},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, method := range tt.methods {
				t.Run(method, func(t *testing.T) {
					t.Run("IsInstallMethod", func(t *testing.T) {
						assert.Equal(t, tt.want, IsInstallMethod(method))
					})
				})
			}
		})
	}
}

func TestDefaultVersionResolverConfig(t *testing.T) {
	tests := []struct {
		name          string
		installParams any
		wantMethod    string
		wantParams    any
		// for a git resolver, the directory its path should point at; compared by location, since the
		// path is relative to the working directory and differs in spelling under a symlinked checkout
		wantGitRoot string
		wantErr     assert.ErrorAssertionFunc
	}{
		{
			name: "valid",
			installParams: InstallerParameters{
				Module:     "github.com/anchore/binny",
				Entrypoint: "cmd/binny",
				LDFlags:    []string{"-X main.version=1.0.0"},
			},
			wantMethod: goproxy.ResolveMethod,
			wantParams: goproxy.VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
		},
		{
			// the test runs from tool/goinstall, so the git root is two levels up
			name:          "local module uses the git resolver at the repo root",
			installParams: InstallerParameters{Module: "."},
			wantMethod:    git.ResolveMethod,
			wantGitRoot:   filepath.Join("..", ".."),
		},
		{
			name: "invalid",
			installParams: map[string]string{
				"module": "github.com/anchore/binny",
			},
			wantErr: assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = assert.NoError
			}
			method, params, err := DefaultVersionResolverConfig(tt.installParams)
			if !tt.wantErr(t, err) {
				return
			}
			assert.Equal(t, tt.wantMethod, method)
			if tt.wantGitRoot == "" {
				assert.Equal(t, tt.wantParams, params)
				return
			}
			if _, err := os.Stat(filepath.Join(tt.wantGitRoot, ".git")); err != nil {
				t.Skip("not running from a git checkout (e.g. a source tarball)")
			}
			cfg, ok := params.(git.VersionResolutionParameters)
			if !assert.True(t, ok, "params are %T, not git.VersionResolutionParameters", params) {
				return
			}
			assert.Equal(t, location(t, tt.wantGitRoot), location(t, cfg.Path))
		})
	}
}

// location is where a path relative to the working directory points: Abs joins it to the working directory
// as the shell spells it, then EvalSymlinks resolves any symlinks, so two spellings of one place compare equal.
func location(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if !assert.NoError(t, err) {
		return p
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if !assert.NoError(t, err) {
		return abs
	}
	return resolved
}
