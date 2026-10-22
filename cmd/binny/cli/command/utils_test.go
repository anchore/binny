package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/tool/githubrelease"
)

func Test_toMap(t *testing.T) {
	tests := []struct {
		name   string
		params githubrelease.InstallerParameters
		want   map[string]any
	}{
		{
			name:   "unset fields are dropped",
			params: githubrelease.InstallerParameters{Repo: "oven-sh/bun"},
			want:   map[string]any{"repo": "oven-sh/bun"},
		},
		{
			name:   "keep-archive-files is written when set",
			params: githubrelease.InstallerParameters{Repo: "oven-sh/bun", KeepArchiveFiles: true},
			want:   map[string]any{"repo": "oven-sh/bun", "keep-archive-files": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toMap(tt.params)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
