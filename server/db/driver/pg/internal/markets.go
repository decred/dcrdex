package internal

const (
	CreateMarketSchema = `CREATE SCHEMA IF NOT EXISTS %s;`

	CreateMarketsTable = `CREATE TABLE IF NOT EXISTS %s (
		name TEXT PRIMARY KEY,
		base INT2,
		quote INT2,
		lot_size INT8
	)`

	SelectAllMarkets = `SELECT name, base, quote, lot_size
		FROM %s;`
)
