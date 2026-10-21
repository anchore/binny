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

	return s, s.loadState()
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

	err := s.loadState()
	if err != nil {
		return err
	}

	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		if err := os.MkdirAll(s.root, 0755); err != nil {
			return err
		}
	}

	digests, err := getDigestsForFile(pathOutsideRoot)
	if err != nil {
		return err
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

	binInDir, err := filepath.Rel(dirOutsideRoot, binPath)
	if err != nil || binInDir == ".." || strings.HasPrefix(binInDir, ".."+string(filepath.Separator)) {
		return fmt.Errorf("binary %q is not within %q", binPath, dirOutsideRoot)
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

	// create the link aside and rename it over root/<name> so the replacement is atomic
	targetPath := filepath.Join(s.root, toolName)
	tmpLink := targetPath + ".binny-link"
	_ = os.Remove(tmpLink)
	if err := os.Symlink(filepath.Join(toolDirsName, toolName, binInDir), tmpLink); err != nil {
		return fmt.Errorf("failed to link %q: %w", targetPath, err)
	}
	if err := os.Rename(tmpLink, targetPath); err != nil {
		return err
	}

	return s.upsert(StoreEntry{
		root:             s.root,
		Name:             toolName,
		InstalledVersion: resolvedVersion,
		Digests:          digests,
		PathInRoot:       toolName,
	}, sha256Hash)
}

// kept install dirs live under their own dir in the store so they can never collide with a tool's binary path
const toolDirsName = ".binny-dirs"

func toolDirPath(root, toolName string) string {
	return filepath.Join(root, toolDirsName, toolName)
}

// validateToolName ensures that paths derived from the tool name stay directly within the store root.
func validateToolName(name string) error {
	if name == "" || name == "." || name == ".." || name == toolDirsName || strings.ContainsAny(name, `/\`) {
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
		return nil, fmt.Errorf("failed to open temp copy of binary %q: %w", filePath, err)
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
