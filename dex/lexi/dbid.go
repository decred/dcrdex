// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"encoding"
	"encoding/hex"
)

// DBIDSize is the size of the DBID. It is 8 bytes to match the size of a
// byte-encoded uint64.
const DBIDSize = 8

// DBID is a unique ID mapped to a datum's key. Keys can be any length, but to
// prevent long keys from being echoed in all the indexes, every key is
// translated to a DBID for internal use.
type DBID [DBIDSize]byte

var (
	_ encoding.BinaryMarshaler = DBID{}

	lastDBID = DBID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
)

// MarshalBinary satisfies encoding.BinaryMarshaler for the DBID.
func (dbID DBID) MarshalBinary() ([]byte, error) {
	return dbID[:], nil
}

// String encodes the DBID as a 16-character hexadecimal string.
func (dbID DBID) String() string {
	return hex.EncodeToString(dbID[:])
}

func newDBIDFromBytes(b []byte) (dbID DBID) {
	copy(dbID[:], b)
	return dbID
}
