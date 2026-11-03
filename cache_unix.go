//go:build unix

package binny

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// checkCacheRoot refuses a cache root that someone other than the current user could have put entries in, since binny
// links to and runs what it finds there. A root that does not exist yet is fine, since binny creates it. Group-write is
// refused too: on macOS every local user shares the staff group. The dirs above the root are checked as well, since
// whoever can write one of them can rename the root away and put their own in its place.
//
// ponytail: dirs below the root are not checked. binny creates them 0755 (and clears group and other write from kept
// archive files), and nobody else can add to a root they cannot write. Check them too if roots shared by hand turn up.
func checkCacheRoot(root string) error {
	fi, err := os.Stat(root)
	if err == nil {
		if err := checkCacheDir(root, fi); err != nil {
			return err
		}
		if fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("cache root %q is writable by other users, so binny will not use it: run chmod go-w on it or set cache.root elsewhere", root)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// like OpenSSH's safe_path: each dir above must be ours or root's, and only writable by others when sticky
	for dir := filepath.Dir(root); ; dir = filepath.Dir(dir) {
		fi, err := os.Stat(dir)
		if err == nil {
			if err := checkCacheDir(dir, fi); err != nil {
				return err
			}
			if fi.Mode().Perm()&0o022 != 0 && fi.Mode()&os.ModeSticky == 0 {
				return fmt.Errorf("%q, above cache root %q, is writable by other users, so binny will not use the cache: run chmod go-w on it or set cache.root elsewhere", dir, root)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

// checkCacheDir refuses a dir owned by anyone but the current user or root.
func checkCacheDir(dir string, fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 || int(st.Uid) == os.Geteuid() {
		return nil
	}
	return fmt.Errorf("%q is owned by uid %d, not the current user (uid %d) or root, so binny will not use the cache there: remove it by hand or set cache.root elsewhere", dir, st.Uid, os.Geteuid())
}
