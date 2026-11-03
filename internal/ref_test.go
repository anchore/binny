package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGitRef(t *testing.T) {
	for _, want := range []string{"main", "feature/x", "pull/7", "abcdef1", "1905414228", "bun-v1.4.3", "nightly-2024-01-01"} {
		assert.True(t, IsGitRef(want), want)
	}
	for _, want := range []string{"", "latest", "v1.2.3", "1.2.3", "v0.0.0-20261015120000-abcdef123456", "2024"} {
		assert.False(t, IsGitRef(want), want)
	}
}

func TestIsExactVersion(t *testing.T) {
	for _, v := range []string{"v1.2.3", "1.2.3", "v1.2.3-rc.1", "v2.0.0+incompatible", "v0.0.0-20261015120000-abcdef123456", "bun-v1.4.3", "cli/v2.0.0", "abcdef1", "0123456789abcdef0123456789abcdef01234567"} {
		assert.True(t, IsExactVersion(v), v)
	}
	// a go prefix query (v1, v1.2) builds the newest match, so it moves like a branch does
	for _, v := range []string{"", "latest", "main", "v1", "v2", "v1.2", "1.2", "nightly", "nightly-2024-01-01", "release-v2"} {
		assert.False(t, IsExactVersion(v), v)
	}
}

func TestIsPlainVersion(t *testing.T) {
	for _, v := range []string{"v1.2.3", "1.2.3", "v1.2.3-rc.1", "v0.0.0-20261015120000-abcdef123456"} {
		assert.True(t, IsPlainVersion(v), v)
	}
	// each of these can name a branch
	for _, v := range []string{"", "latest", "v1.2", "deadbee", "bun-v1.4.3", "release/v1.2.3"} {
		assert.False(t, IsPlainVersion(v), v)
	}
}
