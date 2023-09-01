package hostedshell

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/anchore/binny/tool/githubrelease"
)

func TestMethods(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		want    bool
	}{
		{
			name:    "valid",
			methods: []string{"hostedshell", "hosted shell", "hostedscript", "hosted script", "hosted-script", "hosted-shell"},
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
			name: "valid githubusercontent arguments",
			installParams: InstallerParameters{
				URL:  "https://raw.githubusercontent.com/anchore/binny/main/install.sh",
				Args: "-b /usr/local/bin",
			},
			wantMethod: githubrelease.ResolveMethod,
			wantParams: githubrelease.VersionResolutionParameters{
				Repo: "anchore/binny",
			},
		},
		{
			name: "valid github.com arguments",
			installParams: InstallerParameters{
				URL:  "https://github.com/anchore/binny/main/install.sh",
				Args: "-b /usr/local/bin",
			},
			wantMethod: githubrelease.ResolveMethod,
			wantParams: githubrelease.VersionResolutionParameters{
				Repo: "anchore/binny",
			},
		},
		{
			name: "valid but not github arguments",
			installParams: InstallerParameters{
				URL:  "https://raw.somewhere.com/anchore/binny/main/install.sh",
				Args: "-b /usr/local/bin",
			},
			wantErr: assert.Error,
		},
		{
			name: "invalid",
			installParams: map[string]string{
				"repo": "github.com/anchore/binny",
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
