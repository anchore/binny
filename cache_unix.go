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
// refused too: on macOS every local user shares the staff group. The dirs above the root are checked as well, like
// OpenSSH's safe_path: whoever can write one of them can put their own root in its place. That includes a sticky dir
// like /tmp, since once the root is cleaned away anyone can recreate it, and links into it would run what they put
// there. The root is resolved already, so a link anywhere on the path means it changed since.
//
// ponytail: dirs below the root are not checked. binny creates them 0755 (and clears group and other write from kept
// archive files), and nobody else can add to a root they cannot write. Check them too if roots shared by hand turn up.
func checkCacheRoot(root string) error {
	for dir := root; ; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err == nil {
			if err := checkCacheDir(dir, root, fi); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

// checkCacheDir refuses the root, or a dir above it, that is a link, is owned by anyone but the current user or root,
// or is writable by other users.
func checkCacheDir(dir, root string, fi os.FileInfo) error {
	where := fmt.Sprintf("%q, above cache root %q,", dir, root)
	if dir == root {
		where = fmt.Sprintf("cache root %q", root)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is now a link, so binny will not use the cache", where)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is owned by uid %d, not the current user (uid %d) or root, so binny will not use the cache: remove it by hand or set cache.root elsewhere", where, st.Uid, os.Geteuid())
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by other users, so binny will not use the cache: run chmod go-w on it or set cache.root elsewhere", where)
	}
	return nil
}
