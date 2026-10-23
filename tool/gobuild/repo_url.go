package gobuild

import (
	"fmt"
	"net/url"
	"regexp"
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

// redactCredentials removes the credentials in repoURL (e.g. https://TOKEN@host/repo or https://user:pass@host/repo)
// from s, so they can't leak through logs or errors, including git output that echoes the URL back.
func redactCredentials(s, repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil {
		// the credentials can't be told apart from the rest of the URL, so drop anything shaped like userinfo
		return userinfoPattern.ReplaceAllString(s, "$1")
	}
	if u.User == nil {
		return s
	}
	userinfo := u.User.String()
	// the secret is the password, or the username when there is none (https://TOKEN@host)
	secret, hasPassword := u.User.Password()
	if !hasPassword {
		secret = u.User.Username()
	}
	u.User = nil
	redacted := u.String()

	s = strings.ReplaceAll(s, repoURL, redacted)
	s = strings.ReplaceAll(s, userinfo+"@", "")
	// an account name that is also part of the rest of the URL (https://myorg@dev.azure.com/myorg/...) isn't a secret
	if len(secret) >= minBareSecret && (hasPassword || !strings.Contains(redacted, secret)) {
		// also where it appears on its own, e.g. decoded in git's output
		s = strings.ReplaceAll(s, secret, "***")
	}
	return s
}

// userinfoPattern matches the userinfo of a URL in text: "https://TOKEN@host" -> "https://host".
var userinfoPattern = regexp.MustCompile(`(://)[^/@\s]+@`)

// minBareSecret is the shortest secret replaced wherever it appears in text; a shorter one (a one-letter password)
// would mangle unrelated words, and is only removed as part of the URL. Short real passwords still get replaced, at
// the cost of the odd garbled word ("pass" turns "password" into "***word").
const minBareSecret = 4
