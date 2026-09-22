// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

const dbVersion = 9

// The number of upgrades defined MUST be equal to dbVersion.
var upgrades = []func(db *sql.Tx) error{
	// v1 upgrade adds the schema_version column to the meta table, possibly
	// creating the table if it was missing.
	v1Upgrade,

	// v2 upgrade creates epochs_report table, if it does not exist, and
	// populates the table with partial historical data from the epochs and
	// matches table. This includes match volumes, high/low/start/end rates, but
	// does not include the booked volume statistics in the book_buys* and
	// book_sells* columns since this data requires a book snapshot at the time
	// of matching to generate.
	v2Upgrade,

	// v3 upgrade adds the fee_asset column to the accounts table.
	v3Upgrade,

	// v4 upgrade updates the markets tables to use a integer type that can
	// accommodate a 32-bit unsigned integer.
	v4Upgrade,

	// v5 upgrade adds an epoch_gap column to the cancel order tables to
	// facilitate free cancels.
	v5Upgrade,

	// v6 upgrade creates the bonds table. A future upgrade may add a new
	// old_fee_coin column to the accounts table for when a manual refund is
	// processed.
	v6Upgrade,

	// v7 upgrade adds a reputation_ver column to the accounts table. This
	// facilitates a rolling upgrade of reputation tracking to address an issue
	// with the DB design.
	v7Upgrade,

	// v8 upgrade adds per-match swap address columns to the matches tables.
	v8Upgrade,

	// v9 upgrade creates the tables needed for mesh replication, removes the
	// accounts.fee_asset column, adds market indexes, and drops archived order
	// commitment and preimage uniqueness constraints. It converts legacy
	// reputation to points. Databases with existing state receive a genesis event.
	v9Upgrade,
}

// v1Upgrade adds the schema_version column and removes the state_hash column
// from the meta table.
func v1Upgrade(tx *sql.Tx) error {
	// Create the meta table with the v0 scheme. Even if the table does not
	// exists, we should not create it fresh with the current scheme since one
	// or more subsequent upgrades may alter the meta scheme.
	metaV0Stmt := `CREATE TABLE IF NOT EXISTS %s (state_hash BYTEA)`
	metaCreated, err := createTableStmt(tx, metaV0Stmt, publicSchema, metaTableName)
	if err != nil {
		return fmt.Errorf("failed to create meta table: %w", err)
	}
	if metaCreated {
		log.Infof("Created new %q table", metaTableName)    // from 0.2+pre master
		_, err = tx.Exec(`INSERT INTO meta DEFAULT VALUES`) // might be CreateMetaRow, but pin to the v0 stmt
		if err != nil {
			return fmt.Errorf("failed to create row for meta table: %w", err)
		}
	} else {
		log.Infof("Existing %q table", metaTableName) // from release-0.1
	}

	// Create the schema_version column. The caller must set the version to 1.
	_, err = tx.Exec(`ALTER TABLE ` + metaTableName + ` ADD COLUMN IF NOT EXISTS schema_version INT4 DEFAULT 0;`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE ` + metaTableName + ` DROP COLUMN IF EXISTS state_hash;`)
	return err
}

// matchStatsForMarketEpoch is used by v2Upgrade to retrieve match rates and
// quantities for a given epoch.
func matchStatsForMarketEpoch(stmt *sql.Stmt, epochIdx, epochDur uint64) (rates, quantities []uint64, sell []bool, err error) {
	var rows *sql.Rows
	rows, err = stmt.Query(epochIdx, epochDur)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var rate, quantity fastUint64
		var takerSell bool
		err = rows.Scan(&quantity, &rate, &takerSell)
		if err != nil {
			return nil, nil, nil, err
		}
		rates = append(rates, uint64(rate))
		quantities = append(quantities, uint64(quantity))
		sell = append(sell, takerSell)
	}

	if err = rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	return
}

// v2Upgrade populates the epoch_reports table with historical data from the
// matches table.
func v2Upgrade(tx *sql.Tx) error {
	mkts, err := loadMarkets(tx, marketsTableName)
	if err != nil {
		return fmt.Errorf("failed to read markets table: %w", err)
	}

	unitInfo := func(assetID uint32) dex.UnitInfo {
		ui, err := asset.UnitInfo(assetID)
		if err != nil {
			log.Errorf("no unit info found for %d (%q)", assetID, dex.BipIDSymbol(assetID))
			ui.Conventional.ConversionFactor = 1e8
		}
		return ui
	}

	doMarketMatches := func(mkt *dex.MarketInfo) error {
		log.Infof("Populating %s with volume data for market %q matches...", epochsTableName, mkt.Name)

		baseUnitInfo, quoteUnitInfo := unitInfo(mkt.Base), unitInfo(mkt.Quote)

		// Create the epochs_report table if it does not already exist.
		_, err := createTable(tx, mkt.Name, epochReportsTableName)
		if err != nil {
			return err
		}

		// For each unique epoch duration, get the first and last epoch index.
		fullEpochsTableName := mkt.Name + "." + epochsTableName
		stmt := fmt.Sprintf(`SELECT epoch_dur, MIN(epoch_idx), MAX(epoch_idx)
			FROM %s GROUP BY epoch_dur;`, fullEpochsTableName)
		rows, err := tx.Query(stmt)
		if err != nil {
			return err
		}
		defer rows.Close()

		var durs, starts, ends []uint64
		for rows.Next() {
			var dur, first, last uint64
			if err = rows.Scan(&dur, &first, &last); err != nil {
				return err
			}
			durs = append(durs, dur)
			starts = append(starts, first)
			ends = append(ends, last)
		}

		if err = rows.Err(); err != nil {
			return err
		}

		// epoch_reports INSERT statement
		mktEpochReportsTablename := mkt.Name + "." + epochReportsTableName
		reportStmt := fmt.Sprintf(internal.InsertPartialEpochReport, mktEpochReportsTablename)
		reportStmtPrep, err := tx.Prepare(reportStmt)
		if err != nil {
			return err
		}
		defer reportStmtPrep.Close()

		// Create a temporary matches index on (epochidx, epochdur).
		fullMatchesTableName := mkt.Name + "." + matchesTableName
		matchIndexName := "matches_epidxdur_temp_idx"
		_, err = tx.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (epochidx, epochdur);",
			matchIndexName, fullMatchesTableName))
		if err != nil {
			return err
		}
		defer func() {
			if errors.Is(err, sql.ErrTxDone) {
				return // whole transaction including index creation is rolled back
			}
			// Success or other error - drop the index explicitly.
			fullIndexName := mkt.Name + "." + matchIndexName
			_, errDrop := tx.Exec(fmt.Sprintf("DROP INDEX %s;", fullIndexName))
			if errDrop != nil {
				log.Warnf("Failed to drop index %v: %v", fullIndexName, errDrop)
			}
		}()

		// matches(qty,rate,takerSell) SELECT statement
		matchStatsStmt := fmt.Sprintf(internal.RetrieveMatchStatsByEpoch, fullMatchesTableName)
		matchStatsStmtPrep, err := tx.Prepare(matchStatsStmt)
		if err != nil {
			return err
		}
		defer matchStatsStmtPrep.Close()

		var startRate, endRate uint64
		var totalMatches uint64
		var totalVolume, totalQVolume uint64
		for i, dur := range durs {
			log.Infof("Processing all %d of the %d ms %q epochs from idx %d to %d...",
				ends[i]-starts[i]+1, dur, mkt.Name, starts[i], ends[i])
			endIdx := ends[i]
			for idx := starts[i]; idx <= endIdx; idx++ {
				if idx%50000 == 0 {
					to := min(idx+50000, endIdx+1)
					log.Infof(" - Processing epochs [%d, %d)...", idx, to)
				}
				var rates, quantities []uint64 // don't shadow err from outer scope
				rates, quantities, _, err = matchStatsForMarketEpoch(matchStatsStmtPrep, idx, dur)
				if err != nil {
					return err
				}
				epochEnd := (idx + 1) * dur
				if len(rates) == 0 {
					// No trade matches in this epoch.
					_, err = reportStmtPrep.Exec(epochEnd, dur, 0, 0, 0, 0, startRate, startRate)
					if err != nil {
						return err
					}
					continue
				}

				var matchVolume, quoteVolume, highRate uint64
				lowRate := uint64(math.MaxInt64)
				for i, qty := range quantities {
					matchVolume += qty
					rate := rates[i]
					quoteVolume += calc.BaseToQuote(rate, qty)
					if rate > highRate {
						highRate = rate
					}
					if rate < lowRate {
						lowRate = rate
					}
				}
				totalVolume += matchVolume
				totalQVolume += quoteVolume
				totalMatches += uint64(len(quantities))

				// In the absence of a book snapshot, ballpark the rates. Note
				// that cancel order matches that change the mid market book
				// rate are not captured so start/end rates can be inaccurate
				// given long periods with no trades but book changes.
				midRate := (lowRate + highRate) / 2 // maybe average instead
				if startRate == 0 {
					startRate = midRate
				} else {
					startRate = endRate // from previous epoch with matches
				}
				endRate = midRate

				// No book buy / sell depth (see bookVolumes in server/matcher).
				_, err = reportStmtPrep.Exec(epochEnd, dur, matchVolume, quoteVolume,
					highRate, lowRate, startRate, endRate)
				if err != nil {
					return err
				}
			}
		} // range durs
		log.Debugf("Processed %d matches doing %s in %s volume (%s in %s volume)", totalMatches,
			baseUnitInfo.ConventionalString(totalVolume), strings.ToUpper(dex.BipIDSymbol(mkt.Base)),
			quoteUnitInfo.ConventionalString(totalQVolume), strings.ToUpper(dex.BipIDSymbol(mkt.Quote)))
		return nil
	}

	for _, mkt := range mkts {
		err = doMarketMatches(mkt)
		if err != nil {
			return err
		}
	}
	return nil
}

func v3Upgrade(tx *sql.Tx) error {
	// Create the fee_asset column.
	_, err := tx.Exec(`ALTER TABLE ` + accountsTableName + ` ADD COLUMN IF NOT EXISTS fee_asset INT4;`)
	if err != nil {
		return err
	}
	// Set existing rows fee_asset to 42, Decred's asset ID, since prior to this
	// upgrade, only DCR was accepted for registration.
	_, err = tx.Exec(`UPDATE ` + accountsTableName + ` SET fee_asset = 42;`) // not as default in ALTER
	return err
}

func v4Upgrade(tx *sql.Tx) (err error) {
	if _, err = tx.Exec("ALTER TABLE markets ALTER COLUMN base TYPE INT8;"); err != nil {
		return
	}
	_, err = tx.Exec("ALTER TABLE markets ALTER COLUMN quote TYPE INT8;")
	return err
}

func v5Upgrade(tx *sql.Tx) (err error) {
	mkts, err := loadMarkets(tx, marketsTableName)
	if err != nil {
		return fmt.Errorf("failed to read markets table: %w", err)
	}

	doTable := func(tableName string) error {
		_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN epoch_gap INT4 DEFAULT -1;", tableName))
		return err
	}

	log.Infof("Adding epoch_gap column to cancel tables for %d markets", len(mkts))

	for _, mkt := range mkts {
		if err := doTable(mkt.Name + "." + cancelsArchivedTableName); err != nil {
			return err
		}
		if err := doTable(mkt.Name + "." + cancelsActiveTableName); err != nil {
			return err
		}
	}
	return nil
}

// v6Upgrade creates the bonds table and its indexes on account_id and lockTime.
func v6Upgrade(tx *sql.Tx) error {
	bondsCreated, err := createTableStmt(tx, internal.CreateBondsTableV0, publicSchema, bondsTableName)
	if err != nil {
		return fmt.Errorf("failed to create bonds table: %w", err)
	}
	if bondsCreated {
		log.Infof("Created new %q table", bondsTableName)
	} else {
		log.Warnf("Unexpected existing %q table!", bondsTableName)
	}

	namespacedBondsTable := publicSchema + "." + bondsTableName
	err = createIndexStmt(tx, internal.CreateBondsAcctIndexV0, indexBondsOnAccountName, namespacedBondsTable)
	if err != nil {
		return fmt.Errorf("failed to index bonds table on account: %w", err)
	}

	err = createIndexStmt(tx, internal.CreateBondsLockTimeIndexV0, indexBondsOnLockTimeName, namespacedBondsTable)
	if err != nil {
		return fmt.Errorf("failed to index bonds table on lock time: %w", err)
	}

	// drop the accounts.broken_rule column
	namespacedAccountsTable := publicSchema + "." + accountsTableName
	_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS broken_rule;", namespacedAccountsTable))
	if err != nil {
		return fmt.Errorf("failed to drop the accounts.broken_rule column: %w", err)
	}

	return nil
}

func v7Upgrade(tx *sql.Tx) error {
	const columnName = "reputation_ver"
	const tableName = publicSchema + "." + accountsTableName
	// Create the column, setting existing entries to false.
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT2 DEFAULT 0;", tableName, columnName)
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("error adding reputation_ver column: %w", err)
	}
	// New entries should be true.
	query = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT 1;", tableName, columnName)
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("error updating reputation_ver default value: %w", err)
	}
	return nil
}

// safeIdentRE matches valid PostgreSQL schema/table name components used in
// market names (e.g. "dcr_btc", "polygonTKN_eth"). Alphanumeric and
// underscore are expected, including uppercase from the "TKN" replacement
// for '.' in marketSchema.
var safeIdentRE = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// v8Upgrade adds per-match swap address columns to all market matches tables.
func v8Upgrade(tx *sql.Tx) error {
	mkts, err := loadMarkets(tx, marketsTableName)
	if err != nil {
		return fmt.Errorf("failed to read markets table: %w", err)
	}

	log.Infof("Adding per-match swap address columns to matches tables for %d markets", len(mkts))

	for _, mkt := range mkts {
		schema := marketSchema(mkt.Name)
		if !safeIdentRE.MatchString(schema) {
			return fmt.Errorf("market schema %q (from %q) contains disallowed characters", schema, mkt.Name)
		}
		tableName := schema + "." + matchesTableName
		_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS makerSwapAddr TEXT DEFAULT '';", tableName))
		if err != nil {
			return fmt.Errorf("error adding makerSwapAddr column to %s: %w", tableName, err)
		}
		_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS takerSwapAddr TEXT DEFAULT '';", tableName))
		if err != nil {
			return fmt.Errorf("error adding takerSwapAddr column to %s: %w", tableName, err)
		}
	}
	return nil
}

// v9Upgrade creates mesh tables, removes accounts.fee_asset, adds market indexes,
// and drops archived order commitment and preimage uniqueness constraints. It
// also converts legacy reputation to points and adds a genesis event to
// databases with existing state.
//
// The uniqueness constraints are dropped because the list of archived orders may
// differ between two databases. A new order that shares its commitment with an
// archived order may get accepted by one node and rejected by another if we
// keey the contraints.
func v9Upgrade(tx *sql.Tx) error {
	if err := createMeshTables(tx); err != nil {
		return err
	}

	accountsTable := qualifySchemaTable(publicSchema, accountsTableName)
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS fee_asset;", accountsTable)); err != nil {
		return fmt.Errorf("drop legacy accounts.fee_asset column: %w", err)
	}

	markets, err := loadMarkets(tx, marketsTableName)
	if err != nil {
		return fmt.Errorf("load markets: %w", err)
	}

	log.Infof("Updating indexes and archived order constraints for %d markets", len(markets))

	for _, market := range markets {
		schema := marketSchema(market.Name)
		if !safeIdentRE.MatchString(schema) {
			return fmt.Errorf("market schema %q (from %q) contains disallowed characters", schema, market.Name)
		}
		if err := createMarketMatchIndexes(tx, schema); err != nil {
			return fmt.Errorf("create active-match indexes for %s: %w", schema, err)
		}
		if err := dropArchivedOrderUniqueConstraints(tx, schema); err != nil {
			return err
		}
		if err := createMarketArchivedCommitIndexes(tx, schema); err != nil {
			return fmt.Errorf("create archived commit indexes for %s: %w", schema, err)
		}
	}

	if err := upgradeReputationV1(tx, markets); err != nil {
		return err
	}
	return stampMeshGenesis(tx)
}

// upgradeReputationV1 converts every remaining version-0 account to points.
func upgradeReputationV1(tx *sql.Tx, markets []*dex.MarketInfo) error {
	rows, err := tx.Query(`SELECT account_id FROM accounts WHERE reputation_ver = 0 ORDER BY account_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var users []account.AccountID
	for rows.Next() {
		var user account.AccountID
		if err := rows.Scan(&user); err != nil {
			return err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	log.Infof("Converting legacy reputation for %d accounts", len(users))
	insert, err := tx.Prepare(fmt.Sprintf(internal.InsertPoints, qualifySchemaTable(publicSchema, pointsTableName)))
	if err != nil {
		return err
	}
	defer insert.Close()
	for i, user := range users {
		if err := upgradeUserReputationV1(tx, insert, markets, user); err != nil {
			return fmt.Errorf("convert reputation for %s: %w", user, err)
		}
		if (i+1)%1000 == 0 {
			log.Infof("Converted reputation for %d/%d accounts", i+1, len(users))
		}
	}
	log.Infof("Converted legacy reputation for %d accounts", len(users))
	return nil
}

// upgradeUserReputationV1 reconstructs an account's recent outcomes from its
// order and match history, stores the points, and marks the account as version 1.
func upgradeUserReputationV1(tx *sql.Tx, insert *sql.Stmt, markets []*dex.MarketInfo, user account.AccountID) error {
	// Keep the limits used by the original version-0 conversion, independently
	// of future changes to reputation scoring.
	const preimageLimit, matchLimit, orderLimit, freeCancelThreshold = 40, 60, 100, 2
	type stampedOutcome struct {
		link    order.OrderID
		stamp   int64
		outcome db.Outcome
	}
	var preimages, matches, orders []stampedOutcome
	// The transaction's context, supplied by upgradeDB, controls cancellation.
	ctx := context.Background()
	for _, market := range markets {
		schema := marketSchema(market.Name)
		matchTable := qualifySchemaTable(schema, matchesTableName)
		matchResults, err := completedAndAtFaultMatches(ctx, tx, matchTable, user, matchLimit, market.Base, market.Quote)
		if err != nil {
			return fmt.Errorf("read matches in %s: %w", schema, err)
		}
		for _, m := range matchResults {
			outcome := db.OutcomeSwapSuccess
			if m.Fail {
				switch m.Status {
				case order.NewlyMatched:
					outcome = db.OutcomeNoSwapAsMaker
				case order.MakerSwapCast:
					outcome = db.OutcomeNoSwapAsTaker
				case order.TakerSwapCast:
					outcome = db.OutcomeNoRedeemAsMaker
				case order.MakerRedeemed:
					outcome = db.OutcomeNoRedeemAsTaker
				}
			}
			matches = append(matches, stampedOutcome{order.OrderID(m.ID), m.Time, outcome})
		}

		orderTable := qualifySchemaTable(schema, ordersArchivedTableName)
		cancelTable := qualifySchemaTable(schema, cancelsArchivedTableName)
		for _, stmt := range []string{
			fmt.Sprintf(internal.PreimageResultsLastN, orderTable),
			fmt.Sprintf(internal.CancelPreimageResultsLastN, cancelTable),
		} {
			results, err := preimageStats(ctx, tx, stmt, user, preimageLimit)
			if err != nil {
				return fmt.Errorf("read preimages in %s: %w", schema, err)
			}
			for _, p := range results {
				outcome := db.OutcomePreimageSuccess
				if p.Miss {
					outcome = db.OutcomePreimageMiss
				}
				preimages = append(preimages, stampedOutcome{p.ID, p.Time, outcome})
			}
		}

		completed, err := completedUserOrders(ctx, tx, orderTable, user, orderLimit)
		if err != nil {
			return fmt.Errorf("read completed orders in %s: %w", schema, err)
		}
		for _, o := range completed {
			orders = append(orders, stampedOutcome{o.oid, o.t, db.OutcomeOrderComplete})
		}
		epochTable := qualifySchemaTable(schema, epochsTableName)
		stmt := fmt.Sprintf(internal.RetrieveCancelTimesForUserByStatus, cancelTable, epochTable)
		cancels, err := executedCancelsForUser(ctx, tx, stmt, user, orderLimit)
		if err != nil {
			return fmt.Errorf("read executed cancels in %s: %w", schema, err)
		}
		// Filter exempt revokes before LIMIT so they cannot hide older counted ones.
		stmt = fmt.Sprintf(`SELECT oid, target_order, server_time, epoch_idx
			FROM %s WHERE account_id = $1 AND status = $2 AND epoch_idx != -1
			ORDER BY server_time DESC LIMIT $3`, cancelTable)
		revokes, err := revokeGeneratedCancelsForUser(ctx, tx, stmt, user, orderLimit)
		if err != nil {
			return fmt.Errorf("read revokes in %s: %w", schema, err)
		}
		for _, o := range append(cancels, revokes...) {
			outcome := db.OutcomeOrderComplete
			if o.EpochGap >= 0 && o.EpochGap < freeCancelThreshold {
				outcome = db.OutcomeOrderCanceled
			}
			orders = append(orders, stampedOutcome{o.ID, o.MatchTime, outcome})
		}
	}

	for _, group := range []struct {
		outcomes []stampedOutcome
		class    db.OutcomeClass
		limit    int
	}{
		{preimages, db.OutcomeClassPreimage, preimageLimit},
		{matches, db.OutcomeClassMatch, matchLimit},
		{orders, db.OutcomeClassOrder, orderLimit},
	} {
		outcomes := group.outcomes
		sort.Slice(outcomes, func(i, j int) bool {
			if outcomes[i].stamp == outcomes[j].stamp {
				return bytes.Compare(outcomes[i].link[:], outcomes[j].link[:]) < 0
			}
			return outcomes[i].stamp < outcomes[j].stamp
		})
		if len(outcomes) > group.limit {
			outcomes = outcomes[len(outcomes)-group.limit:]
		}
		// Point IDs must increase from oldest to newest within each class.
		for _, o := range outcomes {
			var id int64
			if err := insert.QueryRow(user, o.link, group.class, o.outcome).Scan(&id); err != nil {
				return fmt.Errorf("insert reputation point: %w", err)
			}
		}
	}
	_, err := tx.Exec(fmt.Sprintf(internal.UpdateReputationVersion, qualifySchemaTable(publicSchema, accountsTableName)), 1, user)
	return err
}

func dropArchivedOrderUniqueConstraints(tx *sql.Tx, schema string) error {
	constraints := []struct{ table, name string }{
		{ordersArchivedTableName, "orders_archived_commit_key"},
		{ordersArchivedTableName, "orders_archived_preimage_key"},
		{cancelsArchivedTableName, "cancels_archived_commit_key"},
		{cancelsArchivedTableName, "cancels_archived_preimage_key"},
	}
	for _, constraint := range constraints {
		stmt := fmt.Sprintf("ALTER TABLE %s.%s DROP CONSTRAINT IF EXISTS %s;",
			schema, constraint.table, constraint.name)
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("drop %s on %s.%s: %w", constraint.name, schema, constraint.table, err)
		}
	}
	return nil
}

// createMeshTables creates the public tables needed by the v9 upgrade.
func createMeshTables(tx *sql.Tx) error {
	for _, table := range []struct{ stmt, name string }{
		{internal.CreateEventLogTable, eventLogTableName},
		{internal.CreateMarketLifecycleTable, marketLifecycleTableName},
		{internal.CreatePointsTable, pointsTableName},
		{internal.CreatePrepaidBondsTable, prepaidBondsTableName},
	} {
		if _, err := createTableStmt(tx, table.stmt, publicSchema, table.name); err != nil {
			return fmt.Errorf("create %s: %w", table.name, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(internal.CreatePointsIndex, publicSchema+"."+pointsTableName)); err != nil {
		return fmt.Errorf("create points index: %w", err)
	}
	return nil
}

// meshGenesisPayload is the payload of a mesh genesis event.
type meshGenesisPayload struct {
	// Nonce distinguishes independent databases.
	Nonce dex.Bytes `json:"nonce"`
	// UnixMs is the event creation time in Unix milliseconds.
	UnixMs int64 `json:"unixMs"`
}

// stampMeshGenesis adds the first event to a database with existing state and
// an empty event log. Empty databases remain available for snapshot loading.
func stampMeshGenesis(tx *sql.Tx) error {
	const genesisSeq = 1

	eventLog := qualifySchemaTable(publicSchema, eventLogTableName)
	query := fmt.Sprintf(internal.SelectEventLogFrontier, eventLog)
	frontier, err := scanEventLogFrontier(tx.QueryRow(query))
	if err != nil {
		return fmt.Errorf("read event log frontier: %w", err)
	}
	if frontier.Seq > 0 {
		return nil
	}
	empty, err := hasNoEventSourcedState(context.Background(), tx)
	if err != nil {
		return fmt.Errorf("check for event-sourced state: %w", err)
	}
	if empty {
		return nil
	}

	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate genesis nonce: %w", err)
	}
	payload, err := json.Marshal(&meshGenesisPayload{
		Nonce:  nonce[:],
		UnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("encode genesis payload: %w", err)
	}
	tipHash := eventLogHash(nil, genesisSeq, db.MeshGenesisKind, payload, nil)
	stmt := fmt.Sprintf(internal.InsertEventLog, eventLog)
	if _, err := tx.Exec(stmt, int64(genesisSeq), db.MeshGenesisKind, payload, []byte{}, tipHash); err != nil {
		return fmt.Errorf("insert genesis row: %w", err)
	}
	log.Infof("Inserted mesh genesis at sequence %d with tip %x", genesisSeq, tipHash)
	return nil
}

// DBVersion retrieves the database version from the meta table.
func DBVersion(db *sql.DB) (ver uint32, err error) {
	err = db.QueryRow(internal.SelectDBVersion).Scan(&ver)
	return
}

func setDBVersion(db sqlExecutor, ver uint32) error {
	res, err := db.Exec(internal.SetDBVersion, ver)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("set the DB version in %d rows instead of 1", n)
	}
	return nil
}

func upgradeDB(ctx context.Context, db *sql.DB) error {
	// Get the DB version from the meta table. Nonexistent meta table or
	// meta.schema_version column implies v0, the upgrade from which adds the
	// table and schema_version column.
	var current uint32
	found, err := tableExists(db, metaTableName)
	if err != nil {
		return err
	}
	if found {
		found, err = columnExists(db, "public", metaTableName, "schema_version")
		if err != nil {
			return err
		}
		if found {
			current, err = DBVersion(db)
			if err != nil {
				return fmt.Errorf("failed to get DB version: %w", err)
			}
		} // else v1 upgrade creates meta.schema_version column
	} // else v1 upgrade creates meta table

	if current == dbVersion {
		log.Infof("DCRDEX database ready at version %d", dbVersion)
		return nil // all upgraded
	}

	if current > dbVersion {
		return fmt.Errorf("current DB version %d is newer than highest recognized version %d",
			current, dbVersion)
	}

	runUpgradeTx := func(targetVer uint32, up func(db *sql.Tx) error) error {
		// Canceling the context automatically rolls back the transaction.
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() {
			// On error, rollback the transaction unless ctx was canceled
			// (sql.ErrTxDone) because then rollback is automatic. See the
			// (*sql.DB).BeginTx docs.
			if err == nil || errors.Is(err, sql.ErrTxDone) {
				return
			}
			log.Warnf("Rolling back upgrade to version %d", targetVer-1)
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Errorf("Rollback failed: %v", errRollback)
			}
		}()

		if err = up(tx); err != nil {
			return fmt.Errorf("failed to upgrade to db version %d: %w", targetVer, err)
		}

		if err = setDBVersion(tx, targetVer); err != nil {
			return fmt.Errorf("failed to set new DB version %d: %w", targetVer, err)
		}

		err = tx.Commit() // for the defer
		return err
	}

	log.Infof("Upgrading DB scheme from %d to %d", current, len(upgrades))
	for i, up := range upgrades[current:] {
		targetVer := current + uint32(i) + 1
		log.Debugf("Upgrading DB scheme to %d...", targetVer)
		if err = runUpgradeTx(targetVer, up); err != nil {
			if errors.Is(err, sql.ErrTxDone) {
				return fmt.Errorf("upgrade cancelled (rolled back to version %d)", current+uint32(i))
			}
			return err
		}
	}

	current, err = DBVersion(db)
	if err != nil {
		return fmt.Errorf("failed to get DB version: %w", err)
	}
	log.Infof("Upgrades complete. DB is at version %d", current)
	return nil
}
