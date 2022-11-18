package internal

const (
	// CreateFeeKeysTable creates the fee_keys table, which is a small table that
	// is used as a persistent child key counter for master extended public key.
	CreateFeeKeysTable = `CREATE TABLE IF NOT EXISTS %s (
		key_hash BYTEA PRIMARY KEY,    -- UNIQUE INDEX
		child INT8 DEFAULT 0
		);`

	// CreateAccountsTable creates the account table.
	CreateAccountsTable = `CREATE TABLE IF NOT EXISTS %s (
		account_id BYTEA PRIMARY KEY,  -- UNIQUE INDEX
		pubkey BYTEA,
		fee_asset INT4, -- NOTE upgrade will add this column after broken_rule 
		fee_address TEXT,          -- DEPRECATED
		fee_coin BYTEA             -- DEPRECATED
		);`

	CreateBondsTableV0 = `CREATE TABLE IF NOT EXISTS %s (
		version INT2,
		bond_coin_id BYTEA,
		asset_id INT4,
		account_id BYTEA,
		amount INT8, -- informative, strength is what matters
		strength int4,
		lock_time INT8,
		PRIMARY KEY (bond_coin_id, asset_id)
		);`
	CreateBondsTable = CreateBondsTableV0

	CreateBondsAcctIndexV0 = `CREATE INDEX IF NOT EXISTS %s ON %s (account_id);`
	CreateBondsAcctIndex   = CreateBondsAcctIndexV0

	CreateBondsLockTimeIndexV0 = `CREATE INDEX IF NOT EXISTS %s ON %s (lock_time);`
	CreateBondsLockTimeIndex   = CreateBondsLockTimeIndexV0

	CreateBondsCoinIDIndexV0 = `CREATE INDEX IF NOT EXISTS %s ON %s (bond_coin_id, asset_id);`
	CreateBondsCoinIDIndex   = CreateBondsCoinIDIndexV0

	AddBond = `INSERT INTO %s (version, bond_coin_id, asset_id, account_id, amount, strength, lock_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7);`

	DeleteBond = `DELETE FROM %s WHERE bond_coin_id = $1 AND asset_id = $2;`

	SelectActiveBondsForUser = `SELECT version, bond_coin_id, asset_id, amount, strength, lock_time FROM %s
		WHERE account_id = $1 AND lock_time >= $2
		ORDER BY lock_time;`

	// InsertKeyIfMissing creates an entry for the specified key hash, if it
	// doesn't already exist.
	InsertKeyIfMissing = `INSERT INTO %s (key_hash)
		VALUES ($1)
		ON CONFLICT (key_hash) DO NOTHING
		RETURNING child;`

	CurrentKeyIndex = `SELECT child FROM %s WHERE key_hash = $1;`

	SetKeyIndex = `UPDATE %s
		SET child = $1
		WHERE key_hash = $2;`

	UpsertKeyIndex = `INSERT INTO %s (child, key_hash)
		VALUES ($1, $2)
		ON CONFLICT (key_hash) DO UPDATE
		SET child = $1;`

	// CloseAccount sets the broken_rule column for the account, which signifies
	// that the account is closed.
	CloseAccount = `UPDATE %s SET broken_rule = $1 WHERE account_id = $2;`

	// SelectAccount gathers account details for the specified account ID.
	SelectAccount = `SELECT pubkey, fee_address IS NOT NULL, fee_coin IS NOT NULL
		FROM %s
		WHERE account_id = $1;`

	// SelectAllAccounts retrieves all accounts.
	SelectAllAccounts = `SELECT account_id, pubkey, fee_asset, fee_address, fee_coin FROM %s;`

	// SelectAccountInfo retrieves all fields for an account.
	SelectAccountInfo = `SELECT account_id, pubkey, fee_asset, fee_address, fee_coin FROM %s
		WHERE account_id = $1;`

	// CreateAccount creates an entry for a new account.
	CreateAccount = `INSERT INTO %s (account_id, pubkey, fee_asset, fee_address)
		VALUES ($1, $2, $3, $4);`

	CreateAccountForBond = `INSERT INTO %s (account_id, pubkey) VALUES ($1, $2);`

	// SelectRegAddress fetches the registration fee address for the account.
	SelectRegAddress = `SELECT fee_asset, fee_address FROM %s WHERE account_id = $1;`

	// SetRegOutput sets the registration fee payment transaction details for the
	// account.
	SetRegOutput = `UPDATE %s SET
		fee_coin = $1
		WHERE account_id = $2;`
)
