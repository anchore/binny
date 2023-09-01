package githubrelease

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMethods(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		want    bool
	}{
		{
			name:    "valid",
			methods: []string{"github-release", "github release", "github", "githubrelease"},
			want:    true,
		},
		{
			name:    "invalid",
			methods: []string{"made up"},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, method := range tt.methods {
				t.Run(method, func(t *testing.T) {
					t.Run("IsInstallMethod", func(t *testing.T) {
						assert.Equal(t, tt.want, IsInstallMethod(method))
					})
					t.Run("IsResolveMethod", func(t *testing.T) {
						assert.Equal(t, tt.want, IsResolveMethod(method))
					})
				})
			}
		})
	}
}

func TestDefaultVersionResolverConfig(t *testing.T) {
	tests := []struct {
		name          string
		installParams any
		wantMethod    string
		wantParams    any
		wantErr       assert.ErrorAssertionFunc
	}{
		{
			name: "valid",
			installParams: InstallerParameters{
				Repo: "anchore/binny",
			},
			wantMethod: ResolveMethod,
			wantParams: VersionResolutionParameters{
				Repo: "anchore/binny",
			},
		},
		{
			name: "invalid",
			installParams: map[string]string{
				"repo": "anchore/binny",
			},
			wantErr: assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = assert.NoError
			}
			method, params, err := DefaultVersionResolverConfig(tt.installParams)
			if !tt.wantErr(t, err) {
				return
			}
			assert.Equal(t, tt.wantMethod, method)
			assert.Equal(t, tt.wantParams, params)
		})
	}
}
