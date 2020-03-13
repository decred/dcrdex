package internal

const (
	// CreateEpochsTable creates a table specified via the %s printf specifier
	// for epoch data.
	CreateEpochsTable = `CREATE TABLE IF NOT EXISTS %s (
		epoch_idx INT8,
		epoch_dur INT4,       -- epoch duration in milliseconds
		match_time INT8,      -- time at which matching and book/unbooks began
		csum BYTEA,           -- commitment checksum
		seed BYTEA,           -- preimage-derived shuffle seed
		revealed BYTEA[],     -- order IDs with revealed preimages
		missed BYTEA[],       -- IDs of orders with no preimage
		PRIMARY KEY(epoch_idx, epoch_dur)  -- epoch idx:dur is unique and the primary key
	);`

	InsertEpoch = `INSERT INTO %s (epoch_idx, epoch_dur, match_time, csum, seed, revealed, missed)
		VALUES ($1, $2, $3, $4, $5, $6, $7);`
)
