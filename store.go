package binny

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/OneOfOne/xxhash"

	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

var ErrMultipleInstallations = fmt.Errorf("too many installations found")

type ErrDigestMismatch struct {
	Path      string
	Algorithm string
	Expected  string
	Actual    string
}

func (e *ErrDigestMismatch) Error() string {
	return fmt.Sprintf("digest mismatch: path=%q algorithm=%q expected=%q actual=%q", e.Path, e.Algorithm, e.Expected, e.Actual)
}

type Store struct {
	root    string
	entries []StoreEntry
	lock    *sync.RWMutex
	// linksWork probes once per store whether symlinks can be made in the root
	linksWork func() bool
}

type state struct {
	Entries []StoreEntry `json:"entries"`
}

type StoreEntry struct {
	root             string
	Name             string            `json:"name"`
	InstalledVersion string            `json:"version"`
	Digests          map[string]string `json:"digests"`
	PathInRoot       string            `json:"path"`
	// DirInRoot is set when the whole install dir was kept in the store (see AddToolDir)
	DirInRoot string `json:"dir,omitempty"`
}

func (e StoreEntry) Path() string {
	return filepath.Join(e.root, e.PathInRoot)
}

func NewStore(root string) (*Store, error) {
	s := &Store{
		root:    root,
		entries: []StoreEntry{},
		lock:    &sync.RWMutex{},
	}
	s.linksWork = sync.OnceValue(s.probeLinks)

	return s, s.loadState()
}

// LinksWork reports whether a symlink can be made in the store root, which is where every tool link is made (symlink
// support is per filesystem, so the cache root may differ). It probes once per Store by creating and removing a link
// in a temp dir under the root. A probe that cannot run at all reports false, so a copy that the link fallback left is
// accepted rather than reinstalled on every run.
func (s *Store) LinksWork() bool {
	return s.linksWork()
}

func (s *Store) probeLinks() bool {
	dir, err := os.MkdirTemp(s.root, ".binny-link-probe-")
	if err != nil {
		log.WithFields("error", err).Debug("unable to probe the store for symlink support")
		return false
	}
	defer os.RemoveAll(dir)

	if err := internal.Symlink(dir, filepath.Join(dir, "link")); err != nil {
		log.WithFields("error", err).Debug("symlinks are not supported in the store, tools are copied from the cache")
		return false
	}
	return true
}

func (s Store) Root() string {
	return s.root
}

// Get returns the store entry for the given tool name and version
func (s *Store) Get(name string, version string) (*StoreEntry, error) {
	// check if the tool is already installed...
	// if the version matches the desired version, skip
	nameVersionEntries := s.GetByName(name, version)

	switch len(nameVersionEntries) {
	case 0:
		nameEntries := s.GetByName(name)
		if len(nameEntries) > 0 {
			return nil, fmt.Errorf("tool already installed with different configuration")
		}
		return nil, fmt.Errorf("tool not installed")

	case 1:
		// pass

	default:
		return nil, ErrMultipleInstallations
	}

	entry := nameVersionEntries[0]

	if entry.InstalledVersion != version {
		return nil, fmt.Errorf("tool %q has different version: %s", name, entry.InstalledVersion)
	}
	return &entry, nil
}

// GetByName returns all entries with the given name, optionally filtered by one or more versions.
func (s Store) GetByName(name string, versions ...string) []StoreEntry {
	var entries []StoreEntry
	for _, en := range s.entries {
		if en.Name == name {
			if len(versions) == 0 {
				entries = append(entries, en)
			} else {
				for _, version := range versions {
					if en.InstalledVersion == version {
						entries = append(entries, en)
					}
				}
			}
		}
	}
	return entries
}

func (s Store) Entries() (entries []StoreEntry) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return append(entries, s.entries...)
}

func (s *Store) AddTool(toolName string, resolvedVersion, pathOutsideRoot string) error {
	log.WithFields("tool", toolName, "from", pathOutsideRoot).Trace("adding tool to store")

	if err := validateToolName(toolName); err != nil {
		return err
	}

	digests, err := getDigestsForFile(pathOutsideRoot)
	if err != nil {
		return err
	}

	return s.addFile(toolName, resolvedVersion, pathOutsideRoot, digests)
}

// addFile moves a file with already computed digests into the store at root/<name>. The name must be validated.
func (s *Store) addFile(toolName, resolvedVersion, pathOutsideRoot string, digests map[string]string) error {
	if err := s.loadState(); err != nil {
		return err
	}

	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		if err := os.MkdirAll(s.root, 0755); err != nil {
			return err
		}
	}

	sha256Hash, ok := digests[internal.SHA256Algorithm]
	if !ok {
		return fmt.Errorf("failed to get sha256 hash for %q", pathOutsideRoot)
	}

	// move the file into the store at root/basename
	targetName := toolName
	targetPath := filepath.Join(s.root, toolName)

	if err := os.Rename(pathOutsideRoot, targetPath); err != nil {
		return err
	}

	// chmod 755 the file
	if err := os.Chmod(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to chmod %q: %w", targetPath, err)
	}

	// a previous install may have kept a whole directory for this tool
	if err := os.RemoveAll(toolDirPath(s.root, toolName)); err != nil {
		return fmt.Errorf("failed to remove previous install dir for %q: %w", toolName, err)
	}

	return s.upsert(StoreEntry{
		root:             s.root,
		Name:             toolName,
		InstalledVersion: resolvedVersion,
		Digests:          digests,
		PathInRoot:       targetName, // path in the store relative to the root
	}, sha256Hash)
}

// AddToolDir is like AddTool, but moves the whole install directory into the store (at root/.binny-dirs/<name>) and links
// root/<name> to the binary inside it. This is for tools that read files relative to their own location.
func (s *Store) AddToolDir(toolName string, resolvedVersion, dirOutsideRoot, binPath string) error {
	log.WithFields("tool", toolName, "from", dirOutsideRoot).Trace("adding tool directory to store")

	if err := validateToolName(toolName); err != nil {
		return err
	}

	binInDir, err := relWithin(dirOutsideRoot, binPath)
	if err != nil {
		return fmt.Errorf("binary %w", err)
	}

	if err := s.loadState(); err != nil {
		return err
	}

	if err := os.MkdirAll(s.root, 0755); err != nil {
		return err
	}

	digests, err := getDigestsForFile(binPath)
	if err != nil {
		return err
	}

	sha256Hash, ok := digests[internal.SHA256Algorithm]
	if !ok {
		return fmt.Errorf("failed to get sha256 hash for %q", binPath)
	}

	// ponytail: the old dir is removed before the new one lands, so a concurrent run of this tool can briefly
	// miss its files. Use versioned dirs and swap the link if that ever matters.
	targetDir := toolDirPath(s.root, toolName)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("failed to remove previous install dir %q: %w", targetDir, err)
	}

	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return err
	}

	if err := os.Rename(dirOutsideRoot, targetDir); err != nil {
		return err
	}

	// the staging dir is created 0700, open it up like the rest of the store
	if err := os.Chmod(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to chmod %q: %w", targetDir, err)
	}

	if err := os.Chmod(filepath.Join(targetDir, binInDir), 0755); err != nil {
		return fmt.Errorf("failed to chmod %q: %w", binInDir, err)
	}

	if _, err := placeLink(filepath.Join(s.root, toolName), filepath.Join(toolDirsName, toolName, binInDir)); err != nil {
		return err
	}

	return s.upsert(StoreEntry{
		root:             s.root,
		Name:             toolName,
		InstalledVersion: resolvedVersion,
		Digests:          digests,
		PathInRoot:       toolName,
		DirInRoot:        filepath.Join(toolDirsName, toolName),
	}, sha256Hash)
}

// LinkCacheEntry points root/<name> at the binary of a cache entry by absolute path, so the store keeps working when the
// checkout moves. It is recorded in the state the same way a copy is. Where a symlink cannot be made, a single-binary
// entry is copied into the store instead, and a keep-archive-files entry returns the link error, since the tool reads
// files beside itself and has no working layout without the link. The entry must already be verified.
func (s *Store) LinkCacheEntry(entry CacheEntry) error {
	log.WithFields("tool", entry.Name, "entry", entry.Dir).Trace("linking tool to cache entry")

	if err := validateToolName(entry.Name); err != nil {
		return err
	}

	if err := s.loadState(); err != nil {
		return err
	}

	if err := os.MkdirAll(s.root, 0755); err != nil {
		return err
	}

	// the entry's fields can be set by any caller, not only read from its entry file
	if err := entry.validate(); err != nil {
		return entry.corrupt(err)
	}
	sha256Hash := entry.Digests[internal.SHA256Algorithm]

	linked, err := placeLink(filepath.Join(s.root, entry.Name), entry.BinPath())
	if !linked {
		if entry.KeepArchiveFiles {
			return err
		}
		log.WithFields("tool", entry.Name, "error", err).Debug("unable to link to the cache, copying instead")
		return s.copyCacheEntry(entry)
	}
	if err != nil {
		return err
	}

	// a previous install may have kept a whole directory for this tool
	if err := os.RemoveAll(toolDirPath(s.root, entry.Name)); err != nil {
		return fmt.Errorf("failed to remove previous install dir for %q: %w", entry.Name, err)
	}

	return s.upsert(StoreEntry{
		root:             s.root,
		Name:             entry.Name,
		InstalledVersion: entry.Version,
		Digests:          entry.Digests,
		PathInRoot:       entry.Name,
	}, sha256Hash)
}

// copyCacheEntry puts a copy of the entry's binary at root/<name>, for systems that refuse symlinks.
func (s *Store) copyCacheEntry(entry CacheEntry) error {
	src, err := os.Open(entry.BinPath())
	if err != nil {
		return err
	}
	defer src.Close()

	// stage the copy within the store so moving it into place is atomic
	dst, err := os.CreateTemp(s.root, fmt.Sprintf(".binny-copy-%s-", entry.Name))
	if err != nil {
		return err
	}
	defer os.Remove(dst.Name())

	// hash while copying, so the binary is read once
	digests, err := getDigestsForReader(io.TeeReader(src, dst))
	if err != nil {
		dst.Close()
		return fmt.Errorf("failed to copy %q: %w", entry.BinPath(), err)
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if digests[internal.SHA256Algorithm] != entry.Digests[internal.SHA256Algorithm] {
		return fmt.Errorf("copy of %q does not match the cache entry's digests", entry.BinPath())
	}

	return s.addFile(entry.Name, entry.Version, dst.Name(), digests)
}

// placeLink makes path a symlink to target, creating the link aside and renaming it over path so the replacement is
// atomic. linked is false when the symlink itself could not be made, as on windows without developer mode.
func placeLink(path, target string) (linked bool, err error) {
	tmpLink := path + ".binny-link"
	_ = os.Remove(tmpLink)
	if err := internal.Symlink(target, tmpLink); err != nil {
		return false, fmt.Errorf("failed to link %q: %w", path, err)
	}
	if err := os.Rename(tmpLink, path); err != nil {
		return true, fmt.Errorf("failed to move link into place at %q: %w", path, err)
	}
	return true, nil
}

// relWithin returns path relative to dir, and fails when path is not within dir.
func relWithin(dir, path string) (string, error) {
	rel, err := filepath.Rel(dir, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%q is not within %q", path, dir)
	}
	return rel, nil
}

// isPathElement reports whether name is usable as one path element directly within a dir: not empty, not . or ..,
// not the given reserved name, and without separators.
func isPathElement(name, reserved string) bool {
	return name != "" && name != "." && name != ".." && name != reserved && !strings.ContainsAny(name, `/\`)
}

// kept install dirs live under their own dir in the store so they can never collide with a tool's binary path
const toolDirsName = ".binny-dirs"

func toolDirPath(root, toolName string) string {
	return filepath.Join(root, toolDirsName, toolName)
}

// validateToolName ensures that paths derived from the tool name stay directly within the store root.
func validateToolName(name string) error {
	if !isPathElement(name, toolDirsName) {
		return fmt.Errorf("invalid tool name %q", name)
	}
	return nil
}

func (s *Store) upsert(fileInfo StoreEntry, sha256Hash string) error {
	// if entry name exists, replace it, otherwise add it
	for i, entry := range s.entries {
		if entry.Name == fileInfo.Name {
			log.WithFields("tool", fileInfo.Name, "sha256", sha256Hash).Trace("replacing existing tool store entry")
			s.entries[i] = fileInfo
			return s.saveState()
		}
	}

	log.WithFields("tool", fileInfo.Name, "sha256", sha256Hash).Trace("adding new tool store entry")

	s.entries = append(s.entries, fileInfo)
	return s.saveState()
}

func (s *Store) stateFilePath() string {
	return filepath.Join(s.root, ".binny.state.json")
}

func (s *Store) loadState() error {
	s.lock.RLock()
	defer s.lock.RUnlock()

	stateFilePath := s.stateFilePath()
	log.WithFields("path", stateFilePath).Trace("loading state")

	if _, err := os.Stat(stateFilePath); os.IsNotExist(err) {
		return nil
	}

	stateFile, err := os.Open(stateFilePath)
	if err != nil {
		return err
	}
	defer stateFile.Close()

	var encodeState state

	decoder := json.NewDecoder(stateFile)
	if err := decoder.Decode(&encodeState); err != nil {
		return err
	}

	var entries []StoreEntry
	for _, entry := range encodeState.Entries {
		entry.root = s.root
		entries = append(entries, entry)
	}

	s.entries = entries

	return nil
}

func (s Store) saveState() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	stateFilePath := s.stateFilePath()
	log.WithFields("path", stateFilePath).Trace("saving state")

	stateFile, err := os.OpenFile(stateFilePath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer stateFile.Close()

	var encodeState state

	for _, entry := range s.entries {
		// check if bin exists on disk
		if _, err := os.Stat(entry.Path()); os.IsNotExist(err) {
			log.WithFields("name", entry.Name, "path", entry.PathInRoot).Trace("binary missing, removing from store")
			continue
		}

		encodeState.Entries = append(encodeState.Entries, entry)
	}

	encoder := json.NewEncoder(stateFile)
	encoder.SetIndent("", "  ")

	return encoder.Encode(encodeState)
}

func (e *StoreEntry) Verify(useXxh64, useSha256 bool) error {
	// at least the file must exist
	if _, err := os.Stat(e.Path()); err != nil {
		return err
	}

	if useXxh64 {
		expect, ok := e.Digests[internal.XXH64Algorithm]
		if !ok {
			return fmt.Errorf("no xxh64 digest found for %q", e.Path())
		}

		actual, err := xxh64File(e.Path())
		if err != nil {
			return fmt.Errorf("failed to calculate xxh64 of %q: %w", e.Path(), err)
		}

		if expect != actual {
			return &ErrDigestMismatch{
				Path:      e.Path(),
				Algorithm: internal.XXH64Algorithm,
				Expected:  expect,
				Actual:    actual,
			}
		}
	}

	if useSha256 {
		expect, ok := e.Digests[internal.SHA256Algorithm]
		if !ok {
			return fmt.Errorf("no sha256 digest found for %q", e.Path())
		}

		actual, err := sha256File(e.Path())
		if err != nil {
			return fmt.Errorf("failed to calculate sha256 of %q: %w", e.Path(), err)
		}

		if expect != actual {
			return &ErrDigestMismatch{
				Path:      e.Path(),
				Algorithm: internal.SHA256Algorithm,
				Expected:  expect,
				Actual:    actual,
			}
		}
	}

	return nil
}

func sha256File(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer fh.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, fh); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func xxh64File(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer fh.Close()

	hash := xxhash.New64()
	if _, err := io.Copy(hash, fh); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func getDigestsForFile(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open binary %q: %w", filePath, err)
	}
	defer file.Close()

	return getDigestsForReader(file)
}

func getDigestsForReader(r io.Reader) (map[string]string, error) {
	sha256Hash := sha256.New()
	xxhHash := xxhash.New64()

	if _, err := io.Copy(io.MultiWriter(sha256Hash, xxhHash), r); err != nil {
		return nil, err
	}
	sha256Str := fmt.Sprintf("%x", sha256Hash.Sum(nil))
	xxhStr := fmt.Sprintf("%x", xxhHash.Sum(nil))

	return map[string]string{
		internal.SHA256Algorithm: sha256Str,
		internal.XXH64Algorithm:  xxhStr,
	}, nil
}
