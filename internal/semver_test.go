package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_filterLatestVersion(t *testing.T) {

	tests := []struct {
		name              string
		versions          []string
		versionConstraint string
		want              string
		wantErr           require.ErrorAssertionFunc
	}{
		{
			name: "no versions",
		},
		{
			name:     "simple version",
			versions: []string{"v0.2.0", "v1.2.0", "v1.0.0", "v1.1.0"},
			want:     "v1.2.0",
		},
		{
			name:     "pre-release version",
			versions: []string{"v0.2.0", "v1.2.0", "v1.2.0-rc0", "v1.0.0", "v1.1.0"},
			want:     "v1.2.0",
		},
		{
			name:              "with version constraint",
			versions:          []string{"v0.2.0", "v1.2.0", "v1.0.0", "v1.1.0"},
			versionConstraint: "<= v1.1.0",
			want:              "v1.1.0",
		},
		{
			name:              "with version constraint outside range",
			versions:          []string{"v0.2.0", "v1.2.0", "v1.0.0", "v1.1.0"},
			versionConstraint: "<= v5, >= v2",
			want:              "",
		},
		{
			name:              "bad constraint",
			versions:          []string{"v0.2.0", "v1.2.0", "v1.0.0", "v1.1.0"},
			versionConstraint: "<=! v1.1.0",
			wantErr:           require.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := FilterToLatestVersion(tt.versions, tt.versionConstraint)
			tt.wantErr(t, err)
			assert.Equal(t, got, tt.want)
		})
	}
}

func Test_SemverTagPrefix(t *testing.T) {
	tests := []struct {
		tag     string
		want    string
		wantOK  bool
		trimmed string
	}{
		{tag: "bun-v1.4.3", want: "bun-", wantOK: true, trimmed: "v1.4.3"},
		{tag: "cli/v2.0.0", want: "cli/", wantOK: true, trimmed: "v2.0.0"},
		{tag: "foo-bar-1.2.3", want: "foo-bar-", wantOK: true, trimmed: "1.2.3"},
		{tag: "jq-1.7.1", want: "jq-", wantOK: true, trimmed: "1.7.1"},
		{tag: "bun-v1.4.3-canary.1", want: "bun-", wantOK: true, trimmed: "v1.4.3-canary.1"},
		// already semver, so the prerelease isn't mistaken for a prefixed version
		{tag: "v1.2.3-rc.1", trimmed: "v1.2.3-rc.1"},
		{tag: "1.2.3-1.0", trimmed: "1.2.3-1.0"},
		// date-like or major-only remainders aren't versions
		{tag: "nightly-2024-01-01", trimmed: "nightly-2024-01-01"},
		{tag: "release-2024", trimmed: "release-2024"},
		{tag: "build-1", trimmed: "build-1"},
		{tag: "nightly", trimmed: "nightly"},
		{tag: "release-candidate", trimmed: "release-candidate"},
		{tag: "bun-", trimmed: "bun-"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got, ok := SemverTagPrefix(tt.tag)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.trimmed, TrimSemverTagPrefix(tt.tag))
		})
	}
}
