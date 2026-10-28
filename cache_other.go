//go:build !unix

package binny

// checkCacheRoot is a no-op off unix. On windows the default root is under the user's own %LOCALAPPDATA%, which the
// profile ACL protects, and a root elsewhere has to be locked down by hand.
//
// ponytail: no ACL check. Compare the owner SID (windows.GetNamedSecurityInfo) if shared cache roots on windows turn up.
func checkCacheRoot(string) error {
	return nil
}
