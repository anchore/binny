package tool

import (
	"context"
	"runtime"
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

			tt.wantErr(t, Check(store, nil, fakeTool{name: tt.toolName}, tt.resolvedVersion, VerifyConfig{
				VerifyXXH64Digest:  false,
				VerifySHA256Digest: tt.verifyDigest,
			}))
		})
	}
}

func Test_check_cacheLinkOtherFingerprint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc", param: "a"}, binny.VersionIntent{Want: "v1"}, store, cache, cfg))
	require.NoError(t, Check(store, cache, fakeTool{name: "tsc", param: "a"}, "v1", cfg))

	// same name and version, but the config changed: the link points at another entry
	require.ErrorContains(t, Check(store, cache, fakeTool{name: "tsc", param: "b"}, "v1", cfg), `"tsc"`)
}

func Test_check_cacheCopy(t *testing.T) {
	cache := newCacheForTest(t)
	store := newStoreForTest(t)
	cfg := VerifyConfig{VerifySHA256Digest: true}

	require.NoError(t, Install(context.Background(), fakeTool{name: "tsc"}, binny.VersionIntent{Want: "v1"}, store, nil, cfg))

	err := Check(store, cache, fakeTool{name: "tsc"}, "v1", cfg)
	require.ErrorContains(t, err, `"tsc"`)
	require.ErrorContains(t, err, "copy")
}
