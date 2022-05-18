package internal

const (
	// CreateMarketsTable creates the DEX's "markets" table, which indicates
	// which markets are currently recognized by the DEX, and their configured
	// lot sizes. This tables should be created in the public schema. This
	// information is stored in a table to facilitate the addition and removal
	// of markets, plus market lot size changes, without having to assume that
	// whatever is specified in a config file is accurately reflected by the DB
	// tables.
	CreateMarketsTable = `CREATE TABLE IF NOT EXISTS %s (
		name TEXT PRIMARY KEY,
		base INT8,
		quote INT8,
		lot_size INT8
	)`

	// SelectAllMarkets retrieves the active market information.
	SelectAllMarkets = `SELECT name, base, quote, lot_size FROM %s;`

	// InsertMarket inserts a new market in to the markets tables
	InsertMarket = `INSERT INTO %s (name, base, quote, lot_size)
		VALUES ($1, $2, $3, $4);`

	// UpdateLotSize updates the market's lot size.
	UpdateLotSize = `UPDATE %s SET lot_size = $2 WHERE name = $1;`
)
