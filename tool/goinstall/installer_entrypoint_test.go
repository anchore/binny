package goinstall

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// path.Join cleans ".." out of module+entrypoint, which would let an entrypoint name a different
// module entirely; go itself rejects an unclean package path ("argument must be a clean package
// path"), so binny should refuse rather than install something else.
func TestInstaller_InstallTo_entrypointMayNotLeaveModule(t *testing.T) {
	for _, entrypoint := range []string{"../other/cmd/x", "../../x/y", "cmd/../../other"} {
		t.Run(entrypoint, func(t *testing.T) {
			var gotSpec string
			i := Installer{
				config: InstallerParameters{Module: "github.com/owner/repo", Entrypoint: entrypoint},
				goInstallRunner: func(_ context.Context, spec, _ string, _, _ []string, _ string) error {
					gotSpec = spec
					return nil
				},
			}
			_, err := i.InstallTo(context.Background(), "v1.0.0", t.TempDir())
			require.Error(t, err, "entrypoint %q left module github.com/owner/repo: installed %q", entrypoint, gotSpec)
		})
	}
}
