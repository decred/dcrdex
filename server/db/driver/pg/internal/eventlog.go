// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package internal

const (
	CreateEventLogTable = `CREATE TABLE IF NOT EXISTS %s (
		seq BIGINT PRIMARY KEY,
		kind TEXT NOT NULL,
		event BYTEA NOT NULL,
		tx_data BYTEA NOT NULL,
		tip_hash BYTEA NOT NULL
	);`

	InsertEventLog = `INSERT INTO %s (seq, kind, event, tx_data, tip_hash)
		VALUES ($1, $2, $3, $4, $5);`

	LockEventLog = `LOCK TABLE %s IN EXCLUSIVE MODE;`

	SelectEventLogFrontier = `SELECT seq, tip_hash FROM %s
		ORDER BY seq DESC
		LIMIT 1;`

	SelectEventLogSince = `SELECT seq, kind, event, tx_data, tip_hash FROM %s
		WHERE seq > $1
		ORDER BY seq
		LIMIT $2;`
)
