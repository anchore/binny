package goproxy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

func TestVersionResolver_ResolveVersion(t *testing.T) {
	tests := []struct {
		name                     string
		config                   VersionResolutionParameters
		version                  string
		constraint               string
		availableVersionsFetcher func(ctx context.Context, url string) ([]string, error)
		want                     string
		wantErr                  require.ErrorAssertionFunc
	}{
		{
			name: "latest will trigger a lookup for the latest version",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
			version: "latest",
			want:    "2.0.0",
			availableVersionsFetcher: func(_ context.Context, url string) ([]string, error) {
				return []string{"1.0.0", "2.0.0", "1.1.0"}, nil
			},
		},
		{
			name: "semver input will be honored as is",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
			version: "1.0.0",
			want:    "1.0.0",
		},
		{
			name: "non-semver input is honored as is",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
			version: "bogus",
			want:    "bogus",
		},
		{
			name: "do not allow for unresolved versions",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
			version: "latest",
			wantErr: require.Error,
			availableVersionsFetcher: func(_ context.Context, url string) ([]string, error) {
				return []string{""}, nil
			},
		},
		{
			name: "allow for unresolved versions",
			config: VersionResolutionParameters{
				Module:                 "github.com/anchore/binny",
				AllowUnresolvedVersion: true,
			},
			version: "latest",
			want:    "latest", // this is a pass through to go-install, which supports this as input
			availableVersionsFetcher: func(_ context.Context, url string) ([]string, error) {
				return nil, fmt.Errorf("should never be called")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(tt.config)
			v.availableVersionsFetcher = tt.availableVersionsFetcher

			got, err := v.ResolveVersion(context.Background(), binny.VersionIntent{Want: tt.version, Constraint: tt.constraint})
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersionResolver_UpdateVersion(t *testing.T) {
	tests := []struct {
		name                     string
		config                   VersionResolutionParameters
		version                  string
		constraint               string
		availableVersionsFetcher func(ctx context.Context, url string) ([]string, error)
		want                     string
		wantErr                  require.ErrorAssertionFunc
	}{
		{
			name: "latest does not update the version value",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
			version: "latest",
			want:    "latest",
		},
		{
			name: "semver input will trigger a lookup for the latest version",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			},
			version: "1.0.0",
			want:    "2.0.0",
			availableVersionsFetcher: func(_ context.Context, url string) ([]string, error) {
				return []string{"1.0.0", "2.0.0", "1.1.0"}, nil
			},
		},
		{
			name: "non-semver input is honored as is",
			config: VersionResolutionParameters{
				Module: "github.com/anchore/binny",
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
			v.availableVersionsFetcher = tt.availableVersionsFetcher

			got, err := v.UpdateVersion(context.Background(), binny.VersionIntent{Want: tt.version, Constraint: tt.constraint})
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
		name                     string
		cooldown                 time.Duration
		version                  string
		availableVersionsFetcher func(ctx context.Context, url string) ([]string, error)
		versionInfoFetcher       func(ctx context.Context, module, version string) (*versionInfo, error)
		want                     string
		wantErr                  require.ErrorAssertionFunc
	}{
		{
			name:     "cooldown filters out too-new versions",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			want:     "1.0.0",
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.0.0", "2.0.0"}, nil
			},
			versionInfoFetcher: func(_ context.Context, _, version string) (*versionInfo, error) {
				switch version {
				case "2.0.0":
					return &versionInfo{Version: "2.0.0", Time: newDate}, nil
				case "1.0.0":
					return &versionInfo{Version: "1.0.0", Time: oldDate}, nil
				}
				return nil, fmt.Errorf("unexpected version: %s", version)
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
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.0.0", "2.0.0"}, nil
			},
			versionInfoFetcher: func(_ context.Context, _, _ string) (*versionInfo, error) {
				return &versionInfo{Version: "2.0.0", Time: newDate}, nil
			},
		},
		{
			name:     "pinned version bypasses cooldown",
			cooldown: 7 * 24 * time.Hour,
			version:  "1.0.0",
			want:     "1.0.0",
		},
		{
			name:     "no cooldown uses standard resolution",
			cooldown: 0,
			version:  "latest",
			want:     "2.0.0",
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.0.0", "2.0.0"}, nil
			},
		},
		{
			name:     "version info fetch error skips to next candidate",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			want:     "1.0.0",
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.0.0", "2.0.0"}, nil
			},
			versionInfoFetcher: func(_ context.Context, _, version string) (*versionInfo, error) {
				switch version {
				case "2.0.0":
					return nil, fmt.Errorf("network error")
				case "1.0.0":
					return &versionInfo{Version: "1.0.0", Time: oldDate}, nil
				}
				return nil, fmt.Errorf("unexpected version: %s", version)
			},
		},
		{
			name:     "candidate limit reached surfaces info in error",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				// generate more candidates than maxCooldownCandidates
				var versions []string
				for i := range maxCooldownCandidates + 5 {
					versions = append(versions, fmt.Sprintf("1.0.%d", i))
				}
				return versions, nil
			},
			versionInfoFetcher: func(_ context.Context, _, _ string) (*versionInfo, error) {
				// all checked versions are too new
				return &versionInfo{Version: "1.0.0", Time: newDate}, nil
			},
			wantErr: func(t require.TestingT, err error, _ ...any) {
				require.Error(t, err)
				var cooldownErr *binny.CooldownError
				require.ErrorAs(t, err, &cooldownErr)
				assert.Equal(t, maxCooldownCandidates, cooldownErr.CheckedCount)
				assert.Equal(t, maxCooldownCandidates+5, cooldownErr.TotalCount)
				assert.Contains(t, cooldownErr.Error(), "only checked")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			})
			v.availableVersionsFetcher = tt.availableVersionsFetcher
			v.versionInfoFetcher = tt.versionInfoFetcher

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
		name                     string
		cooldown                 time.Duration
		version                  string
		constraint               string
		availableVersionsFetcher func(ctx context.Context, url string) ([]string, error)
		versionInfoFetcher       func(ctx context.Context, module, version string) (*versionInfo, error)
		want                     string
		wantErr                  require.ErrorAssertionFunc
	}{
		{
			name:     "update with cooldown filters too-new versions",
			cooldown: 7 * 24 * time.Hour,
			version:  "1.0.0",
			want:     "1.5.0",
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.0.0", "1.5.0", "2.0.0"}, nil
			},
			versionInfoFetcher: func(_ context.Context, _, version string) (*versionInfo, error) {
				switch version {
				case "2.0.0":
					return &versionInfo{Version: "2.0.0", Time: newDate}, nil
				case "1.5.0":
					return &versionInfo{Version: "1.5.0", Time: oldDate}, nil
				case "1.0.0":
					return &versionInfo{Version: "1.0.0", Time: oldDate}, nil
				}
				return nil, fmt.Errorf("unexpected version: %s", version)
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
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.0.0", "2.0.0"}, nil
			},
			versionInfoFetcher: func(_ context.Context, _, _ string) (*versionInfo, error) {
				return &versionInfo{Version: "2.0.0", Time: newDate}, nil
			},
		},
		{
			name:     "latest version bypasses cooldown for update",
			cooldown: 7 * 24 * time.Hour,
			version:  "latest",
			want:     "latest",
		},
		{
			name:       "cooldown combined with version constraint",
			cooldown:   7 * 24 * time.Hour,
			version:    "1.0.0",
			constraint: "< 2.0.0",
			want:       "1.0.0",
			availableVersionsFetcher: func(_ context.Context, _ string) ([]string, error) {
				// constraint filters out 2.x, cooldown filters out 1.5.0, leaving 1.0.0
				return []string{"1.0.0", "1.5.0", "2.0.0"}, nil
			},
			versionInfoFetcher: func(_ context.Context, _, version string) (*versionInfo, error) {
				switch version {
				case "1.5.0":
					return &versionInfo{Version: "1.5.0", Time: newDate}, nil
				case "1.0.0":
					return &versionInfo{Version: "1.0.0", Time: oldDate}, nil
				}
				return nil, fmt.Errorf("unexpected version: %s", version)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			v := NewVersionResolver(VersionResolutionParameters{
				Module: "github.com/anchore/binny",
			})
			v.availableVersionsFetcher = tt.availableVersionsFetcher
			v.versionInfoFetcher = tt.versionInfoFetcher

			got, err := v.UpdateVersion(context.Background(), binny.VersionIntent{Want: tt.version, Cooldown: tt.cooldown, Constraint: tt.constraint})
			tt.wantErr(t, err)
			if err == nil {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
