package tool

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny/tool/gobuild"
	"github.com/anchore/binny/tool/goinstall"
)

func Test_goCommandEnv(t *testing.T) {
	env := []string{"GOPROXY=https://corp.example", "GOFLAGS=-ldflags=-X=main.v={{ .Version }}", "bogus"}
	want := []string{"GOPROXY=https://corp.example"}

	assert.Equal(t, want, goCommandEnv(goinstall.InstallerParameters{Env: env}))
	assert.Equal(t, want, goCommandEnv(gobuild.InstallerParameters{Env: env}))
	assert.Empty(t, goCommandEnv(nil))
}

func TestNew_goBuildTracksGitRefs(t *testing.T) {
	tl, err := New(Config{
		Name: "tool",
		InstallerConfig: DetailConfig{
			Method:     gobuild.InstallMethod,
			Parameters: gobuild.InstallerParameters{Module: "github.com/anchore/binny"},
		},
	})
	require.NoError(t, err)

	// go-build clones from git by default, so refs are resolved against the repo whatever resolver is configured
	assert.Equal(t, "gobuild.gitRefResolver", fmt.Sprintf("%T", tl.(*compositeTool).VersionResolver))
}
