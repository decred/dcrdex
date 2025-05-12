// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package internal

const (
	CreatePointsTable = `CREATE TABLE IF NOT EXISTS %s (
		id BIGSERIAL PRIMARY KEY,
		account BYTEA,
		link BYTEA,             -- Order ID or Match ID
		class INT2,              -- Preimage, order (complete/cancel), or match
		outcome INT2
	);`

	CreatePointsIndex = `CREATE INDEX IF NOT EXISTS idx_points ON %s (account, class);`

	InsertPoints = `INSERT INTO %s (account, link, class, outcome) VALUES ($1, $2, $3, $4) RETURNING id;`

	SelectPoints = `SELECT id, link, class, outcome FROM %s WHERE account = $1 ORDER BY id;`

	PrunePoints = `DELETE FROM %s WHERE account = $1 AND class = $2 AND id <= $3;`

	ForgiveUser = `DELETE FROM %s WHERE account = $1 AND outcome NOT IN ($2, $3, $4);`
)
