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
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

var _ binny.VersionResolver = (*VersionResolver)(nil)

type VersionResolver struct {
	config               VersionResolutionParameters
	latestReleaseFetcher func(user, repo string) (*ghRelease, error)
	releasesFetcher      func(user, repo string) ([]ghRelease, error)
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

func (v VersionResolver) UpdateVersion(want, constraint string) (string, error) {
	if want == "latest" {
		return want, nil
	}

	if internal.IsSemver(want) {
		return v.findLatestVersion(constraint)
	}

	return want, nil
}

func (v VersionResolver) ResolveVersion(want, constraint string) (string, error) {
	log.WithFields("repo", v.config.Repo, "version", want).Trace("resolving version from github release")

	if internal.IsSemver(want) {
		return want, nil
	}

	if want == "latest" {
		return v.findLatestVersion(constraint)
	}

	return want, nil
}

func (v VersionResolver) findLatestVersion(versionConstraint string) (string, error) {
	cfg := v.config
	fields := strings.Split(cfg.Repo, "/")
	if len(fields) != 2 {
		return "", fmt.Errorf("invalid github repo format: %q", cfg.Repo)
	}
	user, repo := fields[0], fields[1]

	latestRelease, err := v.latestReleaseFetcher(user, repo)
	if err != nil {
		return "", fmt.Errorf("unable to fetch latest release: %v", err)
	}

	// try the cheapest path forward first -- if this is compliant to the constraint, use it.
	if latestRelease != nil {
		latestVersion, err := filterToLatestVersion([]ghRelease{*latestRelease}, versionConstraint)
		if err != nil {
			return "", fmt.Errorf("unable to filter to latest version: %v", err)
		}
		if latestVersion != nil {
			return latestVersion.Tag, nil
		}
	}

	// this path requires the most work, but is typically needed if there is a constraint
	releases, err := v.releasesFetcher(user, repo)
	if err != nil {
		return "", fmt.Errorf("unable to fetch all releases: %v", err)
	}

	latestVersion, err := filterToLatestVersion(releases, versionConstraint)
	if err != nil {
		return "", fmt.Errorf("unable to filter to latest version: %v", err)
	}
	if latestVersion == nil {
		return "", fmt.Errorf("no latest version found")
	}

	log.WithFields("latest", latestVersion.Tag, "repo", cfg.Repo).
		Trace("found latest version from the github release")

	return latestVersion.Tag, nil
}

//nolint:gocognit
func filterToLatestVersion(releases []ghRelease, versionConstraint string) (*ghRelease, error) {
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
				log.WithFields("tag", ty.Tag).Warn("unable to parse latest version as semver")
				ver = nil
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

func fetchLatestReleaseFromGithubFacade(user, repo string) (*ghRelease, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/latest", user, repo)
	resp, err := downloadJSON(url)
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

func downloadJSON(url string) (*http.Response, error) {
	headers := map[string]string{"Accept": "application/json"}

	client := &http.Client{
		Timeout: time.Second * 10,
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	log.WithFields("http-status", resp.StatusCode).Tracef("http get [application/json] %q", url)

	return resp, nil
}

//nolint:funlen
func fetchAllReleasesFromGithubV4API(user, repo string) ([]ghRelease, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN environment variable not set but is required to use the GitHub v4 API")
	}
	src := oauth2.StaticTokenSource(
		// TODO: DI this
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	client := githubv4.NewClient(httpClient)
	var allReleases []ghRelease

	// Query some details about a repository, an ghIssue in it, and its comments.
	{
		// TODO: act on hitting a rate limit
		type rateLimit struct {
			Cost      githubv4.Int
			Limit     githubv4.Int
			Remaining githubv4.Int
			ResetAt   githubv4.DateTime
		}

		var query struct {
			Repository struct {
				DatabaseID githubv4.Int
				URL        githubv4.URI
				Releases   struct {
					PageInfo struct {
						EndCursor   githubv4.String
						HasNextPage bool
					}
					Edges []struct {
						Node struct {
							TagName       githubv4.String
							IsLatest      githubv4.Boolean
							IsDraft       githubv4.Boolean
							PublishedAt   githubv4.DateTime
							ReleaseAssets struct {
								PageInfo struct {
									EndCursor   githubv4.String
									HasNextPage bool
								}
								Nodes []struct {
									Name        githubv4.String
									ContentType githubv4.String
									DownloadURL githubv4.URI
								}
							} `graphql:"releaseAssets(first:100, after:$assetsCursor)"`
						}
					}
				} `graphql:"releases(first:100, after:$releasesCursor)"` // note: first 100 releases, where newest releases are first
			} `graphql:"repository(owner:$repositoryOwner, name:$repositoryName)"`

			RateLimit rateLimit
		}
		variables := map[string]interface{}{
			"repositoryOwner": githubv4.String(user),
			"repositoryName":  githubv4.String(repo),
			"releasesCursor":  (*githubv4.String)(nil), // Null after argument to get first page.
			"assetsCursor":    (*githubv4.String)(nil), // Null after argument to get first page.
		}

		// TODO: go to the next page :) (cosign was taking a while here so this needs investigation)
		// var limit rateLimit
		// for {
		err := client.Query(context.Background(), &query, variables)
		if err != nil {
			return nil, err
		}
		//  limit = query.RateLimit

		for iE := range query.Repository.Releases.Edges {
			var assets []ghAsset

			iEdge := query.Repository.Releases.Edges[iE]

			// for {
			for _, a := range iEdge.Node.ReleaseAssets.Nodes {
				//  support charset spec, e.g. "text/plain; charset=utf-8""
				contentType := strings.Split(string(a.ContentType), ";")[0]

				assets = append(assets, ghAsset{
					Name:        string(a.Name),
					ContentType: contentType,
					URL:         a.DownloadURL.String(),
				})
			}

			// 	if !iEdge.Node.ReleaseAssets.PageInfo.HasNextPage {
			// 		break
			// 	}
			// 	variables["assetsCursor"] = githubv4.NewString(iEdge.Node.ReleaseAssets.PageInfo.EndCursor)
			// }

			allReleases = append(allReleases, ghRelease{
				Tag:      string(iEdge.Node.TagName),
				IsLatest: boolRef(bool(iEdge.Node.IsLatest)),
				IsDraft:  boolRef(bool(iEdge.Node.IsDraft)),
				Date:     &iEdge.Node.PublishedAt.Time,
				Assets:   assets,
			})
		}

		//	if !query.Repository.Releases.PageInfo.HasNextPage {
		//		break
		//	}
		//	variables["releasesCursor"] = githubv4.NewString(query.Repository.Releases.PageInfo.EndCursor)
		//}

		// printJSON(allReleases)
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
