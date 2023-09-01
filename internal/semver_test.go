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
