package goinstall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/internal"
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
			got, err := internal.TemplateFlags(tt.args.ldFlags, tt.args.version)
			if !tt.wantErr(t, err, fmt.Sprintf("TemplateFlags(%v, %v)", tt.args.ldFlags, tt.args.version)) {
				return
			}
			assert.Equalf(t, tt.want, got, "TemplateFlags(%v, %v)", tt.args.ldFlags, tt.args.version)
		})
	}
}

func TestInstaller_InstallTo(t *testing.T) {
	type fields struct {
		config          InstallerParameters
		goInstallRunner func(ctx context.Context, spec, ldflags string, args, env []string, destDir string) error
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
					Args: []string{
						"-tags",
						"containers_image_openpgp",
					},
					Env: []string{
						"FOO=BAR",
						"BAZ=0",
					},
				},
				goInstallRunner: func(_ context.Context, spec, ldflags string, userArgs, userEnv []string, destDir string) error {
					assert.Equal(t, "github.com/anchore/binny/cmd/binny@1.2.3", spec)
					assert.Equal(t, "-X github.com/anchore/binny/internal/version.Version=1.2.3", ldflags)
					assert.Equal(t, "/tmp/to/place", destDir)
					assert.Equal(t, []string{"-tags", "containers_image_openpgp"}, userArgs)
					assert.Equal(t, []string{"FOO=BAR", "BAZ=0"}, userEnv)
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
		{
			name: "remote module without entrypoint",
			fields: fields{
				config: InstallerParameters{
					Module:     "github.com/some/repo",
					Entrypoint: "",
				},
				goInstallRunner: func(_ context.Context, spec, ldflags string, userArgs, userEnv []string, destDir string) error {
					assert.Equal(t, "github.com/some/repo@1.2.3", spec)
					assert.Equal(t, "", ldflags)
					assert.Equal(t, "/tmp/to/place", destDir)
					assert.Equal(t, []string{}, userArgs)
					assert.Equal(t, []string{}, userEnv)
					return nil
				},
			},
			args: args{
				version: "1.2.3",
				destDir: "/tmp/to/place",
			},
			want:    "/tmp/to/place/repo",
			wantErr: assert.NoError,
		},
		{
			name: "remote module with major version suffix",
			fields: fields{
				config: InstallerParameters{
					Module: "github.com/some/repo/v2",
				},
				goInstallRunner: func(_ context.Context, spec, _ string, _, _ []string, _ string) error {
					assert.Equal(t, "github.com/some/repo/v2@1.2.3", spec)
					return nil
				},
			},
			args: args{
				version: "1.2.3",
				destDir: "/tmp/to/place",
			},
			// go install drops the /vN suffix when naming the binary
			want:    "/tmp/to/place/repo",
			wantErr: assert.NoError,
		},
		{
			name: "module with complex/nested entrypoint path",
			fields: fields{
				config: InstallerParameters{
					Module:     "github.com/example/project",
					Entrypoint: "cmd/tools/deployment/deployer",
				},
				goInstallRunner: func(_ context.Context, spec, ldflags string, userArgs, userEnv []string, destDir string) error {
					assert.Equal(t, "github.com/example/project/cmd/tools/deployment/deployer@1.2.3", spec)
					assert.Equal(t, "", ldflags)
					assert.Equal(t, "/tmp/to/place", destDir)
					assert.Equal(t, []string{}, userArgs)
					assert.Equal(t, []string{}, userEnv)
					return nil
				},
			},
			args: args{
				version: "1.2.3",
				destDir: "/tmp/to/place",
			},
			want:    "/tmp/to/place/deployer",
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := NewInstaller(tt.fields.config)
			i.goInstallRunner = tt.fields.goInstallRunner

			got, err := i.InstallTo(context.Background(), tt.args.version, tt.args.destDir)
			got = strings.ReplaceAll(got, string(os.PathSeparator), "/")
			if !tt.wantErr(t, err, fmt.Sprintf("InstallTo(%v, %v)", tt.args.version, tt.args.destDir)) {
				return
			}
			want := tt.want
			if runtime.GOOS == "windows" {
				want += ".exe"
			}
			assert.Equalf(t, want, got, "InstallTo(%v, %v)", tt.args.version, tt.args.destDir)
		})
	}
}

// this runs a real go build, since the failure being guarded against comes from go itself resolving the main
// module from the working directory.
func TestInstaller_InstallTo_localModuleRealBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	// keep the caller's workspace and flags out of the build
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := t.TempDir()
	mod := filepath.Join(root, "mod")
	require.NoError(t, os.MkdirAll(filepath.Join(mod, "cmd", "hello"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/hello\n\ngo 1.21\n"), 0o644))
	// print what the build was given, so the test can see that ldflags, args and env all made it through the handoff
	main := `package main

import (
	"fmt"
	"runtime/debug"
)

var v string

func main() {
	fmt.Println("version=" + v)
	bi, _ := debug.ReadBuildInfo()
	for _, s := range bi.Settings {
		fmt.Println(s.Key + "=" + s.Value)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(mod, "cmd", "hello", "main.go"), []byte(main), 0o644))
	elsewhere := filepath.Join(root, "elsewhere")
	require.NoError(t, os.MkdirAll(elsewhere, 0o755))
	t.Chdir(elsewhere)

	dest := t.TempDir()
	got, err := NewInstaller(InstallerParameters{
		Module:     "../mod",
		Entrypoint: "cmd/hello",
		LDFlags:    []string{"-X main.v={{ .Version }}"},
		Args:       []string{"-tags=binnytest"},
		Env:        []string{"CGO_ENABLED=0"},
	}).InstallTo(context.Background(), "v1.0.0", dest)
	require.NoError(t, err)
	assert.Equal(t, dest, filepath.Dir(got))

	out, err := exec.Command(got).Output()
	require.NoError(t, err)
	lines := strings.Split(string(out), "\n")
	assert.Contains(t, lines, "version=v1.0.0")
	assert.Contains(t, lines, "-tags=binnytest")
	assert.Contains(t, lines, "CGO_ENABLED=0")
}

func Test_binaryName(t *testing.T) {
	tests := []struct {
		importPath string
		want       string
		wantErr    bool
	}{
		{importPath: "github.com/owner/repo", want: "repo"},
		{importPath: "github.com/owner/repo/cmd/tool", want: "tool"},
		{importPath: "github.com/owner/repo/v2", want: "repo"},
		{importPath: "github.com/owner/repo/v10", want: "repo"},
		{importPath: "github.com/owner/repo/cmd/tool/v3", want: "tool"},
		// not major version suffixes, go keeps these as the name
		{importPath: "github.com/owner/v2ray", want: "v2ray"},
		{importPath: "github.com/owner/v8go", want: "v8go"},
		{importPath: "github.com/owner/repo/v1", want: "v1"},
		{importPath: "github.com/owner/repo/v0", want: "v0"},
		{importPath: "github.com/owner/repo/vars", want: "vars"},
		{importPath: "v2", want: "v2"},
		{importPath: "", wantErr: true},
		{importPath: "..", wantErr: true},
		{importPath: `github.com/owner/repo/..\x`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			got, err := binaryName(tt.importPath)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			want := tt.want
			if runtime.GOOS == "windows" {
				want += ".exe"
			}
			assert.Equal(t, want, got)
		})
	}
}
