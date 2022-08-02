// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encode

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

var (
	// IntCoder is the DEX-wide integer byte-encoding order. IntCoder must be
	// BigEndian so that variable length data encodings work as intended.
	IntCoder = binary.BigEndian
	// A byte-slice representation of boolean false.
	ByteFalse = []byte{0}
	// A byte-slice representation of boolean true.
	ByteTrue = []byte{1}
	// MaxDataLen is the largest byte slice that can be stored when using
	// (BuildyBytes).AddData.
	MaxDataLen = 0x00fe_ffff // top two bytes in big endian stop at 254, signalling 32-bit len
)

// Uint64Bytes converts the uint16 to a length-2, big-endian encoded byte slice.
func Uint16Bytes(i uint16) []byte {
	b := make([]byte, 2)
	IntCoder.PutUint16(b, i)
	return b
}

// Uint64Bytes converts the uint32 to a length-4, big-endian encoded byte slice.
func Uint32Bytes(i uint32) []byte {
	b := make([]byte, 4)
	IntCoder.PutUint32(b, i)
	return b
}

// BytesToUint32 converts the length-4, big-endian encoded byte slice to a uint32.
func BytesToUint32(i []byte) uint32 {
	return IntCoder.Uint32(i[:4])
}

// Uint64Bytes converts the uint64 to a length-8, big-endian encoded byte slice.
func Uint64Bytes(i uint64) []byte {
	b := make([]byte, 8)
	IntCoder.PutUint64(b, i)
	return b
}

// CopySlice makes a copy of the slice.
func CopySlice(b []byte) []byte {
	newB := make([]byte, len(b))
	copy(newB, b)
	return newB
}

// RandomBytes returns a byte slice with the specified length of random bytes.
func RandomBytes(len int) []byte {
	bytes := make([]byte, len)
	_, err := rand.Read(bytes)
	if err != nil {
		panic("error reading random bytes: " + err.Error())
	}
	return bytes
}

// ClearBytes zeroes the byte slice.
func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// DropMilliseconds returns the time truncated to the previous second.
func DropMilliseconds(t time.Time) time.Time {
	return t.Truncate(time.Second)
}

// DecodeUTime interprets bytes as a uint64 millisecond Unix timestamp and
// creates a time.Time.
func DecodeUTime(b []byte) time.Time {
	return time.UnixMilli(int64(IntCoder.Uint64(b)))
}

// ExtractPushes parses the linearly-encoded 2D byte slice into a slice of
// slices. Empty pushes are nil slices.
func ExtractPushes(b []byte, preAlloc ...int) ([][]byte, error) {
	allocPushes := 2
	if len(preAlloc) > 0 {
		allocPushes = preAlloc[0]
	}
	pushes := make([][]byte, 0, allocPushes)
	for {
		if len(b) == 0 {
			break
		}
		l := int(b[0])
		b = b[1:]
		if l == 255 {
			if len(b) < 2 {
				return nil, fmt.Errorf("2 bytes not available for data length")
			}
			l = int(IntCoder.Uint16(b[:2]))
			if l < 255 {
				// This indicates it's really a uint32 capped at 0x00fe_ffff, and
				// we are looking at the top two bytes. Decode all four.
				if len(b) < 4 {
					return nil, fmt.Errorf("4 bytes not available for 32-bit data length")
				}
				l = int(IntCoder.Uint32(b[:4]))
				b = b[4:]
			} else { // includes 255
				b = b[2:]
			}
		}
		if len(b) < l {
			return nil, fmt.Errorf("data too short for pop of %d bytes", l)
		}
		if l == 0 {
			// If data length is zero, append nil instead of an empty slice.
			pushes = append(pushes, nil)
			continue
		}
		pushes = append(pushes, b[:l])
		b = b[l:]
	}
	return pushes, nil
}

// DecodeBlob decodes a versioned blob into its version and the pushes extracted
// from its data. Empty pushes will be nil.
func DecodeBlob(b []byte, preAlloc ...int) (byte, [][]byte, error) {
	if len(b) == 0 {
		return 0, nil, fmt.Errorf("zero length blob not allowed")
	}
	ver := b[0]
	b = b[1:]
	pushes, err := ExtractPushes(b, preAlloc...)
	return ver, pushes, err
}

// BuildyBytes is a byte-slice with an AddData method for building linearly
// encoded 2D byte slices. The AddData method supports chaining. The canonical
// use case is to create "versioned blobs", where the BuildyBytes is
// instantiated with a single version byte, and then data pushes are added using
// the AddData method. Example use:
//
//	version := 0
//	b := BuildyBytes{version}.AddData(data1).AddData(data2)
//
// The versioned blob can be decoded with DecodeBlob to separate the version
// byte and the "payload". BuildyBytes has some similarities to dcrd's
// txscript.ScriptBuilder, though simpler and less efficient.
type BuildyBytes []byte

// AddData adds the data to the BuildyBytes, and returns the new BuildyBytes.
// The data has hard-coded length limit of MaxDataLen = 16711679 bytes. The
// caller should ensure the data is not larger since AddData panics if it is.
func (b BuildyBytes) AddData(d []byte) BuildyBytes {
	l := len(d)
	var lBytes []byte
	if l >= 0xff {
		if l > MaxDataLen {
			panic("cannot use addData for pushes > 16711679 bytes")
		}
		var i []byte
		if l > math.MaxUint16 { // not >= since that is historically in 2 bytes
			// We are retrofitting for data longer than 65535 bytes, so we
			// cannot switch to uint32 at 65535 itself since it is possible
			// there is data of exactly that length already stored using just
			// two bytes to encode the length. Thus, the decoder should inspect
			// the top two bytes (big endian), switching to uint32 if under 255.
			// Therefore, the highest length with this scheme is 0x00fe_ffff
			// (16,711,679 bytes).
			i = make([]byte, 4)
			IntCoder.PutUint32(i, uint32(l))
		} else { // includes MaxUint16 for historical reasons
			i = make([]byte, 2)
			IntCoder.PutUint16(i, uint16(l))
		}
		lBytes = append([]byte{0xff}, i...)
	} else {
		lBytes = []byte{byte(l)}
	}
	return append(b, append(lBytes, d...)...)
}

// FileHash generates the SHA256 hash of the specified file.
func FileHash(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s for hashing: %w", name, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("error copying file %s for hashing: %w", name, err)
	}
	return h.Sum(nil), nil
}
