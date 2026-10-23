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
