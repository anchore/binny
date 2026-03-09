package gobuild

import (
	"context"
	"errors"
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

func TestDeriveBinaryName(t *testing.T) {
	tests := []struct {
		name       string
		module     string
		entrypoint string
		want       string
	}{
		{
			name:       "simple module",
			module:     "github.com/owner/repo",
			entrypoint: "",
			want:       "repo",
		},
		{
			name:       "module with entrypoint",
			module:     "github.com/owner/repo",
			entrypoint: "cmd/mytool",
			want:       "mytool",
		},
		{
			name:       "module with v2 suffix",
			module:     "github.com/owner/repo/v2",
			entrypoint: "",
			want:       "repo",
		},
		{
			name:       "module with v3 suffix and entrypoint",
			module:     "github.com/owner/repo/v3",
			entrypoint: "cmd/tool",
			want:       "tool",
		},
		{
			name:       "deep entrypoint path",
			module:     "github.com/owner/repo",
			entrypoint: "internal/cmd/service/main",
			want:       "main",
		},
		{
			name:       "single segment entrypoint",
			module:     "github.com/owner/repo",
			entrypoint: "tool",
			want:       "tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveBinaryName(tt.module, tt.entrypoint)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStripVersionSuffix(t *testing.T) {
	tests := []struct {
		name   string
		module string
		want   string
	}{
		{
			name:   "no version suffix",
			module: "github.com/owner/repo",
			want:   "github.com/owner/repo",
		},
		{
			name:   "v2 suffix",
			module: "github.com/owner/repo/v2",
			want:   "github.com/owner/repo",
		},
		{
			name:   "v10 suffix",
			module: "github.com/owner/repo/v10",
			want:   "github.com/owner/repo",
		},
		{
			name:   "subpackage looks like version but isn't",
			module: "github.com/owner/repo/vars",
			want:   "github.com/owner/repo/vars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripVersionSuffix(tt.module)
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
		wantBinName    string
		wantEntrypoint string
		wantLdflags    string
		wantArgs       []string
		wantEnv        []string
		wantWorkDir    string // if empty, defaults to "/tmp/test-source" (mock remote source)
		wantErr        require.ErrorAssertionFunc
	}{
		{
			name: "basic build",
			config: InstallerParameters{
				Module:     "github.com/owner/repo",
				Entrypoint: "cmd/mytool",
			},
			version:        "v1.0.0",
			wantBinName:    "mytool",
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
			wantBinName:    "mytool",
			wantEntrypoint: "cmd/mytool",
			wantLdflags:    "-X main.version=v1.2.3",
			wantArgs:       []string{},
			wantEnv:        []string{},
		},
		{
			name: "no entrypoint uses module name",
			config: InstallerParameters{
				Module: "github.com/owner/mytool",
			},
			version:        "v1.0.0",
			wantBinName:    "mytool",
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
			wantBinName:    "mytool",
			wantEntrypoint: "cmd/mytool",
			wantArgs:       []string{"-trimpath"},
			wantEnv:        []string{"CGO_ENABLED=0"},
		},
		{
			name: "local module uses module path as workdir",
			config: InstallerParameters{
				Module:     ".",
				Entrypoint: "cmd/mytool",
			},
			version:        "v1.0.0",
			wantBinName:    "mytool",
			wantEntrypoint: "cmd/mytool",
			wantArgs:       []string{},
			wantEnv:        []string{},
			wantWorkDir:    ".", // local module uses module path directly
		},
		{
			name: "local module with relative path",
			config: InstallerParameters{
				Module:     "./subdir",
				Entrypoint: "cmd/tool",
			},
			version:        "v1.0.0",
			wantBinName:    "tool",
			wantEntrypoint: "cmd/tool",
			wantArgs:       []string{},
			wantEnv:        []string{},
			wantWorkDir:    "./subdir",
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

			// construct expected path the same way the code does
			expectedBinName := tt.wantBinName
			if runtime.GOOS == "windows" {
				expectedBinName += ".exe"
			}
			expectedPath := filepath.Join(destDir, expectedBinName)

			require.Equal(t, expectedPath, binPath)
			require.Equal(t, expectedPath, capturedOutputPath)
			require.Equal(t, tt.wantEntrypoint, capturedEntrypoint)
			require.Equal(t, tt.wantLdflags, capturedLdflags)

			// check workDir - for local modules it should be the module path,
			// for remote modules it should be the mock source path
			expectedWorkDir := tt.wantWorkDir
			if expectedWorkDir == "" {
				expectedWorkDir = "/tmp/test-source"
				require.True(t, sourceGetterCalled, "sourceGetter should be called for remote modules")
			} else {
				require.False(t, sourceGetterCalled, "sourceGetter should not be called for local modules")
			}
			require.Equal(t, expectedWorkDir, capturedWorkDir)

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
