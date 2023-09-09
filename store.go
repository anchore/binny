package binny

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/OneOfOne/xxhash"

	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
)

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

func (s StoreEntry) Path() string {
	return filepath.Join(s.root, s.PathInRoot)
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

	err := s.loadState()
	if err != nil {
		return err
	}

	if _, err := os.Stat(s.root); os.IsNotExist(err) {
		if err := os.MkdirAll(s.root, 0755); err != nil {
			return err
		}
	}

	file, err := os.Open(pathOutsideRoot)
	if err != nil {
		return fmt.Errorf("failed to open temp copy of binary %q: %w", pathOutsideRoot, err)
	}
	defer file.Close()

	digests, err := getDigestsForReader(file)
	if err != nil {
		return nil
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

	fileInfo := StoreEntry{
		root:             s.root,
		Name:             toolName,
		InstalledVersion: resolvedVersion,
		Digests:          digests,
		PathInRoot:       targetName, // path in the store relative to the root
	}

	// if entry name exists, replace it, otherwise add it
	for i, entry := range s.entries {
		if entry.Name == toolName {
			log.WithFields("tool", toolName, "sha256", sha256Hash, pathOutsideRoot).Trace("replacing existing tool store entry")
			s.entries[i] = fileInfo
			return s.saveState()
		}
	}

	log.WithFields("tool", toolName, "sha256", sha256Hash, pathOutsideRoot).Trace("adding new tool store entry")

	s.entries = append(s.entries, fileInfo)
	return s.saveState()
}

func (s *Store) stateFilePath() string {
	return filepath.Join(s.root, ".binny.state.json")
}

func (s *Store) loadState() error {
	s.lock.Lock()
	defer s.lock.Unlock()

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
