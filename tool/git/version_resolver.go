package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal/log"
)

var _ binny.VersionResolver = (*VersionResolver)(nil)

type VersionResolver struct {
	config VersionResolutionParameters
}

type VersionResolutionParameters struct {
	Path string `json:"path" yaml:"path" mapstructure:"path"`
}

func NewVersionResolver(cfg VersionResolutionParameters) *VersionResolver {
	return &VersionResolver{
		config: cfg,
	}
}

func (v VersionResolver) UpdateVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if intent.Want == "current" {
		// always use the same reference
		return intent.Want, nil
	}
	return v.ResolveVersion(ctx, intent)
}

func (v VersionResolver) ResolveVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	want := intent.Want
	log.FromContext(ctx).WithFields("path", v.config.Path, "version", want).Trace("resolving version from git")

	if intent.Cooldown > 0 {
		log.FromContext(ctx).WithFields("path", v.config.Path).
			Warn("cooldown is configured but not supported by the git version resolver (ignoring)")
	}

	if want == "current" {
		commit, err := headCommit(v.config.Path)
		if err != nil {
			return "", fmt.Errorf("unable to get current commit: %w", err)
		}
		return commit, nil
	}

	ref, err := byReference(v.config.Path, want)
	if err != nil {
		return "", err
	}

	if ref != "" {
		// found it!
		return ref, nil
	}

	// assume is a branch
	return want, nil
}

// openRepo opens the repo that the given path belongs to. DetectDotGit walks up so a module
// subdirectory (e.g. ./cmd/tool) finds the root, and the common dir is wired up by hand for linked
// worktrees, where .git is a file pointing at <main>/.git/worktrees/<name> and refs (HEAD, tags)
// live in the common dir.
//
// note: go-git has an EnableDotGitCommonDir option that does the same wiring, but it leaks the open
// commondir file handle (dotGitCommonDirectory in v5.19.2 never closes it), and windows will not let
// anything unlink a file that is still open. drop this in favor of the option once that is fixed.
func openRepo(repoPath string) (*git.Repository, error) {
	r, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to open repo: %w", err)
	}

	storer, ok := r.Storer.(*filesystem.Storage)
	if !ok {
		return r, nil
	}

	dot := storer.Filesystem()

	common, err := commonDir(dot)
	if err != nil {
		return nil, err
	}

	if common == nil {
		// not a linked worktree, everything already lives in the dir we opened
		return r, nil
	}

	wt, err := r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("unable to get worktree: %w", err)
	}

	s := filesystem.NewStorage(dotgit.NewRepositoryFilesystem(dot, common), cache.NewObjectLRUDefault())

	r, err = git.Open(s, wt.Filesystem)
	if err != nil {
		return nil, fmt.Errorf("unable to open repo against common dir: %w", err)
	}

	return r, nil
}

// commonDir resolves the commondir file of a linked worktree admin dir, returning nil when the
// given dir is not one (i.e. a plain .git directory).
func commonDir(dot billy.Filesystem) (billy.Filesystem, error) {
	f, err := dot.Open("commondir")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read commondir: %w", err)
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("unable to read commondir: %w", err)
	}

	path := strings.TrimSpace(string(b))
	if path == "" {
		return nil, nil
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(dot.Root(), path)
	}

	common := osfs.New(path)
	if _, err := common.Stat(""); err != nil {
		return nil, fmt.Errorf("commondir %q is not readable: %w", path, err)
	}

	return common, nil
}

func headCommit(repoPath string) (string, error) {
	r, err := openRepo(repoPath)
	if err != nil {
		return "", err
	}
	ref, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("unable to fetch head for %q: %w", repoPath, err)
	}
	return ref.Hash().String(), nil
}

func byReference(repoPath, ref string) (string, error) {
	r, err := openRepo(repoPath)
	if err != nil {
		return "", err
	}

	// try by tag first...
	plumbRef, err := r.Tag(ref)
	if err != nil {
		if !errors.Is(err, git.ErrTagNotFound) {
			return "", fmt.Errorf("unable to fetch tag for %q: %w", ref, err)
		}
	}

	if plumbRef != nil {
		return plumbRef.Name().String(), nil
	}

	// then by hash...
	// note: a non-hash ref yields the zero hash here, which is simply not found (allowing the
	// branch fallback below to take over)
	commit, err := r.CommitObject(plumbing.NewHash(ref))
	if err != nil {
		if !errors.Is(err, plumbing.ErrObjectNotFound) {
			return "", fmt.Errorf("unable to fetch hash for %q: %w", ref, err)
		}
	}

	if commit != nil {
		return commit.Hash.String(), nil
	}

	return "", nil
}
