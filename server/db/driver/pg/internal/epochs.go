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

	// InsertEpoch inserts the epoch's match proof data into the epoch table.
	InsertEpoch = `INSERT INTO %s (epoch_idx, epoch_dur, match_time, csum, seed, revealed, missed)
		VALUES ($1, $2, $3, $4, $5, $6, $7);`

	// CreateEpochReportTable creates an epoch_reports table that holds
	// epoch-end reports that can be used to contruct market history data sets.
	CreateEpochReportTable = `CREATE TABLE IF NOT EXISTS %s (
		epoch_end INT8 PRIMARY KEY, -- using timestamp instead of index to facilitate sorting and filtering with less math
		epoch_dur INT4,             -- epoch duration in milliseconds
		match_volume INT8,          -- total matched during epoch's match cycle
		quote_volume INT8,          -- total matched volume in terms of quote asset
		book_buys INT8,             -- booked buy volume
		book_buys_5 INT8,           -- booked buy volume within 5 pct of market
		book_buys_25 INT8,          -- booked buy volume within 25 pct of market
		book_sells INT8,            -- booked sell volume
		book_sells_5 INT8,          -- booked sell volume within 5 pct of market
		book_sells_25 INT8,         -- booked sell volume within 25 pct of market
		high_rate INT8,             -- the highest rate matched
		low_rate INT8,              -- the lowest rate matched
		start_rate INT8,            -- the mid-gap rate at the beginning of the match cycle
		end_rate INT8               -- the mid-gap rate at the end of the match cycle
	);`

	// InsertEpochReport inserts a row into the epoch_reports table.
	InsertEpochReport = `INSERT INTO %s (epoch_end, epoch_dur, match_volume, quote_volume,
			book_buys, book_buys_5, book_buys_25, book_sells, book_sells_5, book_sells_25,
			high_rate, low_rate, start_rate, end_rate)
			VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);`

	// SelectEpochCandles selects all rows from the epoch_reports table sorted
	// by ascending time.
	SelectEpochCandles = `SELECT epoch_end, epoch_dur, match_volume, quote_volume,
			high_rate, low_rate, start_rate, end_rate
		FROM %s
		WHERE epoch_end >= $1
		ORDER BY epoch_end;`
)
