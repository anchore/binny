package tool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

func TestResolveVersion_constraintOnPrefixedTag(t *testing.T) {
	tests := []struct {
		name       string
		want       string
		constraint string
		wantErr    require.ErrorAssertionFunc
	}{
		{
			name:       "constraint applies to the version after the prefix",
			want:       "bun-v1.4.3",
			constraint: ">= 1.4.0",
		},
		{
			name:       "unsatisfied constraint on a prefixed tag is an error",
			want:       "bun-v1.4.3",
			constraint: "< 1.4.0",
			wantErr:    require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := ResolveVersion(context.Background(), fakeTool{name: "bun"}, binny.VersionIntent{Want: tt.want, Constraint: tt.constraint})
			tt.wantErr(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
