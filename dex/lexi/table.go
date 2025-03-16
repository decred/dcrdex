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
	name                    string
	prefix                  keyPrefix
	indexes                 []*Index
	defaultSetOptions       setOpts
	defaultIterationOptions iteratorOpts
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

// GetRaw retrieves a value from the Table as raw bytes.
func (t *Table) GetRaw(k KV) (b []byte, err error) {
	kB, err := parseKV(k)
	if err != nil {
		return nil, fmt.Errorf("error marshaling key: %w", err)
	}
	err = t.View(func(txn *badger.Txn) error {
		dbID, err := t.keyID(txn, kB, true)
		if err != nil {
			return convertError(err)
		}
		d, err := t.get(txn, dbID)
		if err != nil {
			return err
		}
		b = d.v
		return nil
	})
	return
}

// Get retrieves a value from the Table.
func (t *Table) Get(k KV, thing encoding.BinaryUnmarshaler) error {
	b, err := t.GetRaw(k)
	if err != nil {
		return err
	}
	return thing.UnmarshalBinary(b)
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

// UseDefaultSetOptions sets default options for Set.
func (t *Table) UseDefaultSetOptions(setOpts ...SetOption) {
	for i := range setOpts {
		setOpts[i](&t.defaultSetOptions)
	}
}

// Set inserts a new value for the key, and creates index entries.
func (t *Table) Set(k, v KV, setOpts ...SetOption) error {
	kB, err := parseKV(k)
	if err != nil {
		return fmt.Errorf("error marshaling key: %w", err)
	}
	// zero length keys are not allowed because it screws up the reverse
	// iteration scheme.
	if len(kB) == 0 {
		return errors.New("no zero-length keys allowed")
	}
	vB, err := parseKV(v)
	if err != nil {
		return fmt.Errorf("error marshaling value: %w", err)
	}
	opts := t.defaultSetOptions
	for i := range setOpts {
		setOpts[i](&opts)
	}
	d := &datum{v: vB, indexes: make([][]byte, len(t.indexes))}
	return t.Update(func(txn *badger.Txn) error {
		dbID, err := t.keyID(txn, kB, false)
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

		// Add to indexes
		for i, idx := range t.indexes {
			if d.indexes[i], err = idx.add(txn, k, v, dbID); err != nil {
				// Handle unique index conflicts
				var indexConflictError uniqueIndexConflictError
				if errors.As(err, &indexConflictError) {
					// If the index is unique and we're not replacing, return an error
					if !opts.replace {
						return fmt.Errorf("index uniqueness violation on %q", indexConflictError.indexName)
					}

					// If we're replacing, delete the old entry
					if err := t.removeTableEntry(txn, indexConflictError.conflictDBID); err != nil {
						return fmt.Errorf("error deleting conflicting entry from index %q: %w", indexConflictError.indexName, err)
					}

					// Try again
					d.indexes[i], err = idx.add(txn, k, v, dbID)
					if err != nil {
						return fmt.Errorf("error adding entry to index after deleting conflicting entry: %w", err)
					}

					continue
				}

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
		dbID, err := t.keyID(txn, kB, true)
		if err != nil {
			return convertError(err)
		}
		return t.removeTableEntry(txn, dbID)
	})
}

func (t *Table) removeTableEntry(txn *badger.Txn, dbID DBID) error {
	item, err := txn.Get(prefixedKey(t.prefix, dbID[:]))
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

// UseDefaultIterationOptions sets default options for Iterate.
func (t *Table) UseDefaultIterationOptions(optss ...IterationOption) {
	for i := range optss {
		optss[i](&t.defaultIterationOptions)
	}
}

// Iterate iterates the table.
func (t *Table) Iterate(prefixI KV, f func(*Iter) error, iterOpts ...IterationOption) error {
	return t.iterate(t.prefix, t, t.defaultIterationOptions, false, prefixI, f, iterOpts...)
}
