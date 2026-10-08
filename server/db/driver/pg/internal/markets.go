package internal

const (
	// CreateMarketLifecycleTable creates the table that stores each market's state
	// and scheduled transition.
	CreateMarketLifecycleTable = `CREATE TABLE IF NOT EXISTS %s (
		market TEXT PRIMARY KEY REFERENCES public.markets(name),
		state INT2 NOT NULL, -- 1: running, 2: suspended, 3: draining
		start_epoch_idx INT8 NOT NULL,
		start_epoch_dur INT8 NOT NULL,
		final_epoch_idx INT8 NOT NULL,
		final_epoch_dur INT8 NOT NULL,
		pending_action INT2 NOT NULL, -- 0: none, 1: suspend, 2: resume
		pending_epoch_idx INT8 NOT NULL,
		pending_epoch_dur INT8 NOT NULL,
		persist_book BOOLEAN NULL,
		active_epoch_idx INT8 NOT NULL DEFAULT 0,
		processed_epoch_idx INT8 NOT NULL DEFAULT 0,
		lot_size INT8 NOT NULL,
		rate_step INT8 NOT NULL,
		parcel_size INT8 NOT NULL,
		max_user_cancels INT8 NOT NULL,
		minimum_rate INT8 NOT NULL,
		CHECK (lot_size > 0 AND rate_step > 0 AND parcel_size > 0),
		CHECK (max_user_cancels >= 0 AND minimum_rate >= 0),
		CHECK (state IN (1, 2, 3)),
		CHECK (pending_action IN (0, 1, 2)),
		CHECK (start_epoch_idx > 0 AND start_epoch_dur > 0),
		CHECK (active_epoch_idx >= 0),
		CHECK (processed_epoch_idx >= 0),
		CHECK (final_epoch_idx >= 0 AND final_epoch_dur >= 0),
		CHECK (pending_epoch_idx >= 0 AND pending_epoch_dur >= 0),
		CHECK ((final_epoch_idx = 0) = (final_epoch_dur = 0)),
		CHECK ((pending_epoch_idx = 0) = (pending_epoch_dur = 0)),
		CHECK (
			(state = 1
				AND pending_action = 0
				AND final_epoch_idx = 0
				AND final_epoch_dur = 0
				AND pending_epoch_idx = 0
				AND pending_epoch_dur = 0
				AND persist_book IS NULL) OR
			(state = 1
				AND pending_action = 1
				AND final_epoch_idx = pending_epoch_idx
				AND final_epoch_dur = pending_epoch_dur
				AND final_epoch_idx > 0
				AND persist_book IS NOT NULL) OR
			(state = 3
				AND pending_action = 0
				AND pending_epoch_idx = 0
				AND pending_epoch_dur = 0
				AND active_epoch_idx = 0
				AND final_epoch_idx > 0
				AND final_epoch_dur > 0
				AND persist_book IS NOT NULL) OR
			(state = 2
				AND pending_action = 0
				AND final_epoch_idx > 0
				AND final_epoch_dur > 0
				AND pending_epoch_idx = 0
				AND pending_epoch_dur = 0
				AND persist_book IS NOT NULL) OR
			(state = 2
				AND pending_action = 2
				AND start_epoch_idx = pending_epoch_idx
				AND start_epoch_dur = pending_epoch_dur
				AND final_epoch_idx = 0
				AND final_epoch_dur = 0
				AND pending_epoch_idx > 0
				AND pending_epoch_dur > 0
				AND persist_book IS NOT NULL)
		)
	)`

	// CreateMarketsTable creates the DEX's "markets" table, which indicates
	// which markets are currently recognized by the DEX, and their configured
	// lot sizes. This tables should be created in the public schema. This
	// information is stored in a table to facilitate the addition and removal
	// of markets, plus market lot size changes, without having to assume that
	// whatever is specified in a config file is accurately reflected by the DB
	// tables.
	CreateMarketsTable = `CREATE TABLE IF NOT EXISTS %s (
		name TEXT PRIMARY KEY,
		base INT8,
		quote INT8,
		lot_size INT8
	)`

	// SelectAllMarkets retrieves the active market information.
	SelectAllMarkets = `SELECT name, base, quote, lot_size FROM %s;`

	selectMarketLifecycle = `SELECT market, state, start_epoch_idx, start_epoch_dur,
		final_epoch_idx, final_epoch_dur, pending_action, pending_epoch_idx,
		pending_epoch_dur, persist_book, active_epoch_idx, processed_epoch_idx,
		lot_size, rate_step, parcel_size, max_user_cancels, minimum_rate
		FROM %s WHERE market = $1`

	SelectMarketLifecycle          = selectMarketLifecycle + ";"
	SelectMarketLifecycleForUpdate = selectMarketLifecycle + " FOR UPDATE;"

	UpsertMarketLifecycle = `INSERT INTO %s (market, state, start_epoch_idx, start_epoch_dur,
		final_epoch_idx, final_epoch_dur, pending_action, pending_epoch_idx,
		pending_epoch_dur, persist_book, active_epoch_idx, processed_epoch_idx,
		lot_size, rate_step, parcel_size, max_user_cancels, minimum_rate)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (market) DO UPDATE SET
			state = EXCLUDED.state,
			start_epoch_idx = EXCLUDED.start_epoch_idx,
			start_epoch_dur = EXCLUDED.start_epoch_dur,
			final_epoch_idx = EXCLUDED.final_epoch_idx,
			final_epoch_dur = EXCLUDED.final_epoch_dur,
			pending_action = EXCLUDED.pending_action,
			pending_epoch_idx = EXCLUDED.pending_epoch_idx,
			pending_epoch_dur = EXCLUDED.pending_epoch_dur,
			persist_book = EXCLUDED.persist_book,
			active_epoch_idx = EXCLUDED.active_epoch_idx,
			processed_epoch_idx = EXCLUDED.processed_epoch_idx,
			lot_size = EXCLUDED.lot_size,
			rate_step = EXCLUDED.rate_step,
			parcel_size = EXCLUDED.parcel_size,
			max_user_cancels = EXCLUDED.max_user_cancels,
			minimum_rate = EXCLUDED.minimum_rate;`

	UpdateMarketLifecycle = `UPDATE %s SET state = $2, start_epoch_idx = $3,
		start_epoch_dur = $4, final_epoch_idx = $5, final_epoch_dur = $6,
		pending_action = $7, pending_epoch_idx = $8, pending_epoch_dur = $9,
		persist_book = $10, active_epoch_idx = $11, processed_epoch_idx = $12,
		lot_size = $13, rate_step = $14, parcel_size = $15,
		max_user_cancels = $16, minimum_rate = $17
		WHERE market = $1;`

	// InsertMarket inserts a new market in to the markets tables
	InsertMarket = `INSERT INTO %s (name, base, quote, lot_size)
		VALUES ($1, $2, $3, $4);`

	// UpdateLotSize updates the market's lot size.
	UpdateLotSize = `UPDATE %s SET lot_size = $2 WHERE name = $1;`
)
