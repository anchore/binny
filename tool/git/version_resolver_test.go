package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

// a linked worktree has a .git file pointing at <main>/.git/worktrees/<name>, which holds HEAD but
// no refs; those live in the common dir. version resolution must work from either location.
func TestResolveVersion_worktrees(t *testing.T) {
	main, linked, head := repoWithLinkedWorktree(t)

	for _, root := range []string{main, linked} {
		name := "main-checkout"
		if root == linked {
			name = "linked-worktree"
		}

		// a module path may point below the repo root (e.g. go-install with module: ./cmd/tool),
		// so resolution must walk up to find the repo
		for _, path := range []string{root, filepath.Join(root, "cmd", "tool")} {
			name := name
			if path != root {
				name += "/from-subdirectory"
			}
			t.Run(name, func(t *testing.T) {
				resolver := NewVersionResolver(VersionResolutionParameters{Path: path})

				for _, tt := range []struct {
					name string
					want string
					res  string
				}{
					{name: "current resolves to HEAD", want: "current", res: head},
					{name: "tag", want: "v1.0.0", res: "refs/tags/v1.0.0"},
					{name: "commit hash", want: head, res: head},
					{name: "unknown is assumed to be a branch", want: "some-branch", res: "some-branch"},
				} {
					t.Run(tt.name, func(t *testing.T) {
						got, err := resolver.ResolveVersion(context.Background(), binny.VersionIntent{Want: tt.want})
						require.NoError(t, err)
						require.Equal(t, tt.res, got)
					})
				}
			})
		}
	}
}

// repoWithLinkedWorktree returns the main checkout, a linked worktree, and the commit both point at.
func repoWithLinkedWorktree(t *testing.T) (mainPath, linkedPath, head string) {
	t.Helper()

	// note: t.TempDir() cleanup is strict, which is the point -- windows cannot unlink a file that
	// is still open, so a leaked handle in the repo open path fails this test there
	root := t.TempDir()

	mainPath = filepath.Join(root, "main")
	linkedPath = filepath.Join(root, "linked")

	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}

	require.NoError(t, exec.Command("git", "init", mainPath).Run())
	run(mainPath, "commit", "--allow-empty", "-m", "initial")
	run(mainPath, "tag", "-a", "-m", "release", "v1.0.0")
	run(mainPath, "worktree", "add", "-b", "feature", linkedPath)

	// git does not track empty dirs, so make the module subdir in both checkouts
	for _, p := range []string{mainPath, linkedPath} {
		require.NoError(t, os.MkdirAll(filepath.Join(p, "cmd", "tool"), 0700))
	}

	head = run(mainPath, "rev-parse", "HEAD")
	require.Len(t, head, 40)

	return mainPath, linkedPath, head
}
