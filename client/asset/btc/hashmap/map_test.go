package hashmap

import (
	"os"
	"testing"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

func TestMap(t *testing.T) {
	dbDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("error creating temp directory: %v", err)
	}
	defer os.RemoveAll(dbDir)

	m, err := New(dbDir, "test", dex.StdOutLogger("HASHMAPTEST", dex.LevelDebug))
	if err != nil {
		t.Fatalf("constructor error: %v", err)
	}

	k, _ := chainhash.NewHashFromStr("AB")
	v, _ := chainhash.NewHashFromStr("CD")
	m.Set(*k, *v)
	h := m.Get(*k)
	if h == nil {
		t.Fatalf("failed to retrieve value")
	}
	if *h != *v {
		t.Fatalf("wrong hash retrived. expected %s, got %s", h, v)
	}

	noV := m.Get(chainhash.Hash{})
	if noV != nil {
		t.Fatalf("got value %s when none expected", noV)
	}
}
