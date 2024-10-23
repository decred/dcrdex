// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"encoding"
	"errors"
	"fmt"

	"github.com/dgraph-io/badger"
)

// Table is a prefixed section of the k-v DB. A Table can have indexes, such
// that data inserted into the Table will generates index entries for use in
// lookup and iteration.
type Table struct {
	*DB
	name              string
	prefix            keyPrefix
	indexes           []*Index
	defaultSetOptions setOpts
}

// Table constructs a new table in the DB.
func (db *DB) Table(name string) (*Table, error) {
	p, err := db.prefixForName(name)
	if err != nil {
		return nil, err
	}
	return &Table{
		DB:     db,
		name:   name,
		prefix: p,
	}, nil
}

// UseDefaultSetOptions sets default options for Set.
func (t *Table) UseDefaultSetOptions(setOpts ...SetOption) {
	for i := range setOpts {
		setOpts[i](&t.defaultSetOptions)
	}
}

// Get retrieves a value from the Table.
func (t *Table) Get(k encoding.BinaryMarshaler, v encoding.BinaryUnmarshaler) error {
	kB, err := k.MarshalBinary()
	if err != nil {
		return fmt.Errorf("error marshaling key: %w", err)
	}
	return t.View(func(txn *badger.Txn) error {
		dbID, err := t.keyID(txn, kB)
		if err != nil {
			return convertError(err)
		}
		d, err := t.get(txn, dbID)
		if err != nil {
			return err
		}
		return v.UnmarshalBinary(d.v)
	})
}

// func (t *Table) GetDBID(dbID DBID, v encoding.BinaryUnmarshaler) error {
// 	return t.View(func(txn *badger.Txn) error {
// 		d, err := t.get(txn, dbID)
// 		if err != nil {
// 			return err
// 		}
// 		return v.UnmarshalBinary(d.v)
// 	})
// }

func (t *Table) get(txn *badger.Txn, dbID DBID) (d *datum, err error) {
	item, err := txn.Get(prefixedKey(t.prefix, dbID[:]))
	if err != nil {
		return nil, convertError(err)
	}
	err = item.Value(func(dB []byte) error {
		d, err = decodeDatum(dB)
		if err != nil {
			return fmt.Errorf("error decoding datum: %w", err)
		}
		return nil
	})
	return
}

type setOpts struct {
	replace bool
}

// SetOptions is an knob to control how items are inserted into the table with
// Set.
type SetOption func(opts *setOpts)

// WithReplace allows replacing pre-existing values when calling Set.
func WithReplace() SetOption {
	return func(opts *setOpts) {
		opts.replace = true
	}
}

// Set inserts a new value for the key, and creates index entries.
func (t *Table) Set(k, v encoding.BinaryMarshaler, setOpts ...SetOption) error {
	kB, err := k.MarshalBinary()
	if err != nil {
		return fmt.Errorf("error marshaling key: %w", err)
	}
	// zero length keys are not allowed because it screws up the reverse
	// iteration scheme.
	if len(kB) == 0 {
		return errors.New("no zero-length keys allowed")
	}
	vB, err := v.MarshalBinary()
	if err != nil {
		return fmt.Errorf("error marshaling value: %w", err)
	}
	opts := t.defaultSetOptions
	for i := range setOpts {
		setOpts[i](&opts)
	}
	d := &datum{v: vB, indexes: make([][]byte, len(t.indexes))}
	return t.Update(func(txn *badger.Txn) error {
		dbID, err := t.keyID(txn, kB)
		if err != nil {
			return convertError(err)
		}
		// See if an entry already exists
		oldDatum, err := t.get(txn, dbID)
		if !errors.Is(err, ErrKeyNotFound) {
			if err != nil {
				return fmt.Errorf("error looking for existing entry: %w", err)
			}
			// We found an old entry
			if !opts.replace {
				return errors.New("attempted to replace an entry without specifying WithReplace")
			}
			// Delete any old indexes
			for _, k := range oldDatum.indexes {
				if err := txn.Delete(k); err != nil {
					return fmt.Errorf("error deleting replaced datum's index entry; %w", err)
				}
			}
		}
		for i, idx := range t.indexes {
			if d.indexes[i], err = idx.add(txn, k, v, dbID); err != nil {
				return fmt.Errorf("error adding entry to index %q: %w", idx.name, err)
			}
		}
		dB, err := d.bytes()
		if err != nil {
			return fmt.Errorf("error encoding datum: %w", err)
		}
		return txn.Set(prefixedKey(t.prefix, dbID[:]), dB)
	})
}

// Delete deletes the data associated with the key, including any index entries
// and the id<->key mappings.
func (t *Table) Delete(kB []byte) error {
	return t.Update(func(txn *badger.Txn) error {
		dbID, err := t.keyID(txn, kB)
		if err != nil {
			return convertError(err)
		}
		item, err := txn.Get(dbID[:])
		if err != nil {
			return convertError(err)
		}
		return item.Value(func(dB []byte) error {
			d, err := decodeDatum(dB)
			if err != nil {
				return fmt.Errorf("error decoding datum: %w", err)
			}
			return t.deleteDatum(txn, dbID, d)
		})
	})
}

func (t *Table) deleteDatum(txn *badger.Txn, dbID DBID, d *datum) error {
	for _, k := range d.indexes {
		if err := txn.Delete(k); err != nil {
			return fmt.Errorf("error deleting index entry; %w", err)
		}
	}
	if err := txn.Delete(prefixedKey(t.prefix, dbID[:])); err != nil {
		return fmt.Errorf("error deleting table entry: %w", err)
	}
	return t.deleteDBID(txn, dbID)
}
