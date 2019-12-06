// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package internal

const (
	// CreateOrdersTable creates a table specified via the %s printf specifier
	// for market and limit orders.
	CreateOrdersTable = `CREATE TABLE IF NOT EXISTS %s (
		oid BYTEA PRIMARY KEY, -- UNIQUE INDEX
		type INT2,
		sell BOOLEAN,          -- ANALYZE or INDEX?
		account_id BYTEA,      -- INDEX
		address TEXT,          -- INDEX
		client_time TIMESTAMPTZ,
		server_time TIMESTAMPTZ,
		coins BYTEA,
		quantity INT8,
		rate INT8,
		force INT2,
		status INT2,
		filled INT8
	);`

	// InsertOrder inserts a market or limit order into the specified table.
	InsertOrder = `INSERT INTO %s (oid, type, sell, account_id, address,
			client_time, server_time, coins, quantity,
			rate, force, status, filled)
		VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13);`

	// SelectOrder retrieves all columns with the given order ID. This may be
	// used for any table with an "oid" column (orders_active, cancels_archived,
	// etc.).
	SelectOrder = `SELECT * FROM %s WHERE oid = $1;`

	// SelectUserOrders retrieves all columns of all orders for the given
	// account ID.
	SelectUserOrders = `SELECT * FROM %s WHERE account_id = $1;`

	// SelectOrderCoinIDs retrieves the order id, sell flag, and coins for all
	// orders in a table with one of the given statuses. Note that this includes
	// all order statuses.
	SelectOrderCoinIDs = `SELECT oid, sell, coins
		FROM %s
		WHERE type = ANY($1);`

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
	//			coins,
	//			quantity,
	//			rate,
	//			force,
	//			2,                                      -- new status ($d)
	//			123456789                               -- new filled ($d)
	//		)
	//		INSERT INTO dcrdex.dcr_btc.orders_archived  -- destination table (%s)
	//		SELECT * FROM moved;
	MoveOrder = `WITH moved AS (
		DELETE FROM %s
		WHERE oid = $1
		RETURNING oid, type, sell, account_id, address,
			client_time, server_time, coins, quantity,
			rate, force, %d, %d
	)
	INSERT INTO %s
	SELECT * FROM moved;`
	// TODO: consider a MoveOrderSameFilled query

	// CreateCancelOrdersTable creates a table specified via the %s printf
	// specifier for cancel orders.
	CreateCancelOrdersTable = `CREATE TABLE IF NOT EXISTS %s (
		oid BYTEA PRIMARY KEY, -- UNIQUE INDEX
		account_id BYTEA,      -- INDEX
		client_time TIMESTAMPTZ,
		server_time TIMESTAMPTZ,
		target_order BYTEA,    -- cancel orders ref another order
		status INT2
	);`

	// InsertCancelOrder inserts a cancel order row into the specified table.
	InsertCancelOrder = `INSERT INTO %s (oid, account_id, client_time, server_time, target_order, status)
		VALUES ($1, $2, $3, $4, $5, $6);`

	// CancelOrderStatus retrieves an order's status
	CancelOrderStatus = `SELECT status FROM %s WHERE oid = $1;`

	// MoveCancelOrder, like MoveOrder, moves an order row from one table to
	// another. However, for a cancel order, only status column is updated.
	MoveCancelOrder = `WITH moved AS (
		DELETE FROM %s
		WHERE oid = $1
		RETURNING oid, account_id, client_time, server_time, target_order, %d
	)
	INSERT INTO %s
	SELECT * FROM moved;`
)
