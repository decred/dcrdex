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
		fee_address TEXT,
		fee_coin BYTEA,
		broken_rule INT2 DEFAULT 0 -- TODO: change to banned BOOL
		);`

	// InsertKeyIfMissing creates an entry for the specified key hash, if it
	// doesn't already exist.
	InsertKeyIfMissing = `INSERT INTO %s (key_hash)
		VALUES ($1)
		ON CONFLICT (key_hash) DO NOTHING;`

	// IncrementKey increments the child counter and returns the new value.
	IncrementKey = `UPDATE %s
		SET child = child + 1
		WHERE key_hash = $1
		RETURNING child;`

	// CloseAccount sets the broken_rule column for the account, which signifies
	// that the account is closed.
	CloseAccount = `UPDATE %s SET broken_rule = $1 WHERE account_id = $2;`

	// SelectAccount gathers account details for the specified account ID. The
	// details returned from this query are sufficient to determine 1) whether the
	// registration fee has been paid, or 2) whether the account has been closed.
	SelectAccount = `SELECT pubkey, fee_coin, broken_rule
		FROM %s
		WHERE account_id = $1;`

	// SelectAllAccounts retrieves all accounts.
	SelectAllAccounts = `SELECT * FROM %s;`

	// SelectAccountInfo retrieves all fields for an account.
	SelectAccountInfo = `SELECT * FROM %s
		WHERE account_id = $1;`

	// CreateAccount creates an entry for a new account.
	CreateAccount = `INSERT INTO %s (account_id, pubkey, fee_address)
		VALUES ($1, $2, $3);`

	// SelectRegAddress fetches the registration fee address for the account.
	SelectRegAddress = `SELECT fee_address FROM %s WHERE account_id = $1;`

	// SetRegOutput sets the registration fee payment transaction details for the
	// account.
	SetRegOutput = `UPDATE %s SET
		fee_coin = $1
		WHERE account_id = $2;`
)
