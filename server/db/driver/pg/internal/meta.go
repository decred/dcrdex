package internal

const (
	// CreateMetaTable creates a table to hold DEX metadata. This query has a %s
	// specifier for "meta" / metaTableName so it can work with createTable.
	CreateMetaTable = `CREATE TABLE IF NOT EXISTS %s (
		schema_version INT4 DEFAULT 0
	);`

	// CreateMetaRow creates the single row of the meta table.
	CreateMetaRow = "INSERT INTO meta DEFAULT VALUES;"

	SelectDBVersion = `SELECT schema_version FROM meta;`

	SetDBVersion = `UPDATE meta SET schema_version = $1;`
)
