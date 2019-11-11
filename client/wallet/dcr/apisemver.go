// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"fmt"

	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
)

var (
	// Expected api and daemon versions. Currently placeholders.
	dcrd = &dcrdtypes.VersionResult{
		VersionString: "1.5.0",
		Major:         1,
		Minor:         5,
		Patch:         0,
	}

	dcrdjsonrpcapi = &dcrdtypes.VersionResult{
		VersionString: "6.1.0",
		Major:         6,
		Minor:         1,
		Patch:         0,
	}

	dcrwalletjsonrpcapi = &dcrdtypes.VersionResult{
		VersionString: "6.2.0",
		Major:         6,
		Minor:         2,
		Patch:         0,
	}
)

// checkSemVer asserts the provided semantic version is at least equal to or
// better than the expected version.
func checkSemVer(id string, expected *dcrdtypes.VersionResult, provided *dcrdtypes.VersionResult) error {
	if provided.Major < expected.Major {
		return fmt.Errorf("%s's major version (%s) is lower than "+
			"expected (%v)", id, provided.VersionString,
			expected.VersionString)
	}

	if provided.Major == expected.Major {
		if provided.Minor < expected.Minor {
			return fmt.Errorf("%s's minor version (%s) is lower than "+
				"expected (%v)", id, provided.VersionString,
				expected.VersionString)
		}

		if provided.Minor == expected.Minor {
			if provided.Patch < expected.Patch {
				return fmt.Errorf("%s's patch version (%s) is lower than "+
					"expected (%v)", id, provided.VersionString,
					expected.VersionString)
			}
		}
	}

	return nil
}

// checkVersionInfo ensures the provided api version info are at least
// the expected or better.
func checkVersionInfo(versionInfo map[string]dcrdtypes.VersionResult, api ...string) error {
	for _, id := range api {
		semver, ok := versionInfo[id]
		if !ok {
			return fmt.Errorf("no version info found for %s", id)
		}

		var expected *dcrdtypes.VersionResult
		switch id {
		case "dcrd":
			expected = dcrd

		case "dcrdjsonrpcapi":
			expected = dcrdjsonrpcapi

		case "dcrwalletjsonrpcapi":
			expected = dcrwalletjsonrpcapi

		default:
			return fmt.Errorf("unknown api id provided: %s", id)
		}

		err := checkSemVer(id, expected, &semver)
		if err != nil {
			return err
		}
	}

	return nil
}
