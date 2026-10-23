package option

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/tool/git"
)

// the README's local go-install example sets `version.method: git` with a `path`; that config has to parse.
func TestDeriveVersionResolveParameters_Git(t *testing.T) {
	method, params, err := deriveVersionResolveParameters("git", map[string]any{"path": "../mytool"})
	require.NoError(t, err)
	require.Equal(t, "git", method)
	require.Equal(t, git.VersionResolutionParameters{Path: "../mytool"}, params)
}
