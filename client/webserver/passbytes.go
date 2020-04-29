// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
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

// PassBytes is an alias of type []byte that implements json.UnmarshalJSON
// to enable parsing json-encoded strings as []byte.
type PassBytes []byte

// UnmarshalJSON satisfies the json.Unmarshaler interface, parses json-encoded
// strings as []byte. Returns an error if the raw bytes to be unmarshalled is
// not a valid json-encoded string.
func (pb *PassBytes) UnmarshalJSON(rawBytes []byte) error {
	rawBytes, err := sanitizeJSONEncodedString(rawBytes)
	if err != nil {
		return fmt.Errorf("cannot unmarshal password: %v", err)
	}
	*pb = make([]byte, len(rawBytes))
	copy((*pb)[:], rawBytes)
	return nil
}

func (pb PassBytes) String() string {
	return string(pb)
}

// sanitizeJSONEncodedString receives a json-encoded string in []byte and
// returns a new []byte representing the same string but without quotes and
// escape characters.
func sanitizeJSONEncodedString(stringBytes []byte) ([]byte, error) {
	if stringBytes[0] == '"' {
		if len(stringBytes) < 2 || stringBytes[len(stringBytes)-1] != '"' {
			return nil, fmt.Errorf("data is not properly quoted")
		}
		// string is quoted, remove first and last bytes
		stringBytes = stringBytes[1 : len(stringBytes)-1]
	}

	outputBuffer := make([]byte, len(stringBytes)+2*utf8.UTFMax)
	pos, lastWriteIndex := 0, 0

	for pos < len(stringBytes) {
		// Out of room? Can only happen if s is full of
		// malformed UTF-8 and we're replacing each
		// byte with RuneError.
		// todo revisit this snippet!
		if lastWriteIndex >= len(outputBuffer)-2*utf8.UTFMax {
			nb := make([]byte, (len(outputBuffer)+utf8.UTFMax)*2)
			copy(nb, outputBuffer[0:lastWriteIndex])
			outputBuffer = nb
		}

		byteAtPos := stringBytes[pos]
		switch {
		case byteAtPos == escapeSequence:
			// Escape sequence hit, next byte (or byte sequence) should tell us
			// what char was escaped. Error if there is no next byte.
			if pos+1 >= len(stringBytes) {
				return nil, fmt.Errorf("unexpected end of data: escape sequence")
			}
			nextByte := stringBytes[pos+1]
			if nextByte == unicodePrefix {
				// must be a unicode char in the form \uXXXX
				// or a surrogate pair in the form \uXXXX\uYYYY
				unicodeChar, bytesRead := unicodeSequenceToCharacter(stringBytes[pos:])
				if unicodeChar < 0 {
					return nil, fmt.Errorf("malformed unicode sequence in data")
				}
				pos += bytesRead
				lastWriteIndex += utf8.EncodeRune(outputBuffer[lastWriteIndex:], unicodeChar)
			} else if unescapedChar, ok := parseEscapedCharacter(nextByte); ok {
				outputBuffer[lastWriteIndex] = unescapedChar
				pos += 2 // escape sequence + escaped char
				lastWriteIndex++
			} else {
				return nil, fmt.Errorf("malformed unicode sequence in data")
			}

		case byteAtPos == '"', byteAtPos < ' ':
			// Invalid char, error out.
			return nil, fmt.Errorf("invalid character %v", string(byteAtPos))

		case byteAtPos < utf8.RuneSelf:
			// Valid ASCII char, use without parsing.
			outputBuffer[lastWriteIndex] = byteAtPos
			pos++
			lastWriteIndex++

		default:
			// Attempt to decode char as UTF-8, may get utf8.RuneError.
			// Should probably error out if the char cannot be decoded.
			char, size := utf8.DecodeRune(stringBytes[pos:])
			pos += size
			lastWriteIndex += utf8.EncodeRune(outputBuffer[lastWriteIndex:], char)
		}
	}

	return outputBuffer[0:lastWriteIndex], nil
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
