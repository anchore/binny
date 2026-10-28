package binny

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

// Cache is a per-user directory of installed tools that stores link to, so a tool is downloaded or built once per
// machine instead of once per checkout. Each entry lives at <root>/<name>/<version>/<fingerprint>/ and is never
// changed once it is in place: installs stage under <root>/.tmp/ and rename into place, and the first rename wins.
type Cache struct {
	root      string
	probeOnce sync.Once
	linksWork bool
}

// CacheEntry is one installed tool in the cache.
type CacheEntry struct {
	Name        string
	Version     string
	Fingerprint string
	// Dir is the absolute path of the entry
	Dir string
	cacheEntryMetadata
}

// cacheEntryMetadata is what each entry records about itself in its digests file, so there is no cache-wide state.
type cacheEntryMetadata struct {
	// PathInDir is the binary's path relative to the entry dir
	PathInDir string `json:"path"`
	// KeepArchiveFiles is set when the entry holds the whole install dir, not only the binary
	KeepArchiveFiles bool              `json:"keepArchiveFiles,omitempty"`
	Digests          map[string]string `json:"digests"`
}

const (
	cacheStagingDirName = ".tmp"
	cacheDigestsFile    = "digests.json"
)

func NewCache(root string) (*Cache, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for cache root %q: %w", root, err)
	}
	return &Cache{root: abs}, nil
}

func (c *Cache) Root() string {
	return c.root
}

// StagingRoot is where installs are staged, on the same filesystem as the entries so the final rename is atomic.
func (c *Cache) StagingRoot() string {
	return filepath.Join(c.root, cacheStagingDirName)
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

// Get returns the entry for the given tool, version and fingerprint, or nil when there is none. An entry whose binary
// does not match its recorded digests is an error and is left in place: only `binny cache delete` removes entries.
func (c *Cache) Get(name, version, fingerprint string) (*CacheEntry, error) {
	if err := validateCacheKey(name, url.PathEscape(version), fingerprint); err != nil {
		return nil, err
	}

	dir := c.EntryDir(name, version, fingerprint)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	entry := &CacheEntry{Name: name, Version: version, Fingerprint: fingerprint, Dir: dir}
	if err := entry.verify(); err != nil {
		return nil, fmt.Errorf("cache entry %q is corrupt (remove it with \"binny cache delete %s@%s\"): %w", dir, name, version, err)
	}

	return entry, nil
}

func (e *CacheEntry) verify() error {
	contents, err := os.ReadFile(filepath.Join(e.Dir, cacheDigestsFile))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(contents, &e.cacheEntryMetadata); err != nil {
		return fmt.Errorf("failed to read %s: %w", cacheDigestsFile, err)
	}

	actual, err := getDigestsForFile(e.BinPath())
	if err != nil {
		return err
	}

	if !maps.Equal(e.Digests, actual) {
		return fmt.Errorf("binary %q does not match its recorded digests", e.BinPath())
	}
	return nil
}

// Add moves an install staged under StagingRoot into the cache as an entry and returns it. With keepArchiveFiles the
// whole staged dir becomes the entry, otherwise only the binary does. If another install put the same entry in place
// first, that entry is returned and the staged copy is left for the caller to clean up.
func (c *Cache) Add(name, version, fingerprint, stagedDir, binPath string, keepArchiveFiles bool) (*CacheEntry, error) {
	log.WithFields("tool", name, "version", version, "fingerprint", fingerprint).Trace("adding tool to cache")

	if err := validateCacheKey(name, url.PathEscape(version), fingerprint); err != nil {
		return nil, err
	}

	binInDir, err := filepath.Rel(stagedDir, binPath)
	if err != nil || binInDir == ".." || strings.HasPrefix(binInDir, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("binary %q is not within %q", binPath, stagedDir)
	}

	entryDir := stagedDir
	if !keepArchiveFiles {
		// the entry holds only the binary, named after the tool
		entryDir, err = os.MkdirTemp(stagedDir, ".binny-entry-")
		if err != nil {
			return nil, err
		}
		binInDir = name
		if err := os.Rename(binPath, filepath.Join(entryDir, binInDir)); err != nil {
			return nil, err
		}
	}

	if err := writeCacheEntryMetadata(entryDir, binInDir, keepArchiveFiles); err != nil {
		return nil, err
	}

	target := c.EntryDir(name, version, fingerprint)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return nil, err
	}

	if err := os.Rename(entryDir, target); err != nil {
		// a rename onto an existing (non-empty) dir fails, which means another install got there first
		existing, getErr := c.Get(name, version, fingerprint)
		if getErr != nil {
			return nil, getErr
		}
		if existing == nil {
			return nil, fmt.Errorf("failed to add %q to the cache: %w", target, err)
		}
		log.WithFields("tool", name, "version", version, "entry", target).Debug("cache entry already exists, using it")
		return existing, nil
	}

	return c.Get(name, version, fingerprint)
}

// LinksWork reports whether symlinks can be created here, by creating and removing one under the staging root. The
// probe runs once per Cache value.
func (c *Cache) LinksWork() bool {
	c.probeOnce.Do(func() {
		c.linksWork = c.probeLinks()
	})
	return c.linksWork
}

func (c *Cache) probeLinks() bool {
	if err := os.MkdirAll(c.StagingRoot(), 0755); err != nil {
		// unknown, and an install will fail on the same dir anyway
		log.WithFields("error", err).Debug("unable to probe for symlink support")
		return true
	}
	dir, err := os.MkdirTemp(c.StagingRoot(), "link-probe-")
	if err != nil {
		log.WithFields("error", err).Debug("unable to probe for symlink support")
		return true
	}
	defer os.RemoveAll(dir)

	err = internal.Symlink(dir, filepath.Join(dir, "link"))
	if err != nil {
		log.WithFields("error", err).Debug("symlinks are not supported, tools are copied from the cache")
	}
	return err == nil
}

// List returns every entry in the cache sorted by name then version, without verifying them. A cache root that does
// not exist has no entries.
func (c *Cache) List() ([]CacheEntry, error) {
	var entries []CacheEntry
	names, err := readDirNames(c.root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if name == cacheStagingDirName {
			continue
		}
		versions, err := readDirNames(filepath.Join(c.root, name))
		if err != nil {
			return nil, err
		}
		for _, escapedVersion := range versions {
			version, err := url.PathUnescape(escapedVersion)
			if err != nil {
				return nil, fmt.Errorf("unexpected version dir %q in the cache: %w", escapedVersion, err)
			}
			fingerprints, err := readDirNames(filepath.Join(c.root, name, escapedVersion))
			if err != nil {
				return nil, err
			}
			for _, fingerprint := range fingerprints {
				entries = append(entries, CacheEntry{
					Name:        name,
					Version:     version,
					Fingerprint: fingerprint,
					Dir:         c.EntryDir(name, version, fingerprint),
				})
			}
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Version < entries[j].Version
	})

	return entries, nil
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

func (r CacheRef) path(root string) string {
	if r.Version == "" {
		return filepath.Join(root, r.Name)
	}
	return filepath.Join(root, r.Name, url.PathEscape(r.Version))
}

// Delete removes every entry the refs select. A ref that selects nothing is an error, and then nothing is removed.
func (c *Cache) Delete(refs ...CacheRef) error {
	for _, r := range refs {
		parts := []string{r.Name}
		if r.Version != "" {
			parts = append(parts, url.PathEscape(r.Version))
		}
		if err := validateCacheKey(parts...); err != nil {
			return err
		}
		if _, err := os.Stat(r.path(c.root)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("no cached tool matches %q", r.String())
			}
			return err
		}
	}

	for _, r := range refs {
		log.WithFields("tool", r.String()).Debug("deleting from cache")
		if err := os.RemoveAll(r.path(c.root)); err != nil {
			return err
		}
		// removing the last version of a tool removes the tool dir too
		nameDir := filepath.Join(c.root, r.Name)
		if versions, err := os.ReadDir(nameDir); err == nil && len(versions) == 0 {
			if err := os.Remove(nameDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteAll removes everything in the cache, including staging dirs left by installs that did not finish.
func (c *Cache) DeleteAll() error {
	des, err := os.ReadDir(c.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, de := range des {
		if err := os.RemoveAll(filepath.Join(c.root, de.Name())); err != nil {
			return err
		}
	}
	return nil
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

func writeCacheEntryMetadata(entryDir, binInDir string, keepArchiveFiles bool) error {
	// staging dirs are created 0700, open the entry up like the rest of the cache
	if err := os.Chmod(entryDir, 0755); err != nil {
		return fmt.Errorf("failed to chmod %q: %w", entryDir, err)
	}

	binPath := filepath.Join(entryDir, binInDir)
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("failed to chmod %q: %w", binPath, err)
	}

	digests, err := getDigestsForFile(binPath)
	if err != nil {
		return err
	}

	contents, err := json.MarshalIndent(cacheEntryMetadata{
		PathInDir:        binInDir,
		KeepArchiveFiles: keepArchiveFiles,
		Digests:          digests,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(entryDir, cacheDigestsFile), contents, 0644) //nolint:gosec // entries are read by every store, like the rest of the cache
}

// validateCacheKey ensures that the entry path derived from the key stays within the cache root.
func validateCacheKey(parts ...string) error {
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || p == cacheStagingDirName || strings.ContainsAny(p, `/\`) {
			return fmt.Errorf("invalid cache entry key %q", strings.Join(parts, "/"))
		}
	}
	return nil
}
