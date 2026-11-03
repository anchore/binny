package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/binny/tool/gobuild"
	"github.com/anchore/binny/tool/goinstall"
	"github.com/anchore/binny/tool/goproxy"
	"github.com/anchore/binny/tool/hostedshell"
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

func Test_compositeTool_cacheable(t *testing.T) {
	remote := goinstall.InstallerParameters{Module: "github.com/anchore/binny"}
	tests := []struct {
		name   string
		method string
		params any
		want   bool
	}{
		{name: "go-install local module", method: goinstall.InstallMethod, params: goinstall.InstallerParameters{Module: "."}},
		{name: "go-install remote module", method: goinstall.InstallMethod, params: remote, want: true},
		{name: "go-build local module", method: gobuild.InstallMethod, params: gobuild.InstallerParameters{Module: "."}},
		{name: "go-build remote module", method: gobuild.InstallMethod, params: gobuild.InstallerParameters{Module: "github.com/anchore/binny"}, want: true},
		{name: "github-release", method: githubrelease.InstallMethod, params: githubrelease.InstallerParameters{Repo: "cli/cli"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl := compositeTool{config: Config{Name: "binny", InstallerConfig: DetailConfig{Method: tt.method, Parameters: tt.params}}}
			assert.Equal(t, tt.want, tl.cacheable())
			assert.Equal(t, tt.want, tl.Fingerprint() != "")
		})
	}
}

func Test_cacheFingerprint_exactVersion(t *testing.T) {
	cache, err := binny.NewCache(t.TempDir())
	require.NoError(t, err)
	// the github release resolver hands back any non-semver tag as-is, such as a rolling nightly
	tl := compositeTool{config: Config{
		Name:            "gh",
		InstallerConfig: DetailConfig{Method: githubrelease.InstallMethod, Parameters: githubrelease.InstallerParameters{Repo: "cli/cli"}},
	}}

	versions := map[string]bool{"v0.7.0": true, "bun-v1.4.3": true, "0123abc": true, "latest": false, "main": false, "nightly": false, "v1": false, "v1.2": false}
	for version, want := range versions {
		_, skipped := cacheFingerprint(cache, tl, version)
		assert.Equal(t, want, skipped == "", version)
	}
}

func Test_cacheFingerprint_passedThroughVersion(t *testing.T) {
	fakeGoEnv(t, func([]string) string { return `{"GOVERSION":"go1.26.0"}` })
	cache, err := binny.NewCache(t.TempDir())
	require.NoError(t, err)
	remote := DetailConfig{Method: goinstall.InstallMethod, Parameters: goinstall.InstallerParameters{Module: "github.com/anchore/binny"}}
	// these hand back an unresolvable want as-is, so it may be a branch named like a version or a sha
	tools := map[string]compositeTool{
		"allow-unresolved-version": {config: Config{Name: "binny", InstallerConfig: remote, VersionResolverConfig: DetailConfig{
			Method: goproxy.ResolveMethod, Parameters: goproxy.VersionResolutionParameters{AllowUnresolvedVersion: true},
		}}},
		"git resolver": {config: Config{Name: "binny", InstallerConfig: remote, VersionResolverConfig: DetailConfig{Method: git.ResolveMethod}}},
	}
	versions := map[string]bool{"v0.7.0": true, "v0.7.0-rc.1": true, "deadbee": false, "release/v1.2.3": false, "bun-v1.4.3": false}
	for name, tl := range tools {
		for version, want := range versions {
			_, skipped := cacheFingerprint(cache, tl, version)
			assert.Equal(t, want, skipped == "", "%s at %s", name, version)
		}
	}

	// a resolver that resolves every ref does vouch for a sha
	resolved := compositeTool{config: Config{Name: "binny", InstallerConfig: remote}}
	_, skipped := cacheFingerprint(cache, resolved, "deadbee")
	assert.Empty(t, skipped)
}

// fakeGoEnv makes every go env read return env, and forgets the reads before and after the test.
func fakeGoEnv(t *testing.T, env func([]string) string) {
	t.Helper()
	orig := goEnv
	goEnvs.Clear()
	t.Cleanup(func() {
		goEnv = orig
		goEnvs.Clear()
	})
	goEnv = env
}

func Test_compositeTool_Fingerprint_goEnvUnreadable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	assert.Empty(t, readGoEnv(nil))

	fakeGoEnv(t, func([]string) string { return "" })

	// a go tool without a readable go env would otherwise share one key across every toolchain
	goTool := compositeTool{config: Config{Name: "binny", InstallerConfig: DetailConfig{Method: goinstall.InstallMethod, Parameters: goinstall.InstallerParameters{Module: "github.com/anchore/binny"}}}}
	assert.Empty(t, goTool.Fingerprint())

	other := compositeTool{config: Config{Name: "gh", InstallerConfig: DetailConfig{Method: githubrelease.InstallMethod, Parameters: githubrelease.InstallerParameters{Repo: "cli/cli"}}}}
	assert.NotEmpty(t, other.Fingerprint())
}

func Test_readGoEnv_ignoresCheckoutGoWork(t *testing.T) {
	// a go line newer than the local go makes the go command switch toolchains, which fails with the proxy off. Under
	// GOTOOLCHAIN=local it would just report the local go, and these would pass whatever the probe ran in.
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "off")
	want := readGoEnv(nil)
	require.NotEmpty(t, want)

	// the checkout's go.mod would otherwise pick the toolchain the probe reports, here by failing on a go it can't get
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.999\n"), 0o600))
	t.Chdir(dir)
	assert.Equal(t, want, readGoEnv(nil))

	// and so would a go.work named by an inherited GOWORK
	work := filepath.Join(dir, "go.work")
	require.NoError(t, os.WriteFile(work, []byte("go 1.999\n"), 0o600))
	t.Setenv("GOWORK", work)
	assert.Equal(t, want, readGoEnv(nil))
}

func Test_compositeTool_Fingerprint_probesWithToolEnv(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-trimpath")
	goEnvs.Clear()
	t.Cleanup(goEnvs.Clear)

	// the build runs with the tool's env on top, so that is what the probe has to see
	tl := compositeTool{config: Config{Name: "binny", InstallerConfig: DetailConfig{
		Method:     goinstall.InstallMethod,
		Parameters: goinstall.InstallerParameters{Module: "github.com/anchore/binny", Env: []string{"GOFLAGS=-mod=mod"}},
	}}}
	var fp struct{ GoEnv string }
	require.NoError(t, json.Unmarshal([]byte(tl.Fingerprint()), &fp))
	assert.Contains(t, fp.GoEnv, "-mod=mod")
	assert.NotContains(t, fp.GoEnv, "-trimpath")
}

func Test_cacheFingerprint_goEnvRekeys(t *testing.T) {
	cache, err := binny.NewCache(t.TempDir())
	require.NoError(t, err)
	tl := compositeTool{config: Config{Name: "binny", InstallerConfig: DetailConfig{Method: goinstall.InstallMethod, Parameters: goinstall.InstallerParameters{Module: "github.com/anchore/binny"}}}}

	fakeGoEnv(t, func([]string) string { return `{"GOVERSION": "go1.25.0"}` })
	before, skipped := cacheFingerprint(cache, tl, "v0.7.0")
	require.Empty(t, skipped)

	fakeGoEnv(t, func([]string) string { return `{"GOVERSION": "go1.26.0"}` })
	after, skipped := cacheFingerprint(cache, tl, "v0.7.0")
	require.Empty(t, skipped)
	assert.NotEqual(t, before, after, "a toolchain upgrade builds new bytes, so it needs a new entry")
}

// Test_fingerprint pins the fingerprint and cache key of one config per install method. A change here means every
// user's cache entries for that method are re-keyed and every tool is fetched again, so it should only happen on
// purpose (with cacheKeyVersion bumped when what the key covers changes). Renaming a parameter's json tag changes these.
func Test_fingerprint(t *testing.T) {
	fakeGoEnv(t, func([]string) string { return `{"GOVERSION":"go1.26.0"}` })
	tests := []struct {
		name    string
		cfg     DetailConfig
		want    string
		wantKey string
	}{
		{
			name:    "go-install",
			cfg:     DetailConfig{Method: goinstall.InstallMethod, Parameters: goinstall.InstallerParameters{Module: "github.com/anchore/binny", Entrypoint: "cmd/binny", LDFlags: []string{"-X main.version={{ .Version }}"}}},
			want:    `{"Installer":{"Method":"go-install","Parameters":{"module":"github.com/anchore/binny","entrypoint":"cmd/binny","ldflags":["-X main.version={{ .Version }}"],"args":null,"env":null}},"GoEnv":"{\"GOVERSION\":\"go1.26.0\"}"}`,
			wantKey: "984fd81358915f0ef6576083c49f6fdc",
		},
		{
			name:    "go-build",
			cfg:     DetailConfig{Method: gobuild.InstallMethod, Parameters: gobuild.InstallerParameters{Module: "github.com/anchore/binny", Entrypoint: "cmd/binny", Source: gobuild.SourceModeGoProxy}},
			want:    `{"Installer":{"Method":"go-build","Parameters":{"module":"github.com/anchore/binny","entrypoint":"cmd/binny","source":"go-proxy"}},"GoEnv":"{\"GOVERSION\":\"go1.26.0\"}"}`,
			wantKey: "733e36d09a927c4551c6c0955df98f98",
		},
		{
			name:    "github-release",
			cfg:     DetailConfig{Method: githubrelease.InstallMethod, Parameters: githubrelease.InstallerParameters{Repo: "cli/cli", Binary: "gh", KeepArchiveFiles: true}},
			want:    `{"Installer":{"Method":"github-release","Parameters":{"binary":"gh","repo":"cli/cli","assets":null,"keep-archive-files":true}}}`,
			wantKey: "edba6e64e0b8226eeae469916f128537",
		},
		{
			name:    "hosted-shell",
			cfg:     DetailConfig{Method: hostedshell.InstallMethod, Parameters: hostedshell.InstallerParameters{URL: "https://example.com/install.sh", Args: "-b {{ .Destination }}"}},
			want:    `{"Installer":{"Method":"hosted-shell","Parameters":{"url":"https://example.com/install.sh","args":"-b {{ .Destination }}"}}}`,
			wantKey: "86bfd5884add7cc9a25473efc380cba4",
		},
	}
	fingerprint := func(cfg DetailConfig) string {
		return compositeTool{config: Config{Name: "tool", InstallerConfig: cfg}}.Fingerprint()
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fingerprint(tt.cfg)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantKey, cacheKey("linux", "amd64", got, false))
		})
	}

	t.Run("platform and layout are part of the key", func(t *testing.T) {
		fp := fingerprint(tests[0].cfg)
		key := cacheKey("linux", "amd64", fp, false)
		assert.Len(t, key, 32)
		assert.NotEqual(t, key, cacheKey("darwin", "amd64", fp, false))
		assert.NotEqual(t, key, cacheKey("linux", "arm64", fp, false))
		assert.NotEqual(t, key, cacheKey("linux", "amd64", fp, true))
	})

	t.Run("list order and duplicates are part of it", func(t *testing.T) {
		args := func(a ...string) DetailConfig {
			return DetailConfig{Method: gobuild.InstallMethod, Parameters: gobuild.InstallerParameters{Module: "github.com/anchore/binny", Args: a}}
		}
		// go build keeps the last -tags, so these build different binaries
		assert.NotEqual(t, fingerprint(args("-tags", "a", "-tags", "b")), fingerprint(args("-tags", "b", "-tags", "a")))
		assert.NotEqual(t, fingerprint(args("-trimpath", "-trimpath")), fingerprint(args("-trimpath")))
	})
}
