package goproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

// fileProxy writes a GOPROXY=file:// layout for example.com/tool and returns the env that points the go command at
// it (and only it), so these tests run the real go command without touching the network or the user's module cache.
func fileProxy(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "example.com", "tool", "@v")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	files := map[string]string{
		"list":        "v1.0.0\nv1.1.0\nv1.2.0-rc.1\n",
		"v1.0.0.info": `{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`,
		"v1.1.0.info": `{"Version":"v1.1.0","Time":"2026-02-01T00:00:00Z"}`,
		// a prerelease newer than the latest release, which @latest skips
		"v1.2.0-rc.1.info": `{"Version":"v1.2.0-rc.1","Time":"2026-03-01T00:00:00Z"}`,
		// a branch: the proxy answers the ref query with a pseudo-version
		"main.info": `{"Version":"v1.1.1-0.20261015120000-abcdef123456","Time":"2026-10-15T12:00:00Z"}`,
	}
	for _, v := range []string{"v1.0.0", "v1.1.0", "v1.2.0-rc.1", "v1.1.1-0.20261015120000-abcdef123456"} {
		files[v+".mod"] = "module example.com/tool\n"
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	// file URLs need a leading slash before a windows drive letter (file:///C:/...)
	proxyPath := filepath.ToSlash(root)
	if !strings.HasPrefix(proxyPath, "/") {
		proxyPath = "/" + proxyPath
	}

	return []string{
		"GOPROXY=file://" + proxyPath,
		"GONOPROXY=",
		"GOPRIVATE=",
		"GOSUMDB=off",
		"GOFLAGS=-modcacherw",
		"GOMODCACHE=" + t.TempDir(),
		"GOTOOLCHAIN=local",
	}
}

func TestVersionResolver_goList(t *testing.T) {
	ctx := context.Background()
	v := NewVersionResolver(VersionResolutionParameters{Module: "example.com/tool", Env: fileProxy(t)})

	got, err := v.ResolveVersion(ctx, binny.VersionIntent{Want: "main"})
	require.NoError(t, err)
	assert.Equal(t, "v1.1.1-0.20261015120000-abcdef123456", got)

	// what `go install @latest` would pick: the newest release, not the newer prerelease
	got, err = v.ResolveVersion(ctx, binny.VersionIntent{Want: "latest"})
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", got)

	// cooldown looks up each candidate's publish time through the go command too
	got, err = v.UpdateVersion(ctx, binny.VersionIntent{Want: "v1.0.0", Cooldown: time.Since(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))})
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", got)

	// the go command's reason is passed along
	_, err = v.ResolveVersion(ctx, binny.VersionIntent{Want: "nope"})
	require.ErrorContains(t, err, `"nope"`)
	require.ErrorContains(t, err, "nope.info")
}

func TestVersionResolver_goList_honorsGOPROXYOff(t *testing.T) {
	// the tool's env wins over the process env, and GOPROXY=off means no lookup at all (no fallback to a default proxy)
	v := NewVersionResolver(VersionResolutionParameters{Module: "example.com/tool", Env: append(fileProxy(t), "GOPROXY=off")})
	_, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.ErrorContains(t, err, "GOPROXY=off")
}

func Test_goList_rejectsBadModulePath(t *testing.T) {
	_, err := goList(context.Background(), nil, "-x@main")
	require.ErrorContains(t, err, "invalid module path")
}

func Test_quoteOutput(t *testing.T) {
	got := quoteOutput([]byte("not found: \x1b[2J unknown revision\n"))
	assert.NotContains(t, got, "\x1b")
	assert.Contains(t, got, "unknown revision")

	long := make([]byte, maxErrorSize*2)
	for i := range long {
		long[i] = 'A'
	}
	assert.Less(t, len(quoteOutput(long)), maxErrorSize+10)
}

func TestVersionResolver_goList_toolEnvOverridesProcessEnv(t *testing.T) {
	// a process-level GOPROXY=off would block every lookup, the tool's own GOPROXY (fileProxy) should win
	t.Setenv("GOPROXY", "off")
	v := NewVersionResolver(VersionResolutionParameters{Module: "example.com/tool", Env: fileProxy(t)})
	got, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.NoError(t, err)
	assert.Equal(t, "v1.1.1-0.20261015120000-abcdef123456", got)
}

func TestVersionResolver_goList_ignoresCurrentModuleAndWorkspace(t *testing.T) {
	// lookups run outside any module or workspace: a go.mod needing a go we don't have in the current
	// directory, or a GOWORK that doesn't exist, must not affect them
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/here\n\ngo 9.99\n"), 0o600))
	t.Chdir(dir)
	t.Setenv("GOWORK", filepath.Join(dir, "does-not-exist.work"))

	v := NewVersionResolver(VersionResolutionParameters{Module: "example.com/tool", Env: fileProxy(t)})
	got, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.NoError(t, err)
	assert.Equal(t, "v1.1.1-0.20261015120000-abcdef123456", got)
}

func TestVersionResolver_goList_skipsRetractedLatest(t *testing.T) {
	// go list -m honours retractions: v1.1.0 retracts itself, so latest is v1.0.0 (the old proxy list lookup didn't)
	env := fileProxy(t)
	root := strings.TrimPrefix(strings.TrimPrefix(env[0], "GOPROXY=file://"), "/")
	if !filepath.IsAbs(root) {
		root = "/" + root
	}
	mod := filepath.Join(filepath.FromSlash(root), "example.com", "tool", "@v", "v1.1.0.mod")
	require.NoError(t, os.WriteFile(mod, []byte("module example.com/tool\n\nretract v1.1.0\n"), 0o600))

	v := NewVersionResolver(VersionResolutionParameters{Module: "example.com/tool", Env: env})
	got, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: "latest"})
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", got)
}
