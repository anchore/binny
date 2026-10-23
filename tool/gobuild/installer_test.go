package gobuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/internal"
)

func TestIsLocalModule(t *testing.T) {
	tests := []struct {
		name   string
		module string
		want   bool
	}{
		{
			name:   "current directory",
			module: ".",
			want:   true,
		},
		{
			name:   "relative path",
			module: "./cmd/tool",
			want:   true,
		},
		{
			name:   "parent directory",
			module: "../other-repo",
			want:   true,
		},
		{
			name:   "absolute path",
			module: "/home/user/project",
			want:   true,
		},
		{
			name:   "github module",
			module: "github.com/owner/repo",
			want:   false,
		},
		{
			name:   "other remote module",
			module: "gitlab.com/owner/repo",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsLocalModule(tt.module)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTemplateFlags(t *testing.T) {
	tests := []struct {
		name    string
		ldFlags []string
		version string
		want    string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name:    "simple flags",
			ldFlags: []string{"-X main.version=1.0.0"},
			version: "v1.0.0",
			want:    "-X main.version=1.0.0",
		},
		{
			name:    "template with version",
			ldFlags: []string{"-X main.version={{ .Version }}"},
			version: "v1.2.3",
			want:    "-X main.version=v1.2.3",
		},
		{
			name:    "multiple flags with templates",
			ldFlags: []string{"-X main.version={{ .Version }}", "-X main.commit=abc123"},
			version: "v2.0.0",
			want:    "-X main.version=v2.0.0 -X main.commit=abc123",
		},
		{
			name:    "invalid template",
			ldFlags: []string{"-X main.version={{ .Invalid }"},
			version: "v1.0.0",
			wantErr: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			got, err := internal.TemplateFlags(tt.ldFlags, tt.version)
			tt.wantErr(t, err)

			if err != nil {
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateEnvSlice(t *testing.T) {
	tests := []struct {
		name    string
		env     []string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name: "valid env vars",
			env:  []string{"FOO=bar", "BAZ=qux"},
		},
		{
			name: "empty slice",
			env:  []string{},
		},
		{
			name:    "missing equals sign",
			env:     []string{"FOO"},
			wantErr: require.Error,
		},
		{
			name: "empty value is valid",
			env:  []string{"FOO="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			err := internal.ValidateEnvSlice(tt.env)
			tt.wantErr(t, err)
		})
	}
}

func TestInstaller_InstallTo(t *testing.T) {
	tests := []struct {
		name           string
		config         InstallerParameters
		version        string
		wantEntrypoint string
		wantLdflags    string
		wantArgs       []string
		wantEnv        []string
		wantErr        require.ErrorAssertionFunc
	}{
		{
			name: "basic build",
			config: InstallerParameters{
				Module:     "github.com/owner/repo",
				Entrypoint: "cmd/mytool",
			},
			version:        "v1.0.0",
			wantEntrypoint: "cmd/mytool",
			wantArgs:       []string{},
			wantEnv:        []string{},
		},
		{
			name: "build with ldflags template",
			config: InstallerParameters{
				Module:     "github.com/owner/repo",
				Entrypoint: "cmd/mytool",
				LDFlags:    []string{"-X main.version={{ .Version }}"},
			},
			version:        "v1.2.3",
			wantEntrypoint: "cmd/mytool",
			wantLdflags:    "-X main.version=v1.2.3",
			wantArgs:       []string{},
			wantEnv:        []string{},
		},
		{
			name: "no entrypoint builds the module root",
			config: InstallerParameters{
				Module: "github.com/owner/mytool",
			},
			version:        "v1.0.0",
			wantEntrypoint: "",
			wantArgs:       []string{},
			wantEnv:        []string{},
		},
		{
			name: "build with args and env",
			config: InstallerParameters{
				Module:     "github.com/owner/repo",
				Entrypoint: "cmd/mytool",
				Args:       []string{"-trimpath"},
				Env:        []string{"CGO_ENABLED=0"},
			},
			version:        "v1.0.0",
			wantEntrypoint: "cmd/mytool",
			wantArgs:       []string{"-trimpath"},
			wantEnv:        []string{"CGO_ENABLED=0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			var capturedWorkDir, capturedOutputPath, capturedEntrypoint, capturedLdflags string
			var capturedArgs, capturedEnv []string
			sourceGetterCalled := false

			mockRunner := func(ctx context.Context, workDir, outputPath, entrypoint, ldflags string, args, env []string) error {
				capturedWorkDir = workDir
				capturedOutputPath = outputPath
				capturedEntrypoint = entrypoint
				capturedLdflags = ldflags
				capturedArgs = args
				capturedEnv = env
				return nil
			}

			mockSourceGetter := func(ctx context.Context, module, version, repoURL string, mode SourceMode) (string, func(), error) {
				sourceGetterCalled = true
				return "/tmp/test-source", func() {}, nil
			}

			installer := Installer{
				config:        tt.config,
				goBuildRunner: mockRunner,
				sourceGetter:  mockSourceGetter,
			}

			destDir := "/dest"
			binPath, err := installer.InstallTo(context.Background(), tt.version, destDir)
			tt.wantErr(t, err)

			if err != nil {
				return
			}

			expectedPath := filepath.Join(destDir, stagingBinName)
			if runtime.GOOS == "windows" {
				expectedPath += ".exe"
			}

			require.Equal(t, expectedPath, binPath)
			require.Equal(t, expectedPath, capturedOutputPath)
			require.Equal(t, tt.wantEntrypoint, capturedEntrypoint)
			require.Equal(t, tt.wantLdflags, capturedLdflags)

			require.True(t, sourceGetterCalled, "sourceGetter should be called for remote modules")
			require.Equal(t, "/tmp/test-source", capturedWorkDir)

			if d := cmp.Diff(tt.wantArgs, capturedArgs); d != "" {
				t.Errorf("args mismatch (-want +got):\n%s", d)
			}
			if d := cmp.Diff(tt.wantEnv, capturedEnv); d != "" {
				t.Errorf("env mismatch (-want +got):\n%s", d)
			}
		})
	}
}

func TestInstaller_InstallTo_SourceGetterError(t *testing.T) {
	mockRunner := func(ctx context.Context, workDir, outputPath, entrypoint, ldflags string, args, env []string) error {
		t.Fatal("goBuildRunner should not be called when sourceGetter fails")
		return nil
	}

	mockSourceGetter := func(ctx context.Context, module, version, repoURL string, mode SourceMode) (string, func(), error) {
		// return nil cleanup to also test nil cleanup handling
		return "", nil, errors.New("simulated source getter error")
	}

	installer := Installer{
		config: InstallerParameters{
			Module:     "github.com/owner/repo",
			Entrypoint: "cmd/mytool",
		},
		goBuildRunner: mockRunner,
		sourceGetter:  mockSourceGetter,
	}

	_, err := installer.InstallTo(context.Background(), "v1.0.0", "/dest")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get source")
	require.Contains(t, err.Error(), "simulated source getter error")
}

func TestInstaller_InstallTo_localModule(t *testing.T) {
	// resolve symlinks (e.g. /var -> /private/var on macOS) so paths compare equal after chdir
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	mod := filepath.Join(root, "mod")
	require.NoError(t, os.MkdirAll(filepath.Join(mod, "cmd", "hello"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/hello\n"), 0o644))
	elsewhere := filepath.Join(root, "elsewhere")
	require.NoError(t, os.MkdirAll(elsewhere, 0o755))
	aFile := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(aFile, nil, 0o644))

	tests := []struct {
		name       string
		cwd        string
		module     string
		entrypoint string
		wantDir    string
		wantErr    string
	}{
		{
			name:       "absolute module outside the working directory",
			cwd:        elsewhere,
			module:     mod,
			entrypoint: "cmd/hello",
			wantDir:    mod,
		},
		{
			name:       "relative module resolves against the working directory",
			cwd:        elsewhere,
			module:     "../mod",
			entrypoint: "cmd/hello",
			wantDir:    mod,
		},
		{
			name:    "module is the working directory",
			cwd:     mod,
			module:  ".",
			wantDir: mod,
		},
		{
			name:    "module is the parent directory",
			cwd:     filepath.Join(mod, "cmd"),
			module:  "..",
			wantDir: mod,
		},
		{
			name:    "module is a package directory within a module",
			cwd:     mod,
			module:  "./cmd/hello",
			wantDir: filepath.Join(mod, "cmd", "hello"),
		},
		{
			name:       "package directory within a module with an entrypoint",
			cwd:        mod,
			module:     "./cmd",
			entrypoint: "hello",
			wantDir:    filepath.Join(mod, "cmd"),
		},
		{
			name:    "missing directory",
			cwd:     elsewhere,
			module:  "../missing",
			wantErr: fmt.Sprintf("%q does not exist", filepath.Join(root, "missing")),
		},
		{
			name:    "module is a file",
			cwd:     elsewhere,
			module:  aFile,
			wantErr: "is not a directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.cwd)
			destDir := t.TempDir()
			var gotDir, gotOutput string
			i := Installer{
				config: InstallerParameters{Module: tt.module, Entrypoint: tt.entrypoint},
				goBuildRunner: func(_ context.Context, workDir, outputPath, _, _ string, _, _ []string) error {
					gotDir, gotOutput = workDir, outputPath
					return nil
				},
				sourceGetter: func(context.Context, string, string, string, SourceMode) (string, func(), error) {
					t.Fatal("sourceGetter should not be called for local modules")
					return "", nil, nil
				},
			}

			binPath, err := i.InstallTo(context.Background(), "v1.0.0", destDir)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tt.wantDir, gotDir)
			require.Equal(t, destDir, filepath.Dir(binPath))
			require.Equal(t, binPath, gotOutput)
		})
	}
}
