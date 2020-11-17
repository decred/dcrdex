// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Bytes is a byte slice that marshals to and unmarshals from a hexadecimal
// string. The default go behavior is to marshal []byte to a base-64 string.
type Bytes []byte

// String return the hex encoding of the Bytes.
func (b Bytes) String() string {
	return hex.EncodeToString(b)
}

// MarshalJSON satisfies the json.Marshaller interface, and will marshal the
// bytes to a hex string.
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(b))
}

// Scan implements the sql.Scanner interface.
func (b *Bytes) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		// src may be reused, so create a new slice.
		dst := make(Bytes, len(src))
		copy(dst, src)
		*b = dst
		return nil
	case nil:
		return nil
	}
	return fmt.Errorf("cannot convert %T to Bytes", src)
}

// UnmarshalJSON satisfies the json.Unmarshaler interface, and expects a UTF-8
// encoding of a hex string in double quotes.
func (b *Bytes) UnmarshalJSON(encHex []byte) (err error) {
	if len(encHex) < 2 {
		return fmt.Errorf("marshalled Bytes, %q, not valid", string(encHex))
	}
	if encHex[0] != '"' || encHex[len(encHex)-1] != '"' {
		return fmt.Errorf("marshalled Bytes, %q, not quoted", string(encHex))
	}
	// DecodeString overallocates by at least double, and it makes a copy.
	src := encHex[1 : len(encHex)-1]
	dst := make([]byte, len(src)/2)
	_, err = hex.Decode(dst, src)
	if err == nil {
		*b = dst
	}
	return err
}

// Equal is true if otherB has identical []byte contents to the Bytes.
func (b Bytes) Equal(otherB []byte) bool {
	return bytes.Equal(b, otherB)
}
