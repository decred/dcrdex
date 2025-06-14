package lexi

import (
	"bytes"
	"fmt"
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

	tbl, err := db.Table("T", &TableCfg{
		Indexes: map[string]*IndexCfg{
			"I": {
				F: valueIndex,
			},
			"K": {
				F: valueKey,
			},
		},
	})
	if err != nil {
		t.Fatalf("Error creating table: %v", err)
	}

	idx, ok := tbl.Indexes["I"]
	if !ok {
		t.Fatalf("I index not found")
	}

	keyIdx, ok := tbl.Indexes["K"]
	if !ok {
		t.Fatalf("K index not found")
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

	uniqueIndexFunc := func(k, v KV) ([]byte, error) {
		return v.(*tValue).idx, nil
	}
	tbl, err := db.Table("UniqueTest", &TableCfg{
		Indexes: map[string]*IndexCfg{
			"unique": {
				F:      uniqueIndexFunc,
				Unique: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Error creating table: %v", err)
	}

	uniqueIdx, ok := tbl.Indexes["unique"]
	if !ok {
		t.Fatalf("Unique index not found")
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

	// Create an index that only indexes even values where the first byte is
	// even.
	evenOnlyIndexFunc := func(k, v KV) ([]byte, error) {
		val := v.(*tValue)
		if len(val.v) > 0 && val.v[0]%2 == 0 {
			return []byte{val.v[0]}, nil
		}
		return nil, ErrNotIndexed
	}
	tableCfg := &TableCfg{
		Indexes: map[string]*IndexCfg{
			"evenOnly": {
				F: evenOnlyIndexFunc,
			},
		},
	}
	tbl, err := db.Table("NotIndexedTest", tableCfg)
	if err != nil {
		t.Fatalf("Error creating table: %v", err)
	}
	idx, ok := tbl.Indexes["evenOnly"]
	if !ok {
		t.Fatalf("Index not found")
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

func TestTableIndexManagement(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	createTestValue := func(i int) *tValue {
		return &tValue{
			k:   []byte{byte(i)},
			v:   []byte{byte(i + 100)},
			idx: []byte{byte(i)},
		}
	}

	indexFunc := func(k, v KV) ([]byte, error) {
		return v.(*tValue).idx, nil
	}
	// Upgraded index function that returns keys in reverse order by prepending 0xFF - idx
	indexFuncBytes := func(kB, vB []byte) ([]byte, error) {
		if len(vB) == 0 {
			return nil, fmt.Errorf("empty value")
		}
		originalIdx := vB[0] - 100             // Convert back from value to index
		return []byte{0xFF - originalIdx}, nil // This will reverse the sort order
	}

	// Test 1: Create new table with F function (should work)
	t.Run("CreateTableWithF", func(t *testing.T) {
		tbl, err := db.Table("TestTable1", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"idx1": {
					F: indexFunc,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error creating table with F: %v", err)
		}

		// Add some test data
		for i := 0; i < 5; i++ {
			if err := tbl.Set([]byte{byte(i)}, createTestValue(i)); err != nil {
				t.Fatalf("Error setting value %d: %v", i, err)
			}
		}

		// Verify index works
		count := 0
		tbl.Indexes["idx1"].Iterate(nil, func(it *Iter) error {
			count++
			return nil
		})
		if count != 5 {
			t.Fatalf("Expected 5 items in index, got %d", count)
		}
	})

	// Test 2: Error cases for index configuration
	t.Run("IndexConfigErrors", func(t *testing.T) {
		// Neither F nor FBytes provided
		_, err := db.Table("ErrorTable1", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"bad": {},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "has no F or FBytes") {
			t.Fatalf("Expected error for missing F/FBytes, got: %v", err)
		}

		// Both F and FBytes provided
		_, err = db.Table("ErrorTable2", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"bad": {
					F:      indexFunc,
					FBytes: indexFuncBytes,
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "has both F and FBytes") {
			t.Fatalf("Expected error for both F/FBytes, got: %v", err)
		}
	})

	// Test 3: Update existing table - upgrade index by using a new name
	t.Run("UpgradeIndex", func(t *testing.T) {
		// First create table with initial index
		tbl, err := db.Table("UpgradeTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"idx1": {
					F: indexFunc,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error creating initial table: %v", err)
		}

		// Add test data
		for i := 0; i < 3; i++ {
			if err := tbl.Set([]byte{byte(i)}, createTestValue(i)); err != nil {
				t.Fatalf("Error setting initial value %d: %v", i, err)
			}
		}

		// Try to upgrade without FBytes (should fail)
		_, err = db.Table("UpgradeTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"idx1": {
					F: indexFunc,
				},
				"idx1_v2": {
					F: indexFunc,
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "updating index") {
			t.Fatalf("Expected error for upgrading without FBytes, got: %v", err)
		}

		// Upgrade with FBytes (should work)
		tbl, err = db.Table("UpgradeTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"idx1": {
					F: indexFunc,
				},
				"idx1_v2": {
					FBytes: indexFuncBytes,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error upgrading index: %v", err)
		}

		// Verify both indexes work and return values in correct order
		count := 0
		expectedOrder := []int{0, 1, 2} // Original order
		actualOrder := make([]int, 0, 3)
		tbl.Indexes["idx1"].Iterate(nil, func(it *Iter) error {
			count++
			return it.V(func(vB []byte) error {
				if len(vB) > 0 {
					actualOrder = append(actualOrder, int(vB[0]-100)) // Convert back to original index
				}
				return nil
			})
		})
		if count != 3 {
			t.Fatalf("Expected 3 items in original index, got %d", count)
		}
		if !reflect.DeepEqual(actualOrder, expectedOrder) {
			t.Fatalf("Original index order failed: expected %v, got %v", expectedOrder, actualOrder)
		}

		count = 0
		expectedOrder = []int{2, 1, 0} // Reverse order due to 0xFF - originalIdx transformation
		actualOrder = make([]int, 0, 3)
		tbl.Indexes["idx1_v2"].Iterate(nil, func(it *Iter) error {
			count++
			return it.V(func(vB []byte) error {
				if len(vB) > 0 {
					actualOrder = append(actualOrder, int(vB[0]-100)) // Convert back to original index
				}
				return nil
			})
		})
		if count != 3 {
			t.Fatalf("Expected 3 items in upgraded index, got %d", count)
		}
		if !reflect.DeepEqual(actualOrder, expectedOrder) {
			t.Fatalf("Upgraded index order failed: expected %v, got %v", expectedOrder, actualOrder)
		}
	})

	// Test 4: Add new index to existing table (requires FBytes)
	t.Run("AddNewIndexToExistingTable", func(t *testing.T) {
		// Create table with one index
		tbl, err := db.Table("AddIndexTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"original": {
					F: indexFunc,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error creating table: %v", err)
		}

		// Add test data
		for i := 0; i < 3; i++ {
			if err := tbl.Set([]byte{byte(i)}, createTestValue(i)); err != nil {
				t.Fatalf("Error setting value %d: %v", i, err)
			}
		}

		// Try to add new index without FBytes (should fail)
		_, err = db.Table("AddIndexTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"original": {
					F: indexFunc,
				},
				"new": {
					F: indexFunc,
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "updating index") {
			t.Fatalf("Expected error for adding index without FBytes, got: %v", err)
		}

		// Add new index with FBytes (should work)
		tbl, err = db.Table("AddIndexTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"original": {
					F: indexFunc,
				},
				"new": {
					FBytes: indexFuncBytes,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error adding new index: %v", err)
		}

		// Verify both indexes work
		count := 0
		tbl.Indexes["original"].Iterate(nil, func(it *Iter) error {
			count++
			return nil
		})
		if count != 3 {
			t.Fatalf("Expected 3 items in original index, got %d", count)
		}

		count = 0
		tbl.Indexes["new"].Iterate(nil, func(it *Iter) error {
			count++
			return nil
		})
		if count != 3 {
			t.Fatalf("Expected 3 items in new index, got %d", count)
		}
	})

	// Test 5: Remove existing indexes
	t.Run("RemoveExistingIndexes", func(t *testing.T) {
		// Create table with two indexes
		tbl, err := db.Table("RemoveTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"keep": {
					F: indexFunc,
				},
				"remove": {
					F: indexFunc,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error creating table: %v", err)
		}

		// Add test data
		for i := 0; i < 3; i++ {
			if err := tbl.Set([]byte{byte(i)}, createTestValue(i)); err != nil {
				t.Fatalf("Error setting value %d: %v", i, err)
			}
		}

		// Verify both indexes exist
		if _, ok := tbl.Indexes["keep"]; !ok {
			t.Fatal("keep index should exist")
		}
		if _, ok := tbl.Indexes["remove"]; !ok {
			t.Fatal("remove index should exist")
		}

		// Update table to remove one index
		tbl, err = db.Table("RemoveTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"keep": {
					F: indexFunc,
				},
				// "remove" index is intentionally omitted
			},
		})
		if err != nil {
			t.Fatalf("Error removing index: %v", err)
		}

		// Verify only the kept index exists
		if _, ok := tbl.Indexes["keep"]; !ok {
			t.Fatal("keep index should still exist")
		}
		if _, ok := tbl.Indexes["remove"]; ok {
			t.Fatal("remove index should not exist")
		}

		// Verify kept index still works
		count := 0
		tbl.Indexes["keep"].Iterate(nil, func(it *Iter) error {
			count++
			return nil
		})
		if count != 3 {
			t.Fatalf("Expected 3 items in kept index, got %d", count)
		}
	})

	// Test 6: Complex scenario - mix of add, remove, and upgrade
	t.Run("ComplexIndexChanges", func(t *testing.T) {
		// Initial table with two indexes
		tbl, err := db.Table("ComplexTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"upgrade": {
					F: indexFunc,
				},
				"remove": {
					F: indexFunc,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error creating complex table: %v", err)
		}

		// Add test data
		for i := 0; i < 5; i++ {
			if err := tbl.Set([]byte{byte(i)}, createTestValue(i)); err != nil {
				t.Fatalf("Error setting value %d: %v", i, err)
			}
		}

		// Complex update: upgrade one index, remove one, add one new
		tbl, err = db.Table("ComplexTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"upgrade": {
					F: indexFunc,
				},
				"upgrade_v2": {
					FBytes: indexFuncBytes,
				},
				// "remove" is omitted
				"add": {
					FBytes: indexFuncBytes, // New index
				},
			},
		})
		if err != nil {
			t.Fatalf("Error in complex update: %v", err)
		}

		// Verify final state
		if _, ok := tbl.Indexes["upgrade"]; !ok {
			t.Fatal("upgrade index should exist")
		}
		if _, ok := tbl.Indexes["upgrade_v2"]; !ok {
			t.Fatal("upgrade_v2 index should exist")
		}
		if _, ok := tbl.Indexes["remove"]; ok {
			t.Fatal("remove index should not exist")
		}
		if _, ok := tbl.Indexes["add"]; !ok {
			t.Fatal("add index should exist")
		}

		// Verify all remaining indexes work
		count := 0
		expectedUpgradeOrder := []int{0, 1, 2, 3, 4} // Original order
		actualUpgradeOrder := make([]int, 0, 5)
		tbl.Indexes["upgrade"].Iterate(nil, func(it *Iter) error {
			count++
			return it.V(func(vB []byte) error {
				if len(vB) > 0 {
					actualUpgradeOrder = append(actualUpgradeOrder, int(vB[0]-100)) // Convert back to original index
				}
				return nil
			})
		})
		if count != 5 {
			t.Fatalf("Expected 5 items in original upgrade index, got %d", count)
		}
		if !reflect.DeepEqual(actualUpgradeOrder, expectedUpgradeOrder) {
			t.Fatalf("Original upgrade index order failed: expected %v, got %v", expectedUpgradeOrder, actualUpgradeOrder)
		}

		count = 0
		expectedUpgradeOrder = []int{4, 3, 2, 1, 0} // Reverse order due to 0xFF - originalIdx transformation
		actualUpgradeOrder = make([]int, 0, 5)
		tbl.Indexes["upgrade_v2"].Iterate(nil, func(it *Iter) error {
			count++
			return it.V(func(vB []byte) error {
				if len(vB) > 0 {
					actualUpgradeOrder = append(actualUpgradeOrder, int(vB[0]-100)) // Convert back to original index
				}
				return nil
			})
		})
		if count != 5 {
			t.Fatalf("Expected 5 items in upgraded index, got %d", count)
		}
		if !reflect.DeepEqual(actualUpgradeOrder, expectedUpgradeOrder) {
			t.Fatalf("Upgraded index order failed: expected %v, got %v", expectedUpgradeOrder, actualUpgradeOrder)
		}

		count = 0
		tbl.Indexes["add"].Iterate(nil, func(it *Iter) error {
			count++
			return nil
		})
		if count != 5 {
			t.Fatalf("Expected 5 items in added index, got %d", count)
		}
	})

	// Test 7: Table with no indexes (should work)
	t.Run("TableWithoutIndexes", func(t *testing.T) {
		tbl, err := db.Table("NoIndexTable", nil)
		if err != nil {
			t.Fatalf("Error creating table without indexes: %v", err)
		}

		// Should be able to add data
		if err := tbl.Set([]byte("key"), createTestValue(1)); err != nil {
			t.Fatalf("Error setting value in table without indexes: %v", err)
		}

		// Later add indexes
		tbl, err = db.Table("NoIndexTable", &TableCfg{
			Indexes: map[string]*IndexCfg{
				"new": {
					FBytes: indexFuncBytes,
				},
			},
		})
		if err != nil {
			t.Fatalf("Error adding index to previously index-less table: %v", err)
		}

		// Verify index works
		count := 0
		tbl.Indexes["new"].Iterate(nil, func(it *Iter) error {
			count++
			return nil
		})
		if count != 1 {
			t.Fatalf("Expected 1 item in new index, got %d", count)
		}
	})
}
