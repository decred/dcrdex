package lexi

import (
	"bytes"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
)

func newTestDB(t *testing.T) (*DB, func()) {
	tmpDir := t.TempDir()
	db, err := New(&Config{
		Path: filepath.Join(tmpDir, "test.db"),
		Log:  dex.StdOutLogger("T", dex.LevelInfo),
	})
	if err != nil {
		t.Fatalf("error constructing db: %v", err)
	}
	return db, func() {}
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

func valueIndex(k, v KV) ([]byte, error) {
	return v.(*tValue).idx, nil
}

func valueKey(k, v KV) ([]byte, error) {
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
		if err := tbl.Set(k, v); err != nil {
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

	// Make sure we can iterate the table directly
	i = 0
	if err := tbl.Iterate(nil, func(it *Iter) error {
		i++
		return nil
	}); err != nil {
		t.Fatalf("Error iterating table: %v", err)
	}
	if i != 50 {
		t.Fatal("table didn't have 50")
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

func TestUniqueIndex(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	tbl, err := db.Table("UniqueTest")
	if err != nil {
		t.Fatalf("Error creating table: %v", err)
	}

	uniqueIdx, err := tbl.AddUniqueIndex("Unique", func(k, v KV) ([]byte, error) {
		return v.(*tValue).idx, nil
	})
	if err != nil {
		t.Fatalf("Error adding unique index: %v", err)
	}

	key1 := []byte("key1")
	key2 := []byte("key2")

	// Both values have the same index key to test uniqueness constraint
	sameIndexKey := []byte{0x01}

	val1 := &tValue{
		k:   key1,
		v:   []byte("value1"),
		idx: sameIndexKey,
	}
	val2 := &tValue{
		k:   key2,
		v:   []byte("value2"),
		idx: sameIndexKey,
	}

	// First insert should succeed
	if err := tbl.Set(key1, val1); err != nil {
		t.Fatalf("Error setting first value: %v", err)
	}

	// Second insert with same index key should fail without WithReplace()
	err = tbl.Set(key2, val2)
	if err == nil {
		t.Fatal("Expected error when inserting duplicate index value without WithReplace(), but got none")
	}
	if !strings.Contains(err.Error(), "index uniqueness violation") {
		t.Fatalf("Expected unique index error, got: %v", err)
	}

	// Verify the first value is still there
	var retrievedVal []byte
	numEntries := 0
	err = tbl.Iterate(nil, func(it *Iter) error {
		numEntries++
		err := it.V(func(v []byte) error {
			retrievedVal = v
			return nil
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Error iterating table: %v", err)
	}
	if !bytes.Equal(retrievedVal, val1.v) {
		t.Fatalf("Expected value1 to still be in table, got %s", retrievedVal)
	}
	if numEntries != 1 {
		t.Fatalf("Expected 1 entry in table, got %d", numEntries)
	}

	// Second insert with same index key should succeed with WithReplace()
	if err := tbl.Set(key2, val2, WithReplace()); err != nil {
		t.Fatalf("Error setting second value with WithReplace(): %v", err)
	}

	// Verify the replacement worked by checking the table content
	numEntries = 0
	var lastVal []byte
	err = tbl.Iterate(nil, func(it *Iter) error {
		numEntries++
		return it.V(func(v []byte) error {
			lastVal = v
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Error iterating table after replace: %v", err)
	}
	if numEntries != 1 {
		t.Fatalf("Expected 1 value in table after replace, got %d", numEntries)
	}
	if !bytes.Equal(lastVal, val2.v) {
		t.Fatalf("Expected replaced value to be value2, got %s", lastVal)
	}

	// Verify we can iterate through the unique index and get the correct result
	numEntries = 0
	uniqueIdx.Iterate(nil, func(it *Iter) error {
		numEntries++
		return it.V(func(v []byte) error {
			if !bytes.Equal(v, val2.v) {
				t.Fatalf("Expected value2 when iterating unique index, got %s", v)
			}
			return nil
		})
	})
	if numEntries != 1 {
		t.Fatalf("Expected 1 value in unique index, got %d", numEntries)
	}
}

// TestNotIndexed tests that when an index mapping function returns
// ErrNotIndexed, the datum is not added to the index.
func TestNotIndexed(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	tbl, err := db.Table("NotIndexedTest")
	if err != nil {
		t.Fatalf("Error creating table: %v", err)
	}

	// Create an index that only indexes even values where the first byte is
	// even.
	idx, err := tbl.AddIndex("EvenOnly", func(k, v KV) ([]byte, error) {
		val := v.(*tValue)
		if len(val.v) > 0 && val.v[0]%2 == 0 {
			return []byte{val.v[0]}, nil
		}
		return nil, ErrNotIndexed
	})
	if err != nil {
		t.Fatalf("Error adding index: %v", err)
	}

	// Insert 10 values, with alternating even/odd first bytes
	for i := 0; i < 10; i++ {
		k := []byte{byte(i)}
		v := &tValue{
			k:   k,
			v:   append([]byte{byte(i)}, encode.RandomBytes(5)...),
			idx: []byte{byte(i)},
		}
		if err := tbl.Set(k, v); err != nil {
			t.Fatalf("Error setting value %d: %v", i, err)
		}
	}

	count := 0
	expectedValues := []byte{0, 2, 4, 6, 8}
	foundValues := make([]byte, 0, 5)
	err = idx.Iterate(nil, func(it *Iter) error {
		count++
		return it.V(func(vB []byte) error {
			if len(vB) > 0 {
				foundValues = append(foundValues, vB[0])
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Error iterating index: %v", err)
	}

	// Ensure that the number of values in the index is correct,
	// and that the expected values were found.
	if count != 5 {
		t.Fatalf("Expected 5 indexed values, got %d", count)
	}
	sort.Slice(foundValues, func(i, j int) bool {
		return foundValues[i] < foundValues[j]
	})
	if !reflect.DeepEqual(foundValues, expectedValues) {
		t.Fatalf("Expected values %v, got %v", expectedValues, foundValues)
	}

	// Ensure the table has all the values, even those that were not indexed.
	tableCount := 0
	err = tbl.Iterate(nil, func(it *Iter) error {
		tableCount++
		return nil
	})
	if err != nil {
		t.Fatalf("Error iterating table: %v", err)
	}
	if tableCount != 10 {
		t.Fatalf("Expected 10 table entries, got %d", tableCount)
	}

	// Delete an even and an odd value
	evenKey := []byte{2}
	oddKey := []byte{3}
	if err := tbl.Delete(evenKey); err != nil {
		t.Fatalf("Error deleting even key: %v", err)
	}
	if err := tbl.Delete(oddKey); err != nil {
		t.Fatalf("Error deleting odd key: %v", err)
	}

	// Recheck the index after deletion
	count = 0
	expectedValues = []byte{0, 4, 6, 8} // 2 was removed
	foundValues = make([]byte, 0, 4)
	err = idx.Iterate(nil, func(it *Iter) error {
		count++
		return it.V(func(vB []byte) error {
			if len(vB) > 0 {
				foundValues = append(foundValues, vB[0])
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Error iterating index after deletion: %v", err)
	}

	// Ensure that the number of values in the index is correct after deletion,
	// and that the expected values were found.
	if count != 4 {
		t.Fatalf("Expected 4 indexed values after deletion, got %d", count)
	}
	sort.Slice(foundValues, func(i, j int) bool {
		return foundValues[i] < foundValues[j]
	})
	if !reflect.DeepEqual(foundValues, expectedValues) {
		t.Fatalf("Expected values %v after deletion, got %v", expectedValues, foundValues)
	}

	// Recheck the table after deletion
	tableCount = 0
	err = tbl.Iterate(nil, func(it *Iter) error {
		tableCount++
		return nil
	})
	if err != nil {
		t.Fatalf("Error iterating table after deletion: %v", err)
	}
	if tableCount != 8 {
		t.Fatalf("Expected 8 table entries after deletion, got %d", tableCount)
	}
}
