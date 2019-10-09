// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package internal

const (
	CreateOrdersTable = `CREATE TABLE IF NOT EXISTS %s (
		oid BYTEA PRIMARY KEY, -- UNIQUE INDEX
		type INT2,
		sell BOOLEAN,          -- ANALYZE or INDEX?
		account_id BYTEA,      -- INDEX
		address TEXT,          -- INDEX
		client_time INT8,
		server_time INT8,
		utxos TEXT[],
		quantity INT8,
		status INT2,
		filled INT8
	);`

	CreateCancelOrdersTable = `CREATE TABLE IF NOT EXISTS %s (
		oid BYTEA PRIMARY KEY, -- UNIQUE INDEX
		account_id BYTEA,      -- INDEX
		client_time INT8,
		server_time INT8,
		target_order BYTEA,    -- cancel orders ref another order
	);`

	InsertOrder = `INSERT INTO %s (oid, type, sell, account_id, address,
			client_time, server_time, utxos, quantity, status, filled)
		VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11);`

	SelectOrder = `SELECT * FROM %s WHERE oid = $1;`

	UpdateOrderStatus    = `UPDATE %s SET status = $1 WHERE oid = $2;`
	UpdateOrderFilledAmt = `UPDATE %s SET filled = $1 WHERE oid = $2;`

	OrderStatus = `SELECT status, filled FROM %s WHERE oid = $1;`

	// MoveOrder moves and order row from one table to another (e.g.
	// orders_active to orders_archived). e.g.:
	//	WITH moved AS (									-- temporary table
	//		DELETE FROM dcrdex.dcr_btc.orders_active	-- origin table (%s)
	//		WHERE oid = '\xDEADBEEF'					-- the order to move ($1)
	//		RETURNING
	//			oid,
	//			type,
	//			sell,
	//			account_id,
	//			address,
	//			client_time,
	//			server_time,
	//			utxos,
	//			quantity,
	//			2,										-- new status ($2)
	//			123456789								-- new filled ($3)
	//		)
	//		INSERT INTO dcrdex.dcr_btc.orders_archived	-- destination table (%s)
	//		SELECT * FROM moved;
	MoveOrder = `WITH moved AS (
		DELETE FROM %s
		WHERE oid = $1
		RETURNING oid, type, sell, account_id, address,
			client_time, server_time, utxos, quantity, $2, $3
	)
	INSERT INTO %s
	SELECT * FROM moved;`
	// TODO: consider a MoveOrderSameFilled query
)
