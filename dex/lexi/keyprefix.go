// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/dgraph-io/badger"
)

const prefixSize = 2

// keyPrefix is a prefix for a key in the badger DB. Every table and index has
// a unique keyPrefix. This enables sorting and iteration of data.
type keyPrefix [prefixSize]byte

func (p keyPrefix) String() string {
	return hex.EncodeToString(p[:])
}

// NO RAW BADGER KEYS CAN BE LENGTH 2. IT CAN'T JUST BE A PREFIX, OR ELSE
// REVERSE ITERATION OF INDEXES FAILS

var (
	// reserved prefixes
	prefixToNamePrefix    = keyPrefix{0x00, 0x00}
	nameToPrefixPrefix    = keyPrefix{0x00, 0x01}
	primarySequencePrefix = keyPrefix{0x00, 0x02}
	keyToIDPrefix         = keyPrefix{0x00, 0x03}
	idToKeyPrefix         = keyPrefix{0x00, 0x04}

	firstAvailablePrefix = keyPrefix{0x01, 0x00}
)

func incrementPrefix(prefix keyPrefix) (p keyPrefix) {
	v := binary.BigEndian.Uint16(prefix[:])
	binary.BigEndian.PutUint16(p[:], v+1)
	return p
}

func bytesToPrefix(b []byte) (p keyPrefix) {
	copy(p[:], b)
	return
}

func lastKeyForPrefix(txn *badger.Txn, p keyPrefix) (k []byte) {
	reverseIteratePrefix(txn, p[:], nil, func(iter *badger.Iterator) error {
		k = iter.Item().Key()[prefixSize:]
		return ErrEndIteration
	}, withPrefetchSize(1))
	return
}

func prefixedKey(p keyPrefix, k []byte) []byte {
	pk := make([]byte, prefixSize+len(k))
	copy(pk, p[:])
	copy(pk[prefixSize:], k)
	return pk
}
