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
	defaultIterationOptions iteratorOpts
}

// AddIndex adds an index to a Table. Once an Index is added, every datum
// Set in the Table will generate an entry in the Index too.
func (t *Table) AddIndex(name string, f func(k, v KV) ([]byte, error)) (*Index, error) {
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
	}
	t.indexes = append(t.indexes, idx)
	return idx, nil
}

func (idx *Index) add(txn *badger.Txn, k, v KV, dbID DBID) ([]byte, error) {
	idxB, err := idx.f(k, v)
	if err != nil {
		return nil, fmt.Errorf("error getting index value: %w", err)
	}
	b := prefixedKey(idx.prefix, append(idxB, dbID[:]...))
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
	return i.d, err
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
				copy(dbID[:], k)
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
