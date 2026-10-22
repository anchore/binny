package gobuild

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
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

func TestGitRefResolver(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	first := repo.commit("one")
	repo.git("tag", "-a", "release-1", "-m", "annotated")
	repo.git("branch", "feature/x")
	second := repo.commit("two")

	inner := &delegate{}
	r := WithGitRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: repo.url()}, inner)

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
	assert.True(t, isGitRef("nightly-2024-01-01"))
}

func TestWithGitRefResolution_onlyForGitSource(t *testing.T) {
	inner := &delegate{}
	assert.IsType(t, gitRefResolver{}, WithGitRefResolution(InstallerParameters{Module: "example.com/tool"}, inner))
	assert.IsType(t, gitRefResolver{}, WithGitRefResolution(InstallerParameters{Module: "example.com/tool", Source: "GIT"}, inner))
	assert.Same(t, inner, WithGitRefResolution(InstallerParameters{Module: "example.com/tool", Source: SourceModeGoProxy}, inner))
	assert.Same(t, inner, WithGitRefResolution(InstallerParameters{Module: "./local"}, inner))
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

func Test_redactCredentials(t *testing.T) {
	tests := []struct {
		name, repoURL, in, want string
	}{
		{name: "token as user", repoURL: "https://TOKEN123@github.com/o/r", in: "clone https://TOKEN123@github.com/o/r failed", want: "clone https://github.com/o/r failed"},
		{name: "user and password", repoURL: "https://bot:s3cr3t@git.corp/o/r", in: "fatal: https://bot:s3cr3t@git.corp/o/r: auth failed for s3cr3t", want: "fatal: https://git.corp/o/r: auth failed for ***"},
		{name: "echoed in another form", repoURL: "https://bot:s3cr3t@git.corp/o/r", in: "remote: bot:s3cr3t@git.corp", want: "remote: git.corp"},
		{name: "no credentials", repoURL: "https://github.com/o/r", in: "clone https://github.com/o/r", want: "clone https://github.com/o/r"},
		{name: "scp-style is left alone", repoURL: "git@github.com:o/r.git", in: "git@github.com:o/r.git", want: "git@github.com:o/r.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redactCredentials(tt.in, tt.repoURL))
		})
	}
}

func TestGitRefResolver_doesNotLeakCredentials(t *testing.T) {
	// nothing listens on port 1, so git fails fast and its error (and ours) would carry the URL
	r := WithGitRefResolution(InstallerParameters{Module: "example.com/tool", RepoURL: "https://bot:s3cr3t@127.0.0.1:1/o/r.git"}, &delegate{})
	_, err := r.ResolveVersion(context.Background(), binny.VersionIntent{Want: "main"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t")
	assert.Contains(t, err.Error(), "https://127.0.0.1:1/o/r.git")
}
