package goinstall

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// go install now runs via exec.CommandContext, so a cancelled install stops instead of running to completion.
func Test_runGoInstall_cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runGoInstall(ctx, "github.com/owner/repo@v1.0.0", "", nil, nil, t.TempDir())
	// runGoInstall formats with %v, so this checks the message rather than errors.Is
	require.ErrorContains(t, err, context.Canceled.Error())
}

// the spec is built from the cleaned module/entrypoint path, so "./cmd/tool" and "cmd/tool/" install the
// same package, with the same binary name, as "cmd/tool".
func TestInstaller_InstallTo_cleanedEntrypoint(t *testing.T) {
	for _, entrypoint := range []string{"./cmd/tool", "cmd/tool/", "cmd//tool"} {
		t.Run(entrypoint, func(t *testing.T) {
			var gotSpec string
			i := Installer{
				config: InstallerParameters{Module: "github.com/owner/repo", Entrypoint: entrypoint},
				goInstallRunner: func(_ context.Context, spec, _ string, _, _ []string, _ string) error {
					gotSpec = spec
					return nil
				},
			}
			got, err := i.InstallTo(context.Background(), "v1.0.0", t.TempDir())
			require.NoError(t, err)
			require.Equal(t, "github.com/owner/repo/cmd/tool@v1.0.0", gotSpec)
			require.Equal(t, "tool", trimExe(filepath.Base(got)))
		})
	}
}

func trimExe(name string) string {
	if filepath.Ext(name) == ".exe" {
		return name[:len(name)-len(".exe")]
	}
	return name
}
