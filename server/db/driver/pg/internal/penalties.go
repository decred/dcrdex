package internal

const (
	// CreatePenaltyTable created the penalties table. It is keyed by id
	// which is an int64 that is incremented automatically by the database.
	CreatePenaltyTable = `CREATE TABLE IF NOT EXISTS %s (
		id BIGSERIAL PRIMARY KEY,   -- UNIQUE INDEX
		account_id BYTEA,
		broken_rule INT2,
		time INT8,
		duration INT8,
		details TEXT,
		forgiven INT2 DEFAULT 0
	);`

	// InsertPenalty inserts a row. The id is assigned automatically, and
	// forgiven is left as the default of 0.
	InsertPenalty = `INSERT INTO %s (account_id, broken_rule, time, duration, details)
		VALUES ($1, $2, $3, $4, $5);`

	// ForgivePenalty sets forgiven for a record with id to 1.
	ForgivePenalty = `UPDATE %s
		SET forgiven = 1
		WHERE id = $1;`

	// ForgivePenalties sets forgiven for not expired records with
	// account_id to 1.
	ForgivePenalties = `UPDATE %s
		SET forgiven = 1
		WHERE account_id = $1
		AND time+duration > $2;`

	// SelectPenalties selects all not forgiven, not expired rows for
	// account_id.
	SelectPenalties = `SELECT * FROM %s
		WHERE account_id = $1
		AND forgiven = 0
		AND time+duration > $2;`

	// SelectAllPenalties selects all rows for account_id.
	SelectAllPenalties = `SELECT * FROM %s
		WHERE account_id = $1;`
)
