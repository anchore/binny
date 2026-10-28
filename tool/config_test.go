package tool

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/tool/gobuild"
	"github.com/anchore/binny/tool/goinstall"
)

func Test_goCommandEnv(t *testing.T) {
	env := []string{"GOPROXY=https://corp.example", "GOFLAGS=-ldflags=-X=main.v={{ .Version }}", "bogus"}
	want := []string{"GOPROXY=https://corp.example"}

	assert.Equal(t, want, goCommandEnv(goinstall.InstallerParameters{Env: env}))
	assert.Equal(t, want, goCommandEnv(gobuild.InstallerParameters{Env: env}))
	assert.Empty(t, goCommandEnv(nil))
}

func TestNew_goBuildTracksGitRefs(t *testing.T) {
	tl, err := New(Config{
		Name: "tool",
		InstallerConfig: DetailConfig{
			Method:     gobuild.InstallMethod,
			Parameters: gobuild.InstallerParameters{Module: "github.com/anchore/binny"},
		},
	})
	require.NoError(t, err)

	// go-build clones from git by default, so refs are resolved against the repo whatever resolver is configured
	assert.Equal(t, "gobuild.refResolver", fmt.Sprintf("%T", tl.(*compositeTool).VersionResolver))
}

func TestNew_goBuildFromProxyTracksRefs(t *testing.T) {
	// a GitHub module defaults to the github-release resolver, which passes refs through untouched. With go-proxy
	// source refs still have to be resolved (by the go command), or a moved branch is never noticed
	tl, err := New(Config{
		Name: "tool",
		InstallerConfig: DetailConfig{
			Method: gobuild.InstallMethod,
			Parameters: gobuild.InstallerParameters{
				Module: "github.com/anchore/binny",
				Source: gobuild.SourceModeGoProxy,
				// keeps this offline, and shows the tool's env reaches the go command
				Env: []string{"GOPROXY=off"},
			},
		},
	})
	require.NoError(t, err)

	_, err = tl.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.ErrorContains(t, err, "GOPROXY=off", "the ref should reach the go command, not be passed through")
}

func TestNew_goProxyLookupsUseToolEnv(t *testing.T) {
	// the tool's env reaches the go command that resolves its version, so GOPROXY=off there means no lookup. The
	// process env points at a dead proxy, so the error can only say GOPROXY=off if the tool's env got through
	t.Setenv("GOPROXY", "http://127.0.0.1:1")
	tl, err := New(Config{
		Name: "tool",
		InstallerConfig: DetailConfig{
			Method:     goinstall.InstallMethod,
			Parameters: goinstall.InstallerParameters{Module: "github.com/anchore/binny", Env: []string{"GOPROXY=off"}},
		},
	})
	require.NoError(t, err)

	_, err = tl.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.ErrorContains(t, err, "GOPROXY=off")
}

func TestNew_goBuildResolvesRefsAgainstTheRepo(t *testing.T) {
	// go-build from git resolves a branch to the commit the repo says it points at, whatever the version method
	// (this checks the behaviour TestNew_goBuildTracksGitRefs checks by type name)
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "one")
	head := git("rev-parse", "HEAD")

	tl, err := New(Config{
		Name: "tool",
		InstallerConfig: DetailConfig{
			Method:     gobuild.InstallMethod,
			Parameters: gobuild.InstallerParameters{Module: "example.com/tool", RepoURL: "file://" + filepath.ToSlash(dir)},
		},
	})
	require.NoError(t, err)

	got, err := tl.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.NoError(t, err)
	assert.Equal(t, head, got)
}

func Test_compositeTool_BypassesCache(t *testing.T) {
	tests := []struct {
		name   string
		method string
		params any
		want   bool
	}{
		{name: "go-install local module", method: goinstall.InstallMethod, params: goinstall.InstallerParameters{Module: "."}, want: true},
		{name: "go-install remote module", method: goinstall.InstallMethod, params: goinstall.InstallerParameters{Module: "github.com/anchore/binny"}},
		{name: "go-build local module", method: gobuild.InstallMethod, params: gobuild.InstallerParameters{Module: "."}, want: true},
		{name: "go-build remote module", method: gobuild.InstallMethod, params: gobuild.InstallerParameters{Module: "github.com/anchore/binny"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl := compositeTool{config: Config{Name: "binny", InstallerConfig: DetailConfig{Method: tt.method, Parameters: tt.params}}}
			assert.Equal(t, tt.want, tl.BypassesCache())
		})
	}
}
