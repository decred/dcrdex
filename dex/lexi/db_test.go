package lexi

import (
	"bytes"
	"encoding"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
)

func newTestDB(t *testing.T) (*DB, func()) {
	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("error making temp dir: %v", err)
	}
	db, err := New(&Config{
		Path: filepath.Join(tmpDir, "test.db"),
		Log:  dex.StdOutLogger("T", dex.LevelInfo),
	})
	if err != nil {
		t.Fatalf("error constructing db: %v", err)
	}
	return db, func() { os.RemoveAll(tmpDir) }
}

func TestPrefixes(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	pfix, err := db.prefixForName("1")
	if err != nil {
		t.Fatalf("error getting prefix 1: %v", err)
	}
	if pfix != firstAvailablePrefix {
		t.Fatalf("expected prefix %s, got %s", firstAvailablePrefix, pfix)
	}

	pfix, err = db.prefixForName("2")
	if err != nil {
		t.Fatalf("error getting prefix 2: %v", err)
	}
	if secondPfix := incrementPrefix(firstAvailablePrefix); pfix != secondPfix {
		t.Fatalf("expected prefix %s, got %s", secondPfix, pfix)
	}

	// Make sure requests for the same table name return the already-registered
	// prefix.
	pfix, err = db.prefixForName("1")
	if err != nil {
		t.Fatalf("error getting prefix 1 again: %v", err)
	}
	if pfix != firstAvailablePrefix {
		t.Fatalf("expected prefix %s, got %s", firstAvailablePrefix, pfix)
	}
}

type tValue struct {
	k, v, idx []byte
}

func (v *tValue) MarshalBinary() ([]byte, error) {
	return v.v, nil
}

func valueIndex(k, v encoding.BinaryMarshaler) ([]byte, error) {
	return v.(*tValue).idx, nil
}

func valueKey(k, v encoding.BinaryMarshaler) ([]byte, error) {
	return v.(*tValue).k, nil
}

func TestIndex(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	tbl, err := db.Table("T")
	if err != nil {
		t.Fatalf("Error creating table: %v", err)
	}

	idx, err := tbl.AddIndex("I", valueIndex)
	if err != nil {
		t.Fatalf("Error adding index: %v", err)
	}

	keyIdx, err := tbl.AddIndex("K", valueKey)
	if err != nil {
		t.Fatalf("Error adding index: %v", err)
	}

	// Put 100 values in.
	const nVs = 100
	vs := make([]*tValue, nVs)
	for i := 0; i < nVs; i++ {
		// Random value, but with a flag at the end.
		k := append(encode.RandomBytes(5), byte(i))
		// The index is keyed on i, with a prefix of 0, until 40, after which
		// the prefix is 1.
		indexKey := []byte{byte(i)}
		prefix := []byte{0}
		if i >= 40 {
			prefix = []byte{1}
		}
		indexKey = append(prefix, indexKey...)
		v := &tValue{k: indexKey, v: encode.RandomBytes(10), idx: []byte{byte(i)}}
		vs[i] = v
		if err := tbl.Set(B(k), v); err != nil {
			t.Fatalf("Error setting table entry: %v", err)
		}
	}

	// Iterate forwards.
	var i int
	idx.Iterate(nil, func(it *Iter) error {
		v := vs[i]
		it.V(func(vB []byte) error {
			if !bytes.Equal(vB, v.v) {
				t.Fatalf("Wrong bytes for forward iteration index %d", i)
			}
			return nil
		})
		i++
		return nil
	})
	if i != nVs {
		t.Fatalf("Expected to iterate %d items but only did %d", nVs, i)
	}

	// Iterate backwards
	i = nVs
	idx.Iterate(nil, func(it *Iter) error {
		i--
		v := vs[i]
		return it.V(func(vB []byte) error {
			if !bytes.Equal(vB, v.v) {
				t.Fatalf("Wrong bytes for reverse iteration index %d", i)
			}
			return nil
		})
	}, WithReverse())
	if i != 0 {
		t.Fatalf("Expected to iterate back to zero but only got to %d", i)
	}

	// Iterate forwards with prefix.
	keyIdx.Iterate([]byte{0}, func(it *Iter) error {
		v := vs[i]
		it.V(func(vB []byte) error {
			if !bytes.Equal(vB, v.v) {
				t.Fatalf("Wrong bytes for forward iteration index %d", i)
			}
			return nil
		})
		i++
		return nil
	})
	if i != 40 {
		t.Fatalf("Expected to iterate 40 items but only did %d", i)
	}

	// Iterate backwards with prefix.
	keyIdx.Iterate([]byte{0}, func(it *Iter) error {
		i--
		v := vs[i]
		return it.V(func(vB []byte) error {
			if !bytes.Equal(vB, v.v) {
				t.Fatalf("Wrong bytes for reverse iteration index %d", i)
			}
			return nil
		})
	}, WithReverse())
	if i != 0 {
		t.Fatalf("Expected to iterate back to zero but only got to %d", i)
	}

	// Iterate forward and delete the first half.
	i = 0
	if err := idx.Iterate(nil, func(it *Iter) error {
		if i < 50 {
			i++
			return it.Delete()
		}
		return ErrEndIteration
	}, WithUpdate()); err != nil {
		t.Fatalf("Error iterating forward to delete entries: %v", err)
	}
	if i != 50 {
		t.Fatalf("Expected to iterate forward to 50, but only got to %d", i)
	}

	idx.Iterate(nil, func(it *Iter) error {
		return it.V(func(vB []byte) error {
			if !bytes.Equal(vB, vs[50].v) {
				t.Fatal("Wrong first iteration item after deletion")
			}
			return ErrEndIteration
		})
	})

	// Seek a specific item.
	i = 75
	idx.Iterate(nil, func(it *Iter) error {
		if i == 75 {
			i--
			return it.V(func(vB []byte) error {
				if !bytes.Equal(vB, vs[75].v) {
					t.Fatal("first item wasn't 25")
				}
				return nil
			})
		} else if i == 74 {
			return ErrEndIteration
		}
		t.Fatal("reached an unexpected value")
		return nil
	}, WithSeek(vs[75].idx), WithReverse())
	if i != 74 {
		t.Fatal("never reached 74")
	}
}

func TestDatum(t *testing.T) {
	testEncodeDecode := func(tag string, d *datum) {
		t.Helper()
		b, err := d.bytes()
		if err != nil {
			t.Fatalf("%s: error encoding simple datum: %v", tag, err)
		}
		reD, err := decodeDatum(b)
		if err != nil {
			t.Fatalf("%s: error decoding simple datum: %v", tag, err)
		}
		if !bytes.Equal(reD.v, d.v) {
			t.Fatalf("%s: decoding datum value incorrect. %x != %x", tag, reD.v, d.v)
		}
		if d.version != 0 {
			t.Fatalf("%s: wrong datum version. expected %d, got %d", tag, d.version, reD.version)
		}
		if len(d.indexes) != len(reD.indexes) {
			t.Fatalf("%s: wrong number of indexes. wanted %d, got %d", tag, len(d.indexes), reD.indexes)
		}
		for i, idx := range d.indexes {
			if !bytes.Equal(idx, reD.indexes[i]) {
				t.Fatalf("%s: Wrong index # %d", tag, i)
			}
		}
	}

	d := &datum{version: 1, v: []byte{0x01}}
	if _, err := d.bytes(); err == nil || !strings.Contains(err.Error(), "unknown datum version") {
		t.Fatalf("Wrong error for unknown datum version: %v", err)
	}
	d.version = 0

	testEncodeDecode("simple", d)

	d = &datum{v: encode.RandomBytes(300)}
	d.indexes = append(d.indexes, encode.RandomBytes(5))
	d.indexes = append(d.indexes, encode.RandomBytes(300))
	testEncodeDecode("complex", d)
}
