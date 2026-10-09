// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"strings"
	"testing"
)

func TestClassifyTable(t *testing.T) {
	for _, tt := range []struct {
		schema, table string
		want          tableClass
	}{
		{publicSchema, accountsTableName, classSnapshot},
		{publicSchema, bondsTableName, classSnapshot},
		{publicSchema, prepaidBondsTableName, classSnapshot},
		{publicSchema, pointsTableName, classSnapshot},
		{publicSchema, marketLifecycleTableName, classSnapshot},
		{publicSchema, eventLogTableName, classChecked},
		{publicSchema, marketsTableName, classConfig},
		{publicSchema, metaTableName, classConfig},
		{publicSchema, legacyFeeKeysTableName, classConfig},
		{"dcr_btc", ordersActiveTableName, classSnapshot},
		{"dcr_btc", ordersArchivedTableName, classSnapshot},
		{"dcr_btc", cancelsActiveTableName, classSnapshot},
		{"dcr_btc", cancelsArchivedTableName, classSnapshot},
		{"dcr_btc", matchesTableName, classSnapshot},
		{"dcr_btc", epochReportsTableName, classSnapshot},
		{"dcr_btc", epochsTableName, classChecked},
		{"dcr_btc", candlesTableName + "_5m", classSnapshot},
		{"dcr_btc", candlesTableName + "_epoch", classSnapshot},
	} {
		got, err := classifyTable(tt.schema, tt.table)
		if err != nil {
			t.Fatalf("classifyTable(%s, %s): %v", tt.schema, tt.table, err)
		}
		if got != tt.want {
			t.Fatalf("classifyTable(%s, %s) = %d, want %d", tt.schema, tt.table, got, tt.want)
		}
	}

	for _, tt := range []struct {
		schema, table string
	}{
		{publicSchema, "shiny_new_projection"},
		{"dcr_btc", "shiny_new_projection"},
		// Candle tables must be in market schemas, and account tables in public.
		{publicSchema, candlesTableName + "_5m"},
		{"dcr_btc", accountsTableName},
	} {
		_, err := classifyTable(tt.schema, tt.table)
		if err == nil || !strings.Contains(err.Error(), "no mesh snapshot classification") {
			t.Fatalf("classifyTable(%s, %s): err = %v, want classification error", tt.schema, tt.table, err)
		}
	}
}
