package internal

import (
	"fmt"
	"regexp"
)

// commitPattern matches a full or abbreviated commit sha.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// IsVersion reports whether want is a semver version. A sha that is all digits (1905414) parses as a lenient semver
// version too, but no version is written that way, so it isn't one.
func IsVersion(want string) bool {
	return IsSemver(want) && !commitPattern.MatchString(want)
}

// IsGitRef reports whether a wanted version names a git ref (branch, tag, PR head, or commit) rather than a version or
// "latest". Prefixed semver tags (bun-v1.4.3, cli/v2.0.0) are refs by this definition, a resolver that treats them as
// versions checks SemverTagPrefix on top.
func IsGitRef(want string) bool {
	return want != "" && want != "latest" && !IsVersion(want)
}

// CheckRefConstraint rejects a version constraint on a git ref, a ref names one commit so there is nothing to
// constrain.
func CheckRefConstraint(ref, constraint string) error {
	if constraint != "" {
		return fmt.Errorf("cannot specify a version constraint with git ref %q", ref)
	}
	return nil
}
