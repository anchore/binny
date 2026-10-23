package goinstall

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/gobuild"
)

// a local go-install is handed to go-build field by field, so every go-install parameter needs a go-build
// counterpart of the same type. This pins only that parity: forgetting a field in the copy at InstallTo is
// not caught here (today's five fields are checked end to end by TestInstaller_InstallTo_localModuleRealBuild).
func TestLocalModule_handoffKeepsEveryParameter(t *testing.T) {
	gi := reflect.TypeOf(InstallerParameters{})
	gb := reflect.TypeOf(gobuild.InstallerParameters{})
	for i := 0; i < gi.NumField(); i++ {
		f := gi.Field(i)
		got, ok := gb.FieldByName(f.Name)
		require.Truef(t, ok, "go-build has no %s field, so a local go-install drops it", f.Name)
		require.Equalf(t, f.Type, got.Type, "%s has a different type in go-build", f.Name)
	}
}

// the default resolver path for a local module should point at the repo root wherever the checkout lives,
// including under a symlink (e.g. /tmp on macOS), where it is not the literal "../..": compare locations
// (Abs, then EvalSymlinks, the way the resolver itself reads it), and check that it resolves HEAD.
func TestLocalModule_defaultResolverFindsRepoRootThroughSymlink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	real, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo := filepath.Join(real, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "tool", "thing"), 0o755))
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		require.NoError(t, exec.Command("git", args...).Run())
	}
	head, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(repo, link))
	t.Chdir(filepath.Join(link, "tool", "thing"))

	_, params, err := DefaultVersionResolverConfig(InstallerParameters{Module: "."})
	require.NoError(t, err)
	cfg := params.(git.VersionResolutionParameters)
	abs, err := filepath.Abs(cfg.Path)
	require.NoError(t, err)
	got, err := filepath.EvalSymlinks(abs)
	require.NoError(t, err)
	require.Equal(t, repo, got)

	commit, err := git.NewVersionResolver(cfg).ResolveVersion(context.Background(), binny.VersionIntent{Want: "current"})
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(head)), commit)
}

// gitRepo makes dir a git repo with one commit (holding whatever files are already there) and returns HEAD.
func gitRepo(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "add", "-A"},
		{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		require.NoError(t, exec.Command("git", args...).Run(), "git %v", args)
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(head))
}

// the README's sibling-checkout example: run from one repo with module: ../other, and the default resolver
// gives the other checkout's HEAD, not the one binny runs from. A module below a repo root resolves to that
// repo's HEAD.
func TestLocalModule_defaultResolverUsesTheModulesCheckout(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	here, sibling := filepath.Join(root, "here"), filepath.Join(root, "sibling")
	require.NoError(t, os.MkdirAll(filepath.Join(sibling, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "sub", "f"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(here, 0o755))
	hereHead, siblingHead := gitRepo(t, here), gitRepo(t, sibling)
	require.NotEqual(t, hereHead, siblingHead)
	t.Chdir(here)

	for module, want := range map[string]string{"../sibling": siblingHead, "../sibling/sub": siblingHead, ".": hereHead} {
		t.Run(module, func(t *testing.T) {
			method, params, err := DefaultVersionResolverConfig(InstallerParameters{Module: module})
			require.NoError(t, err)
			require.Equal(t, git.ResolveMethod, method)
			got, err := git.NewVersionResolver(params.(git.VersionResolutionParameters)).
				ResolveVersion(context.Background(), binny.VersionIntent{Want: "current"})
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

// README: a local module's installed version is its resolved version (HEAD for want: current), not the state
// of the working tree, so uncommitted edits don't change it.
func TestLocalModule_currentIgnoresUncommittedEdits(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	tracked := filepath.Join(repo, "main.go")
	require.NoError(t, os.WriteFile(tracked, []byte("package main\n"), 0o644))
	head := gitRepo(t, repo)
	require.NoError(t, os.WriteFile(tracked, []byte("package main\n\n// edited\n"), 0o644))
	t.Chdir(repo)

	_, params, err := DefaultVersionResolverConfig(InstallerParameters{Module: "."})
	require.NoError(t, err)
	got, err := git.NewVersionResolver(params.(git.VersionResolutionParameters)).
		ResolveVersion(context.Background(), binny.VersionIntent{Want: "current"})
	require.NoError(t, err)
	require.Equal(t, head, got)
}

// localModule writes a buildable module at dir with the given go directive and a cmd/hello main.
func localModule(t *testing.T, dir, goVersion string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "hello"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/"+filepath.Base(dir)+"\n\ngo "+goVersion+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "hello", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
}

// README: go runs from the module directory, so that's where it finds a go.work; a parent go.work that doesn't
// list the module breaks the build, and GOWORK=off in the tool's env fixes it.
func TestLocalModule_parentWorkspaceNeedsGOWORKOff(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	t.Setenv("GOWORK", "") // let go look for a workspace, as it would for a user
	t.Setenv("GOFLAGS", "")
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	localModule(t, filepath.Join(root, "mod"), "1.21")
	localModule(t, filepath.Join(root, "other"), "1.21")
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.21\n\nuse ./other\n"), 0o644))
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	params := InstallerParameters{Module: filepath.Join(root, "mod"), Entrypoint: "cmd/hello"}
	_, err = NewInstaller(params).InstallTo(context.Background(), "v1.0.0", t.TempDir())
	require.ErrorContains(t, err, "workspace")

	params.Env = []string{"GOWORK=off"}
	_, err = NewInstaller(params).InstallTo(context.Background(), "v1.0.0", t.TempDir())
	require.NoError(t, err)
}

// README: the go / toolchain directives are read from the module directory, not where binny runs.
func TestLocalModule_goDirectiveReadFromModuleDir(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	localModule(t, filepath.Join(root, "mod"), "1.999")
	localModule(t, filepath.Join(root, "here"), "1.21") // binny runs from a module that asks for nothing new
	t.Chdir(filepath.Join(root, "here"))

	_, err = NewInstaller(InstallerParameters{
		Module:     "../mod",
		Entrypoint: "cmd/hello",
		Env:        []string{"GOTOOLCHAIN=local"}, // stay offline: fail rather than download go1.999
	}).InstallTo(context.Background(), "v1.0.0", t.TempDir())
	require.ErrorContains(t, err, "requires go >= 1.999")
}

// outside any git repo (e.g. a source tarball) the default resolver falls back to the module path itself,
// which is why the "../.." expectation in TestDefaultVersionResolverConfig only holds inside a git checkout.
func TestLocalModule_defaultResolverOutsideARepoFallsBackToModule(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	if exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Run() == nil {
		t.Skip("temp dir is inside a git repo")
	}
	t.Chdir(dir)
	_, params, err := DefaultVersionResolverConfig(InstallerParameters{Module: "."})
	require.NoError(t, err)
	require.Equal(t, ".", params.(git.VersionResolutionParameters).Path)
}
