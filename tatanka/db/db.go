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

	metaTable, err := db.Table("meta")
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
	scoreTable, err := db.Table("scores")
	if err != nil {
		return nil, fmt.Errorf("error constructing reputation table: %w", err)
	}
	// Scored peer index with timestamp sorting.
	scoredIdx, err := scoreTable.AddIndex("scored-stamp", func(_, v lexi.KV) ([]byte, error) {
		s, is := v.(*dbScore)
		if !is {
			return nil, fmt.Errorf("wrong type %T", v)
		}
		tB := make([]byte, 8)
		binary.BigEndian.PutUint64(tB, uint64(s.stamp.UnixMilli()))
		return append(s.scored[:], tB...), nil
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing reputation index: %w", err)
	}
	scoredIdx.UseDefaultIterationOptions(lexi.WithReverse())

	// Bonds. Keyed on coin ID
	bondsTable, err := db.Table("bonds")
	if err != nil {
		return nil, fmt.Errorf("error initializing bonds table: %w", err)
	}
	// Retrieve bonds by peer ID.
	bonderIdx, err := bondsTable.AddIndex("bonder", func(_, v lexi.KV) ([]byte, error) {
		b, is := v.(*dbBond)
		if !is {
			return nil, fmt.Errorf("wrong type %T", v)
		}
		return b.PeerID[:], nil
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing bonder index: %w", err)
	}
	// We'll periodically prune expired bonds.
	bondStampIdx, err := bondsTable.AddIndex("bond-stamp", func(_, v lexi.KV) ([]byte, error) {
		b, is := v.(*dbBond)
		if !is {
			return nil, fmt.Errorf("wrong type %T", v)
		}
		return encode.Uint64Bytes(uint64(b.Expiration.Unix())), nil
	})
	bondStampIdx.UseDefaultIterationOptions(lexi.WithReverse())
	if err != nil {
		return nil, fmt.Errorf("error initializing bond stamp index: %w", err)
	}
	return &DB{
		DB:           db,
		scores:       scoreTable,
		scoredIdx:    scoredIdx,
		log:          log,
		bonds:        bondsTable,
		bonderIdx:    bonderIdx,
		bondStampIdx: bondStampIdx,
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
