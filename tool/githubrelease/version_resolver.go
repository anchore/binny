package githubrelease

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	internalhttp "github.com/anchore/binny/internal/http"
	"github.com/anchore/binny/internal/log"
)

var _ binny.VersionResolver = (*VersionResolver)(nil)

type VersionResolver struct {
	config               VersionResolutionParameters
	latestReleaseFetcher func(ctx context.Context, user, repo string) (*ghRelease, error)
	releasesFetcher      func(ctx context.Context, user, repo string) ([]ghRelease, error)
}

type VersionResolutionParameters struct {
	Repo string `json:"repo" yaml:"repo" mapstructure:"repo"`
}

func NewVersionResolver(cfg VersionResolutionParameters) *VersionResolver {
	return &VersionResolver{
		config:               cfg,
		latestReleaseFetcher: fetchLatestReleaseFromGithubFacade,
		releasesFetcher:      fetchAllReleasesFromGithubV4API,
	}
}

func (v VersionResolver) UpdateVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	if intent.Want == "latest" {
		return intent.Want, nil
	}

	if internal.IsSemver(intent.Want) {
		return v.findLatestVersion(ctx, intent.Constraint, intent.Cooldown)
	}

	return intent.Want, nil
}

func (v VersionResolver) ResolveVersion(ctx context.Context, intent binny.VersionIntent) (string, error) {
	log.FromContext(ctx).WithFields("repo", v.config.Repo, "version", intent.Want).Trace("resolving version from github release")

	if internal.IsSemver(intent.Want) {
		return intent.Want, nil
	}

	if intent.Want == "latest" {
		return v.findLatestVersion(ctx, intent.Constraint, intent.Cooldown)
	}

	return intent.Want, nil
}

func (v VersionResolver) findLatestVersion(ctx context.Context, versionConstraint string, cooldown time.Duration) (string, error) {
	lgr := log.FromContext(ctx)
	cfg := v.config
	fields := strings.Split(cfg.Repo, "/")
	if len(fields) != 2 {
		return "", fmt.Errorf("invalid github repo format: %q", cfg.Repo)
	}
	user, repo := fields[0], fields[1]

	var cutoff *time.Time
	if cooldown > 0 {
		t := time.Now().Add(-cooldown)
		cutoff = &t
	}

	// when cooldown is active, skip the cheap facade path since it doesn't return publish dates
	// (we need dates to enforce the cooldown). Fall through to the full API path instead.
	if cutoff == nil {
		latestRelease, err := v.latestReleaseFetcher(ctx, user, repo)
		if err != nil {
			return "", fmt.Errorf("unable to fetch latest release: %v", err)
		}

		// try the cheapest path forward first -- if this is compliant to the constraint, use it.
		if latestRelease != nil {
			latestVersion, err := filterToLatestVersion([]ghRelease{*latestRelease}, versionConstraint, nil)
			if err != nil {
				return "", fmt.Errorf("unable to filter to latest version: %v", err)
			}
			if latestVersion != nil {
				return latestVersion.Tag, nil
			}
		}
	} else {
		lgr.WithFields("repo", cfg.Repo, "cooldown", cooldown.String()).
			Trace("skipping facade path for cooldown enforcement (requires release dates from API)")
	}

	// this path requires the most work, but is typically needed if there is a constraint or cooldown
	releases, err := v.releasesFetcher(ctx, user, repo)
	if err != nil {
		return "", fmt.Errorf("unable to fetch all releases: %v", err)
	}

	latestVersion, err := filterToLatestVersion(releases, versionConstraint, cutoff)
	if err != nil {
		return "", fmt.Errorf("unable to filter to latest version: %v", err)
	}
	if latestVersion == nil {
		if cutoff != nil {
			// find the absolute latest (without cooldown) to produce a helpful error message
			absoluteLatest, _ := filterToLatestVersion(releases, versionConstraint, nil)
			var latestTag string
			var latestDate *time.Time
			if absoluteLatest != nil {
				latestTag = absoluteLatest.Tag
				latestDate = absoluteLatest.Date
			}
			return "", &binny.CooldownError{
				Cooldown:      cooldown,
				LatestVersion: latestTag,
				LatestDate:    latestDate,
			}
		}
		return "", fmt.Errorf("no latest version found")
	}

	lgr.WithFields("latest", latestVersion.Tag, "repo", cfg.Repo).
		Trace("found latest version from the github release")

	return latestVersion.Tag, nil
}

// filterToLatestVersion finds the latest release that satisfies the version constraint and cooldown cutoff.
// If cutoff is non-nil, releases published after the cutoff time are skipped (too new).
//
//nolint:gocognit
func filterToLatestVersion(releases []ghRelease, versionConstraint string, cutoff *time.Time) (*ghRelease, error) {
	var constraint *semver.Constraints
	var err error

	if versionConstraint != "" {
		constraint, err = semver.NewConstraint(versionConstraint)
		if err != nil {
			return nil, fmt.Errorf("unable to parse version constraint %q: %v", versionConstraint, err)
		}
	}

	var latest *ghRelease
	for i := range releases {
		ty := releases[i]
		if ty.IsDraft != nil && *ty.IsDraft {
			continue
		}

		// cooldown check: skip releases that are too new
		if cutoff != nil {
			if ty.Date == nil || ty.Date.After(*cutoff) {
				continue
			}
		}

		ver, err := semver.NewVersion(ty.Tag)
		if err != nil {
			log.WithFields("tag", ty.Tag).Warn("unable to parse version as semver")
			ver = nil
		}

		if ty.IsLatest != nil && *ty.IsLatest {
			if constraint != nil && ver != nil {
				if constraint.Check(ver) {
					latest = &ty
					break
				}
			} else {
				latest = &ty
				break
			}
		}

		if latest != nil {
			latestVer, err := semver.NewVersion(latest.Tag)
			if err != nil {
				log.WithFields("tag", latest.Tag).Warn("unable to parse current latest version as semver")
				// can't compare semver, so skip this candidate entirely since we already have a latest
				continue
			}

			if ver != nil {
				if ver.LessThan(latestVer) || ver.Equal(latestVer) {
					continue
				}
			}
		}

		if constraint != nil && ver != nil {
			if constraint.Check(ver) {
				latest = &ty
			}
		} else {
			latest = &ty
		}
	}

	return latest, nil
}

func fetchLatestReleaseFromGithubFacade(ctx context.Context, user, repo string) (*ghRelease, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/latest", user, repo)
	resp, err := downloadJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	type ghResponse struct {
		TagName string `json:"tag_name"`
	}

	var ghResp ghResponse
	if err := json.Unmarshal(content, &ghResp); err != nil {
		return nil, fmt.Errorf("unable to unmarshal response from %q: %w", url, err)
	}

	if ghResp.TagName == "" {
		return nil, nil
	}

	return &ghRelease{
		Tag: ghResp.TagName,
	}, nil
}

func downloadJSON(ctx context.Context, url string) (*http.Response, error) {
	lgr := log.FromContext(ctx)
	client := internalhttp.ClientFromContext(ctx)

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	lgr.WithFields("http-status", resp.StatusCode).Tracef("http get [application/json] %q", url)

	return resp, nil
}

// newRetryableGitHubClient creates an HTTP client with OAuth2 authentication and retry logic.
func newRetryableGitHubClient(ctx context.Context, token string) *http.Client {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	oauth2Client := oauth2.NewClient(ctx, src)

	// get base client from context and use oauth2 transport
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient.Transport = oauth2Client.Transport
	retryClient.Logger = nil

	// keep retries short-lived: the default 1->30s backoff over 5 attempts could waste
	// 30+ seconds on a request that's never going to succeed (e.g. transport-level errors
	// during a GitHub secondary rate-limit window). Capping at 3 retries with a 4s ceiling
	// bounds the worst case to roughly 8s before surfacing the failure to the user.
	retryClient.RetryMax = 3
	retryClient.RetryWaitMax = 4 * time.Second
	retryClient.CheckRetry = githubRetryPolicy

	return retryClient.StandardClient()
}

// githubRetryPolicy wraps the default policy and pins "never retry 403" explicitly.
// GitHub returns 403 for both auth failures and secondary rate limits; neither resolves
// by retrying. The current default policy already declines 403 (it only retries 429 and
// 5xx), so this is forward-compat insurance: an upstream change can't reintroduce the
// wasted backoff window.
func githubRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if resp != nil && resp.StatusCode == http.StatusForbidden {
		return false, nil
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

// graphql node budget per request. Larger pages bunch up against GitHub's secondary
// rate limit (point-cost / node-limit) for high-volume repos. Multiple smaller pages
// stay safely under the per-query threshold.
const releasesPerPage = 25

// soft ceiling on releases fetched. Matches the original (un-paginated) behavior of
// `first:100`, but spread across cheaper pages. Plenty to find the latest version
// satisfying a constraint or cooldown for any reasonable repo. Note: this cap is
// approximate — the loop checks after appending a full page, so the realized cap is
// up to maxReleasesFetched + releasesPerPage - 1. Also note: a constraint that only
// matches releases beyond this cap will silently fail to find a match (caller will
// see "no latest version found"). Same trade-off as the original `first:100`.
const maxReleasesFetched = 100

func fetchAllReleasesFromGithubV4API(ctx context.Context, user, repo string) ([]ghRelease, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN environment variable not set but is required to use the GitHub v4 API")
	}

	client := githubv4.NewClient(newRetryableGitHubClient(ctx, token))

	// release assets are intentionally omitted from this query — they're not used for
	// version resolution and pulling them inflates the GraphQL node count by ~100x per
	// release, which trips GitHub's secondary rate limit (403) for high-volume repos.
	// The installer fetches assets separately for the chosen release.
	var query struct {
		Repository struct {
			Releases struct {
				PageInfo struct {
					EndCursor   githubv4.String
					HasNextPage bool
				}
				Nodes []struct {
					TagName     githubv4.String
					IsLatest    githubv4.Boolean
					IsDraft     githubv4.Boolean
					PublishedAt githubv4.DateTime
				}
			} `graphql:"releases(first:$releasesPerPage, after:$releasesCursor)"` // newest first
		} `graphql:"repository(owner:$repositoryOwner, name:$repositoryName)"`
	}
	variables := map[string]any{
		"repositoryOwner": githubv4.String(user),
		"repositoryName":  githubv4.String(repo),
		"releasesPerPage": githubv4.Int(releasesPerPage),
		"releasesCursor":  (*githubv4.String)(nil), // null = first page
	}

	var allReleases []ghRelease
	for {
		if err := client.Query(ctx, &query, variables); err != nil {
			return nil, err
		}

		for _, node := range query.Repository.Releases.Nodes {
			publishedAt := node.PublishedAt.Time
			allReleases = append(allReleases, ghRelease{
				Tag:      string(node.TagName),
				IsLatest: boolRef(bool(node.IsLatest)),
				IsDraft:  boolRef(bool(node.IsDraft)),
				Date:     &publishedAt,
			})
		}

		if !query.Repository.Releases.PageInfo.HasNextPage {
			break
		}
		if len(allReleases) >= maxReleasesFetched {
			break
		}
		variables["releasesCursor"] = githubv4.NewString(query.Repository.Releases.PageInfo.EndCursor)
	}

	sort.Slice(allReleases, func(i, j int) bool {
		// sort from latest to earliest
		if allReleases[i].Date == nil && allReleases[j].Date == nil {
			return false
		}

		if allReleases[i].Date == nil {
			return false
		}

		if allReleases[j].Date == nil {
			return true
		}

		return allReleases[i].Date.After(*allReleases[j].Date)
	})

	return allReleases, nil
}

func boolRef(b bool) *bool {
	return &b
}

// func printJSON(v interface{}) {
//	b, err := json.MarshalIndent(v, "", "  ")
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Println(string(b))
// }
