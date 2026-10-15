package gobuild

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

var (
	fullSHAPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// WithGitRefResolution makes a go-build tool that clones from git resolve git refs (branches, tags that aren't
// semver, PR heads, see refCandidates) against the repo itself, so the commit that gets built is the one the repo says the ref points at rather
// than what a module proxy reports. Semver versions and "latest" still go to the given resolver. For any other
// source mode, or a local module, the resolver is returned as-is.
func WithGitRefResolution(params InstallerParameters, resolver binny.VersionResolver) binny.VersionResolver {
	if (params.Source != "" && normalizeSourceMode(params.Source) != SourceModeGit) || IsLocalModule(params.Module) {
		return resolver
	}
	return gitRefResolver{
		VersionResolver: resolver,
		module:          params.Module,
		repoURL:         params.RepoURL,
		lsRemote:        lsRemote,
	}
}

type gitRefResolver struct {
	binny.VersionResolver
	module   string
	repoURL  string
	lsRemote func(ctx context.Context, repoURL, ref string) (string, error)
}

// isGitRef reports whether want should be resolved against the repo. Prefixed semver tags (bun-v1.4.3, cli/v2.0.0)
// are versions too, so the wrapped resolver keeps handling constraints and update bumps for them.
func isGitRef(want string) bool {
	if want == "" || want == "latest" || internal.IsSemver(want) {
		return false
	}
	_, prefixed := internal.SemverTagPrefix(want)
	return !prefixed
}

func (r gitRefResolver) ResolveVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if !isGitRef(intent.Want) {
		return r.VersionResolver.ResolveVersion(ctx, intent)
	}
	if intent.Constraint != "" {
		return "", fmt.Errorf("cannot specify a version constraint with git ref %q", intent.Want)
	}

	lgr := log.FromContext(ctx)
	if intent.Cooldown > 0 {
		lgr.WithFields("module", r.module, "ref", intent.Want).
			Warn("cooldown is configured but not enforced for git refs (ignoring)")
	}

	repoURL := r.repoURL
	if repoURL == "" {
		var err error
		if repoURL, err = DeriveRepoURL(r.module); err != nil {
			return "", fmt.Errorf("failed to derive repo URL: %w", err)
		}
	}

	sha, err := r.lsRemote(ctx, repoURL, intent.Want)
	if err != nil {
		return "", fmt.Errorf("unable to resolve git ref %q from %q: %w", intent.Want, repoURL, err)
	}
	if sha == "" {
		if shaPattern.MatchString(intent.Want) {
			// not a branch or tag, so a commit sha, which can't move
			return intent.Want, nil
		}
		return "", fmt.Errorf("git ref %q not found in %q", intent.Want, repoURL)
	}

	lgr.WithFields("module", r.module, "ref", intent.Want, "commit", sha).Trace("resolved git ref")
	return sha, nil
}

func (r gitRefResolver) UpdateVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if !isGitRef(intent.Want) {
		return r.VersionResolver.UpdateVersion(ctx, intent)
	}
	if intent.Constraint != "" {
		return "", fmt.Errorf("cannot specify a version constraint with git ref %q", intent.Want)
	}
	// the ref is kept as the intent (like "latest"), where it points now is picked up by ResolveVersion
	return intent.Want, nil
}

// refCandidates lists the full ref names a want may mean, most preferred first. Tags win over branches (the order git
// itself uses for an ambiguous name), then anything else under refs/, so PR and MR heads work: "pull/123/head",
// "refs/pull/123/head", or the "pull/123" / "merge-requests/45" shorthand for their head ref. "^{}" is the commit an
// annotated tag points at (the tag ref itself names the tag object).
func refCandidates(ref string) []string {
	if strings.HasPrefix(ref, "refs/") {
		return []string{ref + "^{}", ref}
	}
	return []string{
		"refs/tags/" + ref + "^{}",
		"refs/tags/" + ref,
		"refs/heads/" + ref,
		"refs/" + ref,
		"refs/" + ref + "/head",
	}
}

// lsRemote returns the commit a ref points to in the remote repo (see refCandidates), or "" if there is no such ref.
func lsRemote(ctx context.Context, repoURL, ref string) (string, error) {
	order := refCandidates(ref)

	args := append([]string{"ls-remote", "--", repoURL}, order...)
	log.Trace("running: git " + strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "git", args...)
	// fail rather than hang on a credential prompt
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed: %w: %q", err, strings.TrimSpace(stderr.String()))
	}

	found := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && fullSHAPattern.MatchString(sha) {
			found[name] = sha
		}
	}
	for _, name := range order {
		if sha, ok := found[name]; ok {
			return sha, nil
		}
	}
	return "", nil
}
