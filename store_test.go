package binny

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_GetByName(t *testing.T) {
	tests := []struct {
		name      string
		storeRoot string
		toolName  string
		versions  []string
		want      []StoreEntry
	}{
		{
			name:      "empty",
			storeRoot: "testdata/store/empty",
			toolName:  "name",
		},
		{
			name:      "missing",
			storeRoot: "testdata/store/missing",
			toolName:  "name",
		},
		{
			name:      "empty request",
			storeRoot: "testdata/store/valid",
			toolName:  "",
		},
		{
			name:      "hit by name only",
			storeRoot: "testdata/store/valid",
			toolName:  "golangci-lint",
			want: []StoreEntry{
				{
					Name:             "golangci-lint",
					InstalledVersion: "v1.54.2",
					Digests:          "06c3715b43f4e92d0e9ec98ba8aa0f0c08c8963b2862ec130ec8e1c1ad9e1d1d",
					PathInRoot:       "golangci-lint",
				},
			},
		},
		{
			name:      "hit by name and exact version",
			storeRoot: "testdata/store/valid",
			toolName:  "golangci-lint",
			versions:  []string{"v1.54.2"},
			want: []StoreEntry{
				{
					Name:             "golangci-lint",
					InstalledVersion: "v1.54.2",
					Digests:          "06c3715b43f4e92d0e9ec98ba8aa0f0c08c8963b2862ec130ec8e1c1ad9e1d1d",
					PathInRoot:       "golangci-lint",
				},
			},
		},
		{
			name:      "hit by name and multiple versions",
			storeRoot: "testdata/store/valid",
			toolName:  "golangci-lint",
			versions:  []string{"v1.54.1", "v1.54.2", "v1.54.3"},
			want: []StoreEntry{
				{
					Name:             "golangci-lint",
					InstalledVersion: "v1.54.2",
					Digests:          "06c3715b43f4e92d0e9ec98ba8aa0f0c08c8963b2862ec130ec8e1c1ad9e1d1d",
					PathInRoot:       "golangci-lint",
				},
			},
		},
		{
			name:      "miss by bad version",
			storeRoot: "testdata/store/valid",
			toolName:  "golangci-lint",
			versions:  []string{"v1.54.3"},
		},
		{
			name:      "miss",
			storeRoot: "testdata/store/valid",
			toolName:  "best-tool", // bogus
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, err := NewStore(tt.storeRoot)
			for i := range tt.want {
				tt.want[i].root = tt.storeRoot
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, str.GetByName(tt.toolName, tt.versions...))
		})
	}
}

func TestStore_Entries(t *testing.T) {
	tests := []struct {
		name      string
		storeRoot string
		want      []StoreEntry
	}{
		{
			name:      "empty",
			storeRoot: "testdata/store/empty",
		},
		{
			name:      "missing",
			storeRoot: "testdata/store/missing",
		},
		{
			name:      "valid store",
			storeRoot: "testdata/store/valid",
			want: []StoreEntry{
				{
					root:             "testdata/store/valid",
					Name:             "quill",
					InstalledVersion: "v0.4.1",
					Digests:          "56656877b8b0e0c06a96e83df12157565b91bb8f6b55c4051c0466edf0f08b85",
					PathInRoot:       "quill",
				},
				{
					root:             "testdata/store/valid",
					Name:             "chronicle",
					InstalledVersion: "v0.7.0",
					Digests:          "e011590e5d55188e03a2fd58524853ddacd23ec2e5d58535e061339777c4043f",
					PathInRoot:       "chronicle",
				},
				{
					root:             "testdata/store/valid",
					Name:             "gosimports",
					InstalledVersion: "v0.3.8",
					Digests:          "9e5837236320efadb7a94675866cbd95e7a9716d635f3863603859698a37591a",
					PathInRoot:       "gosimports",
				},
				{
					root:             "testdata/store/valid",
					Name:             "glow",
					InstalledVersion: "v1.5.1",
					Digests:          "c6f05b9383f97fbb6fb2bb84b87b3b99ed7a1708d8a1634ff66d5bff8180f3b0",
					PathInRoot:       "glow",
				},
				{
					root:             "testdata/store/valid",
					Name:             "goreleaser",
					InstalledVersion: "v1.20.0",
					Digests:          "307dd15253ab292a57dff221671659f3133593df485cc08fdd8158d63222bb16",
					PathInRoot:       "goreleaser",
				},
				{
					root:             "testdata/store/valid",
					Name:             "golangci-lint",
					InstalledVersion: "v1.54.2",
					Digests:          "06c3715b43f4e92d0e9ec98ba8aa0f0c08c8963b2862ec130ec8e1c1ad9e1d1d",
					PathInRoot:       "golangci-lint",
				},
				{
					root:             "testdata/store/valid",
					Name:             "bouncer",
					InstalledVersion: "v0.4.0",
					Digests:          "de42a2453c8e9b2587358c1f244a5cc0091c71385126f0fa3c0b3aec0feeaa4d",
					PathInRoot:       "bouncer",
				},
				{
					root:             "testdata/store/valid",
					Name:             "task",
					InstalledVersion: "v3.29.1",
					Digests:          "8d92c81f07960c5363a1f424e88dd4b64a1dd4251378d53873fa65ea1aab271b",
					PathInRoot:       "task",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, err := NewStore(tt.storeRoot)
			for i := range tt.want {
				tt.want[i].root = tt.storeRoot
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, str.Entries())
		})
	}
}

func TestStore_AddTool(t *testing.T) {
	// setup
	outsideRoot := t.TempDir()

	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	// utils

	createFile := func(path, contents string) {
		fh, err := os.Create(path)
		require.NoError(t, err)
		_, err = fh.WriteString(contents)
		require.NoError(t, err)
	}

	assertStoreHasString := func(content string) {
		storeStatePath := filepath.Join(store.root, ".binny.state.json")
		contents, err := os.ReadFile(storeStatePath)
		require.NoError(t, err)
		assert.Contains(t, string(contents), content)
	}

	// case 1: add a tool /////////////////////////////////////////////////

	tool1OutsideRoot := filepath.Join(outsideRoot, "tool-1-exists")

	// note: this is without a newline character
	tool1ExpectedSha := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	createFile(tool1OutsideRoot, "hello world")

	tool1 := "tool-1"
	tool1Intent := VersionIntent{
		Want:       "v0.1.0",
		Constraint: "<0.2.0",
	}

	// add the first tool
	require.NoError(t, store.AddTool(tool1, tool1Intent.Want, tool1OutsideRoot))

	// check that digest is in the store state
	assertStoreHasString(tool1ExpectedSha)

	// case 2: add a tool that does not exist /////////////////////////////////////////////////

	// add a tool without an existing path (expect err)
	tool2OutsideRoot := filepath.Join(outsideRoot, "tool-2-does-not-exist")
	tool2 := "tool-2"
	tool2Intent := VersionIntent{
		Want:       "v0.1.0",
		Constraint: "<0.2.0",
	}

	// add the second tool
	require.Error(t, store.AddTool(tool2, tool2Intent.Want, tool2OutsideRoot))

	// create the path and add it again
	tool2ExpectedSha := "6c607e095402c38173aeb767b4980455249993c4f40450528a3a99ea67f75c35"
	createFile(tool2OutsideRoot, "nope hello world")

	require.NoError(t, store.AddTool(tool2, tool2Intent.Want, tool2OutsideRoot))

	assertStoreHasString(tool1ExpectedSha)
	assertStoreHasString(tool2ExpectedSha)

	// case 3: replace tool 1 /////////////////////////////////////////////////
	createFile(tool1OutsideRoot, "replace hello world")
	expectedReplaceSha := "781ac3727fddd802c1f7f540b4cd91398c034dd0bd1eafad808561c189dc0501"
	require.NoError(t, store.AddTool(tool1, tool1Intent.Want, tool1OutsideRoot))

	assertStoreHasString(expectedReplaceSha)
	assertStoreHasString(tool2ExpectedSha)

	// there should only be tool1 and 2, no duplicates
	assert.Len(t, store.Entries(), 2)
}

func TestStore_Entries_IsACopy(t *testing.T) {
	store, err := NewStore("testdata/store/valid")
	require.NoError(t, err)

	gotEntries := store.Entries()

	assert.Equal(t, store.entries, gotEntries)

	for idx := range store.entries {
		assert.Equal(t, &store.entries[idx], &gotEntries[idx])
	}

}
