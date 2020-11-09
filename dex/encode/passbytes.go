// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encode

import (
	"encoding/json"
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	escapeSequence        = '\\'
	unicodePrefix         = 'u'
	unicodeSequenceLength = 6
)

// PassBytes represents a UTF8-encoded byte slice.
type PassBytes []byte

// MarshalJSON satisfies the json.Unmarshaler interface, returns a quoted copy
// of this byte slice. Returns an error if this byte slice is not a valid
// UTF8-encoded byte slice.
func (pb PassBytes) MarshalJSON() ([]byte, error) {
	// pb may have been created by calling PassBytes("some invalid string").
	// Returning the quoted copy of pb may lead to errors later when trying to
	// unmarshall. Sanity check that pb is a valid UTF8-encoded byte slice.
	if !isUTF8Encoded(pb) {
		return nil, fmt.Errorf("invalid PassBytes data")
	}
	data := make([]byte, len(pb)+2)
	data[0], data[len(data)-1] = '"', '"'
	copy(data[1:], pb)
	return data, nil
}

// UnmarshalJSON satisfies the json.Unmarshaler interface, parses JSON-encoded
// data into UTF8-encoded bytes and stores the result in the `PassBytes` pointer.
func (pb *PassBytes) UnmarshalJSON(rawBytes []byte) error {
	utf8EncodedBytes, err := parseJSONEncodedDataAsUTF8Bytes(rawBytes)
	ClearBytes(rawBytes)
	if err != nil {
		return fmt.Errorf("cannot unmarshal password: %w", err)
	}
	*pb = utf8EncodedBytes
	return nil
}

// Clear zeroes the slice.
func (pb PassBytes) Clear() {
	ClearBytes(pb)
}

func isUTF8Encoded(data []byte) bool {
	if len(data) == 0 {
		return true // represents an empty string
	}
	if data[0] == '"' {
		// should not be quoted, quotes must be escaped!
		return false
	}

	readIndex := 0
	for readIndex < len(data) {
		byteAtPos := data[readIndex]
		switch {
		case byteAtPos == escapeSequence:
			// Escape sequence hit, expect a valid escape char from next byte
			// (or byte sequence).
			if readIndex+1 >= len(data) {
				return false
			}
			nextByte := data[readIndex+1]
			if nextByte == unicodePrefix {
				// expect a unicode char in the form \uXXXX
				// or a surrogate pair in the form \uXXXX\uYYYY
				_, bytesRead := unicodeSequenceToCharacter(data[readIndex:])
				if bytesRead <= 0 {
					return false
				}
				readIndex += bytesRead
			} else {
				// some other escaped character?
				_, ok := parseEscapedCharacter(nextByte)
				if !ok {
					return false
				}
				readIndex += 2
			}

		case byteAtPos == '"', byteAtPos < ' ':
			// invalid char
			return false

		default:
			// Attempt to decode char as UTF8, may get utf8.RuneError.
			_, bytesRead := utf8.DecodeRune(data[readIndex:])
			if bytesRead <= 0 {
				return false
			}
			readIndex += bytesRead
		}
	}

	// all bytes check out
	return true
}

// parseJSONEncodedDataAsUTF8Bytes parses the provided JSON-encoded data into a
// UTF8-encoded byte slice.
// Returns an error if any of the following conditions is hit:
// - `data` is not a valid JSON encoding
// - `data` is not quoted
// - `data` contains a byte or byte sequence that cannot be parsed into a
//    UTF8-encoded byte or byte sequence.
// Inspired by encoding/json.(*decodeState).unquoteBytes.
func parseJSONEncodedDataAsUTF8Bytes(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return nil, fmt.Errorf("json-encoded data is not quoted")
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("data is not json-encoded")
	}

	// unquote data before parsing
	data = data[1 : len(data)-1]

	outputBuffer := make([]byte, len(data))
	// Separate because a sequence of bytes could be parsed into fewer bytes
	// than was read, causing readIndex to be > than writeIndex.
	// This guarantees that readIndex will always be >= writeIndex, with the
	// implication that `outputBuffer` can never grow beyond len(data).
	readIndex, writeIndex := 0, 0

	for readIndex < len(data) {
		byteAtPos := data[readIndex]
		switch {
		case byteAtPos == escapeSequence:
			// Escape sequence hit, next byte (or byte sequence) should tell us
			// what char was escaped. Error if there is no next byte.
			if readIndex+1 >= len(data) {
				return nil, fmt.Errorf("unexpected end of data: escape sequence")
			}
			nextByte := data[readIndex+1]
			if nextByte == unicodePrefix {
				// must be a unicode char in the form \uXXXX
				// or a surrogate pair in the form \uXXXX\uYYYY
				unicodeChar, bytesRead := unicodeSequenceToCharacter(data[readIndex:])
				if unicodeChar < 0 {
					return nil, fmt.Errorf("malformed unicode sequence in data")
				}
				readIndex += bytesRead
				writeIndex += utf8.EncodeRune(outputBuffer[writeIndex:], unicodeChar)
			} else if unescapedChar, ok := parseEscapedCharacter(nextByte); ok {
				outputBuffer[writeIndex] = unescapedChar
				readIndex += 2 // escape sequence + escaped char
				writeIndex++
			} else {
				return nil, fmt.Errorf("malformed unicode sequence in data")
			}

		case byteAtPos == '"', byteAtPos < ' ':
			// Invalid char, error out.
			return nil, fmt.Errorf("non-utf8 character %v", string(byteAtPos))

		case byteAtPos < utf8.RuneSelf:
			// ASCII char, use without parsing.
			outputBuffer[writeIndex] = byteAtPos
			readIndex++
			writeIndex++

		default:
			// Attempt to decode char as UTF8, may get utf8.RuneError.
			char, bytesRead := utf8.DecodeRune(data[readIndex:])
			if char == utf8.RuneError {
				return nil, fmt.Errorf("invalid character %v", string(byteAtPos))
			}
			readIndex += bytesRead
			writeIndex += utf8.EncodeRune(outputBuffer[writeIndex:], char)
		}
	}

	return outputBuffer[0:writeIndex], nil
}

// unicodeSequenceToCharacter returns the unicode character represented by the
// first 6-12 bytes of a byte slice and the number of bytes read from the slice
// to produce the unicode character.
// Expects the first 6 bytes of the slice to represent a valid unicode character
// (e.g. \u5b57) otherwise -1, 0 is returned indicating that the provided slice
// cannot be converted to a unicode character.
func unicodeSequenceToCharacter(seq []byte) (rune, int) {
	hexNumber, ok := unicodeSequenceToHexNumber(seq)
	if !ok {
		return -1, 0
	}

	unicodeChar := rune(hexNumber)
	if unicodeChar == unicode.ReplacementChar {
		// unknown unicode char
		return -1, 0
	}

	// check if `unicodeChar` can appear in a surrogate pair, if so, attempt to
	// parse another unicode char from the next sequence, and check if the second
	// character pairs with the first character.
	if utf16.IsSurrogate(unicodeChar) {
		nextSequence := seq[unicodeSequenceLength:]
		hexNumber, ok := unicodeSequenceToHexNumber(nextSequence)
		if ok {
			unicodeChar2 := rune(hexNumber)
			pairedChar := utf16.DecodeRune(unicodeChar, unicodeChar2)
			if pairedChar != unicode.ReplacementChar {
				// valid pair, return the pair
				return pairedChar, unicodeSequenceLength * 2
			}
		}
	}

	return unicodeChar, unicodeSequenceLength
}

// unicodeSequenceToHexNumber converts the last 4 bytes of a valid unicode
// []byte sequence to a number in base10. Expects the provided sequence to have
// at least 6 bytes, with first 2 bytes being `\u`.
func unicodeSequenceToHexNumber(unicodeSequence []byte) (int64, bool) {
	if len(unicodeSequence) < unicodeSequenceLength ||
		unicodeSequence[0] != escapeSequence ||
		unicodeSequence[1] != unicodePrefix {
		return -1, false
	}

	hexSequence := unicodeSequence[2:unicodeSequenceLength]
	hexN, err := strconv.ParseInt(string(hexSequence), 16, 32)
	if err != nil {
		return -1, false
	}

	return hexN, true
}

// parseEscapedCharacter returns the character represented by the byte
// following an escape sequence character if it is recognized.
// parseEscapedCharacter does not handle parsing of escaped unicode
// characters. Use unicodeSequenceToCharacter for that instead.
func parseEscapedCharacter(char byte) (byte, bool) {
	switch char {
	default:
		return 0, false
	case '"', '\\', '/', '\'':
		return char, true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	}
}
