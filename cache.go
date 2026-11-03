package binny

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

// Cache is a per-user directory of installed tools that stores link to, so a tool is downloaded or built once per
// machine instead of once per checkout. Each entry lives at <root>/<name>/<version>/<fingerprint>/ and is never
// changed once it is in place: installs stage under <root>/.tmp/ and rename into place, and the first rename wins.
type Cache struct {
	root string
}

// CacheEntry is one installed tool in the cache, with the metadata it recorded about itself.
type CacheEntry struct {
	Name        string
	Version     string
	Fingerprint string
	// Dir is the absolute path of the entry
	Dir string
	cacheEntryMetadata
}

// cacheEntryMetadata is what each entry records about itself in its entry file, so there is no cache-wide state.
type cacheEntryMetadata struct {
	// PathInDir is the binary's path relative to the entry dir
	PathInDir string `json:"path"`
	// KeepArchiveFiles is set when the entry holds the whole install dir, not only the binary
	KeepArchiveFiles bool              `json:"keepArchiveFiles,omitempty"`
	Digests          map[string]string `json:"digests"`
}

const (
	cacheStagingDirName = ".tmp"
	// cacheEntryFile holds each entry's metadata. Its name is binny's own, so it cannot collide with a file in a kept
	// archive, and it marks a dir as an entry: list and delete act only on dirs that have it.
	cacheEntryFile = ".binny-entry.json"
	// cacheStagingPrefix starts the name of every dir an install stages in, so `delete --all` removes only those
	cacheStagingPrefix = "binny-"
)

// NewCache returns the cache at root. Symlinks in the root are resolved once, so the links binny makes into the cache
// never pass through a link that could later be pointed elsewhere. It fails for an empty root and for a root that
// someone other than the current user could have put entries in.
func NewCache(root string) (*Cache, error) {
	if root == "" {
		// an empty path would otherwise resolve to the current dir, putting the cache in the checkout
		return nil, errors.New("cache root is empty: set cache.root to a dir, or leave it unset for the default")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for cache root %q: %w", root, err)
	}
	resolved, err := resolveExisting(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve cache root %q: %w", root, err)
	}
	c := &Cache{root: resolved}
	if err := c.checkRoot(); err != nil {
		return nil, err
	}
	return c, nil
}

// resolveExisting resolves the symlinks in path, or in its nearest existing parent when path does not exist yet.
func resolveExisting(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if !errors.Is(err, os.ErrNotExist) {
		return resolved, err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	resolvedParent, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

// checkRoot refuses a root that someone other than the current user could have put entries in, or that has become a
// link since the cache resolved it. A root that does not exist yet has nothing in it to trust.
func (c *Cache) checkRoot() error {
	resolved, err := filepath.EvalSymlinks(c.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if resolved != c.root {
		return fmt.Errorf("cache root %q now resolves to %q, so binny will not use it", c.root, resolved)
	}
	return checkCacheRoot(c.root)
}

func (c *Cache) Root() string {
	return c.root
}

// StagingRoot is where installs are staged, on the same filesystem as the entries so the final rename is atomic.
func (c *Cache) StagingRoot() string {
	return filepath.Join(c.root, cacheStagingDirName)
}

// Stage creates a dir under StagingRoot to install a tool into before Add moves it into the cache. The returned cleanup
// removes what Add left behind, and is safe to call whatever Stage returned.
func (c *Cache) Stage(name string) (string, func(), error) {
	return c.makeStagingDir("install-" + name + "-")
}

// makeStagingDir creates a dir under StagingRoot named so that `binny cache delete --all` reclaims it, creating the
// root when it does not exist yet.
func (c *Cache) makeStagingDir(pattern string) (string, func(), error) {
	cleanup := func() {}
	// check before creating anything, and again after, since the root may have appeared in between
	if err := c.checkRoot(); err != nil {
		return "", cleanup, err
	}
	if err := os.MkdirAll(c.StagingRoot(), 0755); err != nil {
		return "", cleanup, fmt.Errorf("failed to create cache staging root: %w", err)
	}
	if err := c.checkRoot(); err != nil {
		return "", cleanup, err
	}
	if err := checkStagingRoot(c.StagingRoot()); err != nil {
		return "", cleanup, err
	}

	dir, err := os.MkdirTemp(c.StagingRoot(), cacheStagingPrefix+pattern)
	if err != nil {
		return "", cleanup, fmt.Errorf("failed to create temp directory in %q: %w", c.StagingRoot(), err)
	}
	return dir, func() {
		if err := os.RemoveAll(dir); err != nil {
			log.WithFields("dir", dir).Warnf("failed to remove cache staging dir: %v", err)
		}
	}, nil
}

// checkStagingRoot refuses a staging root that is not a plain dir, since a link there could point anywhere and binny
// never makes one.
func checkStagingRoot(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("cache staging root %q is not a dir, so binny will not use it", path)
	}
	return nil
}

// EntryDir is the absolute path an entry for the given tool, version and fingerprint lives at, whether or not it exists.
func (c *Cache) EntryDir(name, version, fingerprint string) string {
	// a resolved version can be a git ref with a slash in it (cli/v2.0.0), which must stay one path element
	return filepath.Join(c.root, name, url.PathEscape(version), fingerprint)
}

// BinPath is the absolute path of the entry's binary.
func (e CacheEntry) BinPath() string {
	return filepath.Join(e.Dir, e.PathInDir)
}

// Get returns the entry for the given tool, version and fingerprint, or nil when there is none. It reads the entry's
// metadata without hashing its binary, so call Verify before running or linking it. Get fails when the cache root is
// not safe to run tools from.
func (c *Cache) Get(name, version, fingerprint string) (*CacheEntry, error) {
	if err := c.checkRoot(); err != nil {
		return nil, err
	}
	return c.read(name, version, fingerprint)
}

// read loads an entry's metadata, or returns nil when there is no entry.
func (c *Cache) read(name, version, fingerprint string) (*CacheEntry, error) {
	if err := validateCacheKey(name, url.PathEscape(version), fingerprint); err != nil {
		return nil, err
	}

	entry := &CacheEntry{Name: name, Version: version, Fingerprint: fingerprint, Dir: c.EntryDir(name, version, fingerprint)}
	contents, err := os.ReadFile(filepath.Join(entry.Dir, cacheEntryFile))
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(entry.Dir); errors.Is(statErr, os.ErrNotExist) {
			return nil, nil
		} else if statErr != nil {
			return nil, statErr
		}
		// the entry file is written before an entry is renamed into place, so binny did not make this dir
		return nil, fmt.Errorf("%q is where a cache entry belongs but has no %s, so binny did not create it: remove it by hand", entry.Dir, cacheEntryFile)
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(contents, &entry.cacheEntryMetadata); err != nil {
		return nil, entry.corrupt(fmt.Errorf("failed to read %s: %w", cacheEntryFile, err))
	}
	if err := entry.validate(); err != nil {
		return nil, entry.corrupt(err)
	}
	return entry, nil
}

// validate checks what linking and copying an entry rely on. The metadata is read back from disk, and its fields can be
// set by any caller, so this runs both where it is read and where it is linked.
func (m cacheEntryMetadata) validate() error {
	if !filepath.IsLocal(m.PathInDir) || filepath.Clean(m.PathInDir) == "." {
		return fmt.Errorf("binary path %q is not within the entry", m.PathInDir)
	}
	if _, ok := m.Digests[internal.SHA256Algorithm]; !ok {
		return errors.New("no sha256 digest is recorded")
	}
	return nil
}

// Verify hashes the entry's binary and compares it with the digests the entry recorded. An entry that fails is left
// in place, since only `binny cache delete` removes entries.
func (e CacheEntry) Verify() error {
	actual, err := getDigestsForFile(e.BinPath())
	if errors.Is(err, os.ErrNotExist) {
		return e.corrupt(err)
	}
	if err != nil {
		return err
	}

	// compare the algorithms both sides know, so adding or dropping one in a later binny does not fail every entry
	var checked int
	for algorithm, want := range e.Digests {
		got, ok := actual[algorithm]
		if !ok {
			continue
		}
		if got != want {
			return e.corrupt(fmt.Errorf("binary %q does not match its recorded %s digest", e.BinPath(), algorithm))
		}
		checked++
	}
	if checked == 0 {
		return e.corrupt(fmt.Errorf("no recorded digest of %q can be checked", e.BinPath()))
	}
	return nil
}

func (e CacheEntry) corrupt(err error) error {
	return fmt.Errorf("cache entry %q is corrupt (remove it with \"binny cache delete %s@%s\"): %w", e.Dir, e.Name, e.Version, err)
}

// Add moves an install staged by Stage into the cache as an entry and returns it. With
// keepArchiveFiles the whole staged dir becomes the entry, otherwise only the binary does. If another install put the
// same entry in place first, that entry is verified and returned, and the staged copy is left for the caller to clean
// up.
func (c *Cache) Add(name, version, fingerprint, stagedDir, binPath string, keepArchiveFiles bool) (*CacheEntry, error) {
	log.WithFields("tool", name, "version", version, "fingerprint", fingerprint).Trace("adding tool to cache")

	if err := validateCacheKey(name, url.PathEscape(version), fingerprint); err != nil {
		return nil, err
	}

	// staging must be within the staging root, so the final rename stays on one filesystem and delete --all can reclaim it
	if rel, err := relWithin(c.StagingRoot(), stagedDir); err != nil || rel == "." {
		return nil, fmt.Errorf("staged dir %q is not within the cache staging root %q", stagedDir, c.StagingRoot())
	}

	if err := c.checkRoot(); err != nil {
		return nil, err
	}

	binInDir, err := relWithin(stagedDir, binPath)
	if err != nil {
		return nil, fmt.Errorf("binary %w", err)
	}

	entryDir := stagedDir
	if !keepArchiveFiles {
		// the entry holds only the binary, named after the tool
		entryDir, err = os.MkdirTemp(stagedDir, ".binny-entry-")
		if err != nil {
			return nil, fmt.Errorf("failed to stage cache entry for %q: %w", name, err)
		}
		binInDir = name
		if err := os.Rename(binPath, filepath.Join(entryDir, binInDir)); err != nil {
			return nil, fmt.Errorf("failed to stage cache entry for %q: %w", name, err)
		}
	}

	metadata, err := writeCacheEntryMetadata(entryDir, binInDir, keepArchiveFiles)
	if err != nil {
		return nil, err
	}

	target := c.EntryDir(name, version, fingerprint)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir for %q: %w", name, err)
	}

	if err := os.Rename(entryDir, target); err != nil {
		// a rename onto an existing (non-empty) dir fails, which means another install got there first
		existing, readErr := c.read(name, version, fingerprint)
		if readErr != nil {
			return nil, readErr
		}
		if existing == nil {
			return nil, fmt.Errorf("failed to add %q to the cache: %w", target, err)
		}
		log.WithFields("tool", name, "version", version, "fingerprint", fingerprint, "entry", target).Debug("cache entry already exists, using it")
		if err := existing.Verify(); err != nil {
			return nil, err
		}
		return existing, nil
	}

	return &CacheEntry{Name: name, Version: version, Fingerprint: fingerprint, Dir: target, cacheEntryMetadata: metadata}, nil
}

// List returns every entry in the cache sorted by name then version, without verifying their binaries. Anything under
// the root that binny did not create is skipped, and so is an entry whose metadata cannot be read, which `binny cache
// delete` can still remove. A cache root that does not exist has no entries.
func (c *Cache) List() ([]CacheEntry, error) {
	locations, err := c.entryLocations()
	if err != nil {
		return nil, err
	}

	entries := make([]CacheEntry, 0, len(locations))
	for _, l := range locations {
		entry, err := c.read(l.name, l.version, l.fingerprint)
		if err != nil || entry == nil {
			log.WithFields("entry", l.dir, "error", err).Warn("skipping unreadable cache entry")
			continue
		}
		entries = append(entries, *entry)
	}
	return entries, nil
}

// cacheEntryLocation is where an entry is on disk, without its metadata.
type cacheEntryLocation struct {
	name, version, fingerprint string
	dir                        string
}

// entryLocations finds every entry dir by its entry file, sorted by name then version. It never reads entries, so it
// also finds those whose metadata is corrupt, and it skips everything else under the root.
func (c *Cache) entryLocations() ([]cacheEntryLocation, error) {
	var locations []cacheEntryLocation
	names, err := readDirNames(c.root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if validateCacheKey(name) != nil {
			// including the staging dir
			continue
		}
		versions, err := readDirNames(filepath.Join(c.root, name))
		if err != nil {
			// below the root, an unreadable dir is skipped like anything else binny did not create
			log.WithFields("dir", filepath.Join(c.root, name), "error", err).Debug("skipping unreadable dir in the cache root")
			continue
		}
		for _, escapedVersion := range versions {
			version, err := url.PathUnescape(escapedVersion)
			if err != nil || url.PathEscape(version) != escapedVersion {
				log.WithFields("dir", filepath.Join(c.root, name, escapedVersion)).Debug("skipping dir that is not a cached version")
				continue
			}
			fingerprints, err := readDirNames(filepath.Join(c.root, name, escapedVersion))
			if err != nil {
				log.WithFields("dir", filepath.Join(c.root, name, escapedVersion), "error", err).Debug("skipping unreadable dir in the cache root")
				continue
			}
			for _, fingerprint := range fingerprints {
				dir := filepath.Join(c.root, name, escapedVersion, fingerprint)
				if _, err := os.Lstat(filepath.Join(dir, cacheEntryFile)); err != nil {
					continue
				}
				locations = append(locations, cacheEntryLocation{name: name, version: version, fingerprint: fingerprint, dir: dir})
			}
		}
	}

	slices.SortStableFunc(locations, func(a, b cacheEntryLocation) int {
		return cmp.Or(strings.Compare(a.name, b.name), compareVersions(a.version, b.version))
	})
	return locations, nil
}

// compareVersions orders semver versions by precedence (v1.9.0 before v1.10.0), and anything else as strings after them.
func compareVersions(a, b string) int {
	va, errA := semver.NewVersion(a)
	vb, errB := semver.NewVersion(b)
	switch {
	case errA == nil && errB == nil:
		return cmp.Or(va.Compare(vb), strings.Compare(a, b))
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// CacheRef selects cache entries by tool name, and by version when one is given.
type CacheRef struct {
	Name    string
	Version string
}

func (r CacheRef) String() string {
	if r.Version == "" {
		return r.Name
	}
	return r.Name + "@" + r.Version
}

// Delete removes every entry the refs select. A ref that selects nothing is an error, and then nothing is removed. Refs
// only select among entries binny created, so nothing else under the root is ever removed.
func (c *Cache) Delete(refs ...CacheRef) error {
	locations, err := c.entryLocations()
	if err != nil {
		return err
	}

	var selected []cacheEntryLocation
	for _, r := range refs {
		matched := false
		for _, l := range locations {
			if l.name == r.Name && (r.Version == "" || l.version == r.Version) {
				selected = append(selected, l)
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("no cached tool matches %q", r.String())
		}
	}

	return c.removeEntries(selected)
}

// DeleteAll removes every entry in the cache and the staging dirs left by installs that did not finish. Anything else
// under the root is left alone, in case the root is misconfigured to a dir that holds more than the cache.
func (c *Cache) DeleteAll() error {
	locations, err := c.entryLocations()
	if err != nil {
		return err
	}
	if err := c.removeEntries(locations); err != nil {
		return err
	}

	if err := checkStagingRoot(c.StagingRoot()); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	staged, err := os.ReadDir(c.StagingRoot())
	if err != nil {
		return err
	}
	for _, de := range staged {
		if strings.HasPrefix(de.Name(), cacheStagingPrefix) {
			if err := os.RemoveAll(filepath.Join(c.StagingRoot(), de.Name())); err != nil {
				return err
			}
		}
	}
	// only removed when nothing else is in it
	_ = os.Remove(c.StagingRoot())
	return nil
}

// removeEntries moves each entry into a staging dir before removing it, so an entry disappears whole. A delete that stops
// part way leaves a staging dir that delete --all reclaims, never an entry dir without its entry file, which every
// install would then refuse and nothing would find to delete.
func (c *Cache) removeEntries(locations []cacheEntryLocation) error {
	if len(locations) == 0 {
		return nil
	}
	trash, cleanup, err := c.makeStagingDir("delete-")
	if err != nil {
		return err
	}

	for i, l := range locations {
		log.WithFields("tool", l.name, "version", l.version, "fingerprint", l.fingerprint).Debug("deleting from cache")
		if err := os.Rename(l.dir, filepath.Join(trash, strconv.Itoa(i))); err != nil {
			cleanup()
			return err
		}
		// the version dir, then the tool dir, are removed once they are empty (os.Remove fails on a dir with anything in it)
		versionDir := filepath.Dir(l.dir)
		if os.Remove(versionDir) == nil {
			_ = os.Remove(filepath.Dir(versionDir))
		}
	}
	return os.RemoveAll(trash)
}

// readDirNames returns the names of the dirs within dir, and nothing when dir does not exist.
func readDirNames(dir string) ([]string, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, de := range des {
		if de.IsDir() {
			names = append(names, de.Name())
		}
	}
	return names, nil
}

// Size is the sum of the sizes of the files in the entry.
func (e CacheEntry) Size() (int64, error) {
	var size int64
	err := filepath.WalkDir(e.Dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func writeCacheEntryMetadata(entryDir, binInDir string, keepArchiveFiles bool) (cacheEntryMetadata, error) {
	// staging dirs are created 0700, open the entry up like the rest of the cache
	if err := os.Chmod(entryDir, 0755); err != nil {
		return cacheEntryMetadata{}, fmt.Errorf("failed to chmod %q: %w", entryDir, err)
	}

	binPath := filepath.Join(entryDir, binInDir)
	// read-only, so writing to a store's link (cp new-build .tool/<name>) fails instead of changing the binary every
	// store shares
	if err := os.Chmod(binPath, 0555); err != nil {
		return cacheEntryMetadata{}, fmt.Errorf("failed to chmod %q: %w", binPath, err)
	}

	digests, err := getDigestsForFile(binPath)
	if err != nil {
		return cacheEntryMetadata{}, err
	}

	metadata := cacheEntryMetadata{
		PathInDir:        binInDir,
		KeepArchiveFiles: keepArchiveFiles,
		Digests:          digests,
	}
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return cacheEntryMetadata{}, err
	}

	//nolint:gosec // entries are read by every store, like the rest of the cache
	if err := os.WriteFile(filepath.Join(entryDir, cacheEntryFile), contents, 0644); err != nil {
		return cacheEntryMetadata{}, err
	}
	return metadata, nil
}

// validateCacheKey ensures that the entry path derived from the key stays within the cache root.
func validateCacheKey(parts ...string) error {
	for _, p := range parts {
		if !isPathElement(p, cacheStagingDirName) {
			return fmt.Errorf("invalid cache entry key %q", strings.Join(parts, "/"))
		}
	}
	return nil
}
