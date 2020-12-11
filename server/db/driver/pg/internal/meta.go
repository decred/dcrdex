package internal

const (
	// CreateMetaTable creates a table to hold DEX metadata.
	CreateMetaTable = `CREATE TABLE IF NOT EXISTS %s (
		state_hash BYTEA -- hash of the swapper state file
	);`

	// TODO: alter table to drop the state_hash column and add version.

	// CreateMetaRow creates the single row of the meta table.
	CreateMetaRow = "INSERT INTO meta DEFAULT VALUES;"
)
