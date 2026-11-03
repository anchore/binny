package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_quoteOutput(t *testing.T) {
	got := quoteOutput([]byte("not found: \x1b[2J unknown revision\n"))
	assert.NotContains(t, got, "\x1b")
	assert.Contains(t, got, "unknown revision")

	long := make([]byte, maxErrorSize*2)
	for i := range long {
		long[i] = 'A'
	}
	assert.Less(t, len(quoteOutput(long)), maxErrorSize+10)
}
