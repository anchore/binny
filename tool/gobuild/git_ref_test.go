package gobuild

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/go-logger"
	"github.com/anchore/go-logger/adapter/logrus"
)

// gitRepo is a local repo used as the "remote" in these tests.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "tag.gpgsign=false", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) commit(msg string) string {
	r.t.Helper()
	r.git("commit", "-q", "--allow-empty", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// prHead makes a commit reachable only from refs/pull/<n>/head, the way a forge publishes a PR from a fork.
func (r *gitRepo) prHead(n int) string {
	r.t.Helper()
	head := r.git("rev-parse", "HEAD")
	sha := r.git("commit-tree", "-m", "pr", "-p", head, head+"^{tree}")
	r.git("update-ref", fmt.Sprintf("refs/pull/%d/head", n), sha)
	return sha
}

func (r *gitRepo) url() string {
	return "file://" + filepath.ToSlash(r.dir)
}

// delegate is the resolver the git ref resolver wraps, it records what it was asked.
type delegate struct{ asked []string }

func (d *delegate) ResolveVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	d.asked = append(d.asked, intent.Want)
	return "from-delegate", nil
}

func (d *delegate) UpdateVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	d.asked = append(d.asked, intent.Want)
	return "from-delegate", nil
}

func TestRefResolver(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	first := repo.commit("one")
	repo.git("tag", "-a", "release-1", "-m", "annotated")
	repo.git("branch", "feature/x")
	second := repo.commit("two")

	inner := &delegate{}
	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, inner, false)

	resolve := func(want string) (string, error) {
		return r.ResolveVersion(ctx, binny.VersionIntent{Want: want})
	}

	got, err := resolve("main")
	require.NoError(t, err)
	assert.Equal(t, second, got)

	// annotated tags resolve to the commit, not the tag object
	got, err = resolve("release-1")
	require.NoError(t, err)
	assert.Equal(t, first, got)

	got, err = resolve("feature/x")
	require.NoError(t, err)
	assert.Equal(t, first, got)

	// a commit sha isn't a ref and can't move, so it is kept as-is
	got, err = resolve(first[:12])
	require.NoError(t, err)
	assert.Equal(t, first[:12], got)

	// PR / MR heads, which live outside refs/heads, in every spelling
	pr := repo.prHead(7)
	for _, want := range []string{"pull/7", "pull/7/head", "refs/pull/7/head"} {
		got, err = resolve(want)
		require.NoError(t, err, want)
		assert.Equal(t, pr, got, want)
	}

	// a full ref name is taken literally, an annotated tag still peels to its commit
	got, err = resolve("refs/tags/release-1")
	require.NoError(t, err)
	assert.Equal(t, first, got)

	_, err = resolve("no-such-branch")
	require.ErrorContains(t, err, `git ref "no-such-branch" not found`)

	_, err = r.ResolveVersion(ctx, binny.VersionIntent{Want: "main", Constraint: "< 2.0"})
	require.ErrorContains(t, err, "cannot specify a version constraint")

	// the branch moves: the next resolve sees it
	third := repo.commit("three")
	got, err = resolve("main")
	require.NoError(t, err)
	assert.Equal(t, third, got)

	// update keeps the ref as the intent
	got, err = r.UpdateVersion(ctx, binny.VersionIntent{Want: "main"})
	require.NoError(t, err)
	assert.Equal(t, "main", got)

	// semver and latest are the wrapped resolver's job
	assert.Empty(t, inner.asked)
	for _, want := range []string{"v1.2.3", "latest"} {
		got, err = resolve(want)
		require.NoError(t, err)
		assert.Equal(t, "from-delegate", got)
	}
	assert.Equal(t, []string{"v1.2.3", "latest"}, inner.asked)

	// prefixed semver tags are versions, so constraints and update bumps still go to the wrapped resolver
	inner.asked = nil
	got, err = r.ResolveVersion(ctx, binny.VersionIntent{Want: "cli/v2.0.0", Constraint: "< 3.0"})
	require.NoError(t, err)
	assert.Equal(t, "from-delegate", got)
	got, err = r.UpdateVersion(ctx, binny.VersionIntent{Want: "bun-v1.4.3"})
	require.NoError(t, err)
	assert.Equal(t, "from-delegate", got)
	assert.Equal(t, []string{"cli/v2.0.0", "bun-v1.4.3"}, inner.asked)

	// a date-like tag isn't a prefixed version, it stays a ref
	assert.False(t, versionTag("nightly-2024-01-01", gitRef{}, nil))
}

func TestRefResolver_refWithCooldown(t *testing.T) {
	// a cooldown can't be enforced for a ref (there's no older candidate), so a just-made commit resolves anyway
	// rather than failing with a CooldownError
	repo := newGitRepo(t)
	head := repo.commit("just now")

	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, &delegate{}, false)
	got, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main", Cooldown: 24 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, head, got)
}

func TestRefResolver_HEAD(t *testing.T) {
	// a remote's HEAD is its default branch; go-install resolves `want: HEAD` (go list -m mod@HEAD), and go-build
	// used to build it (the full clone fallback ran `git checkout HEAD`), so it should resolve here too
	repo := newGitRepo(t)
	head := repo.commit("one")

	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, &delegate{}, false)
	got, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: "HEAD"})
	require.NoError(t, err, "want: HEAD should resolve to the remote's default branch")
	assert.Equal(t, head, got)
}

func TestRefResolver_fullSHASkipsLsRemote(t *testing.T) {
	// a full commit sha can't move, so there is nothing to ask the remote
	sha := strings.Repeat("ab", 20)
	r := refResolver{
		VersionResolver: &delegate{},
		module:          "example.com/tool",
		repoURL:         "https://example.com/tool.git",
		lsRemote: func(context.Context, string, string) (gitRef, error) {
			t.Errorf("ls-remote was called for a full sha")
			return gitRef{}, nil
		},
	}
	got, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: sha})
	require.NoError(t, err)
	assert.Equal(t, sha, got)
}

func Test_lsRemote_failsInsteadOfPrompting(t *testing.T) {
	// a remote that wants credentials should make lsRemote fail rather than wait on a prompt. Without a terminal git
	// fails either way, but it only says "terminal prompts disabled" when GIT_TERMINAL_PROMPT=0 is what stopped it
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	// keep the user's credential helpers and askpass programs out of it
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")

	_, err := lsRemote(context.Background(), srv.URL+"/o/r.git", "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminal prompts disabled")
}

// captureLogs makes the global logger and the context's logger write to separate files, and returns a context
// carrying the latter and a func that reads what it got.
func captureLogs(t *testing.T) (context.Context, func() string) {
	ctx, ctxLog, _ := captureAllLogs(t)
	return ctx, ctxLog
}

// captureAllLogs is captureLogs that can also read what the global logger got.
func captureAllLogs(t *testing.T) (context.Context, func() string, func() string) {
	t.Helper()
	newLogger := func(path string) logger.Logger {
		l, err := logrus.New(logrus.Config{FileLocation: path, Level: logger.TraceLevel})
		require.NoError(t, err)
		return l
	}
	dir := t.TempDir()
	ctxLog, globalLog := filepath.Join(dir, "ctx.log"), filepath.Join(dir, "global.log")

	prev := log.Get()
	log.Set(newLogger(globalLog))
	t.Cleanup(func() { log.Set(prev) })

	read := func(path string) func() string {
		return func() string {
			got, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				return ""
			}
			require.NoError(t, err)
			return string(got)
		}
	}
	return log.WithLogger(context.Background(), newLogger(ctxLog)), read(ctxLog), read(globalLog)
}

func Test_lsRemote_logsThroughContext(t *testing.T) {
	// lsRemote is given the caller's context, so its trace line should go to the context's logger (which carries
	// the tool=module@version field the installer attaches), not the global one
	ctx, ctxLog := captureLogs(t)
	repo := newGitRepo(t)
	repo.commit("one")

	_, err := lsRemote(ctx, repo.url(), "main")
	require.NoError(t, err)
	assert.Contains(t, ctxLog(), "ls-remote", "expected the git command to be traced on the context's logger")
}

func Test_fetchCommit_logsThroughContext(t *testing.T) {
	// same for fetchCommit, which cloneSource calls with its context
	ctx, ctxLog := captureLogs(t)
	repo := newGitRepo(t)
	sha := repo.commit("one")

	require.NoError(t, fetchCommit(ctx, repo.url(), sha, t.TempDir()))
	assert.Contains(t, ctxLog(), "fetch", "expected the git commands to be traced on the context's logger")
}

func TestCloneSource_logsWhichAttemptFailed(t *testing.T) {
	// a full sha is fetched by commit, not shallow cloned, so when that fails the log should say so
	ctx, ctxLog := captureLogs(t)
	repo := newGitRepo(t)
	repo.commit("one")
	missing := strings.Repeat("0123456789", 4) // a full sha that isn't in the repo

	_, _, err := cloneSource(ctx, "example.com/tool", missing, repo.url())
	require.Error(t, err)
	assert.Contains(t, ctxLog(), "trying full clone", "the fallback should still be logged")
	assert.Contains(t, ctxLog(), "fetch by commit", "the log should name the attempt that failed")
	assert.NotContains(t, ctxLog(), "shallow clone failed", "the attempt that failed was the fetch by commit")
}

func TestCloneSource_doesNotLeakCredentials(t *testing.T) {
	// nothing listens on port 1, so every git call fails fast; whichever path runs (fetch by commit, shallow clone,
	// full clone), the token must not show up in the error or in either logger
	const repoURL = "https://bot:s3cr3t@127.0.0.1:1/o/r.git"
	for _, version := range []string{strings.Repeat("ab", 20), "main"} {
		t.Run(version, func(t *testing.T) {
			ctx, ctxLog, globalLog := captureAllLogs(t)
			_, _, err := cloneSource(ctx, "example.com/tool", version, repoURL)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "s3cr3t")
			assert.Contains(t, ctxLog()+globalLog(), "127.0.0.1:1", "expected the git calls to be logged")
			assert.NotContains(t, ctxLog(), "s3cr3t")
			assert.NotContains(t, globalLog(), "s3cr3t")
		})
	}
}

// filteringRepo is a remote with a real file that allows partial clones (uploadpack.allowFilter), as forges do.
func filteringRepo(t *testing.T) (*gitRepo, string) {
	t.Helper()
	repo := newGitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo.dir, "main.go"), []byte("package main\n"), 0o600))
	repo.git("add", "main.go")
	first := repo.commit("one")
	repo.commit("two")
	repo.git("config", "uploadpack.allowFilter", "true")
	return repo, first
}

func assertPartialCloneOf(t *testing.T, dir, commit string) {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "config", "remote.origin.partialclonefilter").Output()
	require.NoError(t, err, "expected a partial clone")
	assert.Equal(t, "blob:none", strings.TrimSpace(string(out)))
	out, err = exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, commit, strings.TrimSpace(string(out)))
	assert.FileExists(t, filepath.Join(dir, "main.go"))
}

func TestCloneSource_fallbackIsAPartialClone(t *testing.T) {
	// a short sha can't be fetched on its own, so the full clone fallback runs, and only fetches the blobs of the
	// commit it checks out (--filter=blob:none)
	repo, first := filteringRepo(t)

	dir, cleanup, err := cloneSource(context.Background(), "example.com/tool", first[:12], repo.url())
	require.NoError(t, err)
	t.Cleanup(cleanup)
	assertPartialCloneOf(t, dir, first)
}

func TestCloneSource_fallbackOnServerThatRefusesShaFetches(t *testing.T) {
	// the fallback is there for servers that refuse to fetch a commit by sha (source.go's comment says so). A partial
	// clone fetches blobs by id at checkout, which such a server also refuses, so the fallback has to still work
	// there, e.g. by retrying without --filter
	repo, first := filteringRepo(t)
	for _, setting := range []string{"allowReachableSHA1InWant", "allowAnySHA1InWant", "allowTipSHA1InWant"} {
		repo.git("config", "uploadpack."+setting, "false")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.version")
	t.Setenv("GIT_CONFIG_VALUE_0", "0")

	dir, cleanup, err := cloneSource(context.Background(), "example.com/tool", first, repo.url())
	require.NoError(t, err, "the full clone fallback should work against a server that refuses sha fetches")
	t.Cleanup(cleanup)
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, first, strings.TrimSpace(string(out)))
	assert.FileExists(t, filepath.Join(dir, "main.go"))
}

func TestRefResolver_updateRejectsConstraint(t *testing.T) {
	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: "https://example.com/tool.git"}, &delegate{}, false)
	_, err := r.UpdateVersion(context.Background(), binny.VersionIntent{Want: "main", Constraint: "< 2.0"})
	require.ErrorContains(t, err, "cannot specify a version constraint")
}

func TestRefResolver_derivesRepoURL(t *testing.T) {
	// with no repo-url, a github module's repo is derived from the module path
	var asked string
	r := refResolver{
		VersionResolver: &delegate{},
		module:          "github.com/o/r/v2",
		lsRemote: func(_ context.Context, repoURL, _ string) (gitRef, error) {
			asked = repoURL
			return gitRef{name: "refs/heads/main", sha: strings.Repeat("ab", 20)}, nil
		},
	}
	_, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.NoError(t, err)
	want, err := DeriveRepoURL("github.com/o/r/v2")
	require.NoError(t, err)
	assert.Equal(t, want, asked)
	assert.Contains(t, asked, "github.com/o/r")
}

func TestRefResolver_mergeRequestHead(t *testing.T) {
	// GitLab publishes merge requests as refs/merge-requests/<n>/head; the merge-requests/<n> shorthand finds it
	repo := newGitRepo(t)
	base := repo.commit("one")
	mr := repo.git("commit-tree", "-m", "mr", "-p", base, base+"^{tree}")
	repo.git("update-ref", "refs/merge-requests/45/head", mr)

	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, &delegate{}, false)
	for _, want := range []string{"merge-requests/45", "merge-requests/45/head", "refs/merge-requests/45/head"} {
		got, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: want})
		require.NoError(t, err, want)
		assert.Equal(t, mr, got, want)
	}
}

func TestRefResolver_tagBeatsBranch(t *testing.T) {
	// a name that is both a tag and a branch means the tag, the order git itself uses
	repo := newGitRepo(t)
	tagged := repo.commit("one")
	repo.git("tag", "x")
	repo.git("branch", "x")
	repo.git("checkout", "-q", "x")
	branchHead := repo.commit("two")
	require.NotEqual(t, tagged, branchHead)

	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, &delegate{}, false)
	got, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: "x"})
	require.NoError(t, err)
	assert.Equal(t, tagged, got)
}

func TestRefResolver_fullSHAIsNotRedirected(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	pinned := repo.commit("one")
	other := repo.commit("two")
	// a branch and a tag named after the pinned commit, pointing somewhere else
	repo.git("update-ref", "refs/heads/"+pinned, other)
	repo.git("update-ref", "refs/tags/"+pinned, other)

	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, &delegate{}, false)
	got, err := r.ResolveVersion(ctx, binny.VersionIntent{Want: pinned})
	require.NoError(t, err)
	assert.Equal(t, pinned, got)
}

func TestRefResolver_versionShapedNames(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	repo.commit("one")
	repo.git("tag", "release-1.2")
	repo.git("branch", "release-1.30")
	branchHead := repo.commit("two")
	repo.git("branch", "-f", "release-1.30", branchHead)

	inner := &delegate{}
	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, inner, false)

	// a branch that looks like a prefixed version is still tracked
	got, err := r.ResolveVersion(ctx, binny.VersionIntent{Want: "release-1.30"})
	require.NoError(t, err)
	assert.Equal(t, branchHead, got)
	got, err = r.UpdateVersion(ctx, binny.VersionIntent{Want: "release-1.30"})
	require.NoError(t, err)
	assert.Equal(t, "release-1.30", got)
	assert.Empty(t, inner.asked)

	// a tag of that shape is a version, so the wrapped resolver handles it
	got, err = r.ResolveVersion(ctx, binny.VersionIntent{Want: "release-1.2"})
	require.NoError(t, err)
	assert.Equal(t, "from-delegate", got)
	got, err = r.UpdateVersion(ctx, binny.VersionIntent{Want: "release-1.2"})
	require.NoError(t, err)
	assert.Equal(t, "from-delegate", got)
	assert.Equal(t, []string{"release-1.2", "release-1.2"}, inner.asked)
}

func TestRefResolver_allowUnresolved(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	repo.commit("one")

	tests := []struct {
		name    string
		repoURL string
	}{
		{name: "ref not found", repoURL: repo.url()},
		{name: "repo unreachable", repoURL: "file://" + filepath.ToSlash(filepath.Join(t.TempDir(), "missing"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := InstallerParameters{Module: "example.com/tool", RepoURL: tt.repoURL}

			_, err := WithRefResolution(params, &delegate{}, false).ResolveVersion(ctx, binny.VersionIntent{Want: "gone"})
			require.Error(t, err)

			got, err := WithRefResolution(params, &delegate{}, true).ResolveVersion(ctx, binny.VersionIntent{Want: "gone"})
			require.NoError(t, err)
			assert.Equal(t, "gone", got)
		})
	}
}

func TestWithRefResolution_sourceModes(t *testing.T) {
	inner := &delegate{}
	for _, source := range []SourceMode{"", "GIT"} {
		r := WithRefResolution(InstallerParameters{Module: "example.com/tool", Source: source}, inner, false)
		require.IsType(t, refResolver{}, r)
		assert.Nil(t, r.(refResolver).proxy, "git source resolves refs with git")
	}

	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", Source: SourceModeGoProxy}, inner, false)
	require.IsType(t, refResolver{}, r)
	assert.NotNil(t, r.(refResolver).proxy, "go-proxy source resolves refs with the go command")

	assert.Same(t, inner, WithRefResolution(InstallerParameters{Module: "./local"}, inner, false))
}

func TestCloneSource_commits(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	first := repo.commit("one")
	repo.commit("two")
	pr := repo.prHead(9)

	tests := []struct {
		name        string
		version     string
		wantCommit  string
		wantShallow bool
	}{
		{name: "full sha is fetched on its own", version: first, wantShallow: true},
		{name: "short sha falls back to a full clone", version: first[:12]},
		{name: "pseudo-version is mapped to its commit", version: "v0.0.0-20261015120000-" + first[:12]},
		{name: "commit only reachable from a PR ref", version: pr, wantCommit: pr, wantShallow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, cleanup, err := cloneSource(ctx, "example.com/tool", tt.version, repo.url())
			require.NoError(t, err)
			t.Cleanup(cleanup)

			out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
			require.NoError(t, err)
			want := first
			if tt.wantCommit != "" {
				want = tt.wantCommit
			}
			assert.Equal(t, want, strings.TrimSpace(string(out)))

			_, err = os.Stat(filepath.Join(dir, ".git", "shallow"))
			assert.Equal(t, tt.wantShallow, err == nil, "shallow fetch")
		})
	}
}

func TestCloneSource_fetchByCommitRefused(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	first := repo.commit("one")
	repo.commit("two")

	// a server that only serves advertised refs, spoken to over protocol v0 (where that setting is enforced), refuses
	// to fetch a commit that isn't a ref tip, so the fallback full clone has to take over in the dir git init used
	for _, setting := range []string{"allowReachableSHA1InWant", "allowAnySHA1InWant", "allowTipSHA1InWant"} {
		repo.git("config", "uploadpack."+setting, "false")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.version")
	t.Setenv("GIT_CONFIG_VALUE_0", "0")

	dir, cleanup, err := cloneSource(ctx, "example.com/tool", first, repo.url())
	require.NoError(t, err)
	t.Cleanup(cleanup)

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, first, strings.TrimSpace(string(out)))

	_, err = os.Stat(filepath.Join(dir, ".git", "shallow"))
	assert.True(t, os.IsNotExist(err), "expected the full clone fallback, not a shallow fetch")
}

func TestCloneSource_keepsFirstAttemptError(t *testing.T) {
	// when both the fetch by commit and the full clone fallback fail, the error should say why the first attempt
	// failed too, not only the fallback's reason
	repo := newGitRepo(t)
	repo.commit("one")
	missing := strings.Repeat("0123456789", 4) // a full sha that isn't in the repo

	_, _, err := cloneSource(context.Background(), "example.com/tool", missing, repo.url())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git checkout failed", "the fallback's error")
	assert.Contains(t, err.Error(), "git fetch failed", "the fetch by commit's error was dropped")
}

func TestCloneSource_repoURLIsNotAnOption(t *testing.T) {
	// a repo-url that starts with "-" should reach git as a repository, the way lsRemote and fetchCommit already pass
	// it after "--", not be parsed as a clone option
	marker := filepath.Join(t.TempDir(), "ran")
	repoURL := "--upload-pack=touch " + marker

	_, _, err := cloneSource(context.Background(), "example.com/tool", "main", repoURL)
	require.Error(t, err)
	assert.NoFileExists(t, marker)
	assert.Contains(t, err.Error(), "--upload-pack", "git should report repo-url as the repository it couldn't read")
}

func Test_shallowClone_repoURLIsNotAnOption(t *testing.T) {
	// cloneSource only reaches shallowClone for a branch or tag, so check it on its own too
	marker := filepath.Join(t.TempDir(), "ran")
	err := shallowClone(context.Background(), "--upload-pack=touch "+marker, "main", t.TempDir())
	require.Error(t, err)
	assert.NoFileExists(t, marker)
	assert.Contains(t, err.Error(), "--upload-pack", "git should report repo-url as the repository it couldn't read")
}

func Test_fullCloneAndCheckout_repoURLIsNotAnOption(t *testing.T) {
	// the fallback clone on its own: cloneSource's error may already carry the first attempt's output
	marker := filepath.Join(t.TempDir(), "ran")
	err := fullCloneAndCheckout(context.Background(), "--upload-pack=touch "+marker, "main", t.TempDir())
	require.Error(t, err)
	assert.NoFileExists(t, marker)
	assert.Contains(t, err.Error(), "--upload-pack", "git should report repo-url as the repository it couldn't read")
}

func Test_redactCredentials(t *testing.T) {
	tests := []struct {
		name, repoURL, in, want string
	}{
		{name: "token as user", repoURL: "https://TOKEN123@github.com/o/r", in: "clone https://TOKEN123@github.com/o/r failed", want: "clone https://github.com/o/r failed"},
		{name: "user and password", repoURL: "https://bot:s3cr3t@git.corp/o/r", in: "fatal: https://bot:s3cr3t@git.corp/o/r: auth failed for s3cr3t", want: "fatal: https://git.corp/o/r: auth failed for ***"},
		{name: "echoed in another form", repoURL: "https://bot:s3cr3t@git.corp/o/r", in: "remote: bot:s3cr3t@git.corp", want: "remote: git.corp"},
		{name: "no credentials", repoURL: "https://github.com/o/r", in: "clone https://github.com/o/r", want: "clone https://github.com/o/r"},
		{name: "scp-style is left alone", repoURL: "git@github.com:o/r.git", in: "git@github.com:o/r.git", want: "git@github.com:o/r.git"},
		{name: "unparseable URL still has its token removed", repoURL: "https://ghp_TOK@h/o/r%zz", in: "clone https://ghp_TOK@h/o/r%zz failed", want: "clone https://h/o/r%zz failed"},
		{name: "short password doesn't mangle other text", repoURL: "https://bot:x@h/o/r", in: "text with x", want: "text with x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redactCredentials(tt.in, tt.repoURL))
		})
	}
}

func Test_redactCredentials_escapedTokenEchoedDecoded(t *testing.T) {
	// a token-as-username with an escape (%2F) can come back decoded in git's output
	got := redactCredentials("auth failed for tok/x", "https://tok%2Fx@h/o/r")
	assert.NotContains(t, got, "tok/x")
}

func TestRefResolver_doesNotLeakCredentials(t *testing.T) {
	// nothing listens on port 1, so git fails fast and its error (and ours) would carry the URL
	r := WithRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: "https://bot:s3cr3t@127.0.0.1:1/o/r.git"}, &delegate{}, false)
	_, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t")
	assert.Contains(t, err.Error(), "https://127.0.0.1:1/o/r.git")
}

func Test_redactCredentials_usernameAlsoInPath(t *testing.T) {
	// some hosts put the account name in both the userinfo and the path (Azure DevOps clone URLs look like
	// https://myorg@dev.azure.com/myorg/proj/_git/repo); the path is not a secret and should survive redaction
	for _, tt := range []struct{ repoURL, want string }{
		{repoURL: "https://myorg@dev.azure.com/myorg/proj/_git/repo", want: "https://dev.azure.com/myorg/proj/_git/repo"},
		{repoURL: "https://deploy@git.corp/team/deploy-tools", want: "https://git.corp/team/deploy-tools"},
	} {
		assert.Equal(t, "clone "+tt.want+" failed", redactCredentials("clone "+tt.repoURL+" failed", tt.repoURL))
	}
}

func TestCloneSource_badShaIsNotClonedTwice(t *testing.T) {
	// a sha that isn't in the repo fails the full clone's checkout; that is not a reason to clone the repo again.
	// git's trace2 events record each git process, so count the top-level clones
	events := filepath.Join(t.TempDir(), "trace2.json")
	t.Setenv("GIT_TRACE2_EVENT", events)
	repo := newGitRepo(t)
	repo.commit("one")

	_, _, err := cloneSource(context.Background(), "example.com/tool", strings.Repeat("0123456789", 4), repo.url())
	require.Error(t, err)

	trace, err := os.ReadFile(events)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(trace), `"hierarchy":"clone"`), "a missing commit should be cloned only once")
}

func TestRefResolver_fullSHAStillNeedsRepoURL(t *testing.T) {
	// a full sha needs no lookup, but a module whose repo can't be derived (and has no repo-url) still can't be
	// built, so check should say so rather than pass
	r := refResolver{
		VersionResolver: &delegate{},
		module:          "example.com/tool",
		lsRemote: func(context.Context, string, string) (gitRef, error) {
			return gitRef{}, nil
		},
	}
	_, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: strings.Repeat("ab", 20)})
	require.ErrorContains(t, err, "failed to derive repo URL")
}

func Test_lsRemote_repoURLIsNotAnOption(t *testing.T) {
	// lsRemote passes repo-url after "--", so one starting with "-" can't become an ls-remote option
	marker := filepath.Join(t.TempDir(), "ran")
	_, err := lsRemote(context.Background(), "--upload-pack=touch "+marker, "main")
	require.Error(t, err)
	assert.NoFileExists(t, marker)
	assert.Contains(t, err.Error(), "--upload-pack", "git should report repo-url as the repository it couldn't read")
}

func Test_fetchCommit_repoURLIsNotAnOption(t *testing.T) {
	// same for fetchCommit's git fetch
	marker := filepath.Join(t.TempDir(), "ran")
	err := fetchCommit(context.Background(), "--upload-pack=touch "+marker, strings.Repeat("ab", 20), t.TempDir())
	require.Error(t, err)
	assert.NoFileExists(t, marker)
	assert.Contains(t, err.Error(), "--upload-pack", "git should report repo-url as the repository it couldn't read")
}
