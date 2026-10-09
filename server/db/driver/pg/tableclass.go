// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// tableClass describes how a table participates in snapshots and recovery.
type tableClass int

const (
	// classSnapshot tables are included in snapshots and must be empty before
	// loading a snapshot.
	classSnapshot tableClass = iota + 1

	// classChecked tables must be empty before loading a snapshot, but their
	// rows are not included in snapshots. This covers epochs and the event log.
	classChecked

	// classConfig tables hold configuration that is preserved during recovery.
	// Their rows are excluded from snapshots and empty state checks.
	classConfig
)

// legacyFeeKeysTableName names the legacy fee key table. It remains classified
// so databases containing it pass the table classification check.
const legacyFeeKeysTableName = "fee_keys"

var publicTableClasses = map[string]tableClass{
	accountsTableName:        classSnapshot,
	bondsTableName:           classSnapshot,
	prepaidBondsTableName:    classSnapshot,
	pointsTableName:          classSnapshot,
	marketLifecycleTableName: classSnapshot,
	eventLogTableName:        classChecked,
	marketsTableName:         classConfig,
	metaTableName:            classConfig,
	legacyFeeKeysTableName:   classConfig,
}

var marketTableClasses = map[string]tableClass{
	ordersActiveTableName:    classSnapshot,
	ordersArchivedTableName:  classSnapshot,
	cancelsActiveTableName:   classSnapshot,
	cancelsArchivedTableName: classSnapshot,
	matchesTableName:         classSnapshot,
	epochReportsTableName:    classSnapshot,
	epochsTableName:          classChecked,
	// classifyTable recognizes candle tables by the candles_ prefix.
}

func classifyTable(schema, table string) (tableClass, error) {
	if schema == publicSchema {
		if class, ok := publicTableClasses[table]; ok {
			return class, nil
		}
	} else {
		if class, ok := marketTableClasses[table]; ok {
			return class, nil
		}
		if strings.HasPrefix(table, candlesTableName+"_") {
			return classSnapshot, nil
		}
	}
	return 0, fmt.Errorf("table %s.%s has no mesh snapshot classification; "+
		"add it to publicTableClasses or marketTableClasses in tableclass.go",
		schema, table)
}

type classifiedTable struct {
	table string
	class tableClass
}

// classifiedTables returns the schema's base tables and their classes, ordered
// by name. It fails if any table has no classification.
func classifiedTables(ctx context.Context, tx *sql.Tx, schema string) ([]classifiedTable, error) {
	names, err := listBaseTables(ctx, tx, schema)
	if err != nil {
		return nil, err
	}
	tables := make([]classifiedTable, 0, len(names))
	for _, name := range names {
		class, err := classifyTable(schema, name)
		if err != nil {
			return nil, err
		}
		tables = append(tables, classifiedTable{table: name, class: class})
	}
	return tables, nil
}

func listBaseTables(ctx context.Context, tx *sql.Tx, schema string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables
			WHERE table_schema = $1 AND table_type = 'BASE TABLE'
			ORDER BY table_name`, schema)
	if err != nil {
		return nil, fmt.Errorf("list %s tables: %w", schema, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// verifyTableClassification checks that every table in public and the schemas
// listed in the markets table has a classification. We use this to ensure that
// a table cannot be added without considering whether it should be included in
// the snapshot.
func (a *Archiver) verifyTableClassification(ctx context.Context) error {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = eventSourcedTables(ctx, tx)
	return err
}
