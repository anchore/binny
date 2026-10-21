package yamlpatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type noopPatcher struct{}

func (noopPatcher) PatchYaml(*yaml.Node) error { return nil }

func TestWrite_flowStyle(t *testing.T) {
	in := `tools:
  - name: bun
    version: {want: bun-v1.4.3}
    method: github-release
    with: {repo: oven-sh/bun, assets: '^bun-[a-z]+-(aarch64|x64)\.zip$'}
`
	path := filepath.Join(t.TempDir(), ".binny.yaml")
	require.NoError(t, os.WriteFile(path, []byte(in), 0o644))

	require.NoError(t, Write(path, noopPatcher{}))

	out, err := os.ReadFile(path)
	require.NoError(t, err)

	// the rewritten file must still be valid yaml with the same content
	var want, got any
	require.NoError(t, yaml.Unmarshal([]byte(in), &want))
	require.NoError(t, yaml.Unmarshal(out, &got), string(out))
	assert.Equal(t, want, got)
}
