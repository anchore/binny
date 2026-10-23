package gobuild

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/module"

	"github.com/anchore/binny/internal/log"
)

// getSource obtains the source code for the given module at the specified version.
// It returns the working directory containing the source, a cleanup function, and any error.
func getSource(ctx context.Context, module, version, repoURL string, mode SourceMode) (workDir string, cleanup func(), err error) {
	switch normalizeSourceMode(mode) {
	case SourceModeGit:
		return cloneSource(ctx, module, version, repoURL)
	case SourceModeGoProxy:
		return downloadFromProxy(ctx, module, version)
	default:
		return "", nil, fmt.Errorf("unknown source mode: %q", mode)
	}
}

// normalizeSourceMode converts various source mode strings to canonical form.
func normalizeSourceMode(mode SourceMode) SourceMode {
	switch strings.ToLower(string(mode)) {
	case "git":
		return SourceModeGit
	case "goproxy", "go-proxy", "go proxy", "proxy":
		return SourceModeGoProxy
	default:
		return mode
	}
}

// cloneSource clones the repository for the given module at the specified version.
func cloneSource(ctx context.Context, module, version, repoURL string) (string, func(), error) {
	lgr := log.FromContext(ctx)

	// derive repo URL if not provided
	if repoURL == "" {
		var err error
		repoURL, err = DeriveRepoURL(module)
		if err != nil {
			return "", nil, fmt.Errorf("failed to derive repo URL: %w", err)
		}
	}

	// create a temp directory for the clone
	tempDir, err := os.MkdirTemp("", "binny-gobuild-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	cleanup := func() {
		if err := os.RemoveAll(tempDir); err != nil {
			lgr.WithFields("dir", tempDir).Warn("failed to clean up temp directory")
		}
	}

	version = gitRev(version)
	lgr.WithFields("repo", redactCredentials(repoURL, repoURL), "version", version).Debug("cloning repository")

	attempt := "shallow clone"
	if fullSHAPattern.MatchString(version) {
		// a full commit (what a tracked git ref resolves to) can be fetched on its own, without history
		attempt = "fetch by commit"
		err = fetchCommit(ctx, repoURL, version, tempDir)
	} else {
		// shallow clone with --branch works for tags and branches
		err = shallowClone(ctx, repoURL, version, tempDir)
	}

	if err != nil {
		// try full clone + checkout (handles short commit hashes, and servers that refuse fetching by sha)
		firstErr := err
		lgr.WithFields("repo", redactCredentials(repoURL, repoURL), "version", version, "error", firstErr).Debug(attempt + " failed, trying full clone")
		if err := resetDir(tempDir); err != nil {
			cleanup()
			return "", nil, err
		}
		if err := fullCloneAndCheckout(ctx, repoURL, version, tempDir); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("failed to clone repository: %w (after %s failed: %v)", err, attempt, firstErr)
		}
	}

	// for modules with subpaths (e.g., github.com/owner/repo/subpkg), the workDir is still the repo root
	return tempDir, cleanup, nil
}

// gitRev maps a version to something git can check out. A go-proxy pseudo-version (e.g. the resolved form of a
// branch) is not a git ref, but it embeds the commit it points to.
func gitRev(version string) string {
	if module.IsPseudoVersion(version) {
		if rev, err := module.PseudoVersionRev(version); err == nil {
			return rev
		}
	}
	return version
}

// fetchCommitTimeout bounds fetching a single commit, so a stalled host or an ssh prompt can't hang the install.
const fetchCommitTimeout = 10 * time.Minute

// gitCommand builds a git command that fails instead of prompting for https credentials, with untranslated messages
// (fullCloneAndCheckout reads them). ssh prompts (unknown host key, key passphrase) aren't covered, forcing ssh
// BatchMode would override a user's core.sshCommand, so callers that talk to a remote bound the time instead.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return cmd
}

// shallowClone performs a shallow clone (depth=1) for a specific tag or branch.
func shallowClone(ctx context.Context, repoURL, version, destDir string) error {
	args := []string{"clone", "--depth", "1", "--branch", version, "--", repoURL, destDir}
	log.Trace("running: git " + redactCredentials(strings.Join(args, " "), repoURL))

	output, err := gitCommand(ctx, "", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v\nOutput: %s", err, redactCredentials(string(output), repoURL))
	}
	return nil
}

// fetchCommit fetches a single commit (depth=1) into an empty repo and checks it out.
func fetchCommit(ctx context.Context, repoURL, sha, destDir string) error {
	ctx, cancel := context.WithTimeout(ctx, fetchCommitTimeout)
	defer cancel()

	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", "--", repoURL, sha},
		{"checkout", "-q", "--detach", "FETCH_HEAD"},
	} {
		log.FromContext(ctx).Trace("running: git " + redactCredentials(strings.Join(args, " "), repoURL))
		if output, err := gitCommand(ctx, destDir, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s failed: %v\nOutput: %s", args[0], err, redactCredentials(string(output), repoURL))
		}
	}
	return nil
}

// resetDir empties a directory so a failed clone attempt doesn't block the next one.
func resetDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// fullCloneAndCheckout clones the full history and then checks out the specified version. It is the fallback when
// fetchCommit or shallowClone fails (e.g. a short commit hash, or a server that refuses fetching by sha). It tries a
// partial clone first, where blobs are only fetched for the commit that gets checked out.
func fullCloneAndCheckout(ctx context.Context, repoURL, version, destDir string) error {
	err := cloneAndCheckout(ctx, repoURL, version, destDir, "--filter=blob:none")
	if err == nil || !strings.Contains(err.Error(), "promisor remote") {
		return err
	}
	// a partial clone fetches the blobs it needs by id at checkout, which a server that refuses unadvertised objects
	// also refuses; a plain clone sends them up front
	log.FromContext(ctx).WithFields("error", err).Debug("partial clone failed, trying a plain clone")
	if err := resetDir(destDir); err != nil {
		return err
	}
	return cloneAndCheckout(ctx, repoURL, version, destDir)
}

func cloneAndCheckout(ctx context.Context, repoURL, version, destDir string, cloneFlags ...string) error {
	cloneArgs := append(append([]string{"clone"}, cloneFlags...), "--", repoURL, destDir)
	log.Trace("running: git " + redactCredentials(strings.Join(cloneArgs, " "), repoURL))

	output, err := gitCommand(ctx, "", cloneArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v\nOutput: %s", err, redactCredentials(string(output), repoURL))
	}

	// checkout the specific version
	checkoutArgs := []string{"checkout", version}
	log.Trace("running: git " + redactCredentials(strings.Join(checkoutArgs, " "), repoURL))

	output, err = gitCommand(ctx, destDir, checkoutArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %v\nOutput: %s", err, redactCredentials(string(output), repoURL))
	}
	return nil
}

// downloadFromProxy downloads the source code via the go proxy using "go mod download".
func downloadFromProxy(ctx context.Context, module, version string) (string, func(), error) {
	lgr := log.FromContext(ctx)

	spec := module + "@" + version

	lgr.WithFields("module", module, "version", version).Debug("downloading source from go proxy")

	// run go mod download -json to get the source location
	args := []string{"mod", "download", "-json", spec}
	log.Trace("running: go " + strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("go mod download failed: %v\nOutput: %s", err, output)
	}

	// parse the JSON output
	var downloadInfo struct {
		Dir     string `json:"Dir"`
		Version string `json:"Version"`
		Error   string `json:"Error"`
	}
	if err := json.Unmarshal(output, &downloadInfo); err != nil {
		return "", nil, fmt.Errorf("failed to parse go mod download output: %w", err)
	}

	if downloadInfo.Error != "" {
		return "", nil, fmt.Errorf("go mod download error: %s", downloadInfo.Error)
	}

	if downloadInfo.Dir == "" {
		return "", nil, fmt.Errorf("go mod download did not return a directory")
	}

	// the go mod cache is read-only, so we need to copy the source to a temp directory
	tempDir, err := os.MkdirTemp("", "binny-gobuild-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	cleanup := func() {
		if err := os.RemoveAll(tempDir); err != nil {
			lgr.WithFields("dir", tempDir).Warn("failed to clean up temp directory")
		}
	}

	// copy the source to the temp directory
	if err := copyDir(downloadInfo.Dir, tempDir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to copy source: %w", err)
	}

	return tempDir, cleanup, nil
}

// copyDir recursively copies a directory tree, handling symlinks.
// Directories are created with write permissions to allow file creation inside them,
// which is necessary when copying from read-only sources like the go module cache.
// Uses os.Root for safe, race-free filesystem operations within the source and destination directories.
func copyDir(src, dst string) error {
	srcRoot, err := os.OpenRoot(src)
	if err != nil {
		return fmt.Errorf("failed to open source directory: %w", err)
	}
	defer srcRoot.Close()

	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("failed to open destination directory: %w", err)
	}
	defer dstRoot.Close()

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// compute relative path for root-scoped operations
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// skip the root directory itself
		if relPath == "." {
			return nil
		}

		// handle symlinks - read target and recreate in destination.
		// Note: os.Root.Readlink/Symlink require Go 1.25+, so we use standard functions here.
		// This is safe because we're copying from controlled sources (go module cache or our git clone)
		// to a temp directory we created.
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			dstPath := filepath.Join(dst, relPath)
			return os.Symlink(target, dstPath) //nolint:gosec // G122: destination is our temp directory
		}

		if info.IsDir() {
			// ensure directories are writable so we can create files inside them
			// (go module cache directories are read-only)
			perm := info.Mode().Perm() | 0200 // add user write permission
			return dstRoot.Mkdir(relPath, perm)
		}

		// copy file using root-scoped operations
		return copyFileWithRoot(srcRoot, dstRoot, relPath, info.Mode().Perm()|0200)
	})
}

// copyFileWithRoot copies a single file using root-scoped file handles for safe operations.
func copyFileWithRoot(srcRoot, dstRoot *os.Root, relPath string, perm os.FileMode) error {
	srcFile, err := srcRoot.Open(relPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := dstRoot.OpenFile(relPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
