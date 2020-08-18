// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import "fmt"

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

// Semver formats the Semver as major.minor.patch (e.g. 1.2.3).
func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}
