package internal

const (
	// CreateFeeKeysTable creates the fee_keys table, which is a small table that
	// is used as a persistent child key counter for a master extended public key.
	CreateFeeKeysTable = `CREATE TABLE IF NOT EXISTS %s (
		key_hash BYTEA PRIMARY KEY,    -- UNIQUE INDEX
		child INT8 DEFAULT 0
		);`

	// CreateAccountsTable creates the account table.
	CreateAccountsTable = `CREATE TABLE IF NOT EXISTS %s (
		account_id BYTEA PRIMARY KEY,  -- UNIQUE INDEX
		pubkey BYTEA,
		fee_asset INT4, -- NOTE upgrade will add this column after broken_rule 
		fee_address TEXT,
		fee_coin BYTEA,
		broken_rule INT2 DEFAULT 0 -- TODO: change to banned BOOL
		);`

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

	// SelectAccount gathers account details for the specified account ID. The
	// details returned from this query are sufficient to determine 1) whether the
	// registration fee has been paid, or 2) whether the account has been closed.
	SelectAccount = `SELECT pubkey, fee_asset, fee_coin, broken_rule
		FROM %s
		WHERE account_id = $1;`

	// SelectAllAccounts retrieves all accounts.
	SelectAllAccounts = `SELECT account_id, pubkey, fee_asset, fee_address, fee_coin, broken_rule FROM %s;`

	// SelectAccountInfo retrieves all fields for an account.
	SelectAccountInfo = `SELECT account_id, pubkey, fee_asset, fee_address, fee_coin, broken_rule FROM %s
		WHERE account_id = $1;`

	// CreateAccount creates an entry for a new account.
	CreateAccount = `INSERT INTO %s (account_id, pubkey, fee_asset, fee_address)
		VALUES ($1, $2, $3, $4);`

	// SelectRegAddress fetches the registration fee address for the account.
	SelectRegAddress = `SELECT fee_asset, fee_address FROM %s WHERE account_id = $1;`

	// SetRegOutput sets the registration fee payment transaction details for the
	// account.
	SetRegOutput = `UPDATE %s SET
		fee_coin = $1
		WHERE account_id = $2;`
)
