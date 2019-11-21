package internal

const (
	CreateMatchesTable = `CREATE TABLE IF NOT EXISTS %s (
		matchid BYTEA PRIMARY KEY,
		takerOrder BYTEA,      -- INDEX this
		takerAccount BYTEA,    -- INDEX this
		takerAddress TEXT,
		makerOrder BYTEA,      -- INDEX this
		makerAccount BYTEA,    -- INDEX this
		makerAddress TEXT,
		epochID TEXT,          -- maybe split this into two INT columns
		quantity INT8,
		rate INT8,
		status INT2,
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

	RetrieveMatchByID = `SELECT * from %s WHERE matchid = $1;`

	RetrieveUserMatches = `SELECT matchid, takerOrder, takerAccount, takerAddress,
		makerOrder, makerAccount, makerAddress, epochID, quantity, rate, status
	FROM %s WHERE takerAccount = $1 OR makerAccount = $1;`
)
