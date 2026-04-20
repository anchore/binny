package githubrelease

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
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

			got, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: tt.version, Constraint: tt.constraint})
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

			got, err := v.UpdateVersion(context.Background(), binny.VersionIntent{Want: tt.version, Constraint: tt.constraint})
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
			got, err := filterToLatestVersion(tt.releases, tt.versionConstraint, nil)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_filterToLatestVersion_withCooldown(t *testing.T) {
	now := time.Now()
	oldDate := now.Add(-14 * 24 * time.Hour) // 14 days ago
	newDate := now.Add(-2 * 24 * time.Hour)  // 2 days ago
	cutoff := now.Add(-7 * 24 * time.Hour)   // 7 day cooldown

	tests := []struct {
		name              string
		releases          []ghRelease
		versionConstraint string
		cutoff            *time.Time
		want              *ghRelease
		wantErr           require.ErrorAssertionFunc
	}{
		{
			name:   "filter out releases that are too new",
			cutoff: &cutoff,
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &newDate},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "1.0.0", Date: &oldDate},
		},
		{
			name:   "all releases too new returns nil",
			cutoff: &cutoff,
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &newDate},
				{Tag: "1.0.0", Date: &newDate},
			},
			want: nil,
		},
		{
			name:   "nil cutoff allows all releases",
			cutoff: nil,
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &newDate},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "2.0.0", Date: &newDate},
		},
		{
			name:   "releases with nil date are skipped when cutoff is active",
			cutoff: &cutoff,
			releases: []ghRelease{
				{Tag: "2.0.0"},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "1.0.0", Date: &oldDate},
		},
		{
			name:              "cooldown combined with constraint",
			cutoff:            &cutoff,
			versionConstraint: "< 2.0.0",
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &oldDate},
				{Tag: "1.5.0", Date: &newDate},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "1.0.0", Date: &oldDate},
		},
		{
			name:   "drafts still filtered with cooldown",
			cutoff: &cutoff,
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &oldDate, IsDraft: boolRef(true)},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "1.0.0", Date: &oldDate},
		},
		{
			name:   "IsLatest release within cooldown period is skipped",
			cutoff: &cutoff,
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &newDate, IsLatest: boolRef(true)},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "1.0.0", Date: &oldDate},
		},
		{
			name:   "IsLatest release outside cooldown period is used",
			cutoff: &cutoff,
			releases: []ghRelease{
				{Tag: "2.0.0", Date: &oldDate, IsLatest: boolRef(true)},
				{Tag: "1.0.0", Date: &oldDate},
			},
			want: &ghRelease{Tag: "2.0.0", Date: &oldDate, IsLatest: boolRef(true)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := filterToLatestVersion(tt.releases, tt.versionConstraint, tt.cutoff)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersionResolver_ResolveVersion_withCooldown(t *testing.T) {
	now := time.Now()
	oldDate := now.Add(-14 * 24 * time.Hour)
	newDate := now.Add(-2 * 24 * time.Hour)

	tests := []struct {
		name            string
		cooldown        time.Duration
		version         string
		releasesFetcher func(ctx context.Context, user, repo string) ([]ghRelease, error)
		want            string
		wantErr         require.ErrorAssertionFunc
	}{
		{
			name:     "cooldown skips facade and uses API with dates",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			want:     "1.0.0",
			releasesFetcher: func(_ context.Context, _, _ string) ([]ghRelease, error) {
				return []ghRelease{
					{Tag: "2.0.0", Date: &newDate},
					{Tag: "1.0.0", Date: &oldDate},
				}, nil
			},
		},
		{
			name:     "cooldown error when all versions are too new",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			wantErr: func(t require.TestingT, err error, _ ...any) {
				require.Error(t, err)
				var cooldownErr *binny.CooldownError
				require.ErrorAs(t, err, &cooldownErr)
				assert.Equal(t, "2.0.0", cooldownErr.LatestVersion)
			},
			releasesFetcher: func(_ context.Context, _, _ string) ([]ghRelease, error) {
				return []ghRelease{
					{Tag: "2.0.0", Date: &newDate},
					{Tag: "1.0.0", Date: &newDate},
				}, nil
			},
		},
		{
			name:     "pinned version bypasses cooldown",
			cooldown: 7 * 24 * time.Hour,
			version:  "2.0.0",
			want:     "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(VersionResolutionParameters{Repo: "anchore/binny"})
			v.latestReleaseFetcher = func(_ context.Context, _, _ string) (*ghRelease, error) {
				t.Fatal("facade should not be called when cooldown is active")
				return nil, nil
			}
			v.releasesFetcher = tt.releasesFetcher

			got, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: tt.version, Cooldown: tt.cooldown})
			tt.wantErr(t, err)
			if err == nil {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestVersionResolver_UpdateVersion_withCooldown(t *testing.T) {
	now := time.Now()
	oldDate := now.Add(-14 * 24 * time.Hour)
	newDate := now.Add(-2 * 24 * time.Hour)

	tests := []struct {
		name            string
		cooldown        time.Duration
		version         string
		releasesFetcher func(ctx context.Context, user, repo string) ([]ghRelease, error)
		want            string
		wantErr         require.ErrorAssertionFunc
	}{
		{
			name:     "update with cooldown filters too-new versions",
			cooldown: 7 * 24 * time.Hour,
			version:  "1.0.0",
			want:     "1.5.0",
			releasesFetcher: func(_ context.Context, _, _ string) ([]ghRelease, error) {
				return []ghRelease{
					{Tag: "2.0.0", Date: &newDate},
					{Tag: "1.5.0", Date: &oldDate},
					{Tag: "1.0.0", Date: &oldDate},
				}, nil
			},
		},
		{
			name:     "update with cooldown error when all too new",
			cooldown: 7 * 24 * time.Hour,
			version:  "1.0.0",
			wantErr: func(t require.TestingT, err error, _ ...any) {
				require.Error(t, err)
				var cooldownErr *binny.CooldownError
				require.ErrorAs(t, err, &cooldownErr)
			},
			releasesFetcher: func(_ context.Context, _, _ string) ([]ghRelease, error) {
				return []ghRelease{
					{Tag: "2.0.0", Date: &newDate},
					{Tag: "1.0.0", Date: &newDate},
				}, nil
			},
		},
		{
			name:     "latest version bypasses cooldown for update",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			want:     "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(VersionResolutionParameters{Repo: "anchore/binny"})
			v.latestReleaseFetcher = func(_ context.Context, _, _ string) (*ghRelease, error) {
				t.Fatal("facade should not be called when cooldown is active")
				return nil, nil
			}
			v.releasesFetcher = tt.releasesFetcher

			got, err := v.UpdateVersion(context.Background(), binny.VersionIntent{Want: tt.version, Cooldown: tt.cooldown})
			tt.wantErr(t, err)
			if err == nil {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
