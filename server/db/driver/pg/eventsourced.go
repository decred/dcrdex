// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// eventSourcedTables discovers tables in public and the schemas listed in the
// markets table, including markets absent from the current configuration.
// It returns qualified table names, excluding configuration tables, and fails
// if any discovered table has no classification.
func eventSourcedTables(ctx context.Context, tx *sql.Tx) ([]string, error) {
	markets, err := loadMarkets(tx, marketsTableName)
	if err != nil {
		return nil, fmt.Errorf("load markets: %w", err)
	}
	schemas := make([]string, 0, len(markets)+1)
	for _, market := range markets {
		schemas = append(schemas, marketSchema(market.Name))
	}
	slices.Sort(schemas)
	schemas = append([]string{publicSchema}, schemas...)

	var tableNames []string
	for _, schema := range schemas {
		tables, err := classifiedTables(ctx, tx, schema)
		if err != nil {
			return nil, err
		}
		for _, table := range tables {
			if table.class == classConfig {
				continue
			}
			tableNames = append(tableNames, qualifySchemaTable(schema, table.table))
		}
	}
	return tableNames, nil
}

// hasNoEventSourcedState reports whether all tables returned by
// eventSourcedTables are empty within tx.
func hasNoEventSourcedState(ctx context.Context, tx *sql.Tx) (bool, error) {
	tables, err := eventSourcedTables(ctx, tx)
	if err != nil {
		return false, err
	}
	for _, tableName := range tables {
		var hasRows bool
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s)", tableName)).Scan(&hasRows); err != nil {
			return false, fmt.Errorf("event-sourced state probe of %s: %w", tableName, err)
		}
		if hasRows {
			return false, nil
		}
	}
	return true, nil
}

// HasNoEventSourcedState reports whether the event log and all event projections
// are empty. Configuration tables are ignored.
func (a *Archiver) HasNoEventSourcedState(ctx context.Context) (bool, error) {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	return hasNoEventSourcedState(ctx, tx)
}

// WipeEventSourcedState clears the event log and all event projections in one
// transaction and resets their identity sequences. It preserves configuration
// tables and includes markets absent from the current configuration.
func (a *Archiver) WipeEventSourcedState(ctx context.Context) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables, err := eventSourcedTables(ctx, tx)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, "TRUNCATE TABLE "+strings.Join(tables, ", ")+" RESTART IDENTITY"); err != nil {
		return fmt.Errorf("truncate event-sourced tables: %w", err)
	}
	return tx.Commit()
}
