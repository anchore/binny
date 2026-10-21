package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

// fakeTool installs an archive-like tree with the binary next to a file it reads relative to itself
type fakeTool struct {
	name string
	keep bool
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) KeepArchiveFiles() bool { return f.keep }

func (f fakeTool) InstallTo(_ context.Context, version, destDir string) (string, error) {
	lib := filepath.Join(destDir, "package", "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(lib, "lib.d.ts"), []byte("decl"), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(lib, f.name)
	return bin, os.WriteFile(bin, []byte(version), 0o755)
}

func (f fakeTool) ResolveVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func (f fakeTool) UpdateVersion(_ context.Context, intent binny.VersionIntent) (string, error) {
	return intent.Want, nil
}

func TestInstall_keepArchiveFilesToggle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	store, err := binny.NewStore(t.TempDir())
	require.NoError(t, err)

	intent := binny.VersionIntent{Want: "v1"}
	cfg := VerifyConfig{VerifySHA256Digest: true}
	keptDir := filepath.Join(store.Root(), ".binny-dirs", "tsc")

	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc"}, intent, store, cfg))
	assert.NoDirExists(t, keptDir)

	// same version, but now the archive files should be kept, which needs a reinstall
	keeper := fakeTool{name: "tsc", keep: true}
	require.Error(t, Check(store, keeper, "v1", cfg))
	require.NoError(t, Install(context.Background(), keeper, intent, store, cfg))
	assert.FileExists(t, filepath.Join(keptDir, "package", "lib", "lib.d.ts"))
	require.NoError(t, Check(store, keeper, "v1", cfg))
	require.ErrorIs(t, Install(context.Background(), keeper, intent, store, cfg), ErrAlreadyInstalled)

	// and turning it back off reinstalls the plain binary
	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc"}, intent, store, cfg))
	assert.NoDirExists(t, keptDir)
	require.NoError(t, Check(store, fakeTool{name: "tsc"}, "v1", cfg))
}
