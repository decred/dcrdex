// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"testing"

	"decred.org/dcrdex/server/db"
)

func TestForkResetToken(t *testing.T) {
	frontier := &db.EventLogPosition{
		Seq:     41,
		TipHash: []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}
	if token := forkResetToken(frontier); token != "41:deadbeef01020304" {
		t.Fatalf("token = %q", token)
	}
	for name, p := range map[string]*db.EventLogPosition{
		"nil":        nil,
		"empty log":  {},
		"short hash": {Seq: 7, TipHash: []byte{0x01, 0x02}},
	} {
		if token := forkResetToken(p); token != "" {
			t.Fatalf("%s: token = %q, want empty", name, token)
		}
	}
}

func TestValidateForkResetToken(t *testing.T) {
	frontier := &db.EventLogPosition{
		Seq:     41,
		TipHash: []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}
	valid := []string{
		forkResetToken(frontier),  // the generated token round-trips
		"41:deadbeef01020304",     // minimum hash prefix
		"41:deadbeef010203040506", // the full hash from Position{...}
		" 41:deadbeef01020304 ",   // surrounding whitespace tolerated
	}
	for _, token := range valid {
		if err := ValidateForkResetToken(token, frontier); err != nil {
			t.Fatalf("token %q rejected: %v", token, err)
		}
	}

	invalid := map[string]string{
		"no separator":      "41deadbeef",
		"zero seq":          "0:deadbeef",
		"non-numeric seq":   "x:deadbeef",
		"wrong seq":         "40:deadbeef",
		"short hash prefix": "41:deadbeef",
		"odd hex":           "41:deadbee",
		"non-hex":           "41:zzzzzzzz",
		"wrong hash":        "41:deadbeee",
		"empty":             "",
	}
	for name, token := range invalid {
		if err := ValidateForkResetToken(token, frontier); err == nil {
			t.Fatalf("%s: token %q accepted", name, token)
		}
	}

	if err := ValidateForkResetToken("41:deadbeef", &db.EventLogPosition{}); err == nil {
		t.Fatal("token accepted against an empty frontier")
	}
	if err := ValidateForkResetToken("41:deadbeef", nil); err == nil {
		t.Fatal("token accepted against a nil frontier")
	}

	// Same seq, different tip hash → token must not match (single-shot).
	reseeded := &db.EventLogPosition{
		Seq:     41,
		TipHash: []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa},
	}
	if err := ValidateForkResetToken(forkResetToken(frontier), reseeded); err == nil {
		t.Fatal("pre-reset token accepted against the reseeded history")
	}
}
