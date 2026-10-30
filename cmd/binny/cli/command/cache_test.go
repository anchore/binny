package command

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"

	"github.com/anchore/binny"
)

type renderCacheListTest struct {
	name    string
	entries []cacheEntryInfo
}

func renderCacheListTestCases() []renderCacheListTest {
	return []renderCacheListTest{
		{
			name: "empty",
		},
		{
			name: "entries",
			entries: []cacheEntryInfo{
				{Name: "binny", Version: "v0.7.0", Fingerprint: "0123456789abcdef", Size: 12_345_678, Path: "/cache/binny/binny/v0.7.0/0123456789abcdef"},
				{Name: "binny", Version: "v0.8.0", Fingerprint: "0123456789abcdef", Size: 900, Path: "/cache/binny/binny/v0.8.0/0123456789abcdef"},
				{Name: "tsc", Version: "v5.0.0", Fingerprint: "fedcba9876543210", Size: 2048, Path: "/cache/binny/tsc/v5.0.0/fedcba9876543210"},
			},
		},
	}
}

func Test_renderCacheListJSON(t *testing.T) {
	for _, tt := range renderCacheListTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCacheListJSON(tt.entries, "")
			require.NoError(t, err)
			snaps.MatchSnapshot(t, got)
		})
	}
}

func Test_renderCacheListTable(t *testing.T) {
	for _, tt := range renderCacheListTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			snaps.MatchSnapshot(t, renderCacheListTable(tt.entries))
		})
	}
}

func Test_parseCacheRefs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []binny.CacheRef
		wantErr require.ErrorAssertionFunc
	}{
		{name: "name", args: []string{"binny"}, want: []binny.CacheRef{{Name: "binny"}}},
		{name: "name and version", args: []string{"binny@v0.7.0"}, want: []binny.CacheRef{{Name: "binny", Version: "v0.7.0"}}},
		{name: "several", args: []string{"binny", "tsc@v5.0.0"}, want: []binny.CacheRef{{Name: "binny"}, {Name: "tsc", Version: "v5.0.0"}}},
		{name: "missing name", args: []string{"@v0.7.0"}, wantErr: require.Error},
		{name: "missing version", args: []string{"binny@"}, wantErr: require.Error},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := parseCacheRefs(tt.args)
			tt.wantErr(t, err)
			if err == nil {
				require.Equal(t, tt.want, got)
			}
		})
	}
}
