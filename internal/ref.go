package internal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// commitPattern matches a full or abbreviated commit sha.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// IsVersion reports whether want is a semver version. A sha that is all digits (1905414) parses as a lenient semver
// version too, but no version is written that way, so it isn't one.
func IsVersion(want string) bool {
	return IsSemver(want) && !commitPattern.MatchString(want)
}

// IsExactVersion reports whether v names one build rather than something that can move: a full major.minor.patch
// version (bare or prefixed, like bun-v1.4.3) or a commit sha. Shorthands like v1 or v1.2 are queries to the go command
// and can be branches elsewhere, so they are not exact.
func IsExactVersion(v string) bool {
	if commitPattern.MatchString(v) {
		return true
	}
	_, err := semver.StrictNewVersion(strings.TrimPrefix(TrimSemverTagPrefix(v), "v"))
	return err == nil
}

// IsPlainVersion reports whether v is a full semver version with an optional v and nothing else around it (v1.2.3,
// v1.2.3-rc.1), the one form the go command always reads as a version, never as a branch.
func IsPlainVersion(v string) bool {
	_, err := semver.StrictNewVersion(strings.TrimPrefix(v, "v"))
	return err == nil
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
