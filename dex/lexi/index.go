// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"bytes"
	"errors"
	"fmt"

	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger"
)

const (
	// ErrEndIteration can be returned from the function passed to Iterate
	// to end iteration. No error will be returned from Iterate.
	ErrEndIteration = dex.ErrorKind("end iteration")
	// ErrNotIndexed can be returned from the function passed to AddIndex
	// to indicate that a datum should not be added to the index.
	ErrNotIndexed = dex.ErrorKind("not indexed")
)

// Index is just a lexicographically-ordered list of byte slices. An Index is
// associated with a Table, and a datum inserted into a table can put entries
// into the Index. The Index can be iterated to view sorted data in the table.
type Index struct {
	*DB
	name                    string
	table                   *Table
	prefix                  keyPrefix
	f                       func(k, v KV) ([]byte, error)
	unique                  bool
	defaultIterationOptions iteratorOpts
}

func (t *Table) addIndex(name string, f func(k, v KV) ([]byte, error), unique bool) (*Index, error) {
	p, err := t.prefixForName(t.name + "__idx__" + name)
	if err != nil {
		return nil, err
	}
	idx := &Index{
		DB:     t.DB,
		name:   name,
		table:  t,
		prefix: p,
		f:      f,
		unique: unique,
	}
	t.indexes = append(t.indexes, idx)
	return idx, nil
}

// AddIndex adds an index to a Table. After an index is added, every item
// added to the Table with Set will automatically generate a corresponding
// entry in this index.
func (t *Table) AddIndex(name string, f func(k, v KV) ([]byte, error)) (*Index, error) {
	return t.addIndex(name, f, false)
}

// AddUniqueIndex adds a unique index to a Table. After an index is added, every
// item added to the Table with Set will automatically generate a corresponding
// entry in this index. The unique index guarantees that no two entries in the
// index have the same value. If you attempt to add an entry that would create a
// duplicate in the unique index, an error will be returned. However, if Set() is
// called with the WithReplace() option, the existing entry will be overwritten
// instead.
func (t *Table) AddUniqueIndex(name string, f func(k, v KV) ([]byte, error)) (*Index, error) {
	return t.addIndex(name, f, true)
}

// DeleteIndex deletes all index entries for any index of this table with the
// given name.
func (db *DB) DeleteIndex(tableName, indexName string) error {
	tablePrefix, err := db.existingPrefixForName(tableName)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	indexPrefix, err := db.existingPrefixForName(tableName + "__idx__" + indexName)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	return db.Update(func(txn *badger.Txn) error {
		// Delete all the index entries
		if err := iteratePrefix(txn, indexPrefix[:], nil, func(i *badger.Iterator) error {
			return txn.Delete(i.Item().Key())
		}); err != nil {
			return fmt.Errorf("error deleting index entries: %w", err)
		}

		// Update all the datums to no longer reference the deleted index
		return iteratePrefix(txn, tablePrefix[:], nil, func(iter *badger.Iterator) error {
			return iter.Item().Value(func(dB []byte) error {
				d, err := decodeDatum(dB)
				if err != nil {
					return fmt.Errorf("error decoding datum during index deletion: %w", err)
				}

				// Filter out indexes that match the deleted index prefix
				newIndexes := make([][]byte, 0, len(d.indexes)-1)
				for _, idxB := range d.indexes {
					if !bytes.Equal(idxB[:prefixSize], indexPrefix[:]) {
						newIndexes = append(newIndexes, idxB)
					}
				}
				d.indexes = newIndexes

				b, err := d.bytes()
				if err != nil {
					return fmt.Errorf("error encoding datum after index deletion: %w", err)
				}
				if err := txn.Set(iter.Item().Key(), b); err != nil {
					return fmt.Errorf("error storing new datum after index deletion: %w", err)
				}
				return nil
			})
		})
	})
}

// ReIndex updates the index entries on this index for all items in the table.
// decoder should generate the value that would be passed to the index
// generation function supplied to (*Table).AddIndex for this index.
func (db *DB) ReIndex(tableName, indexName string, f func(k, v []byte) ([]byte, error)) error {
	tablePrefix, err := db.existingPrefixForName(tableName)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	indexPrefix, err := db.existingPrefixForName(tableName + "__idx__" + indexName)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	return db.Update(func(txn *badger.Txn) error {
		return iteratePrefix(txn, tablePrefix[:], nil, func(iter *badger.Iterator) error {
			return iter.Item().Value(func(dB []byte) error {
				d, err := decodeDatum(dB)
				if err != nil {
					return fmt.Errorf("error decoding datum: %w", err)
				}

				tableKey := iter.Item().Key()
				dbIDB := tableKey[prefixSize:]

				// Get the original key for this datum
				keyItem, err := txn.Get(prefixedKey(idToKeyPrefix, dbIDB))
				if err != nil {
					return fmt.Errorf("error finding key for entry: %w", err)
				}
				k, err := keyItem.ValueCopy(nil)
				if err != nil {
					return err
				}

				// Generate the new index entry
				idxB, err := f(k, d.v)
				if err != nil {
					return fmt.Errorf("indexer function error: %w", err)
				}
				indexEntry := prefixedKey(indexPrefix, append(idxB, dbIDB...))

				// Remove old index entries for this index and add the new one
				var replaced bool
				for j, idxBi := range d.indexes {
					if bytes.Equal(idxBi[:prefixSize], indexPrefix[:]) {
						// Delete the old index entry from the database
						if err := txn.Delete(idxBi); err != nil {
							return fmt.Errorf("error deleting old index entry during reindex: %w", err)
						}
						d.indexes[j] = indexEntry
						replaced = true
						break
					}
				}
				if !replaced {
					d.indexes = append(d.indexes, indexEntry)
				}

				// Update the datum with the new index references
				b, err := d.bytes()
				if err != nil {
					return fmt.Errorf("error encoding datum after index reindex: %w", err)
				}
				if err := txn.Set(tableKey, b); err != nil {
					return fmt.Errorf("error storing new datum after reindex: %w", err)
				}

				// Store the new index entry
				if err := txn.Set(indexEntry, nil); err != nil {
					return fmt.Errorf("error storing index entry for reindex: %w", err)
				}
				return nil
			})
		})
	})
}

// uniqueIndexConflictError is returned when a unique index conflict is encountered.
// The caller should delete the table entry identified by conflictDBID before adding
// the new entry.
type uniqueIndexConflictError struct {
	indexName    string
	conflictDBID DBID
}

func (e uniqueIndexConflictError) Error() string {
	return fmt.Sprintf("unique index conflict on %s", e.indexName)
}

func (idx *Index) checkForIndexConflict(txn *badger.Txn, idxB []byte, updatingDBID DBID) error {
	conflictPrefix := prefixedKey(idx.prefix, idxB)

	var conflictDBID DBID
	var foundConflict bool

	err := iteratePrefix(txn, conflictPrefix, nil, func(iter *badger.Iterator) error {
		item := iter.Item()
		key := item.Key()

		if len(key) < prefixSize+DBIDSize {
			return fmt.Errorf("index entry too small. length = %d", len(key))
		}

		existingDBID := newDBIDFromBytes(key[len(key)-DBIDSize:])

		if existingDBID != updatingDBID {
			conflictDBID = existingDBID
			foundConflict = true
			return ErrEndIteration
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error checking for index uniqueness: %w", err)
	}

	if foundConflict {
		return uniqueIndexConflictError{
			indexName:    idx.name,
			conflictDBID: conflictDBID,
		}
	}

	return nil
}

// add adds an entry to the index. If this element is not required to be
// indexed, a nil byte slice is returned and no error is returned.
func (idx *Index) add(txn *badger.Txn, k, v KV, dbID DBID) ([]byte, error) {
	idxB, err := idx.f(k, v)
	if err != nil {
		if errors.Is(err, ErrNotIndexed) {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting index value: %w", err)
	}
	b := prefixedKey(idx.prefix, append(idxB, dbID[:]...))

	if idx.unique {
		if err := idx.checkForIndexConflict(txn, idxB, dbID); err != nil {
			return nil, err
		}
	}

	if err := txn.Set(b, nil); err != nil {
		return nil, fmt.Errorf("error writing index entry: %w", err)
	}
	return b, nil
}

type iteratorOpts struct {
	update  bool
	reverse bool
	seek    []byte
}

// IterationOption is a knob to change how Iterate runs on an Index.
type IterationOption func(opts *iteratorOpts)

// WithUpdate must be used if the caller intends to make modifications during
// iteration, such as deleting elements.
func WithUpdate() IterationOption {
	return func(opts *iteratorOpts) {
		opts.update = true
	}
}

// WithReverse sets the direction of iteration to reverse-lexicographical.
func WithReverse() IterationOption {
	return func(opts *iteratorOpts) {
		opts.reverse = true
	}
}

// WithForward sets the direction of iteration to lexicographical.
func WithForward() IterationOption {
	return func(opts *iteratorOpts) {
		opts.reverse = false
	}
}

// WithSeek starts iteration at the specified prefix.
func WithSeek(prefix []byte) IterationOption {
	return func(opts *iteratorOpts) {
		opts.seek = prefix
	}
}

// UseDefaultIterationOptions sets default options for Iterate.
func (idx *Index) UseDefaultIterationOptions(optss ...IterationOption) {
	for i := range optss {
		optss[i](&idx.defaultIterationOptions)
	}
}

// Iter is an entry in the Index or Table. The caller can use Iter to access and
// delete data associated with the entry and it's datum.
type Iter struct {
	table   *Table
	isIndex bool
	item    *badger.Item
	txn     *badger.Txn
	dbID    DBID
	d       *datum
}

// V gives access to the datum bytes. The byte slice passed to f is only valid
// for the duration of the function call. The caller should make a copy if they
// intend to use the bytes outside of the scope of f.
func (i *Iter) V(f func(vB []byte) error) error {
	d, err := i.datum()
	if err != nil {
		return err
	}
	return f(d.v)
}

// K is the key for the datum.
func (i *Iter) K() ([]byte, error) {
	item, err := i.txn.Get(prefixedKey(idToKeyPrefix, i.dbID[:]))
	if err != nil {
		return nil, err
	}
	return item.ValueCopy(nil)
}

// Entry is the actual index entry when iterating an Index. When iterating a
// Table, this method doesn't really have a use, so we'll just return the DBID.
func (i *Iter) Entry(f func(idxB []byte) error) error {
	k := i.item.Key()
	if i.isIndex {
		if len(k) < prefixSize+DBIDSize {
			return fmt.Errorf("index entry too small. length = %d", len(k))
		}
		return f(k[prefixSize : len(k)-DBIDSize])
	}
	if len(k) < prefixSize {
		return fmt.Errorf("table key too small. length = %d", len(k))
	}
	return f(k[prefixSize:])
}

func (i *Iter) datum() (_ *datum, err error) {
	if i.d != nil {
		return i.d, nil
	}
	i.d, err = i.table.get(i.txn, i.dbID)
	if err != nil {
		return nil, err
	}
	return i.d, nil
}

// Delete deletes the indexed datum and any associated index entries.
func (i *Iter) Delete() error {
	d, err := i.datum()
	if err != nil {
		return err
	}
	return i.table.deleteDatum(i.txn, i.dbID, d)
}

// Iterate iterates the index, providing access to the index entry, datum, and
// datum key via the Iter.
func (idx *Index) Iterate(prefixI KV, f func(*Iter) error, iterOpts ...IterationOption) error {
	return idx.iterate(idx.prefix, idx.table, idx.defaultIterationOptions, true, prefixI, f, iterOpts...)
}

// iterate iterates a table or index.
func (db *DB) iterate(keyPfix keyPrefix, table *Table, io iteratorOpts, isIndex bool, prefixI KV, f func(*Iter) error, iterOpts ...IterationOption) error {
	prefix, err := parseKV(prefixI)
	if err != nil {
		return err
	}
	for i := range iterOpts {
		iterOpts[i](&io)
	}
	iterFunc := iteratePrefix
	if io.reverse {
		iterFunc = reverseIteratePrefix
	}
	viewUpdate := db.View
	if io.update {
		viewUpdate = db.Update
	}
	var seek []byte
	if len(io.seek) > 0 {
		seek = prefixedKey(keyPfix, io.seek)
	}
	return viewUpdate(func(txn *badger.Txn) error {
		return iterFunc(txn, prefixedKey(keyPfix, prefix), seek, func(iter *badger.Iterator) error {
			item := iter.Item()
			k := item.Key()

			var dbID DBID
			if isIndex {
				if len(k) < prefixSize+DBIDSize {
					return fmt.Errorf("invalid index entry length %d", len(k))
				}
				dbID = newDBIDFromBytes(k[len(k)-DBIDSize:])
			} else {
				if len(k) != prefixSize+DBIDSize {
					return fmt.Errorf("invalid table key length %d", len(k))
				}
				copy(dbID[:], k[prefixSize:])
			}
			return f(&Iter{
				isIndex: isIndex,
				table:   table,
				item:    iter.Item(),
				txn:     txn,
				dbID:    dbID,
			})
		})
	})
}

type badgerIterationOption func(opts *badger.IteratorOptions)

func withPrefetchSize(n int) badgerIterationOption {
	return func(opts *badger.IteratorOptions) {
		opts.PrefetchSize = n
	}
}

func iteratePrefix(txn *badger.Txn, prefix, seek []byte, f func(iter *badger.Iterator) error, iterOpts ...badgerIterationOption) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	for i := range iterOpts {
		iterOpts[i](&opts)
	}
	iter := txn.NewIterator(opts)
	defer iter.Close()

	if len(seek) == 0 {
		iter.Rewind()
	} else {
		iter.Seek(seek)
	}

	for ; iter.Valid(); iter.Next() {
		if err := f(iter); err != nil {
			if errors.Is(err, ErrEndIteration) {
				return nil
			}
			return err
		}
	}
	return nil
}

// https://github.com/dgraph-io/badger/issues/436#issuecomment-1073008604
func seekLast(it *badger.Iterator, prefix []byte) {
	tweaked := make([]byte, len(prefix))
	copy(tweaked, prefix)
	n := len(prefix)
	for n > 0 {
		if tweaked[n-1] == 0xff {
			n -= 1
		} else {
			tweaked[n-1] += 1
			break
		}
	}
	tweaked = tweaked[0:n]
	it.Seek(tweaked)
	if it.Valid() && bytes.Equal(tweaked, it.Item().Key()) {
		it.Next()
	}
}

func reverseIteratePrefix(txn *badger.Txn, prefix, seek []byte, f func(iter *badger.Iterator) error, iterOpts ...badgerIterationOption) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	opts.Reverse = true
	for i := range iterOpts {
		iterOpts[i](&opts)
	}
	iter := txn.NewIterator(opts)
	defer iter.Close()

	if len(seek) == 0 {
		seekLast(iter, prefix)
	} else {
		iter.Seek(append(seek, lastDBID[:]...))
	}

	for ; iter.ValidForPrefix(prefix); iter.Next() {
		if err := f(iter); err != nil {
			if errors.Is(err, ErrEndIteration) {
				return nil
			}
			return err
		}
	}
	return nil
}
