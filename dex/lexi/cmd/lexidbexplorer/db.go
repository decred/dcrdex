// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"decred.org/dcrdex/dex/lexi"
	"github.com/dgraph-io/badger/v4"
)

// These constants mirror unexported values from the lexi package.
// They are duplicated here because they are internal implementation details
// that shouldn't be exported just for the explorer tool.
const prefixSize = 2 // lexi.prefixSize

// prefixToNamePrefix mirrors lexi.prefixToNamePrefix - the prefix used to
// map table/index prefixes to their names.
var prefixToNamePrefix = [prefixSize]byte{0x00, 0x00}

// nameToPrefixPrefix mirrors lexi.nameToPrefixPrefix - the prefix used to map
// table/index names to their key prefixes.
var nameToPrefixPrefix = [prefixSize]byte{0x00, 0x01}

// idToKeyPrefix mirrors lexi.idToKeyPrefix - the prefix used to map DBID -> key.
var idToKeyPrefix = [prefixSize]byte{0x00, 0x04}

const indexNameSeparator = "__idx__"

// entryData holds the key, optional index key, and value for a database entry.
type entryData struct {
	key      []byte
	indexKey []byte // only set when iterating an index
	value    []byte
}

func prefixedKey(prefix [prefixSize]byte, k []byte) []byte {
	pk := make([]byte, prefixSize+len(k))
	copy(pk, prefix[:])
	copy(pk[prefixSize:], k)
	return pk
}

func cloneBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func prefixForName(bdb *badger.DB, name string) ([prefixSize]byte, error) {
	var prefix [prefixSize]byte
	err := bdb.View(func(txn *badger.Txn) error {
		it, err := txn.Get(prefixedKey(nameToPrefixPrefix, []byte(name)))
		if err != nil {
			return err
		}
		return it.Value(func(b []byte) error {
			if len(b) != prefixSize {
				return fmt.Errorf("unexpected prefix size %d for name %q", len(b), name)
			}
			copy(prefix[:], b)
			return nil
		})
	})
	return prefix, err
}

// listTablesAndIndexes iterates the prefixToNamePrefix to get all table and
// index names, returning a map of tableName -> []indexNames.
func listTablesAndIndexes(bdb *badger.DB) (map[string][]string, error) {
	tables := make(map[string][]string) // tableName -> []indexNames

	err := bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefixToNamePrefix[:]
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(nameB []byte) error {
				name := string(nameB)
				if strings.Contains(name, indexNameSeparator) {
					// It's an index: "tableName__idx__indexName"
					parts := strings.SplitN(name, indexNameSeparator, 2)
					if len(parts) == 2 {
						tableName, indexName := parts[0], parts[1]
						tables[tableName] = append(tables[tableName], indexName)
					}
				} else {
					// It's a table
					if _, exists := tables[name]; !exists {
						tables[name] = []string{}
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	// Sort index names for consistent display
	for _, indexes := range tables {
		sort.Strings(indexes)
	}

	return tables, err
}

// iterateTable iterates all entries in a table and returns the entries.
func iterateTable(db *lexi.DB, tableName string, limit int) ([]entryData, error) {
	table, err := db.Table(tableName)
	if err != nil {
		return nil, err
	}

	var entries []entryData
	err = table.Iterate(nil, func(it *lexi.Iter) error {
		k, err := it.K()
		if err != nil {
			return err
		}

		var v []byte
		if err := it.V(func(vB []byte) error {
			v = cloneBytes(vB)
			return nil
		}); err != nil {
			return err
		}

		entries = append(entries, entryData{
			key:   cloneBytes(k),
			value: v,
		})

		if limit > 0 && len(entries) >= limit {
			return lexi.ErrEndIteration
		}
		return nil
	})

	return entries, err
}

// iterateIndex iterates all entries in an index and returns the entries
// in index order.
func iterateIndex(bdb *badger.DB, db *lexi.DB, tableName, indexName string, limit int) ([]entryData, error) {
	table, err := db.Table(tableName)
	if err != nil {
		return nil, err
	}

	indexPrefix, err := prefixForName(bdb, tableName+indexNameSeparator+indexName)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, fmt.Errorf("index %q not found on table %q", indexName, tableName)
	}
	if err != nil {
		return nil, err
	}

	var entries []entryData
	err = bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = indexPrefix[:]
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			idxEntryKey := item.KeyCopy(nil)
			if len(idxEntryKey) < prefixSize+8 {
				// indexPrefix + (idxKey + dbid) must be at least 8 bytes beyond prefix
				continue
			}

			rest := idxEntryKey[prefixSize:]
			if len(rest) < 8 {
				continue
			}
			idxKey := cloneBytes(rest[:len(rest)-8])
			dbIDB := rest[len(rest)-8:]

			// Resolve the original table key from DBID.
			keyItem, err := txn.Get(prefixedKey(idToKeyPrefix, dbIDB))
			if err != nil {
				return err
			}
			keyB, err := keyItem.ValueCopy(nil)
			if err != nil {
				return err
			}

			// Resolve the value via the table API (decodes the internal datum).
			vB, err := table.GetRaw(keyB, lexi.WithGetTxn(txn))
			if err != nil {
				return err
			}

			entries = append(entries, entryData{
				key:      cloneBytes(keyB),
				indexKey: idxKey,
				value:    cloneBytes(vB),
			})

			if limit > 0 && len(entries) >= limit {
				break
			}
		}
		return nil
	})

	return entries, err
}
