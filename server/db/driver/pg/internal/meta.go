package internal

const (
	// CreateMetaTable creates a table to hold DEX metadata.
	CreateMetaTable = `CREATE TABLE IF NOT EXISTS %s (
		state_hash BYTEA -- hash of the swapper state file
	);`

	// CreateMetaRow creates the single row of the meta table.
	CreateMetaRow = "INSERT INTO meta DEFAULT VALUES;"

	// RetrieveStateHash retrieves the last stored swap state file hash.
	RetrieveStateHash = "SELECT state_hash FROM meta;"

	// SetStateHash sets the hash of the swap state file.
	SetStateHash = "UPDATE meta SET state_hash = $1;"
)
