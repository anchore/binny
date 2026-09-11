package git

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

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

// openRepo opens the repo that the given path belongs to. Both options are needed for paths that
// are not a plain repo root: DetectDotGit walks up so a module subdirectory (e.g. ./cmd/tool)
// finds the root, and EnableDotGitCommonDir handles a linked worktree, where .git is a file
// pointing at <main>/.git/worktrees/<name> and refs (HEAD, tags) live in the common dir.
func openRepo(repoPath string) (*git.Repository, error) {
	r, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to open repo: %w", err)
	}
	return r, nil
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
