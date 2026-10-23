package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

// movingRefTool resolves a ref to whatever `points` currently is, the way the go-proxy resolver does for a branch.
type movingRefTool struct {
	points string
}

func (m *movingRefTool) Name() string { return "tracked" }

func (m *movingRefTool) ResolveVersion(context.Context, binny.VersionIntent) (string, error) {
	return m.points, nil
}

func (m *movingRefTool) UpdateVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func (m *movingRefTool) InstallTo(_ context.Context, version, destDir string) (string, error) {
	p := filepath.Join(destDir, "tracked")
	return p, os.WriteFile(p, []byte(version), 0o755) //nolint:gosec // test binary
}

func TestRefTracksMovedBranch(t *testing.T) {
	ctx := context.Background()
	store, err := binny.NewStore(t.TempDir())
	require.NoError(t, err)

	tool := &movingRefTool{points: "v0.0.0-20261014120000-111111111111"}
	intent := binny.VersionIntent{Want: "main"}
	verify := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(ctx, tool, intent, store, verify))
	require.ErrorIs(t, Install(ctx, tool, intent, store, verify), ErrAlreadyInstalled)

	resolved, err := ResolveVersion(ctx, tool, intent)
	require.NoError(t, err)
	require.NoError(t, Check(store, tool, resolved, verify))

	// the branch moves upstream: check now fails and install picks up the new commit
	tool.points = "v0.0.0-20261015120000-222222222222"
	resolved, err = ResolveVersion(ctx, tool, intent)
	require.NoError(t, err)
	require.Error(t, Check(store, tool, resolved, verify))

	require.NoError(t, Install(ctx, tool, intent, store, verify))
	require.NoError(t, Check(store, tool, resolved, verify))
}
