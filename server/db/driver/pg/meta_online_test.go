// +build pgonline

package pg

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestStateHash(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	tHash := randomBytes(sha256.Size)
	err := archie.SetStateHash(tHash)
	if err != nil {
		t.Fatalf("error setting state hash: %v", err)
	}

	checkHash, err := archie.GetStateHash()
	if err != nil {
		t.Fatalf("error retrieving state hash: %v", err)
	}

	if !bytes.Equal(checkHash, tHash) {
		t.Fatalf("wrong state hash returned: %x != %x", checkHash, tHash)
	}
}
