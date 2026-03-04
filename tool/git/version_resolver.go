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

func (v VersionResolver) UpdateVersion(ctx context.Context, want, constraint string) (string, error) {
	if want == "current" {
		// always use the same reference
		return want, nil
	}
	return v.ResolveVersion(ctx, want, constraint)
}

func (v VersionResolver) ResolveVersion(ctx context.Context, want, _ string) (string, error) {
	log.FromContext(ctx).WithFields("path", v.config.Path, "version", want).Trace("resolving version from git")

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

func headCommit(repoPath string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("unable to open repo: %w", err)
	}
	ref, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("unable to fetch head for %q: %w", repoPath, err)
	}
	return ref.Hash().String(), nil
}

func byReference(repoPath, ref string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("unable to open repo: %w", err)
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
	commit, err := r.CommitObject(plumbing.NewHash(ref))
	if err != nil {
		if !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return "", fmt.Errorf("unable to fetch hash for %q: %w", ref, err)
		}
	}

	if commit != nil {
		return commit.Hash.String(), nil
	}

	return "", nil
}
