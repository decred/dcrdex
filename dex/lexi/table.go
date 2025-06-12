// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dgraph-io/badger"
)

// Table is a prefixed section of the k-v DB. A Table can have indexes, such
// that data inserted into the Table will generates index entries for use in
// lookup and iteration.
type Table struct {
	*DB
	Indexes                 map[string]*Index
	name                    string
	prefix                  keyPrefix
	defaultSetOptions       setOpts
	defaultIterationOptions iteratorOpts
}

// IndexCfg is the configuration for an index.
type IndexCfg struct {
	// Version is the version of the index. If a different version is passed
	// in since the last time the table was created, the index will be
	// re-created. FBytes must be used if an index is being upgraded.
	Version uint32 `json:"version"`
	// Unique is whether the index is unique. If true, the index will not allow
	// duplicate values. If you attempt to add an entry that would create a
	// duplicate in the unique index, an error will be returned. However, if
	// Set() is called with the WithReplace() option, the existing entry will
	// be overwritten instead.
	Unique bool `json:"unique"`
	// F is the function that converts a key and value to an index entry. F
	// can only be used if this is the initial version of the index, and the
	// index was created when the table was initially created. If F is non-nil,
	// FBytes must be nil.
	F func(k, v KV) ([]byte, error) `json:"-"`
	// FBytes is the function that converts the marshalled key and value to an
	// index entry. If FBytes is non-nil, F must be nil.
	FBytes func(kB, vB []byte) ([]byte, error) `json:"-"`
}

// TableCfg is the configuration for a table.
type TableCfg struct {
	// Indexes are the indexes that are created for the table.
	Indexes map[string]*IndexCfg `json:"indexes"`
}

func (cfg *TableCfg) MarshalBinary() ([]byte, error) {
	return json.Marshal(cfg)
}

func (cfg *TableCfg) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, cfg)
}

// Table constructs a new table in the DB.
func (db *DB) Table(name string, cfg *TableCfg) (*Table, error) {
	p, err := db.prefixForName(name)
	if err != nil {
		return nil, err
	}

	cfgPrefix, err := db.prefixForName(name + "__tblcfg__")
	if err != nil {
		return nil, err
	}

	if cfg == nil {
		cfg = &TableCfg{}
	}

	table := &Table{
		DB:      db,
		name:    name,
		prefix:  p,
		Indexes: make(map[string]*Index, len(cfg.Indexes)),
	}

	err = db.Update(func(txn *badger.Txn) error {
		// Try to get existing config
		var currCfg *TableCfg
		var newTable bool
		cfgItem, err := txn.Get(cfgPrefix[:])
		if err != nil {
			if !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
			// No existing config, this is a new table
			currCfg = new(TableCfg)
			newTable = true
		} else {
			currCfg = new(TableCfg)
			err = cfgItem.Value(func(b []byte) error {
				if len(b) == 0 {
					return nil
				}
				return currCfg.UnmarshalBinary(b)
			})
			if err != nil {
				return err
			}
		}

		// Delete indexes that are in the previous config but not in the current one
		for indexName := range currCfg.Indexes {
			if _, exists := cfg.Indexes[indexName]; !exists {
				if err := table.deleteIndex(txn, indexName); err != nil {
					return fmt.Errorf("error deleting index %s: %w", indexName, err)
				}
			}
		}

		// Add all indexes in the current config
		for indexName, indexCfg := range cfg.Indexes {
			if indexCfg.FBytes == nil && indexCfg.F == nil {
				return fmt.Errorf("index %s has no F or FBytes", indexName)
			}
			if indexCfg.FBytes != nil && indexCfg.F != nil {
				return fmt.Errorf("index %s has both F and FBytes", indexName)
			}

			var updateTxn *badger.Txn
			// Check if we need to pass a non-nil txn to addIndex
			if newTable {
				// updateTxn = nil
			} else if oldCfg, exists := currCfg.Indexes[indexName]; !exists {
				updateTxn = txn
			} else if oldCfg.Version != indexCfg.Version {
				updateTxn = txn
			}
			if updateTxn != nil && indexCfg.FBytes == nil {
				return fmt.Errorf("updating index %s with a nil FBytes", indexName)
			}

			err = table.addIndex(indexName, indexCfg.FBytes, indexCfg.F, indexCfg.Unique, updateTxn)
			if err != nil {
				return fmt.Errorf("error adding index %s: %w", indexName, err)
			}
		}

		// Update the config with the new one
		cfgBytes, err := cfg.MarshalBinary()
		if err != nil {
			return fmt.Errorf("error marshaling table config: %w", err)
		}

		return txn.Set(cfgPrefix[:], cfgBytes)
	})

	if err != nil {
		return nil, err
	}

	return table, nil
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
	d := &datum{v: vB, indexes: make([][]byte, 0, len(t.Indexes))}
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
		for _, idx := range t.Indexes {
			var indexEntry []byte
			if indexEntry, err = idx.add(txn, k, v, dbID); err != nil {
				// Handle unique index conflicts
				var indexConflictError uniqueIndexConflictError
				switch {
				case errors.As(err, &indexConflictError):
					// If the index is unique and we're not replacing, return an error
					if !opts.replace {
						return fmt.Errorf("index uniqueness violation on %q", indexConflictError.indexName)
					}

					// If we're replacing, delete the old entry
					if err := t.removeTableEntry(txn, indexConflictError.conflictDBID); err != nil {
						return fmt.Errorf("error deleting conflicting entry from index %q: %w", indexConflictError.indexName, err)
					}

					// Try again
					indexEntry, err = idx.add(txn, k, v, dbID)
					if err != nil {
						return fmt.Errorf("error adding entry to index after deleting conflicting entry: %w", err)
					}
				default:
					return fmt.Errorf("error adding entry to index: %w", err)
				}
			}

			if indexEntry != nil {
				d.indexes = append(d.indexes, indexEntry)
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
