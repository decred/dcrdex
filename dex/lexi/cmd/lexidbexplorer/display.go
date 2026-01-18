// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(r[:maxLen])
	}
	return string(r[:maxLen-3]) + "..."
}

// tryParseJSON attempts to parse raw bytes as JSON.
// Returns the parsed data and true if successful, nil and false otherwise.
func tryParseJSON(raw []byte) (any, bool) {
	var data any
	if err := json.Unmarshal(raw, &data); err == nil {
		return data, true
	}
	return nil, false
}

// isPrintableString returns true if raw is valid UTF-8 with only printable characters.
func isPrintableString(raw []byte) bool {
	return utf8.Valid(raw) && isPrintable(raw)
}

// formatValue attempts to display raw bytes in the most human-readable format.
// It tries JSON first, then UTF-8 string, then falls back to hex dump.
func formatValue(raw []byte) string {
	if len(raw) == 0 {
		return "<empty>"
	}

	// Try JSON (most common encoding)
	if data, ok := tryParseJSON(raw); ok {
		if pretty, err := json.MarshalIndent(data, "", "  "); err == nil {
			return string(pretty)
		}
	}

	// Try UTF-8 string (if printable)
	if isPrintableString(raw) {
		return string(raw)
	}

	// Fall back to hex dump with ASCII sidebar
	return hexDump(raw)
}

// formatKey formats a key for display.
func formatKey(raw []byte) string {
	if len(raw) == 0 {
		return "<empty>"
	}

	if isPrintableString(raw) {
		return string(raw)
	}
	return hex.EncodeToString(raw)
}

// formatKeyShort returns a shortened key representation for list display.
func formatKeyShort(raw []byte, maxLen int) string {
	return truncate(formatKey(raw), maxLen)
}

// formatValuePreview returns a short preview of the value for list display.
func formatValuePreview(raw []byte, maxLen int) string {
	if len(raw) == 0 {
		return "<empty>"
	}

	// Try JSON (compact form for preview)
	if data, ok := tryParseJSON(raw); ok {
		if compact, err := json.Marshal(data); err == nil {
			return truncate(string(compact), maxLen)
		}
	}

	// Try string
	if isPrintableString(raw) {
		return truncate(string(raw), maxLen)
	}

	// Hex
	return truncate(hex.EncodeToString(raw), maxLen)
}

// isPrintable returns true if all runes in the byte slice are printable.
func isPrintable(b []byte) bool {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return false
		}
		b = b[size:]
	}
	return true
}

// hexDump creates a hex dump with ASCII sidebar, similar to hexdump -C.
func hexDump(data []byte) string {
	var sb strings.Builder
	for i := 0; i < len(data); i += 16 {
		// Offset
		sb.WriteString(fmt.Sprintf("%08x  ", i))

		// Hex bytes
		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				sb.WriteString(fmt.Sprintf("%02x ", data[i+j]))
			} else {
				sb.WriteString("   ")
			}
			if j == 7 {
				sb.WriteString(" ")
			}
		}

		// ASCII sidebar
		sb.WriteString(" |")
		for j := 0; j < 16 && i+j < len(data); j++ {
			c := data[i+j]
			if c >= 32 && c < 127 {
				sb.WriteByte(c)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}
