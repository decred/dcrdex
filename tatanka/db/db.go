// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/lexi"
)

const DBVersion = 0

var (
	versionKey = []byte("dbver")
)

type DB struct {
	*lexi.DB
	log          dex.Logger
	scores       *lexi.Table
	scoredIdx    *lexi.Index
	bonds        *lexi.Table
	bonderIdx    *lexi.Index
	bondStampIdx *lexi.Index
}

func New(dir string, log dex.Logger) (*DB, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("error creating db dir: %w", err)
	}
	db, err := lexi.New(&lexi.Config{
		Path: filepath.Join(dir, "reputation.db"),
		Log:  log,
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing db: %w", err)
	}

	metaTable, err := db.Table("meta", nil)
	if err != nil {
		return nil, fmt.Errorf("error initializing meta table: %w", err)
	}
	verB, err := metaTable.GetRaw(versionKey)
	if err != nil {
		if errors.Is(err, lexi.ErrKeyNotFound) {
			// fresh install
			verB = []byte{DBVersion}
			metaTable.Set(versionKey, verB)
		} else {
			return nil, fmt.Errorf("error getting version")
		}
	}
	if len(verB) != 1 {
		return nil, fmt.Errorf("bad version length. bad")
	}
	ver := verB[0]
	if ver > DBVersion {
		return nil, fmt.Errorf("database reporting version from the future")
	}
	if ver < DBVersion {
		log.Warn("database version is older than current version. Upgrading...")
		// TODO: implement lexi upgrade logic
	}

	// Scores. Keyed on (scorer peer ID, scored peer ID).
	// Scored peer index with timestamp sorting.
	scoreIndexFunc := func(_, v lexi.KV) ([]byte, error) {
		s, is := v.(*dbScore)
		if !is {
			return nil, fmt.Errorf("wrong type %T", v)
		}
		tB := make([]byte, 8)
		binary.BigEndian.PutUint64(tB, uint64(s.stamp.UnixMilli()))
		return append(s.scored[:], tB...), nil
	}
	scoreTable, err := db.Table("scores", &lexi.TableCfg{
		Indexes: map[string]*lexi.IndexCfg{
			"scored-stamp": {
				Version: 1,
				F:       scoreIndexFunc,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error constructing reputation table: %w", err)
	}
	scoreTable.Indexes["scored-stamp"].UseDefaultIterationOptions(lexi.WithReverse())

	// Bonds. Keyed on coin ID, with index to retrieve bonds by peer ID
	// and timestamp for pruning.
	bonderIndexFunc := func(_, v lexi.KV) ([]byte, error) {
		b, is := v.(*dbBond)
		if !is {
			return nil, fmt.Errorf("wrong type %T", v)
		}
		return b.PeerID[:], nil
	}
	bondStampIndexFunc := func(_, v lexi.KV) ([]byte, error) {
		b, is := v.(*dbBond)
		if !is {
			return nil, fmt.Errorf("wrong type %T", v)
		}
		return encode.Uint64Bytes(uint64(b.Expiration.Unix())), nil
	}
	bondsTable, err := db.Table("bonds", &lexi.TableCfg{
		Indexes: map[string]*lexi.IndexCfg{
			"bonder": {
				Version: 1,
				F:       bonderIndexFunc,
			},
			"bond-stamp": {
				Version: 1,
				F:       bondStampIndexFunc,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing bonds table: %w", err)
	}
	bondsTable.Indexes["bond-stamp"].UseDefaultIterationOptions(lexi.WithReverse())

	return &DB{
		DB:           db,
		scores:       scoreTable,
		scoredIdx:    scoreTable.Indexes["scored-stamp"],
		log:          log,
		bonds:        bondsTable,
		bonderIdx:    bondsTable.Indexes["bonder"],
		bondStampIdx: bondsTable.Indexes["bond-stamp"],
	}, nil
}

func (db *DB) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	go func() {
		for {
			select {
			case <-time.After(time.Hour):
				wg.Add(1)
				db.pruneOldBonds()
				wg.Done()
			case <-ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

func (db *DB) pruneOldBonds() {
	if err := db.bondStampIdx.Iterate(nil, func(it *lexi.Iter) error {
		return it.Delete()
	}, lexi.WithSeek(encode.Uint64Bytes(uint64(time.Now().Unix()))), lexi.WithUpdate()); err != nil {
		db.log.Errorf("Error pruning bonds: %v", err)
	}
}
