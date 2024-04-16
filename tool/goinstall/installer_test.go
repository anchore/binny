package goinstall

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_templateFlags(t *testing.T) {
	type args struct {
		ldFlags []string
		version string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "no ldflags",
			args: args{
				ldFlags: nil,
				version: "1.2.3",
			},
			want:    "",
			wantErr: assert.NoError,
		},
		{
			name: "ldflags using template",
			args: args{
				ldFlags: []string{
					"-X github.com/anchore/binny/internal/version.Version={{.Version}}",
				},
				version: "1.2.3",
			},
			want:    "-X github.com/anchore/binny/internal/version.Version=1.2.3",
			wantErr: assert.NoError,
		},
		{
			name: "ldflags not using template",
			args: args{
				ldFlags: []string{
					"-X github.com/anchore/binny/internal/something.Else=hardcoded",
				},
				version: "1.2.3",
			},
			want:    "-X github.com/anchore/binny/internal/something.Else=hardcoded",
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := templateFlags(tt.args.ldFlags, tt.args.version)
			if !tt.wantErr(t, err, fmt.Sprintf("templateFlags(%v, %v)", tt.args.ldFlags, tt.args.version)) {
				return
			}
			assert.Equalf(t, tt.want, got, "templateFlags(%v, %v)", tt.args.ldFlags, tt.args.version)
		})
	}
}

func TestInstaller_InstallTo(t *testing.T) {
	type fields struct {
		config          InstallerParameters
		goInstallRunner func(spec, ldflags, destDir string) error
	}
	type args struct {
		version string
		destDir string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "happy path",
			fields: fields{
				config: InstallerParameters{
					Module:     "github.com/anchore/binny",
					Entrypoint: "cmd/binny",
					LDFlags: []string{
						"-X github.com/anchore/binny/internal/version.Version={{.Version}}",
					},
				},
				goInstallRunner: func(spec, ldflags, destDir string) error {
					assert.Equal(t, "github.com/anchore/binny/cmd/binny@1.2.3", spec)
					assert.Equal(t, "-X github.com/anchore/binny/internal/version.Version=1.2.3", ldflags)
					assert.Equal(t, "/tmp/to/place", destDir)
					return nil
				},
			},
			args: args{
				version: "1.2.3",
				destDir: "/tmp/to/place",
			},
			want:    "/tmp/to/place/binny",
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := NewInstaller(tt.fields.config)
			i.goInstallRunner = tt.fields.goInstallRunner

			got, err := i.InstallTo(tt.args.version, tt.args.destDir)
			got = strings.ReplaceAll(got, string(os.PathSeparator), "/")
			if !tt.wantErr(t, err, fmt.Sprintf("InstallTo(%v, %v)", tt.args.version, tt.args.destDir)) {
				return
			}
			assert.Equalf(t, tt.want, got, "InstallTo(%v, %v)", tt.args.version, tt.args.destDir)
		})
	}
}
