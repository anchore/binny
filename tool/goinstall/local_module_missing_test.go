package goinstall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
	"github.com/anchore/binny/tool/git"
)

// the README says binny "fails early, naming the resolved path" for a missing local module. binny resolves the
// version before installing, and the default resolver for a local module is git, so that error has to name
// the path too.
func TestLocalModule_missingDirectoryErrorNamesPath(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cwd := filepath.Join(root, "elsewhere")
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	t.Chdir(cwd)

	method, params, err := DefaultVersionResolverConfig(InstallerParameters{Module: "../missing"})
	require.NoError(t, err)
	require.Equal(t, git.ResolveMethod, method)

	_, err = git.NewVersionResolver(params.(git.VersionResolutionParameters)).
		ResolveVersion(context.Background(), binny.VersionIntent{Want: "current"})
	require.Error(t, err)
	// accept the path raw or %q-quoted (which doubles windows backslashes)
	missing := filepath.Join(root, "missing")
	require.Truef(t, strings.Contains(err.Error(), missing) || strings.Contains(err.Error(), fmt.Sprintf("%q", missing)),
		"error %q does not name the resolved path %q", err.Error(), missing)
}
