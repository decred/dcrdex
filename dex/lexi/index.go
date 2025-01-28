// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

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
	f                       func(k, v encoding.BinaryMarshaler) ([]byte, error)
	defaultIterationOptions iteratorOpts
}

// AddIndex adds an index to a Table. Once an Index is added, every datum
// Set in the Table will generate an entry in the Index too.
func (t *Table) AddIndex(name string, f func(k, v encoding.BinaryMarshaler) ([]byte, error)) (*Index, error) {
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

func (idx *Index) add(txn *badger.Txn, k, v encoding.BinaryMarshaler, dbID DBID) ([]byte, error) {
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

// Iter is an entry in the Index. The caller can use Iter to access and delete
// data associated with the index entry and it's datum.
type Iter struct {
	idx  *Index
	item *badger.Item
	txn  *badger.Txn
	dbID DBID
	d    *datum
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

// Entry is the actual index entry. These are the bytes returned by the
// generator passed to AddIndex.
func (i *Iter) Entry(f func(idxB []byte) error) error {
	k := i.item.Key()
	if len(k) < prefixSize+DBIDSize {
		return fmt.Errorf("index entry too small. length = %d", len(k))
	}
	return f(k[prefixSize : len(k)-DBIDSize])
}

func (i *Iter) datum() (_ *datum, err error) {
	if i.d != nil {
		return i.d, nil
	}
	k := i.item.Key()
	if len(k) < prefixSize+DBIDSize {
		return nil, fmt.Errorf("invalid index entry length %d", len(k))
	}
	dbID := newDBIDFromBytes(k[len(k)-DBIDSize:])
	i.d, err = i.idx.table.get(i.txn, dbID)
	return i.d, err
}

// Delete deletes the indexed datum and any associated index entries.
func (i *Iter) Delete() error {
	d, err := i.datum()
	if err != nil {
		return err
	}
	return i.idx.table.deleteDatum(i.txn, i.dbID, d)
}

// IndexBucket is any one of a number of common types whose binary encoding is
// straight-forward. An IndexBucket restricts Iterate to the entries in the
// index that have the bytes decoded from the IndexBucket as the prefix.
type IndexBucket interface{}

func parseIndexBucket(i IndexBucket) (b []byte, err error) {
	switch it := i.(type) {
	case []byte:
		b = it
	case uint32:
		b = make([]byte, 4)
		binary.BigEndian.PutUint32(b[:], it)
	case time.Time:
		b = make([]byte, 8)
		binary.BigEndian.PutUint64(b[:], uint64(it.UnixMilli()))
	case nil:
	default:
		err = fmt.Errorf("unknown IndexBucket type %T", it)
	}
	return
}

// Iterate iterates the index, providing access to the index entry, datum, and
// datum key via the Iter.
func (idx *Index) Iterate(prefixI IndexBucket, f func(*Iter) error, iterOpts ...IterationOption) error {
	prefix, err := parseIndexBucket(prefixI)
	if err != nil {
		return err
	}
	io := idx.defaultIterationOptions
	for i := range iterOpts {
		iterOpts[i](&io)
	}
	iterFunc := iteratePrefix
	if io.reverse {
		iterFunc = reverseIteratePrefix
	}
	viewUpdate := idx.View
	if io.update {
		viewUpdate = idx.Update
	}
	var seek []byte
	if len(io.seek) > 0 {
		seek = prefixedKey(idx.prefix, io.seek)
	}
	return viewUpdate(func(txn *badger.Txn) error {
		return iterFunc(txn, prefixedKey(idx.prefix, prefix), seek, func(iter *badger.Iterator) error {
			item := iter.Item()
			k := item.Key()
			if len(k) < prefixSize+DBIDSize {
				return fmt.Errorf("invalid index entry length %d", len(k))
			}
			return f(&Iter{
				idx:  idx,
				item: iter.Item(),
				txn:  txn,
				dbID: newDBIDFromBytes(k[len(k)-DBIDSize:]),
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
