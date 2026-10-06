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

	PrunePointsPastLimit = `WITH pruned AS (
			SELECT id
			FROM %[1]s
			WHERE account = $1 AND class = $2
			ORDER BY id DESC
			OFFSET $3
		)
		DELETE FROM %[1]s
		WHERE id IN (SELECT id FROM pruned);`

	// ForgiveUser deletes every non-success outcome for the account. $2-$4 are
	// the success outcomes: swap success, preimage success, order complete.
	ForgiveUser = `DELETE FROM %s WHERE account = $1 AND outcome NOT IN ($2, $3, $4);`

	// ForgiveMatchFailures deletes the account's failure outcomes for a single
	// match. $4 is the swap success outcome, the only non-failure outcome in
	// the match class.
	ForgiveMatchFailures = `DELETE FROM %s
		WHERE account = $1
			AND link = $2
			AND class = $3
			AND outcome <> $4;`
)
