package gobuild

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		module  string
		want    string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name:   "simple github module",
			module: "github.com/owner/repo",
			want:   "https://github.com/owner/repo.git",
		},
		{
			name:   "github module with v2 suffix",
			module: "github.com/owner/repo/v2",
			want:   "https://github.com/owner/repo.git",
		},
		{
			name:   "github module with subpackage",
			module: "github.com/owner/repo/cmd/tool",
			want:   "https://github.com/owner/repo.git",
		},
		{
			name:   "github module with v3 and subpackage",
			module: "github.com/owner/repo/v3/cmd/tool",
			want:   "https://github.com/owner/repo.git",
		},
		{
			name:    "non-github module requires explicit url",
			module:  "gitlab.com/owner/repo",
			wantErr: require.Error,
		},
		{
			name:    "invalid github module path",
			module:  "github.com/owner",
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			got, err := DeriveRepoURL(tt.module)
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDeriveGitHubRepo(t *testing.T) {
	tests := []struct {
		name   string
		module string
		want   string
	}{
		{
			name:   "simple github module",
			module: "github.com/owner/repo",
			want:   "owner/repo",
		},
		{
			name:   "github module with v2",
			module: "github.com/owner/repo/v2",
			want:   "owner/repo",
		},
		{
			name:   "github module with subpackage",
			module: "github.com/owner/repo/cmd/tool",
			want:   "owner/repo",
		},
		{
			name:   "non-github module",
			module: "gitlab.com/owner/repo",
			want:   "",
		},
		{
			name:   "invalid github module",
			module: "github.com/owner",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveGitHubRepo(tt.module)
			require.Equal(t, tt.want, got)
		})
	}
}
