package githubrelease

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionResolver_ResolveVersion(t *testing.T) {
	tests := []struct {
		name                 string
		config               VersionResolutionParameters
		version              string
		constraint           string
		releasesFetcher      func(ctx context.Context, user, repo string) ([]ghRelease, error)
		latestReleaseFetcher func(ctx context.Context, user, repo string) (*ghRelease, error)
		want                 string
		wantErr              require.ErrorAssertionFunc
	}{
		{
			name: "latest will trigger a lookup for the latest version",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "latest",
			want:    "2.0.0",
			latestReleaseFetcher: func(_ context.Context, user, repo string) (*ghRelease, error) {
				return &ghRelease{
					Tag: "2.0.0",
				}, nil
			},
			releasesFetcher: func(_ context.Context, user, repo string) ([]ghRelease, error) {
				t.Fatal("should not have been called")
				return nil, nil
			},
		},
		{
			name: "fallback to fetching all releases if latest is not found",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "latest",
			want:    "2.0.0",
			latestReleaseFetcher: func(_ context.Context, user, repo string) (*ghRelease, error) {
				return nil, nil
			},
			releasesFetcher: func(_ context.Context, user, repo string) ([]ghRelease, error) {
				return []ghRelease{
					{
						Tag: "1.0.0",
					},
					{
						Tag: "2.0.0",
					},
					{
						Tag: "1.1.0",
					},
				}, nil
			},
		},
		{
			name: "semver input will be honored as is",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "1.0.0",
			want:    "1.0.0",
		},
		{
			name: "non-semver input is honored as is",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "bogus",
			want:    "bogus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(tt.config)
			v.latestReleaseFetcher = tt.latestReleaseFetcher
			v.releasesFetcher = tt.releasesFetcher

			got, err := v.ResolveVersion(context.Background(), tt.version, tt.constraint)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersionResolver_UpdateVersion(t *testing.T) {
	tests := []struct {
		name                 string
		config               VersionResolutionParameters
		version              string
		constraint           string
		releaseFetcher       func(ctx context.Context, user, repo string) ([]ghRelease, error)
		latestReleaseFetcher func(ctx context.Context, user, repo string) (*ghRelease, error)
		want                 string
		wantErr              require.ErrorAssertionFunc
	}{
		{
			name: "latest does not update the version value",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "latest",
			want:    "latest",
		},
		{
			name: "semver input will trigger a lookup for the all releases",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "1.0.0",
			want:    "2.0.0",
			latestReleaseFetcher: func(_ context.Context, user, repo string) (*ghRelease, error) {
				return nil, nil
			},
			releaseFetcher: func(_ context.Context, user, repo string) ([]ghRelease, error) {
				return []ghRelease{
					{
						Tag: "1.0.0",
					},
					{
						Tag: "2.0.0",
					},
					{
						Tag: "1.1.0",
					},
				}, nil
			},
		},
		{
			name: "semver input will trigger a lookup for the latest release",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "1.0.0",
			want:    "2.0.0",
			latestReleaseFetcher: func(_ context.Context, user, repo string) (*ghRelease, error) {
				return &ghRelease{
					Tag: "2.0.0",
				}, nil
			},
			releaseFetcher: func(_ context.Context, user, repo string) ([]ghRelease, error) {
				t.Fatal("should not have been called")
				return nil, nil
			},
		},
		{
			name: "non-semver input is honored as is",
			config: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
			version: "bogus",
			want:    "bogus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(tt.config)
			v.latestReleaseFetcher = tt.latestReleaseFetcher
			v.releasesFetcher = tt.releaseFetcher

			got, err := v.UpdateVersion(context.Background(), tt.version, tt.constraint)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_filterToLatestVersion(t *testing.T) {
	tests := []struct {
		name              string
		releases          []ghRelease
		versionConstraint string
		want              *ghRelease
		wantErr           require.ErrorAssertionFunc
	}{
		{
			name:              "use semver comparison",
			versionConstraint: "",
			releases: []ghRelease{
				{
					Tag: "1.0.0",
				},
				{
					Tag: "v2.0.0", // note the v prefix
				},
				{
					Tag: "1.1.0",
				},
			},
			want: &ghRelease{
				Tag: "v2.0.0",
			},
		},
		{
			name:              "use semver comparison with constraint",
			versionConstraint: "< 2.0.0",
			releases: []ghRelease{
				{
					Tag: "1.0.0",
				},
				{
					Tag: "v2.0.0", // note the v prefix
				},
				{
					Tag: "1.1.0",
				},
			},
			want: &ghRelease{
				Tag: "1.1.0",
			},
		},
		{
			name:              "honor the latest flag",
			versionConstraint: "< 2.0.0",
			releases: []ghRelease{
				{
					Tag:      "2.0.0",
					IsLatest: boolRef(false),
				},
				{
					Tag:      "somethingbogus",
					IsLatest: boolRef(true),
				},
				{
					Tag:      "1.1.0",
					IsLatest: boolRef(false),
				},
			},
			want: &ghRelease{
				Tag:      "somethingbogus",
				IsLatest: boolRef(true),
			},
		},
		{
			name:              "honor the draft flag (ignore candidate)",
			versionConstraint: "< 2.0.0",
			releases: []ghRelease{
				{
					Tag:      "2.0.0",
					IsDraft:  boolRef(true),
					IsLatest: boolRef(true),
				},
				{
					Tag:      "1.1.0",
					IsLatest: boolRef(false),
					IsDraft:  boolRef(false),
				},
			},
			want: &ghRelease{
				Tag:      "1.1.0",
				IsLatest: boolRef(false),
				IsDraft:  boolRef(false),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := filterToLatestVersion(tt.releases, tt.versionConstraint)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
