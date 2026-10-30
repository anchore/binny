package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/acarl005/stripansi"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

type traitAssertion func(tb testing.TB, storeRoot, stdout, stderr string, rc int)

func assertFileOutput(tb testing.TB, path string, assertions ...traitAssertion) traitAssertion {
	tb.Helper()

	return func(tb testing.TB, storeRoot, _, stderr string, rc int) {
		content, err := os.ReadFile(path)
		require.NoError(tb, err)
		contentStr := string(content)

		for _, assertion := range assertions {
			// treat the file content as stdout
			assertion(tb, storeRoot, contentStr, stderr, rc)
		}
	}
}

func assertJson(tb testing.TB, _, stdout, _ string, _ int) {
	tb.Helper()
	var data any

	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		tb.Errorf("expected to find a JSON report, but was unmarshalable: %+v", err)
	}
}

func assertLoggingLevel(level string) traitAssertion {
	// match examples:
	//  "[0000]  INFO"
	//  "[0012] DEBUG"
	logPattern := regexp.MustCompile(`(?m)^\[\d\d\d\d\]\s+` + strings.ToUpper(level))
	return func(tb testing.TB, _, _, stderr string, _ int) {
		tb.Helper()
		if !logPattern.MatchString(stripansi.Strip(stderr)) {
			tb.Errorf("output did not indicate the %q logging level", level)
		}
	}
}

func assertNotInOutput(data string) traitAssertion {
	return func(tb testing.TB, _, stdout, stderr string, _ int) {
		tb.Helper()
		if strings.Contains(stripansi.Strip(stderr), data) {
			tb.Errorf("data=%q was found in stderr, but should not have been there", data)
		}
		if strings.Contains(stripansi.Strip(stdout), data) {
			tb.Errorf("data=%q was found in stdout, but should not have been there", data)
		}
	}
}

func assertNoStderr(tb testing.TB, _, _, stderr string, _ int) {
	tb.Helper()
	if len(stderr) > 0 {
		tb.Errorf("expected stderr to be empty, but wasn't")
		if showOutput != nil && *showOutput {
			tb.Errorf("STDERR:%s", stderr)
		}
	}
}

func assertInOutput(data string) traitAssertion {
	return func(tb testing.TB, _, stdout, stderr string, _ int) {
		tb.Helper()
		stdout = stripansi.Strip(stdout)
		stderr = stripansi.Strip(stderr)
		if !strings.Contains(stdout, data) && !strings.Contains(stderr, data) {
			tb.Errorf("data=%q was NOT found in any output, but should have been there", data)
			if showOutput != nil && *showOutput {
				tb.Errorf("STDOUT:%s\nSTDERR:%s", stdout, stderr)
			}
		}
	}
}

func assertStdoutLengthGreaterThan(length uint) traitAssertion {
	return func(tb testing.TB, _, stdout, _ string, _ int) {
		tb.Helper()
		if uint(len(stdout)) < length {
			tb.Errorf("not enough output (expected at least %d, got %d)", length, len(stdout))
		}
	}
}

func assertFailingReturnCode(tb testing.TB, _, _, _ string, rc int) {
	tb.Helper()
	if rc == 0 {
		tb.Errorf("expected a failure but got rc=%d", rc)
	}
}

func assertSuccessfulReturnCode(tb testing.TB, _, _, _ string, rc int) {
	tb.Helper()
	if rc != 0 {
		tb.Errorf("expected no failure but got rc=%d", rc)
	}
}

func assertFileInStoreExists(file string) traitAssertion {
	return func(tb testing.TB, storeRoot, _, _ string, _ int) {
		tb.Helper()
		path := filepath.Join(storeRoot, file)
		if _, err := os.Stat(path); err != nil {
			tb.Errorf("expected file to exist %s", path)
		}
	}
}

// assertDirEntryCount checks the number of entries in a dir given relative to the store root.
func assertDirEntryCount(dir string, count int) traitAssertion {
	return func(tb testing.TB, storeRoot, _, _ string, _ int) {
		tb.Helper()
		entries, err := os.ReadDir(filepath.Join(storeRoot, dir))
		require.NoError(tb, err)
		require.Len(tb, entries, count)
	}
}

// assertLinkedInStore checks that the tool in the store is a symlink, as an install through the cache leaves it.
func assertLinkedInStore(tool string) traitAssertion {
	return func(tb testing.TB, storeRoot, _, _ string, _ int) {
		tb.Helper()
		fi, err := os.Lstat(filepath.Join(storeRoot, tool))
		require.NoError(tb, err)
		require.NotZero(tb, fi.Mode()&os.ModeSymlink, "%q should be linked into the cache", tool)
	}
}

// assertCacheListHasOneEntry checks `binny cache list -o json` output for exactly one entry of the tool, at the version.
func assertCacheListHasOneEntry(name, version string) traitAssertion {
	return func(tb testing.TB, _, stdout, _ string, _ int) {
		tb.Helper()
		var doc struct {
			Entries []struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"entries"`
		}
		require.NoError(tb, json.Unmarshal([]byte(stdout), &doc))

		var versions []string
		for _, e := range doc.Entries {
			if e.Name == name {
				versions = append(versions, e.Version)
			}
		}
		require.Equal(tb, []string{version}, versions)
	}
}

// captureStdout keeps a step's stdout for a later step to compare with assertStdoutEquals.
func captureStdout(out *string) traitAssertion {
	return func(_ testing.TB, _, stdout, _ string, _ int) {
		*out = stdout
	}
}

func assertStdoutEquals(want *string) traitAssertion {
	return func(tb testing.TB, _, stdout, _ string, _ int) {
		tb.Helper()
		require.NotEmpty(tb, *want)
		if d := cmp.Diff(*want, stdout); d != "" {
			tb.Errorf("unexpected output (-want +got):\n%s", d)
		}
	}
}

func assertManagedToolOutput(tool string, args []string, expectedStdout string) traitAssertion {
	return func(tb testing.TB, storeRoot, _, _ string, _ int) {
		tb.Helper()

		path := filepath.Join(storeRoot, tool)
		cmd := exec.Command(path, args...)

		gotStdout, _, err := runCommand(cmd, nil)
		require.NoError(tb, err)

		if d := cmp.Diff(expectedStdout, gotStdout); d != "" {
			tb.Errorf("unexpected output (-want +got):\n%s", d)
		}
	}
}
