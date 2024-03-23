package command

import (
	"errors"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type renderListTableTest struct {
	name     string
	statuses []toolStatus
}

func renderListTableTestCases() []renderListTableTest {
	return []renderListTableTest{
		{
			name:     "empty",
			statuses: []toolStatus{},
		},
		{
			name: "has update",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "latest",
					ResolvedVersion:  "v1.0.0",
					InstalledVersion: "v0.105.1",
					Constraint:       "<= v1.0.0",
					IsInstalled:      true,
					HashIsValid:      true,
					Error:            nil,
				},
			},
		},
		{
			name: "invalid hash",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "v0.105.1",
					ResolvedVersion:  "v0.105.1",
					InstalledVersion: "v0.105.1",
					Constraint:       "<= v1.0.0",
					IsInstalled:      true,
					HashIsValid:      false,
					Error:            nil,
				},
			},
		},
		{
			name: "error",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "latest",
					ResolvedVersion:  "v1.0.0",
					InstalledVersion: "v1.0.0",
					Constraint:       "<= v1.0.0",
					IsInstalled:      true,
					HashIsValid:      false,
					Error:            errors.New("something is wrong"),
				},
			},
		},
		{
			name: "unknown wanted version",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "?",
					ResolvedVersion:  "v1.0.1",
					InstalledVersion: "v1.0.0",
					Constraint:       "<= v1.0.1",
					IsInstalled:      true,
					HashIsValid:      false,
					Error:            nil,
				},
			},
		},
		{
			name: "not installed",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "latest",
					ResolvedVersion:  "v1.0.0",
					InstalledVersion: "",
					Constraint:       "<= v1.0.0",
					IsInstalled:      false,
					HashIsValid:      true,
					Error:            nil,
				},
			},
		},
		{
			name: "no update",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "latest",
					ResolvedVersion:  "v1.0.0",
					InstalledVersion: "v1.0.0",
					Constraint:       "<= v1.0.0",
					IsInstalled:      true,
					HashIsValid:      true,
					Error:            nil,
				},
			},
		},
		{
			name: "sort by name",
			statuses: []toolStatus{
				{
					Name:             "syft",
					WantVersion:      "latest",
					ResolvedVersion:  "v1.0.0",
					InstalledVersion: "v0.105.1",
					Constraint:       "<= v1.0.0",
					IsInstalled:      true,
					HashIsValid:      true,
					Error:            nil,
				},
				{
					Name:             "grype",
					WantVersion:      "v0.74.0",
					ResolvedVersion:  "v0.74.0",
					InstalledVersion: "v0.53.0",
					Constraint:       "<= v1.0.0",
					IsInstalled:      true,
					HashIsValid:      true,
					Error:            nil,
				},
			},
		},
	}
}

func Test_renderListTable(t *testing.T) {

	for _, tt := range renderListTableTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			got := renderListTable(tt.statuses)
			snaps.MatchSnapshot(t, got)
		})
	}
}

func Test_renderListUpdatesTable(t *testing.T) {

	for _, tt := range renderListTableTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			got := renderListUpdatesTable(tt.statuses)
			snaps.MatchSnapshot(t, got)
		})
	}
}

func Test_renderListJSON(t *testing.T) {

	t.Run("updates", func(t *testing.T) {
		for _, tt := range renderListTableTestCases() {
			t.Run(tt.name, func(t *testing.T) {
				got, err := renderListJSON(tt.statuses, true, "")
				require.NoError(t, err)
				snaps.MatchSnapshot(t, got)
			})
		}
	})

	t.Run("no updates", func(t *testing.T) {
		for _, tt := range renderListTableTestCases() {
			t.Run(tt.name, func(t *testing.T) {
				got, err := renderListJSON(tt.statuses, false, "")
				require.NoError(t, err)
				snaps.MatchSnapshot(t, got)
			})
		}
	})

	t.Run("jq", func(t *testing.T) {
		testStatuses := []toolStatus{
			{
				Name:             "syft",
				WantVersion:      "latest",
				ResolvedVersion:  "v1.0.0",
				InstalledVersion: "v0.105.1",
				Constraint:       "<= v1.0.0",
				IsInstalled:      true,
				HashIsValid:      true,
				Error:            nil,
			},
			{
				Name:             "grype",
				WantVersion:      "v0.74.0",
				ResolvedVersion:  "v0.74.0",
				InstalledVersion: "v0.53.0",
				Constraint:       "<= v1.0.0",
				IsInstalled:      true,
				HashIsValid:      true,
				Error:            nil,
			},
		}

		tests := []struct {
			name     string
			statuses []toolStatus
			jq       string
			wantErr  require.ErrorAssertionFunc
		}{
			{
				name:     "show keys",
				statuses: testStatuses,
				jq:       "keys",
			},
			{
				name:     "bad jq expression",
				statuses: testStatuses,
				jq:       "shdjfkshf",
				wantErr:  require.Error,
			},
			{
				name:     "filter by name",
				statuses: testStatuses,
				jq:       ".tools[] | select(.name == \"syft\")",
			},
			{
				name:     "raw scalar values",
				statuses: testStatuses,
				jq:       ".tools[].wantVersion",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.wantErr == nil {
					tt.wantErr = require.NoError
				}
				got, err := renderListJSON(tt.statuses, false, tt.jq)
				tt.wantErr(t, err)
				snaps.MatchSnapshot(t, got)
			})
		}
	})

}

func Test_summarizeVersion(t *testing.T) {

	tests := []struct {
		name string
		v    string
		want string
	}{
		{
			name: "empty",
			v:    "",
			want: "",
		},
		{
			name: "semver",
			v:    "v1.0.0",
			want: "v1.0.0",
		},
		{
			name: "commit-sha",
			v:    "250ca084c8ffd28fad8bf9d8725e2b0d5b8b11e2",
			want: "250ca08",
		},
		{
			name: "40-char non-sha",
			v:    "SOMETHING-ffd28fad8bf9d8725e2b0d5b8b11e2",
			want: "SOMETHING-ffd28fad8bf9d8725e2b0d5b8b11e2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, summarizeGitVersion(tt.v))
		})
	}
}
