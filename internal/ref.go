package internal

import "fmt"

// IsGitRef reports whether a wanted version names a git ref (branch, tag, PR head, or commit) rather than a semver
// version or "latest". Prefixed semver tags (bun-v1.4.3, cli/v2.0.0) are refs by this definition, a resolver that
// treats them as versions checks SemverTagPrefix on top.
func IsGitRef(want string) bool {
	return want != "" && want != "latest" && !IsSemver(want)
}

// CheckRefConstraint rejects a version constraint on a git ref, a ref names one commit so there is nothing to
// constrain.
func CheckRefConstraint(ref, constraint string) error {
	if constraint != "" {
		return fmt.Errorf("cannot specify a version constraint with git ref %q", ref)
	}
	return nil
}
