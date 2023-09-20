package tool

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

func Test_check_sha256(t *testing.T) {
	tests := []struct {
		name            string
		storeRoot       string
		resolvedVersion string
		toolName        string
		verifyDigest    bool
		wantErr         require.ErrorAssertionFunc
	}{
		{
			name:            "valid (sha256)",
			storeRoot:       "testdata/store/valid-sha256-only",
			resolvedVersion: "v1.54.2",
			verifyDigest:    true,
			toolName:        "golangci-lint",
		},
		{
			name:            "different version resolver config",
			wantErr:         require.Error,
			storeRoot:       "testdata/store/valid",
			resolvedVersion: "v1.54.3",
			verifyDigest:    true,
			toolName:        "golangci-lint",
		},
		{
			name:            "different stored sha",
			wantErr:         require.Error,
			storeRoot:       "testdata/store/stale",
			resolvedVersion: "v0.4.1",
			verifyDigest:    true,
			toolName:        "quill",
		},
		{
			name:            "ignore different stored sha",
			wantErr:         require.NoError,
			storeRoot:       "testdata/store/stale",
			resolvedVersion: "v0.4.1",
			verifyDigest:    false,
			toolName:        "quill",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			store, err := binny.NewStore(tt.storeRoot)
			require.NoError(t, err)

			tt.wantErr(t, Check(tt.toolName, tt.resolvedVersion, store, VerifyConfig{
				VerifyXXH64Digest:  false,
				VerifySHA256Digest: tt.verifyDigest,
			}))
		})
	}
}
