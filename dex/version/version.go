// Copyright (c) 2015-2022 The Decred developers
// Use of this source code is governed by an ISC license
// that can be found at https://github.com/decred/dcrd/blob/master/LICENSE.

package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// semanticAlphabet defines the allowed characters for the pre-release and
	// build metadata portions of a semantic version string.
	semanticAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-."
)

// semverRE is a regular expression used to parse a semantic version string into
// its constituent parts.
var semverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*` +
	`[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// parseUint32 converts the passed string to an unsigned integer or returns an
// error if it is invalid.
func parseUint32(s string, fieldName string) (uint32, error) {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("malformed semver %s: %w", fieldName, err)
	}
	return uint32(val), err
}

// checkSemString returns an error if the passed string contains characters that
// are not in the provided alphabet.
func checkSemString(s, alphabet, fieldName string) error {
	for _, r := range s {
		if !strings.ContainsRune(alphabet, r) {
			return fmt.Errorf("malformed semver %s: %q invalid", fieldName, r)
		}
	}
	return nil
}

// ParseSemVer parses various semver components from the provided string.
func ParseSemVer(s string) (major, minor, patch uint32, preRel, build string, err error) {
	// Parse the various semver component from the version string via a regular
	// expression.
	m := semverRE.FindStringSubmatch(s)
	if m == nil {
		err := fmt.Errorf("malformed version string %q: does not conform to "+
			"semver specification", s)
		return 0, 0, 0, "", "", err
	}

	major, err = parseUint32(m[1], "major")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	minor, err = parseUint32(m[2], "minor")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	patch, err = parseUint32(m[3], "patch")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	preRel = m[4]
	err = checkSemString(preRel, semanticAlphabet, "pre-release")
	if err != nil {
		return 0, 0, 0, s, s, err
	}

	build = m[5]
	err = checkSemString(build, semanticAlphabet, "buildmetadata")
	if err != nil {
		return 0, 0, 0, s, s, err
	}

	return major, minor, patch, preRel, build, nil
}

// Parse returns the application version as a properly formed string per the
// semantic versioning 2.0.0 spec (https://semver.org/).
func Parse(version string) string {
	var err error
	Major, Minor, Patch, PreRelease, BuildMetadata, err := ParseSemVer(version)
	if err != nil {
		panic(err)
	}
	if BuildMetadata == "" {
		BuildMetadata = vcsCommitID()
		if BuildMetadata != "" {
			version = fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
			if PreRelease != "" {
				version += "-" + PreRelease
			}
			version += "+" + BuildMetadata
		}
	}

	return version
}

// NormalizeString returns the passed string stripped of all characters which
// are not valid according to the semantic versioning guidelines for pre-release
// and build metadata strings. In particular they MUST only contain characters
// in semanticAlphabet.
func NormalizeString(str string) string {
	var result strings.Builder
	for _, r := range str {
		if strings.ContainsRune(semanticAlphabet, r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}
