package gobuild

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool/goproxy"
)

var (
	fullSHAPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// lsRemoteTimeout bounds a single ref lookup, so a stalled host or an ssh prompt can't hang resolution.
const lsRemoteTimeout = 2 * time.Minute

// WithRefResolution makes a go-build tool resolve git refs (branches, PR heads, tags that aren't versions, see
// refCandidates) to what they point at right now, so the tool is rebuilt when the ref moves. With git source the repo
// itself is asked (git ls-remote) and the answer is a commit sha. With go-proxy source the go command is asked and the
// answer is a pseudo-version, the same as for go-install. Semver versions, "latest", and prefixed version tags
// (bun-v1.4.3) still go to the given resolver. With allowUnresolved a ref that can't be resolved is passed through
// as-is. A local module, or an unknown source mode, gets the resolver back unchanged.
func WithRefResolution(params InstallerParameters, resolver binny.VersionResolver, allowUnresolved bool) binny.VersionResolver {
	if IsLocalModule(params.Module) {
		return resolver
	}
	r := refResolver{
		VersionResolver: resolver,
		module:          params.Module,
		repoURL:         params.RepoURL,
		allowUnresolved: allowUnresolved,
		lsRemote:        lsRemote,
	}
	switch normalizeSourceMode(params.Source) {
	case "", SourceModeGit:
	case SourceModeGoProxy:
		r.proxy = goproxy.NewVersionResolver(goproxy.VersionResolutionParameters{
			Module:                 params.Module,
			AllowUnresolvedVersion: allowUnresolved,
			Env:                    internal.StaticEnv(params.Env),
		})
	default:
		return resolver
	}
	return r
}

type refResolver struct {
	binny.VersionResolver
	module          string
	repoURL         string
	allowUnresolved bool
	// proxy resolves refs through the go command (go-proxy source), when nil refs are resolved with git
	proxy    binny.VersionResolver
	lsRemote func(ctx context.Context, repoURL, ref string) (gitRef, error)
}

// gitRef is a ref found in a remote repo, name is the full ref name (e.g. refs/heads/main). Both are empty when the
// repo has no such ref.
type gitRef struct {
	name string
	sha  string
}

func (r refResolver) ResolveVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if !internal.IsGitRef(intent.Want) {
		return r.VersionResolver.ResolveVersion(ctx, intent)
	}
	if r.proxy == nil {
		return r.resolveFromGit(ctx, intent)
	}
	if r.isVersionTag(ctx, intent.Want) {
		return r.VersionResolver.ResolveVersion(ctx, intent)
	}
	return r.proxy.ResolveVersion(ctx, intent)
}

func (r refResolver) resolveFromGit(ctx context.Context, intent binny.VersionIntent) (string, error) {
	want := intent.Want
	if fullSHAPattern.MatchString(want) {
		// a full commit can't move. It isn't looked up, so a branch or tag named after it can't redirect the build
		return want, nil
	}

	ref, repo, err := r.lookup(ctx, want)
	if versionTag(want, ref, err) {
		return r.VersionResolver.ResolveVersion(ctx, intent)
	}

	lgr := log.FromContext(ctx)
	if err != nil {
		if r.allowUnresolved {
			lgr.WithFields("module", r.module, "ref", want, "error", err).Warn("unable to resolve git ref, using it as-is")
			return want, nil
		}
		return "", err
	}
	if err := internal.CheckRefConstraint(want, intent.Constraint); err != nil {
		return "", err
	}
	if intent.Cooldown > 0 {
		// unlike go-proxy, ls-remote doesn't say when the commit was made, so there is nothing to compare against
		lgr.WithFields("module", r.module, "ref", want).Warn("cooldown is configured but not enforced for git refs (ignoring)")
	}

	if ref.sha == "" {
		switch {
		case shaPattern.MatchString(want):
			// not a branch or tag, so a (short) commit sha, which can't move
			return want, nil
		case r.allowUnresolved:
			lgr.WithFields("module", r.module, "ref", want).Warn("git ref not found, using it as-is")
			return want, nil
		}
		return "", fmt.Errorf("git ref %q not found in %q", want, repo)
	}

	lgr.WithFields("module", r.module, "ref", want, "commit", ref.sha).Trace("resolved git ref")
	return ref.sha, nil
}

func (r refResolver) UpdateVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if !internal.IsGitRef(intent.Want) || r.isVersionTag(ctx, intent.Want) {
		return r.VersionResolver.UpdateVersion(ctx, intent)
	}
	if err := internal.CheckRefConstraint(intent.Want, intent.Constraint); err != nil {
		return "", err
	}
	// the ref is kept as the intent (like "latest"), where it points now is picked up by ResolveVersion
	return intent.Want, nil
}

// isVersionTag is versionTag, only asking the repo when want is shaped like a prefixed version.
func (r refResolver) isVersionTag(ctx context.Context, want string) bool {
	if _, prefixed := internal.SemverTagPrefix(want); !prefixed {
		return false
	}
	ref, _, err := r.lookup(ctx, want)
	return versionTag(want, ref, err)
}

// versionTag reports whether want is a tag shaped like a prefixed version (bun-v1.4.3, cli/v2.0.0), which the wrapped
// resolver handles like any other version (constraints, update bumps). A branch of that shape (release-1.30) is
// tracked as a ref. When the repo can't say (lookup failed or found nothing) the shape alone decides.
func versionTag(want string, ref gitRef, lookupErr error) bool {
	if _, prefixed := internal.SemverTagPrefix(want); !prefixed {
		return false
	}
	return lookupErr != nil || ref.name == "" || strings.HasPrefix(ref.name, "refs/tags/")
}

// lookup finds want in the repo, also returning the repo URL (credentials redacted) for messages.
func (r refResolver) lookup(ctx context.Context, want string) (gitRef, string, error) {
	repoURL := r.repoURL
	if repoURL == "" {
		var err error
		if repoURL, err = DeriveRepoURL(r.module); err != nil {
			return gitRef{}, "", fmt.Errorf("failed to derive repo URL: %w", err)
		}
	}
	repo := redactCredentials(repoURL, repoURL)

	ref, err := r.lsRemote(ctx, repoURL, want)
	if err != nil {
		return gitRef{}, repo, fmt.Errorf("unable to resolve git ref %q from %q: %w", want, repo, err)
	}
	return ref, repo, nil
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

// lsRemote returns the ref a want names in the remote repo (see refCandidates) and the commit it points to, or an empty
// gitRef if there is no such ref.
func lsRemote(ctx context.Context, repoURL, ref string) (gitRef, error) {
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()

	order := refCandidates(ref)

	args := append([]string{"ls-remote", "--", repoURL}, order...)
	log.Trace("running: git " + redactCredentials(strings.Join(args, " "), repoURL))

	cmd := gitCommand(ctx, "", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return gitRef{}, fmt.Errorf("git ls-remote failed: %w: %q", err, redactCredentials(strings.TrimSpace(stderr.String()), repoURL))
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
			return gitRef{name: name, sha: sha}, nil
		}
	}
	return gitRef{}, nil
}
