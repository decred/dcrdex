// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package internal

const (
	// CreateOrdersTable creates a table specified via the %s printf specifier
	// for market and limit orders.
	CreateOrdersTable = `CREATE TABLE IF NOT EXISTS %s (
		oid BYTEA PRIMARY KEY, -- UNIQUE
		type INT2,
		sell BOOLEAN,          -- TODO: ANALYZE or INDEX?
		account_id BYTEA,      -- TODO: INDEX
		address TEXT,          -- TODO:INDEX
		client_time TIMESTAMPTZ,
		server_time TIMESTAMPTZ,
		commit BYTEA UNIQUE,
		coins BYTEA,
		quantity INT8,
		rate INT8,
		force INT2,
		status INT2,
		filled INT8,
		epoch_idx INT8, epoch_dur INT4,
		preimage BYTEA UNIQUE,
		complete_time INT8      -- when the order has successfully completed all swaps
	);`

	// InsertOrder inserts a market or limit order into the specified table.
	InsertOrder = `INSERT INTO %s (oid, type, sell, account_id, address,
			client_time, server_time, commit, coins, quantity,
			rate, force, status, filled,
			epoch_idx, epoch_dur)
		VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16);`

	// SelectOrder retrieves all columns with the given order ID. This may be
	// used for any table with an "oid" column (orders_active, cancels_archived,
	// etc.).
	SelectOrder = `SELECT oid, type, sell, account_id, address, client_time, server_time,
		commit, coins, quantity, rate, force, status, filled
	FROM %s WHERE oid = $1;`

	SelectOrdersByStatus = `SELECT oid, type, sell, account_id, address, client_time, server_time,
		commit, coins, quantity, rate, force, filled
	FROM %s WHERE status = $1;`

	PreimageResultsLastN = `SELECT oid, (preimage IS NULL AND status=$3) AS preimageMiss, 
		(epoch_idx+1) * epoch_dur as epochCloseTime   -- when preimages are requested
	FROM %s -- e.g. dcr_btc.orders_archived
	WHERE account_id = $1
		AND status >= 0         -- exclude forgiven
	ORDER BY epochCloseTime DESC
	LIMIT $2`  // no ;
	// NOTE: we could join with the epochs table if we really want match_time instead of epoch close time

	// SelectUserOrders retrieves all columns of all orders for the given
	// account ID.
	SelectUserOrders = `SELECT oid, type, sell, account_id, address, client_time, server_time,
		commit, coins, quantity, rate, force, status, filled
	FROM %s WHERE account_id = $1;`

	// SelectUserOrderStatuses retrieves the order IDs and statuses of all orders
	// for the given account ID. Only applies to market and limit orders.
	SelectUserOrderStatuses = `SELECT oid, status FROM %s WHERE account_id = $1;`

	// SelectUserOrderStatusesByID retrieves the order IDs and statuses of the
	// orders with the provided order IDs for the given account ID. Only applies
	// to market and limit orders.
	SelectUserOrderStatusesByID = `SELECT oid, status FROM %s WHERE account_id = $1 AND oid = ANY($2);`

	// SelectCanceledUserOrders gets the ID of orders that were either canceled
	// by the user or revoked/canceled by the server, but these statuses can be
	// set by the caller. Note that revoked orders can be market or immediate
	// limit orders that failed to swap.
	SelectCanceledUserOrders = `SELECT oid, match_time
		FROM %[1]s -- a archived orders table
		JOIN %[2]s ON %[2]s.epoch_idx = %[1]s.epoch_idx AND %[2]s.epoch_dur = %[1]s.epoch_dur -- join on epochs table PK
		WHERE account_id = $1 AND status = ANY($2) -- {orderStatusCanceled, orderStatusRevoked}
		ORDER BY match_time DESC
		LIMIT $3;`  // The matchTime is when the order was booked, not canceled!!!

	// SelectOrderByCommit retrieves the order ID for any order with the given
	// commitment value. This applies to the cancel order tables as well.
	SelectOrderByCommit = `SELECT oid FROM %s WHERE commit = $1;`

	// SelectOrderPreimage retrieves the preimage for the order ID;
	SelectOrderPreimage = `SELECT preimage FROM %s WHERE oid = $1;`

	// SelectOrderCoinIDs retrieves the order id, sell flag, and coins for all
	// orders in a certain table.
	SelectOrderCoinIDs = `SELECT oid, sell, coins
		FROM %s;`

	SetOrderPreimage     = `UPDATE %s SET preimage = $1 WHERE oid = $2;`
	SetOrderCompleteTime = `UPDATE %s SET complete_time = $1
		WHERE oid = $2;`

	RetrieveCompletedOrdersForAccount = `SELECT oid, account_id, complete_time
		FROM %s
		WHERE account_id = $1 AND complete_time IS NOT NULL
		ORDER BY complete_time DESC
		LIMIT $2;`

	// UpdateOrderStatus sets the status of an order with the given order ID.
	UpdateOrderStatus = `UPDATE %s SET status = $1 WHERE oid = $2;`
	// UpdateOrderFilledAmt sets the filled amount of an order with the given
	// order ID.
	UpdateOrderFilledAmt = `UPDATE %s SET filled = $1 WHERE oid = $2;`
	// UpdateOrderStatusAndFilledAmt sets the order status and filled amount of
	// an order with the given order ID.
	UpdateOrderStatusAndFilledAmt = `UPDATE %s SET status = $1, filled = $2 WHERE oid = $3;`

	// OrderStatus retrieves the order type, status, and filled amount for an
	// order with the given order ID. This only applies to market and limit
	// orders. For cancel orders, which lack a type and filled column, use
	// CancelOrderStatus.
	OrderStatus = `SELECT type, status, filled FROM %s WHERE oid = $1;`

	// MoveOrder moves an order row from one table to another (e.g.
	// orders_active to orders_archived), while updating the order's status and
	// filled amounts.
	// For example,
	//	WITH moved AS (                                 -- temporary table
	//		DELETE FROM dcrdex.dcr_btc.orders_active    -- origin table (%s)
	//		WHERE oid = '\xDEADBEEF'                    -- the order to move ($1)
	//		RETURNING
	//			oid,
	//			type,
	//			sell,
	//			account_id,
	//			address,
	//			client_time,
	//			server_time,
	//			commit,
	//			coins,
	//			quantity,
	//			rate,
	//			force,
	//			2,                                      -- new status (%d)
	//			123456789,                              -- new filled (%d)
	//          epoch_idx, epoch_dur, preimage, complete_time
	//		)
	//		INSERT INTO dcrdex.dcr_btc.orders_archived  -- destination table (%s)
	//		SELECT * FROM moved;
	MoveOrder = `WITH moved AS (
		DELETE FROM %s
		WHERE oid = $1
		RETURNING oid, type, sell, account_id, address,
			client_time, server_time, commit, coins, quantity,
			rate, force, %d, %d,
			epoch_idx, epoch_dur, preimage, complete_time
	)
	INSERT INTO %s
	SELECT * FROM moved;`
	// TODO: consider a MoveOrderSameFilled query

	PurgeBook = `WITH moved AS (
		DELETE FROM %s       -- active orders table for market X
		WHERE status = $1    -- booked status code
		RETURNING oid, type, sell, account_id, address,
			client_time, server_time, commit, coins, quantity,
			rate, force, %d, filled, -- revoked status code
			epoch_idx, epoch_dur, preimage, complete_time
	)
	INSERT INTO %s -- archived orders table for market X
	SELECT * FROM moved
	RETURNING oid, sell, account_id;`

	// CreateCancelOrdersTable creates a table specified via the %s printf
	// specifier for cancel orders.
	CreateCancelOrdersTable = `CREATE TABLE IF NOT EXISTS %s (
		oid BYTEA PRIMARY KEY, -- UNIQUE INDEX
		account_id BYTEA,      -- TODO: INDEX
		client_time TIMESTAMPTZ,
		server_time TIMESTAMPTZ,
		commit BYTEA UNIQUE,   -- null for server-generated cancels (order revocations)
		target_order BYTEA,    -- cancel orders ref another order
		status INT2,
		epoch_idx INT8, epoch_dur INT4, -- 0 for rule-based revocations, -1 for exempt (e.g. book purge)
		preimage BYTEA UNIQUE  -- null before preimage collection, and all server-generated cancels (revocations)
	);`

	SelectCancelOrder = `SELECT oid, account_id, client_time, server_time,
		commit, target_order, status
	FROM %s WHERE oid = $1;`

	SelectCancelOrdersByStatus = `SELECT account_id, client_time, server_time,
		commit, target_order
	FROM %s WHERE status = $1;`

	CancelPreimageResultsLastN = `SELECT oid, (preimage IS NULL AND status=$3) AS preimageMiss,  -- orderStatusRevoked
		(epoch_idx+1) * epoch_dur AS epochCloseTime   -- when preimages are requested
	FROM %s -- e.g. dcr_btc.cancels_archived
	WHERE account_id = $1
		AND commit IS NOT NULL  -- commit NOT NULL to exclude server-generated cancels
		AND status >= 0         -- not forgiven
	ORDER BY epochCloseTime DESC
	LIMIT $2`  // no ;

	// SelectRevokeCancels retrieves server-initiated cancels (revokes).
	SelectRevokeCancels = `SELECT oid, target_order, server_time, epoch_idx
		FROM %s
		WHERE account_id = $1 AND status = $2 -- use orderStatusRevoked
		ORDER BY server_time DESC
		LIMIT $3;`

	// RetrieveCancelsForUserByStatus gets matched cancel orders by user and
	// status, where status should be orderStatusExecuted. This query may be
	// followed by a SELECT of match_time from the epochs table for the epoch
	// IDs (idx:dur) returned by this query. In general, this query will be used
	// on a market's archived cancels table, which includes matched cancels.
	RetrieveCancelsForUserByStatus = `SELECT oid, target_order, epoch_idx, epoch_dur
		FROM %s
		WHERE account_id = $1 AND status = $2
		ORDER BY epoch_idx * epoch_dur DESC;`
	// RetrieveCancelTimesForUserByStatus is similar to
	// RetrieveCancelsForUserByStatus, but it joins on an epochs table to get
	// the match_time directly instead of the epoch_idx and epoch_dur. The
	// cancels table, with full market schema, is %[1]s, while the epochs table
	// is %[2]s.
	RetrieveCancelTimesForUserByStatus = `SELECT oid, target_order, match_time
		FROM %[1]s -- a cancels table
		JOIN %[2]s ON %[2]s.epoch_idx = %[1]s.epoch_idx AND %[2]s.epoch_dur = %[1]s.epoch_dur -- join on epochs table PK
		WHERE account_id = $1 AND status = $2
		ORDER BY match_time DESC
		LIMIT $3;`  // NOTE: find revoked orders via SelectRevokeCancels

	// InsertCancelOrder inserts a cancel order row into the specified table.
	InsertCancelOrder = `INSERT INTO %s (oid, account_id, client_time, server_time, commit, target_order, status, epoch_idx, epoch_dur)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);`

	// CancelOrderStatus retrieves an order's status
	CancelOrderStatus = `SELECT status FROM %s WHERE oid = $1;`

	// MoveCancelOrder, like MoveOrder, moves an order row from one table to
	// another. However, for a cancel order, only status column is updated.
	MoveCancelOrder = `WITH moved AS (
		DELETE FROM %s
		WHERE oid = $1
		RETURNING oid, account_id, client_time, server_time, commit, target_order, %d, epoch_idx, epoch_dur, preimage
	)
	INSERT INTO %s
	SELECT * FROM moved;`
)
