package gobuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"

	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/binny/tool/goproxy"
)

const InstallMethod = "go-build"

// IsInstallMethod returns true if the given method string refers to the go-build installer.
func IsInstallMethod(method string) bool {
	switch strings.ToLower(method) {
	case "go-build", "gobuild", "go build":
		return true
	}
	return false
}

// DefaultVersionResolverConfig returns the default version resolver configuration
// for the given install parameters. For local modules, it uses the git resolver.
// For GitHub modules, it uses the github-release resolver.
// For other modules, it falls back to the goproxy resolver.
func DefaultVersionResolverConfig(installParams any) (string, any, error) {
	params, ok := installParams.(InstallerParameters)
	if !ok {
		return "", nil, fmt.Errorf("invalid go-build parameters")
	}

	// for local modules, use git resolver (same as go-install)
	if IsLocalModule(params.Module) {
		// find the git repository root - the module path might be a subdirectory
		// (e.g., ./cmd in a multi-module repo where .git is at the root)
		gitRoot, err := findGitRoot(params.Module)
		if err != nil {
			// fall back to the module path if we can't find the git root
			gitRoot = params.Module
		}
		return git.ResolveMethod, git.VersionResolutionParameters{
			Path: gitRoot,
		}, nil
	}

	// for GitHub modules, use github-release resolver (more reliable for release tags)
	if githubRepo := DeriveGitHubRepo(params.Module); githubRepo != "" {
		return githubrelease.ResolveMethod, githubrelease.VersionResolutionParameters{
			Repo: githubRepo,
		}, nil
	}

	// for other modules, fall back to goproxy
	return goproxy.ResolveMethod, goproxy.VersionResolutionParameters{
		Module: params.Module,
	}, nil
}

// findGitRoot finds the git repository root by using go-git's DetectDotGit option
// to search upwards from the given path. This is needed for multi-module repos
// where the module path (e.g., ./cmd) is a subdirectory of the actual git repository.
func findGitRoot(startPath string) (string, error) {
	// convert to absolute path for go-git
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return "", err
	}

	// use go-git to find the repository, searching upwards from the start path
	repo, err := gogit.PlainOpenWithOptions(absPath, &gogit.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return "", fmt.Errorf("no git repository found from %s: %w", startPath, err)
	}

	// get the worktree to find the root path
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("unable to get worktree: %w", err)
	}

	gitRoot := worktree.Filesystem.Root()

	// return relative path if the input was relative
	if !filepath.IsAbs(startPath) {
		cwd, err := os.Getwd()
		if err == nil {
			if rel, err := filepath.Rel(cwd, gitRoot); err == nil {
				return rel, nil
			}
		}
	}

	return gitRoot, nil
}
