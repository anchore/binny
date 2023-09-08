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

	var max *semver.Version
	for _, v := range parsed {
		if constraint != nil && !constraint.Check(v) {
			continue
		}
		if max == nil || v.GreaterThan(max) {
			max = v
		}
	}

	if max == nil {
		return "", nil
	}
	return max.Original(), nil
}

func IsSemver(v string) bool {
	ver, err := semver.NewVersion(v)
	if err != nil {
		return false
	}
	return ver != nil
}
