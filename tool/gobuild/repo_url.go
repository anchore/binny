package gobuild

import (
	"fmt"
	"strings"
)

// DeriveRepoURL derives the git repository URL from a Go module path.
// For GitHub modules, it constructs the HTTPS URL.
// For other hosts, it returns an error asking for an explicit repo-url.
func DeriveRepoURL(module string) (string, error) {
	if strings.HasPrefix(module, "github.com/") {
		return deriveGitHubURL(module)
	}

	// for other hosts, require explicit repo-url
	return "", fmt.Errorf("cannot derive repo URL from module %q, use --repo-url to specify it explicitly", module)
}

// deriveGitHubURL constructs the git URL for a GitHub module.
// It handles module paths like:
// - github.com/owner/repo
// - github.com/owner/repo/v2
// - github.com/owner/repo/cmd/tool
// - github.com/owner/repo/v3/cmd/tool
func deriveGitHubURL(module string) (string, error) {
	// strip the github.com/ prefix
	remainder := strings.TrimPrefix(module, "github.com/")

	// split into path segments
	parts := strings.Split(remainder, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid GitHub module path %q: need at least owner/repo", module)
	}

	owner := parts[0]
	repo := parts[1]

	// handle /vN suffix on repo (e.g., github.com/owner/repo/v2)
	// the repo name itself won't have /vN, it's a Go module versioning convention
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo), nil
}

// DeriveGitHubRepo extracts the owner/repo portion from a GitHub module path.
// Returns empty string for non-GitHub modules.
func DeriveGitHubRepo(module string) string {
	if !strings.HasPrefix(module, "github.com/") {
		return ""
	}

	// strip the github.com/ prefix
	remainder := strings.TrimPrefix(module, "github.com/")

	// split into path segments
	parts := strings.Split(remainder, "/")
	if len(parts) < 2 {
		return ""
	}

	return parts[0] + "/" + parts[1]
}
