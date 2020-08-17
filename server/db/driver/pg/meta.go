// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

// GetStateHash retrieves that last stored swap state file hash.
func (a *Archiver) GetStateHash() ([]byte, error) {
	var h []byte
	if err := a.db.QueryRow(internal.RetrieveStateHash).Scan(&h); err != nil {
		return nil, err
	}
	return h, nil
}

// SetStateHash stores the swap state file hash.
func (a *Archiver) SetStateHash(h []byte) error {
	_, err := a.db.Exec(internal.SetStateHash, h)
	return err
}
