// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mnemonic

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"decred.org/dcrdex/dex/encode"
)

const (
	entropyBytes  = 18 // 144 bits
	timeBytes     = 2
	seedWords     = 15
	secondsPerDay = 86_400
)

// New generates new random entropy and a mnemonic seed that encodes the
// entropy, the current time, and a 5-bit checksum.
func New() ([]byte, string) {
	entropy := encode.RandomBytes(entropyBytes)
	stamp := time.Now()
	return entropy, generateMnemonic(entropy, stamp)
}

// DecodeMnemonic decodes the entropy, time, and checksum from the mnemonic
// seed and validates the checksum.
func DecodeMnemonic(mnemonic string) ([]byte, time.Time, error) {
	words := strings.Fields(mnemonic)
	if len(words) != 15 {
		return nil, time.Time{}, fmt.Errorf("expected 15 words, got %d", len(words))
	}
	buf := make([]byte, entropyBytes+timeBytes+1) // extra byte for checksum bits
	var cursor int
	for i := range words {
		v, err := wordIndex(words[i])
		if err != nil {
			return nil, time.Time{}, err
		}
		bs := make([]byte, 2)
		binary.BigEndian.PutUint16(bs, v)
		b0, b1 := bs[0], bs[1]
		byteIdx := cursor / 8
		avail := 8 - (cursor % 8)
		// Take the last three bits from the first byte, b0.
		if avail < 3 {
			buf[byteIdx] |= b0 >> (3 - avail)
			cursor += avail
			byteIdx++
			n := 3 - avail
			buf[byteIdx] = b0 << (8 - n)
			cursor += n
		} else {
			buf[byteIdx] |= (b0 << (avail - 3))
			cursor += 3
		}
		// Append the entire second byte.
		byteIdx = cursor / 8
		avail = 8 - (cursor % 8)
		buf[byteIdx] |= b1 >> (8 - avail)
		cursor += avail
		if avail < 8 {
			byteIdx++
			n := 8 - avail
			buf[byteIdx] |= b1 << (8 - n)
			cursor += n
		}
	}
	// The first 5 bits of the last byte are the checksum.
	acquiredChecksum := buf[entropyBytes+timeBytes] >> 3
	h := sha256.Sum256(buf[:entropyBytes+timeBytes])
	expectedChecksum := h[0] >> 3
	if acquiredChecksum != expectedChecksum {
		return nil, time.Time{}, errors.New("checksum mismatch")
	}
	entropy := buf[:entropyBytes]
	daysB := buf[entropyBytes : entropyBytes+timeBytes]
	days := binary.BigEndian.Uint16(daysB)
	stamp := time.Unix(int64(days)*secondsPerDay, 0)
	return entropy, stamp, nil
}

// GenerateMnemonic generates a mnemonic seed from the entropy and time.
// Note that the time encoded in the mnemonic seed is truncated to midnight, so
// the time returned when decoding will not be the same as the time passed in
// here.
func GenerateMnemonic(entropy []byte, stamp time.Time) (string, error) {
	if len(entropy) != entropyBytes {
		return "", fmt.Errorf("entropy must be %d bytes", entropyBytes)
	}
	return generateMnemonic(entropy, stamp), nil
}

// The entropy length is assumed to be correct.
func generateMnemonic(entropy []byte, stamp time.Time) string {
	days := uint16(stamp.Unix() / secondsPerDay)
	timeB := make([]byte, 2)
	binary.BigEndian.PutUint16(timeB, days)
	buf := make([]byte, entropyBytes+timeBytes+1) // extra byte for checksum bits
	copy(buf[:entropyBytes], entropy)
	copy(buf[entropyBytes:entropyBytes+timeBytes], timeB)
	// checksum
	h := sha256.Sum256(buf[:entropyBytes+timeBytes])
	buf[entropyBytes+timeBytes] = h[0] & 248 // 11111000
	var cursor int
	words := make([]string, seedWords)
	for i := 0; i < seedWords; i++ {
		idxB := make([]byte, 2)
		byteIdx := cursor / 8
		remain := 8 - (cursor % 8)
		// We only write three bits to the first byte of the uint16.
		if remain < 3 {
			clearN := 8 - remain
			masked := (buf[byteIdx] << clearN) >> clearN
			idxB[0] = masked << (3 - remain)
			cursor += remain
			byteIdx++
			n := 3 - remain
			idxB[0] |= buf[byteIdx] >> (8 - n)
			cursor += n
		} else {
			// Bits we want are from index (8 - remain) to (11 - remain).
			idxB[0] = (buf[byteIdx] << (8 - remain)) >> 5
			cursor += 3
		}
		// Write all 8 bits of the second byte of the uint16.
		byteIdx = cursor / 8
		remain = 8 - (cursor % 8)
		idxB[1] = buf[byteIdx] << (8 - remain)
		cursor += remain
		if remain < 8 {
			n := 8 - remain
			byteIdx++
			idxB[1] |= buf[byteIdx] >> (8 - n)
			cursor += n
		}
		idx := binary.BigEndian.Uint16(idxB)
		words[i] = wordList[idx]
	}
	return strings.Join(words, " ")
}

func wordIndex(word string) (uint16, error) {
	i := sort.Search(len(wordList), func(i int) bool {
		return strings.Compare(wordList[i], word) >= 0
	})
	if i == len(wordList) {
		return 0, fmt.Errorf("word %q exceeded range", word)
	}
	if wordList[i] != word {
		return 0, fmt.Errorf("word %q not known. closest match lexicographically is %q", word, wordList[i])
	}
	return uint16(i), nil
}
