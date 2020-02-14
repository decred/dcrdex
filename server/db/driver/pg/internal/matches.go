package internal

const (
	// CreateMatchesTable creates the matches table for storing data related to
	// a match and the related swap. This only includes trade matches, not
	// cancel order matches that just remove one order from the book (and change
	// the target order status in the orders table).
	//
	// The takerSell column indicates the asset of the address and coinID
	// columns for both maker and taker. Sell refers to sell of the base asset,
	// and the opposite is implied for the counterparty (makerSell = !takerSell)
	//
	//   takerSell   | takerAddress | coinIDTakerFund | coinIDTakerRecv ||  (makerSell)  | makerAddress | coinIDMakerFund | coinIDMakerRecv
	// -------------------------------------------------------------------------------------------------------------------------------------
	//  true (B->Q)  |    quote     |      base       |      quote      ||  false (Q->B) |     base     |      quote      |     base
	//  false (Q->B) |    base      |      quote      |      base       ||  true (B->Q)  |     quote    |      base       |     quote
	CreateMatchesTable = `CREATE TABLE IF NOT EXISTS %s (
		matchid BYTEA PRIMARY KEY,
		takerSell BOOL,        -- to identify asset of address and coinIDs
		takerOrder BYTEA,      -- INDEX this
		takerAccount BYTEA,    -- INDEX this
		takerAddress TEXT,
		makerOrder BYTEA,      -- INDEX this
		makerAccount BYTEA,    -- INDEX this
		makerAddress TEXT,
		epochIdx INT8,          -- maybe split this into two INT columns
		epochDur INT8,
		quantity INT8,
		rate INT8,
		status INT2,
        swapContract BYTEA,
		secretHash BYTEA,
		coinIDTakerFund BYTEA,
		coinIDMakerFund BYTEA,
		coinIDTakerRecv BYTEA,
		coinIDMakerRecv BYTEA,
		sigTakerMatch BYTEA,
		sigMakerMatch BYTEA,
		sigTakerAudit BYTEA,
		sigMakerAudit BYTEA,
		sigTakerRedeem BYTEA,
		sigMakerRedeem BYTEA
	)`

	InsertMatch = `INSERT INTO %s (matchid,
		takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress,
		epochID, quantity, rate, status,
		sigTakerMatch, sigMakerMatch,
		sigTakerAudit, sigMakerAudit,
		sigTakerRedeem, sigMakerRedeem)
	VALUES ($1,
		$2, $3, $4,
		$5, $6, $7,
		$8, $9, $10, $11,
		$12, $13,
		$14, $15,
		$16, $17) `  // do not terminate with ;

	UpsertMatch = InsertMatch + ` ON CONFLICT (matchid) DO
	UPDATE SET quantity = $9, status = $11,
		sigTakerMatch = $12, sigMakerMatch = $13,
		sigTakerAudit = $14, sigMakerAudit = $15,
		sigTakerRedeem = $16, sigMakerRedeem = $17;`

	// UpdateMatch = `UPDATE %s SET quantity = $2, status = $3,
	// 	sigTakerMatch = $4, sigMakerMatch = $5,
	// 	sigTakerAudit = $6, sigMakerAudit = $7,
	// 	sigTakerRedeem = $8, sigMakerRedeem = $9
	// WHERE matchid = $1;`

	SelectActiveMatches = `SELECT makerOrder, takerOrder
		FROM %s WHERE status = %d;`

	RetrieveMatchByID = `SELECT * from %s WHERE matchid = $1;`

	RetrieveUserMatches = `SELECT matchid, takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress, epochID, quantity, rate, status
	FROM %s WHERE takerAccount = $1 OR makerAccount = $1;`

	RetrieveActiveUserMatches = `SELECT matchid, takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress, epochID, quantity, rate, status
	FROM %s
	WHERE (takerAccount = $1 OR makerAccount = $1)
		AND status <> $2;`  // $2 should the code for MatchComplete
)
