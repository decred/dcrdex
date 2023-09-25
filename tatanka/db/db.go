// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
)

type DB struct {
	log dex.Logger
	// peerDB       KeyValueDB
	reputationDB KeyValueDB
	bondsDB      KeyValueDB
}

func New(dir string, log dex.Logger) (*DB, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("error creating db dir: %w", err)
	}
	// peerDB, err := NewFileDB(filepath.Join(dir, "peers.db"), log.SubLogger("PEER.DB"))
	// if err != nil {
	// 	return nil, fmt.Errorf("error opening peer DB: %w", err)
	// }
	reputationDB, err := NewFileDB(filepath.Join(dir, "reputation.db"), log.SubLogger("REP.DB"))
	if err != nil {
		return nil, fmt.Errorf("error opening reputation DB: %w", err)
	}
	bondsDB, err := NewFileDB(filepath.Join(dir, "bonds.db"), log.SubLogger("BONDS.DB"))
	if err != nil {
		return nil, fmt.Errorf("error opening bonds db: %w", err)
	}
	return &DB{
		log: log,
		// peerDB:       peerDB,
		reputationDB: reputationDB,
		bondsDB:      bondsDB,
	}, nil
}
