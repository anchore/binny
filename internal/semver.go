package internal

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/anchore/binny/internal/log"
)

func FilterToLatestVersion(versions []string, versionConstraint string) (string, error) {
	var parsed []*semver.Version
	for _, v := range versions {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		ver, err := semver.NewVersion(v)
		if err != nil {
			log.Tracef("failed to parse version %q: %v", v, err)
			continue
		}
		parsed = append(parsed, ver)
	}

	var constraint *semver.Constraints
	var err error
	if versionConstraint != "" {
		constraint, err = semver.NewConstraint(versionConstraint)
		if err != nil {
			return "", fmt.Errorf("unable to parse version constraint %q: %v", versionConstraint, err)
		}
	}

	var maxVal *semver.Version
	for _, v := range parsed {
		if constraint != nil && !constraint.Check(v) {
			continue
		}
		if maxVal == nil || v.GreaterThan(maxVal) {
			maxVal = v
		}
	}

	if maxVal == nil {
		return "", nil
	}
	return maxVal.Original(), nil
}

func IsSemver(v string) bool {
	ver, err := semver.NewVersion(v)
	if err != nil {
		return false
	}
	return ver != nil
}

// SemverTagPrefix splits a tag like "bun-v1.4.3" (or "cli/v2.0.0") into the prefix before the semver portion. Tags
// that are already semver have no prefix. The semver portion must have at least a major and minor version so that
// date-like tags (e.g. "nightly-2024-01-01") aren't mistaken for a prefixed version.
func SemverTagPrefix(tag string) (string, bool) {
	if IsSemver(tag) {
		return "", false
	}
	for i := 0; i < len(tag)-1; i++ {
		switch tag[i] {
		case '-', '_', '/', '@':
		default:
			continue
		}
		rest := tag[i+1:]
		if rest[0] != 'v' && (rest[0] < '0' || rest[0] > '9') {
			continue
		}
		core, _, _ := strings.Cut(strings.SplitN(rest, "+", 2)[0], "-")
		if strings.Contains(core, ".") && IsSemver(rest) {
			return tag[:i+1], true
		}
	}
	return "", false
}

// TrimSemverTagPrefix returns the semver portion of a tag, dropping any prefix found by SemverTagPrefix.
func TrimSemverTagPrefix(tag string) string {
	prefix, _ := SemverTagPrefix(tag)
	return strings.TrimPrefix(tag, prefix)
}
