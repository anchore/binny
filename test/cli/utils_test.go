package cli

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

var showOutput = flag.Bool("show-output", false, "show stdout and stderr for failing tests")

func logOutputOnFailure(t testing.TB, cmd *exec.Cmd, stdout, stderr string) {
	if t.Failed() && showOutput != nil && *showOutput {
		t.Log("STDOUT:\n", stdout)
		t.Log("STDERR:\n", stderr)
		t.Log("COMMAND:", strings.Join(cmd.Args, " "))
	}
}
func runBinny(t testing.TB, env map[string]string, args ...string) (*exec.Cmd, string, string) {
	return runBinnyCommand(t, env, true, args...)
}

func runBinnyCommand(t testing.TB, env map[string]string, expectError bool, args ...string) (*exec.Cmd, string, string) {
	cancel := make(chan bool, 1)
	defer func() {
		cancel <- true
	}()

	cmd := getBinnyCommand(t, args...)
	if env == nil {
		env = make(map[string]string)
	}

	timeout := func() {
		select {
		case <-cancel:
			return
		case <-time.After(60 * time.Second):
		}

		if cmd != nil && cmd.Process != nil {
			// get a stack trace printed
			err := cmd.Process.Signal(syscall.SIGABRT)
			if err != nil {
				t.Errorf("error aborting: %+v", err)
			}
		}
	}

	go timeout()

	stdout, stderr, err := runCommand(cmd, env)

	if !expectError && err != nil && stdout == "" {
		t.Errorf("error running binny: %+v", err)
		t.Errorf("STDOUT: %s", stdout)
		t.Errorf("STDERR: %s", stderr)

		// this probably indicates a timeout... lets run it again with more verbosity to help debug issues
		args = append(args, "-vv")
		cmd = getBinnyCommand(t, args...)

		go timeout()
		stdout, stderr, err = runCommand(cmd, env)

		if err != nil {
			t.Errorf("error rerunning binny: %+v", err)
			t.Errorf("STDOUT: %s", stdout)
			t.Errorf("STDERR: %s", stderr)
		}
	}

	return cmd, stdout, stderr
}

func runCommand(cmd *exec.Cmd, env map[string]string) (string, string, error) {
	if env != nil {
		cmd.Env = append(os.Environ(), envMapToSlice(env)...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// ignore errors since this may be what the test expects
	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

func envMapToSlice(env map[string]string) (envList []string) {
	for key, val := range env {
		if key == "" {
			continue
		}
		envList = append(envList, fmt.Sprintf("%s=%s", key, val))
	}
	return
}

func getBinnyCommand(t testing.TB, args ...string) *exec.Cmd {
	return exec.Command(getBinaryLocation(t), args...)
}

func getBinaryLocation(t testing.TB) string {
	if os.Getenv("BINNY_BINARY_LOCATION") != "" {
		// BINNY_BINARY_LOCATION is the absolute path to the snapshot binary
		return os.Getenv("BINNY_BINARY_LOCATION")
	}
	return getBinaryLocationByOS(t, runtime.GOOS)
}

func getBinaryLocationByOS(t testing.TB, goOS string) string {
	// note: for amd64 we need to update the snapshot location with the v1 suffix
	// see : https://goreleaser.com/customization/build/#why-is-there-a-_v1-suffix-on-amd64-builds
	archPath := runtime.GOARCH
	if runtime.GOARCH == "amd64" {
		archPath = fmt.Sprintf("%s_v1", archPath)
	}
	// note: there is a subtle - vs _ difference between these versions
	switch goOS {
	case "darwin", "linux":
		return filepath.Join(repoRoot(t), "snapshot", fmt.Sprintf("%s-build_%s_%s", goOS, goOS, archPath), "binny")
	default:
		t.Fatalf("unsupported OS: %s", runtime.GOOS)
	}
	return ""
}

func repoRoot(t testing.TB) string {
	t.Helper()
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("unable to find repo root dir: %+v", err)
	}
	absRepoRoot, err := filepath.Abs(strings.TrimSpace(string(root)))
	if err != nil {
		t.Fatal("unable to get abs path to repo root:", err)
	}
	return absRepoRoot
}

func testRetryIntervals(done <-chan struct{}) <-chan time.Duration {
	return exponentialBackoffDurations(250*time.Millisecond, 4*time.Second, 2, done)
}

func exponentialBackoffDurations(minDuration, maxDuration time.Duration, step float64, done <-chan struct{}) <-chan time.Duration {
	sleepDurations := make(chan time.Duration)
	go func() {
		defer close(sleepDurations)
	retryLoop:
		for attempt := 0; ; attempt++ {
			duration := exponentialBackoffDuration(minDuration, maxDuration, step, attempt)

			select {
			case sleepDurations <- duration:
				break
			case <-done:
				break retryLoop
			}

			if duration == maxDuration {
				break
			}
		}
	}()
	return sleepDurations
}

func exponentialBackoffDuration(minDuration, maxDuration time.Duration, step float64, attempt int) time.Duration {
	duration := time.Duration(float64(minDuration) * math.Pow(step, float64(attempt)))
	if duration < minDuration {
		return minDuration
	} else if duration > maxDuration {
		return maxDuration
	}
	return duration
}
