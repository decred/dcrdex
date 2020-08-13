// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encode

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	// IntCoder is the DEX-wide integer byte-encoding order.
	IntCoder = binary.BigEndian
	// A byte-slice representation of boolean false.
	ByteFalse = []byte{0}
	// A byte-slice representation of boolean true.
	ByteTrue = []byte{1}
	maxU16   = int(^uint16(0))
	bEqual   = bytes.Equal
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

// UnixMilli returns the elapsed time in milliseconds since the Unix Epoch for
// the given time as an int64. The Location does not matter.
func UnixMilli(t time.Time) int64 {
	return t.Unix()*1e3 + int64(t.Nanosecond())/1e6
}

// UnixMilliU returns the elapsed time in milliseconds since the Unix Epoch for
// the given time as a uint64. The Location does not matter.
func UnixMilliU(t time.Time) uint64 {
	return uint64(t.Unix()*1e3) + uint64(t.Nanosecond())/1e6
}

// UnixTimeMilli returns a Time for an elapsed time in milliseconds since the
// Unix Epoch. The time will have Location set to UTC.
func UnixTimeMilli(msEpoch int64) time.Time {
	sec := msEpoch / 1000
	msec := msEpoch % 1000
	return time.Unix(sec, msec*1e6).UTC()
}

// DropMilliseconds returns the time truncated to the previous second.
func DropMilliseconds(t time.Time) time.Time {
	return t.Truncate(time.Second)
}

// DecodeUTime interprets bytes as a uint64 millisecond Unix timestamp and
// creates a time.Time.
func DecodeUTime(b []byte) time.Time {
	return UnixTimeMilli(int64(IntCoder.Uint64(b)))
}

// ExtractPushes parses the linearly-encoded 2D byte slice into a slice of
// slices. Empty pushes are nil slices.
func ExtractPushes(b []byte) ([][]byte, error) {
	pushes := make([][]byte, 0)
	for {
		if len(b) == 0 {
			break
		}
		l := int(b[0])
		b = b[1:]
		if l == 255 {
			if len(b) < 2 {
				return nil, fmt.Errorf("2 bytes not available for uint16 data length")
			}
			l = int(IntCoder.Uint16(b[:2]))
			b = b[2:]
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
func DecodeBlob(b []byte) (byte, [][]byte, error) {
	if len(b) == 0 {
		return 0, nil, fmt.Errorf("zero length blob not allowed")
	}
	ver := b[0]
	b = b[1:]
	pushes, err := ExtractPushes(b)
	return ver, pushes, err
}

// BuildyBytes is a byte-slice with an AddData method for building linearly
// encoded 2D byte slices. The AddData method supports chaining. The canonical
// use case is to create "versioned blobs", where the BuildyBytes is instantated
// with a single version byte, and then data pushes are added using the AddData
// method. Example use:
//
//   version := 0
//   b := BuildyBytes{version}.AddData(data1).AddData(data2)
//
// The versioned blob can be decoded with DecodeBlob to separate the version
// byte and the "payload". BuildyBytes has some similarities to dcrd's
// txscript.ScriptBuilder, though simpler and less efficient.
type BuildyBytes []byte

// AddData adds the data to the BuildyBytes, and returns the new BuildyBytes.
// The data has hard-coded length limit of uint16_max = 65535 bytes.
func (b BuildyBytes) AddData(d []byte) BuildyBytes {
	l := len(d)
	var lBytes []byte
	if l > 0xff-1 {
		if l > maxU16 {
			panic("cannot use addData for pushes > 65535 bytes")
		}
		i := make([]byte, 2)
		IntCoder.PutUint16(i, uint16(l))
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
