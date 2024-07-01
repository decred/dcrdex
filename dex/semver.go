// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"strconv"
	"strings"
)

// Semver models a semantic version major.minor.patch
type Semver struct {
	Major uint32
	Minor uint32
	Patch uint32
}

// NewSemver returns a new Semver with the version major.minor.patch
func NewSemver(major, minor, patch uint32) Semver {
	return Semver{major, minor, patch}
}

// SemverCompatible decides if the actual version is compatible with the required one.
func SemverCompatible(required, actual Semver) bool {
	switch {
	case required.Major != actual.Major:
		return false
	case required.Minor > actual.Minor:
		return false
	case required.Minor == actual.Minor && required.Patch > actual.Patch:
		return false
	default:
		return true
	}
}

// SemverCompatibleAny checks if the version is compatible with any versions in
// a slice of versions.
func SemverCompatibleAny(compatible []Semver, actual Semver) bool {
	for _, v := range compatible {
		if SemverCompatible(v, actual) {
			return true
		}
	}
	return false
}

// Semver formats the Semver as major.minor.patch (e.g. 1.2.3).
func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}

func SemverFromString(ver string) (*Semver, error) {
	fields := strings.Split(ver, ".")
	if len(fields) < 2 {
		return nil, fmt.Errorf("expected semver with 2 or more fields but got %d", len(fields))
	}
	major, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot parse semver major: %v", err)
	}
	minor, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot parse semver minor: %v", err)
	}
	var patch uint64
	if len(fields) > 2 {
		patch, err = strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("cannot parse semver major: %v", err)
		}
	}
	// Some versions will have an extra field. Ignoring.
	return &Semver{uint32(major), uint32(minor), uint32(patch)}, nil
}
