package goproxy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/mod/module"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

const latest = "latest"

var _ binny.VersionResolver = (*VersionResolver)(nil)

type VersionResolver struct {
	config                   VersionResolutionParameters
	availableVersionsFetcher func(ctx context.Context, mod string) ([]string, error)
	versionInfoFetcher       func(ctx context.Context, mod, version string) (*versionInfo, error)
}

type VersionResolutionParameters struct {
	Module                 string `json:"module" yaml:"module" mapstructure:"module"`
	AllowUnresolvedVersion bool   `json:"allow-unresolved-version" yaml:"allow-unresolved-version" mapstructure:"allow-unresolved-version"`

	// Env is added to the go command's environment when resolving, so lookups see the same GOPROXY/GOPRIVATE/etc
	// as the install does. It is filled in from the installer's env, not from version config.
	Env []string `json:"-" yaml:"-" mapstructure:"-"`
}

// versionInfo is a module version and when it was published.
type versionInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

func NewVersionResolver(cfg VersionResolutionParameters) *VersionResolver {
	return &VersionResolver{
		config: cfg,
		availableVersionsFetcher: func(ctx context.Context, mod string) ([]string, error) {
			return listVersions(ctx, cfg.Env, mod)
		},
		versionInfoFetcher: func(ctx context.Context, mod, version string) (*versionInfo, error) {
			return queryVersion(ctx, cfg.Env, mod, version)
		},
	}
}

func (v VersionResolver) ResolveVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	log.FromContext(ctx).WithFields("module", v.config.Module, "version", intent.Want).Trace("resolving version with the go command")
	if internal.IsSemver(intent.Want) {
		return intent.Want, nil
	}

	if v.config.AllowUnresolvedVersion {
		// pass through as-is, `go install` accepts both "latest" and git refs directly
		return intent.Want, nil
	}

	if intent.Want == latest {
		return v.latestVersion(ctx, "", intent.Cooldown)
	}

	return v.resolveRef(ctx, intent)
}

// resolveRef asks the go command what a git ref (branch, tag, or commit sha) currently points to. The answer is the
// canonical version, which is a pseudo-version for anything that isn't a tagged release. Note that the result is a
// module version, not a git ref, so consumers that talk to git need to map it back (see module.PseudoVersionRev).
func (v VersionResolver) resolveRef(ctx context.Context, intent binny.VersionIntent) (string, error) {
	lgr := log.FromContext(ctx)
	ref := intent.Want

	if err := internal.CheckRefConstraint(ref, intent.Constraint); err != nil {
		return "", err
	}

	info, err := v.versionInfoFetcher(ctx, v.config.Module, ref)
	if err != nil {
		return "", fmt.Errorf("unable to resolve ref %q for module %q: %w (set 'allow-unresolved-version: true' to pass it to go install as-is)", ref, v.config.Module, err)
	}

	// the answer (from a proxy or VCS host) ends up in go/git command lines and templated ldflags, so it must be a
	// valid module version
	if err := module.Check(v.config.Module, info.Version); err != nil {
		return "", fmt.Errorf("got an invalid version for ref %q of module %q: %w", ref, v.config.Module, err)
	}

	// note: a ref has no older candidate to fall back to, so cooldown only warns. Enforce via CooldownError if
	// that turns out to be wanted.
	if intent.Cooldown > 0 && info.Time.After(time.Now().Add(-intent.Cooldown)) {
		lgr.WithFields("module", v.config.Module, "ref", ref, "version", info.Version, "published", info.Time, "cooldown", intent.Cooldown).
			Warn("ref points to a commit newer than the configured cooldown (cooldown is not enforced for git refs)")
	}

	lgr.WithFields("module", v.config.Module, "ref", ref, "version", info.Version).Trace("resolved ref")
	return info.Version, nil
}

func (v VersionResolver) UpdateVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if intent.Want == latest {
		if intent.Constraint != "" {
			return "", fmt.Errorf("cannot specify a version constraint with 'latest' go module version")
		}
		return intent.Want, nil
	}

	if internal.IsSemver(intent.Want) {
		return v.latestVersion(ctx, intent.Constraint, intent.Cooldown)
	}

	if err := internal.CheckRefConstraint(intent.Want, intent.Constraint); err != nil {
		return "", err
	}

	// a git ref is kept as the intent (like "latest"). Where it points to now is picked up by ResolveVersion
	// during install and check.
	return intent.Want, nil
}

// latestVersion returns the newest version allowed by the constraint and cooldown. Without either it is whatever
// `go install mod@latest` would install: a release over a prerelease, retracted versions skipped, and a pseudo-version
// for a module with no tags. The version list (and picking from it) is only needed to apply a constraint or cooldown.
func (v VersionResolver) latestVersion(ctx context.Context, versionConstraint string, cooldown time.Duration) (string, error) {
	if versionConstraint != "" || cooldown > 0 {
		return v.findLatestVersion(ctx, versionConstraint, cooldown)
	}

	info, err := v.versionInfoFetcher(ctx, v.config.Module, latest)
	if err != nil {
		return "", fmt.Errorf("could not resolve latest version for module %q: %w", v.config.Module, err)
	}
	if err := module.Check(v.config.Module, info.Version); err != nil {
		return "", fmt.Errorf("got an invalid latest version for module %q: %w", v.config.Module, err)
	}

	log.FromContext(ctx).WithFields(latest, info.Version, "module", v.config.Module).Trace("found latest version")
	return info.Version, nil
}

func (v VersionResolver) findLatestVersion(ctx context.Context, versionConstraint string, cooldown time.Duration) (string, error) {
	lgr := log.FromContext(ctx)

	versions, err := v.availableVersionsFetcher(ctx, v.config.Module)
	if err != nil {
		return "", fmt.Errorf("failed to get available versions for module %q: %w", v.config.Module, err)
	}

	// when cooldown is active, we need each candidate's publish date, which the go command reports per version
	if cooldown > 0 {
		return v.findLatestVersionWithCooldown(ctx, versions, versionConstraint, cooldown)
	}

	latestVersion, err := internal.FilterToLatestVersion(versions, versionConstraint)
	if err != nil {
		return "", fmt.Errorf("failed to filter latest version: %v", err)
	}

	if latestVersion != "" {
		lgr.WithFields(latest, latestVersion, "module", v.config.Module).
			Trace("found latest version with the go command")
	} else {
		lgr.WithFields("module", v.config.Module).Trace("could not resolve latest version with the go command")
	}

	if latestVersion == "" {
		if v.config.AllowUnresolvedVersion {
			// this can happen if the source repo has no tags, so the go command finds no versions.
			lgr.WithFields("module", v.config.Module).Trace("using 'latest' as the version")
			return latest, nil
		}
		return "", fmt.Errorf("could not resolve latest version for module %q", v.config.Module)
	}

	return latestVersion, nil
}

// findLatestVersionWithCooldown checks version candidates in descending order, fetching their publish
// dates from the go command (go list -m). It returns the first version that passes the cooldown check.
// To avoid a go command call per version, it only checks the top candidates (up to maxCooldownCandidates).
const maxCooldownCandidates = 10

type versionCandidate struct {
	original string
	parsed   *semver.Version
}

func (v VersionResolver) findLatestVersionWithCooldown(ctx context.Context, versions []string, versionConstraint string, cooldown time.Duration) (string, error) {
	lgr := log.FromContext(ctx)
	cutoff := time.Now().Add(-cooldown)

	candidates, err := parseAndSortCandidates(versions, versionConstraint)
	if err != nil {
		return "", err
	}

	result := v.checkCandidatesForCooldown(ctx, candidates, cutoff)

	if result.foundVersion != "" {
		lgr.WithFields(latest, result.foundVersion, "module", v.config.Module, "published", result.foundDate).
			Trace("found version that passes cooldown")
		return result.foundVersion, nil
	}

	if result.checkedCount < len(candidates) {
		lgr.WithFields("checked", result.checkedCount, "total", len(candidates), "module", v.config.Module).
			Warn("cooldown candidate limit reached, older versions were not checked")
	}

	return "", result.buildCooldownError(cooldown, len(candidates))
}

// parseAndSortCandidates parses version strings, filters by constraint, and returns them sorted
// in descending order (newest first).
func parseAndSortCandidates(versions []string, versionConstraint string) ([]versionCandidate, error) {
	var constraint *semver.Constraints
	if versionConstraint != "" {
		var err error
		constraint, err = semver.NewConstraint(versionConstraint)
		if err != nil {
			return nil, fmt.Errorf("unable to parse version constraint %q: %v", versionConstraint, err)
		}
	}

	var candidates []versionCandidate
	for _, vs := range versions {
		vs = strings.TrimSpace(vs)
		if vs == "" {
			continue
		}
		ver, err := semver.NewVersion(vs)
		if err != nil {
			continue
		}
		if constraint != nil && !constraint.Check(ver) {
			continue
		}
		candidates = append(candidates, versionCandidate{original: vs, parsed: ver})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].parsed.GreaterThan(candidates[j].parsed)
	})

	return candidates, nil
}

// cooldownCheckResult holds the outcome of checking candidates against a cooldown period.
type cooldownCheckResult struct {
	foundVersion   string
	foundDate      time.Time
	absoluteLatest string
	latestDate     *time.Time
	checkedCount   int
}

func (r cooldownCheckResult) buildCooldownError(cooldown time.Duration, totalCandidates int) *binny.CooldownError {
	err := &binny.CooldownError{
		Cooldown:      cooldown,
		LatestVersion: r.absoluteLatest,
		LatestDate:    r.latestDate,
	}
	if r.checkedCount < totalCandidates {
		err.CheckedCount = r.checkedCount
		err.TotalCount = totalCandidates
	}
	return err
}

// checkCandidatesForCooldown iterates through version candidates and checks their publish dates
// against the cooldown cutoff time. Returns the first version that passes the cooldown check.
func (v VersionResolver) checkCandidatesForCooldown(ctx context.Context, candidates []versionCandidate, cutoff time.Time) cooldownCheckResult {
	lgr := log.FromContext(ctx)

	result := cooldownCheckResult{}
	if len(candidates) > 0 {
		result.absoluteLatest = candidates[0].original
	}

	limit := min(maxCooldownCandidates, len(candidates))
	result.checkedCount = limit

	for i := 0; i < limit; i++ {
		c := candidates[i]
		info, err := v.versionInfoFetcher(ctx, v.config.Module, c.original)
		if err != nil {
			lgr.WithFields("version", c.original, "module", v.config.Module).
				Tracef("failed to fetch version info for cooldown check: %v", err)
			continue
		}

		if i == 0 {
			result.latestDate = &info.Time
		}

		if info.Time.Before(cutoff) || info.Time.Equal(cutoff) {
			result.foundVersion = c.original
			result.foundDate = info.Time
			return result
		}

		lgr.WithFields("version", c.original, "published", info.Time, "cutoff", cutoff).
			Trace("version too new for cooldown, checking older versions")
	}

	return result
}
