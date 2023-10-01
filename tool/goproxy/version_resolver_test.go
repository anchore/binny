package goproxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionResolver_ResolveVersion(t *testing.T) {
	tests := []struct {
		name                     string
		config                   VersionResolutionParameters
		version                  string
		constraint               string
		availableVersionsFetcher func(url string) ([]string, error)
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
			availableVersionsFetcher: func(url string) ([]string, error) {
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
			availableVersionsFetcher: func(url string) ([]string, error) {
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
			availableVersionsFetcher: func(url string) ([]string, error) {
				return []string{""}, nil
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

			got, err := v.ResolveVersion(tt.version, tt.constraint)
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
		availableVersionsFetcher func(url string) ([]string, error)
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
			availableVersionsFetcher: func(url string) ([]string, error) {
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

			got, err := v.UpdateVersion(tt.version, tt.constraint)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
