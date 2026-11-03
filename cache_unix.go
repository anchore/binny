//go:build unix

package binny

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// checkCacheRoot refuses a cache root that someone other than the current user could have put entries in, since binny
// links to and runs what it finds there. A root that does not exist yet is fine, since binny creates it. Group-write is
// refused too: on macOS every local user shares the staff group.
//
// ponytail: only the root is checked. Dirs below it are created 0755 by binny, and nobody else can add to a root they
// cannot write. Check them too if roots shared by hand turn up.
func checkCacheRoot(root string) error {
	fi, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("cache root %q is owned by uid %d, not the current user (uid %d), so binny will not use it: remove it by hand or set cache.root elsewhere", root, st.Uid, os.Geteuid())
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("cache root %q is writable by other users, so binny will not use it: run chmod go-w on it", root)
	}
	return nil
}
