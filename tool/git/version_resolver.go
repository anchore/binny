package git

import (
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

func (v VersionResolver) UpdateVersion(want, constraint string) (string, error) {
	return v.ResolveVersion(want, constraint)
}

func (v VersionResolver) ResolveVersion(want, _ string) (string, error) {
	log.WithFields("path", v.config.Path, "version", want).Trace("resolving version from git")

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
		return "", fmt.Errorf("unable fetch head: %w", err)
	}
	return ref.Hash().String(), nil
}

func byReference(repoPath, ref string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("unable to open repo: %w", err)
	}

	plumbRef, err := r.Tag(ref)
	if err != nil {
		return "", fmt.Errorf("unable fetch tag: %w", err)
	}

	if plumbRef != nil {
		return plumbRef.Name().String(), nil
	}

	plumbRef, err = r.Reference(plumbing.ReferenceName(ref), true)
	if err != nil {
		return "", fmt.Errorf("unable fetch reference: %w", err)
	}

	if plumbRef != nil {
		return plumbRef.Hash().String(), nil
	}

	return "", nil
}
