package goproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/hashicorp/go-retryablehttp"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	internalhttp "github.com/anchore/binny/internal/http"
	"github.com/anchore/binny/internal/log"
)

const latest = "latest"

var _ binny.VersionResolver = (*VersionResolver)(nil)

type VersionResolver struct {
	config                   VersionResolutionParameters
	availableVersionsFetcher func(ctx context.Context, url string) ([]string, error)
	versionInfoFetcher       func(ctx context.Context, module, version string) (*versionInfo, error)
}

type VersionResolutionParameters struct {
	Module                 string `json:"module" yaml:"module" mapstructure:"module"`
	AllowUnresolvedVersion bool   `json:"allow-unresolved-version" yaml:"allow-unresolved-version" mapstructure:"allow-unresolved-version"`
}

// versionInfo represents the JSON response from the go proxy /@v/{version}.info endpoint.
type versionInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

func NewVersionResolver(cfg VersionResolutionParameters) *VersionResolver {
	return &VersionResolver{
		config:                   cfg,
		availableVersionsFetcher: availableVersionsFetcher,
		versionInfoFetcher:       fetchVersionInfo,
	}
}

func (v VersionResolver) ResolveVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	log.FromContext(ctx).WithFields("module", v.config.Module, "version", intent.Want).Trace("resolving version from go proxy")
	if internal.IsSemver(intent.Want) {
		return intent.Want, nil
	}

	if intent.Want == latest && !v.config.AllowUnresolvedVersion {
		return v.findLatestVersion(ctx, "", intent.Cooldown)
	}

	// TODO: dunno

	return intent.Want, nil
}

func (v VersionResolver) UpdateVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if intent.Want == latest {
		if intent.Constraint != "" {
			return "", fmt.Errorf("cannot specify a version constraint with 'latest' go module version")
		}
		return intent.Want, nil
	}

	if internal.IsSemver(intent.Want) {
		return v.findLatestVersion(ctx, intent.Constraint, intent.Cooldown)
	}

	// TODO: dunno

	return intent.Want, nil
}

func (v VersionResolver) findLatestVersion(ctx context.Context, versionConstraint string, cooldown time.Duration) (string, error) {
	lgr := log.FromContext(ctx)

	// ask the go proxy for the latest version
	url := "https://proxy.golang.org/" + v.config.Module + "/@v/list"
	versions, err := v.availableVersionsFetcher(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to get available versions from go proxy: %v", err)
	}

	// when cooldown is active, we need to check publish dates via the /@v/{version}.info endpoint
	if cooldown > 0 {
		return v.findLatestVersionWithCooldown(ctx, versions, versionConstraint, cooldown)
	}

	latestVersion, err := internal.FilterToLatestVersion(versions, versionConstraint)
	if err != nil {
		return "", fmt.Errorf("failed to filter latest version: %v", err)
	}

	if latestVersion != "" {
		lgr.WithFields(latest, latestVersion, "module", v.config.Module).
			Trace("found latest version from the go proxy")
	} else {
		lgr.WithFields("module", v.config.Module).Trace("could not resolve latest version from go proxy")
	}

	if latestVersion == "" {
		if v.config.AllowUnresolvedVersion {
			// this can happen if the source repo has no tags, the proxy then won't know about it.
			lgr.WithFields("module", v.config.Module).Trace("using 'latest' as the version")
			return latest, nil
		}
		return "", fmt.Errorf("could not resolve latest version for module %q", v.config.Module)
	}

	return latestVersion, nil
}

// findLatestVersionWithCooldown checks version candidates in descending order, fetching their publish
// dates from the go proxy info endpoint. It returns the first version that passes the cooldown check.
// To avoid excessive API calls, it only checks the top candidates (up to maxCooldownCandidates).
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
			Trace("found version from go proxy that passes cooldown")
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

func availableVersionsFetcher(ctx context.Context, url string) ([]string, error) {
	// TODO: honor GOPROXY env vars
	lgr := log.FromContext(ctx)
	client := internalhttp.ClientFromContext(ctx)

	lgr.WithFields("url", url).Trace("requesting latest version")

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get available versions from go proxy: %s", resp.Status)
	}

	// get the last entry in a newline delimited list
	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(contents), "\n")
	return lines, nil
}

// fetchVersionInfo retrieves the publish timestamp for a specific version from the go proxy.
func fetchVersionInfo(ctx context.Context, module, version string) (*versionInfo, error) {
	client := internalhttp.ClientFromContext(ctx)

	url := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.info", module, version)
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get version info from go proxy: %s", resp.Status)
	}

	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info versionInfo
	if err := json.Unmarshal(contents, &info); err != nil {
		return nil, fmt.Errorf("failed to parse version info: %w", err)
	}

	return &info, nil
}
