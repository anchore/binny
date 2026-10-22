package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/anchore/clio"
	goredact "github.com/anchore/go-logger/adapter/redact"

	"github.com/anchore/binny/cmd/binny/cli/option"
	"github.com/anchore/binny/internal/redact"
)

func Test_runGithubReleaseConfig_keepArchiveFiles(t *testing.T) {
	// the command reports the change on the bus, which redacts what it reports
	if redact.Get() == nil {
		redact.Set(goredact.NewStore())
	}

	for _, keep := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), ".binny.yaml")

		cmdCfg := AddGithubReleaseConfig{Config: path}
		cmdCfg.Install.GithubRelease.KeepArchiveFiles = keep
		require.NoError(t, runGithubReleaseConfig(cmdCfg, "oven-sh/bun@bun-v1.4.3"))

		by, err := os.ReadFile(path)
		require.NoError(t, err)
		var got struct {
			Tools []option.Tool `yaml:"tools"`
		}
		require.NoError(t, yaml.Unmarshal(by, &got), string(by))
		require.Len(t, got.Tools, 1)

		// the flag lands in the tool's install params, and is left out entirely when not set
		v, ok := got.Tools[0].Parameters["keep-archive-files"]
		assert.Equal(t, keep, ok, string(by))
		if keep {
			assert.Equal(t, true, v)
		}
	}
}

func TestAddGithubRelease_keepArchiveFilesFlag(t *testing.T) {
	app := clio.New(clio.SetupConfig{
		ID: clio.Identification{
			Name:    "test",
			Version: "0.0.0",
		},
	})

	cmd := AddGithubRelease(app)

	require.NotNil(t, cmd.Flags().Lookup("keep-archive-files"))
}
