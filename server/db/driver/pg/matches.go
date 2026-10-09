// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"decred.org/dcrdex/server/meshevents"
	"github.com/lib/pq"
)

func (a *Archiver) matchTableName(match *order.Match) (string, error) {
	marketSchema, err := a.marketSchema(match.Maker.Base(), match.Maker.Quote())
	if err != nil {
		return "", err
	}
	return fullMatchesTableName(a.dbName, marketSchema), nil
}

// ActiveSwaps loads the full details for all active swaps across all markets.
func (a *Archiver) ActiveSwaps() ([]*db.SwapDataFull, error) {
	var sd []*db.SwapDataFull

	for schema, mkt := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, schema)
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		matches, swapData, err := activeSwaps(ctx, a.db, matchesTableName)
		cancel()
		if err != nil {
			return nil, err
		}

		for i := range matches {
			sd = append(sd, &db.SwapDataFull{
				Base:      mkt.Base,
				Quote:     mkt.Quote,
				MatchData: matches[i],
				SwapData:  swapData[i],
			})
		}
	}

	return sd, nil
}

func activeSwaps(ctx context.Context, dbe *sql.DB, tableName string) (matches []*db.MatchData, swapData []*db.SwapData, err error) {
	stmt := fmt.Sprintf(internal.RetrieveActiveMarketMatchesExtended, tableName)
	rows, err := dbe.QueryContext(ctx, stmt)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var m db.MatchData
		var sd db.SwapData

		var status uint8
		var baseRate, quoteRate sql.NullInt64
		var takerSell sql.NullBool
		var takerAddr, makerAddr sql.NullString
		var contractATime, contractBTime, redeemATime, redeemBTime sql.NullInt64

		err = rows.Scan(&m.ID, &takerSell,
			&m.Taker, &m.TakerAcct, &takerAddr,
			&m.Maker, &m.MakerAcct, &makerAddr,
			&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
			&baseRate, &quoteRate, &status,
			&sd.SigMatchAckMaker, &sd.SigMatchAckTaker,
			&sd.MakerSwapAddr, &sd.TakerSwapAddr,
			&sd.ContractACoinID, &sd.ContractA, &contractATime,
			&sd.ContractAAckSig,
			&sd.ContractBCoinID, &sd.ContractB, &contractBTime,
			&sd.ContractBAckSig,
			&sd.RedeemACoinID, &sd.RedeemASecret, &redeemATime,
			&sd.RedeemAAckSig,
			&sd.RedeemBCoinID, &redeemBTime)
		if err != nil {
			return nil, nil, err
		}

		// All are active.
		m.Active = true

		m.Status = order.MatchStatus(status)
		m.TakerSell = takerSell.Bool
		m.TakerAddr = takerAddr.String
		m.MakerAddr = makerAddr.String
		m.BaseRate = uint64(baseRate.Int64)
		m.QuoteRate = uint64(quoteRate.Int64)

		sd.ContractATime = contractATime.Int64
		sd.ContractBTime = contractBTime.Int64
		sd.RedeemATime = redeemATime.Int64
		sd.RedeemBTime = redeemBTime.Int64

		matches = append(matches, &m)
		swapData = append(swapData, &sd)
	}

	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return
}

// CompletedAndAtFaultMatchStats retrieves the outcomes of matches that were (1)
// successfully completed by the specified user, or (2) failed with the user
// being the at-fault party. Note that the MakerRedeemed match status may be
// either a success or failure depending on if the user was the maker or taker
// in the swap, respectively, and the MatchOutcome.Fail flag disambiguates this.
func (a *Archiver) CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*db.MatchOutcome, error) {
	var outcomes []*db.MatchOutcome

	for schema, mkt := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, schema)
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		matchOutcomes, err := completedAndAtFaultMatches(ctx, a.db, matchesTableName, aid, lastN, mkt.Base, mkt.Quote)
		cancel()
		if err != nil {
			return nil, err
		}

		outcomes = append(outcomes, matchOutcomes...)
	}

	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[i].Time < outcomes[j].Time // ascending
	})
	if len(outcomes) > lastN {
		outcomes = outcomes[len(outcomes)-lastN:]
	}
	return outcomes, nil
}

// UserMatchFails retrieves up to the last n most recent failed and unforgiven
// match outcomes for the user.
func (a *Archiver) UserMatchFails(aid account.AccountID, lastN int) ([]*db.MatchFail, error) {
	var fails []*db.MatchFail

	for schema := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, schema)
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		marketFails, err := atFaultMatches(ctx, a.db, matchesTableName, aid, lastN)
		cancel()
		if err != nil {
			return nil, err
		}

		fails = append(fails, marketFails...)
	}

	if len(fails) > lastN {
		fails = fails[:lastN]
	}
	return fails, nil
}

func completedAndAtFaultMatches(ctx context.Context, dbe sqlQueryer, tableName string,
	aid account.AccountID, lastN int, base, quote uint32) (outcomes []*db.MatchOutcome, err error) {
	stmt := fmt.Sprintf(internal.CompletedOrAtFaultMatchesLastN, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, aid, lastN)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var status uint8
		var success bool
		var refTime sql.NullInt64
		var mid order.MatchID
		var value uint64
		err = rows.Scan(&mid, &status, &value, &success, &refTime)
		if err != nil {
			return
		}

		if !refTime.Valid {
			continue // should not happen as all matches will have an epoch time, but don't error
		}

		// A little seat belt in case the query returns inconsistent results
		// where success and status don't jive.
		switch order.MatchStatus(status) {
		case order.NewlyMatched, order.MakerSwapCast, order.TakerSwapCast:
			if success {
				log.Errorf("successfully completed match in status %v returned from DB", status)
				continue
			}
		// MakerRedeemed can be either depending on user role (maker/taker).
		case order.MatchComplete:
			if !success {
				log.Errorf("failed match in status %v returned from DB", status)
				continue
			}
		}

		outcomes = append(outcomes, &db.MatchOutcome{
			Status: order.MatchStatus(status),
			ID:     mid,
			Fail:   !success,
			Time:   refTime.Int64,
			Value:  value,
			Base:   base,
			Quote:  quote,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return
}

func atFaultMatches(ctx context.Context, dbe *sql.DB, tableName string, aid account.AccountID, lastN int) (fails []*db.MatchFail, err error) {
	stmt := fmt.Sprintf(internal.UserMatchFails, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, aid, lastN)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var status uint8
		var mid order.MatchID
		err = rows.Scan(&mid, &status)
		if err != nil {
			return
		}

		fails = append(fails, &db.MatchFail{
			Status: order.MatchStatus(status),
			ID:     mid,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return
}

// activeUserMatches retrieves all active matches involving a user on the given
// market.
func activeUserMatches(ctx context.Context, dbe *sql.DB, tableName string, aid account.AccountID) ([]*db.MatchData, error) {
	stmt := fmt.Sprintf(internal.RetrieveActiveUserMatches, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, aid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []*db.MatchData
	for rows.Next() {
		var m db.MatchData
		var status uint8
		var baseRate, quoteRate sql.NullInt64
		var takerSell sql.NullBool
		var takerAddr, makerAddr sql.NullString
		var makerSwapAddr, takerSwapAddr sql.NullString
		err = rows.Scan(&m.ID, &takerSell,
			&m.Taker, &m.TakerAcct, &takerAddr,
			&m.Maker, &m.MakerAcct, &makerAddr,
			&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
			&baseRate, &quoteRate, &status,
			&makerSwapAddr, &takerSwapAddr)
		if err != nil {
			return nil, err
		}
		m.Active = true
		m.Status = order.MatchStatus(status)
		m.TakerSell = takerSell.Bool
		m.TakerAddr = takerAddr.String
		m.MakerAddr = makerAddr.String
		m.MakerSwapAddr = makerSwapAddr.String
		m.TakerSwapAddr = takerSwapAddr.String
		m.BaseRate = uint64(baseRate.Int64)
		m.QuoteRate = uint64(quoteRate.Int64)

		matches = append(matches, &m)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return matches, nil
}

func (a *Archiver) marketMatches(base, quote uint32, includeInactive bool, N int64, f func(*db.MatchDataWithCoins) error) (int, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return 0, err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	var rows *sql.Rows
	if includeInactive {
		stmt := fmt.Sprintf(internal.RetrieveMarketMatches, matchesTableName)
		if N <= 0 {
			N = math.MaxInt64
		}
		rows, err = a.db.QueryContext(ctx, stmt, N)
	} else {
		stmt := fmt.Sprintf(internal.RetrieveActiveMarketMatches, matchesTableName)
		rows, err = a.db.QueryContext(ctx, stmt) // no N
	}
	if err != nil {
		return 0, err
	}

	return rowsToMatchDataWithCoinsStreaming(rows, includeInactive, f)
}

// MarketMatches retrieves all active matches for a market.
func (a *Archiver) MarketMatches(base, quote uint32) ([]*db.MatchDataWithCoins, error) {
	var ms []*db.MatchDataWithCoins
	f := func(m *db.MatchDataWithCoins) error {
		ms = append(ms, m)
		return nil
	}
	_, err := a.marketMatches(base, quote, false, -1, f) // N ignored with only active
	if err != nil {
		return nil, err
	}
	return ms, nil
}

// MarketMatchesStreaming streams all active matches for a market into the
// provided function. If includeInactive, all matches are streamed. A limit may
// be specified, where <=0 means unlimited.
func (a *Archiver) MarketMatchesStreaming(base, quote uint32, includeInactive bool, N int64, f func(*db.MatchDataWithCoins) error) (int, error) {
	return a.marketMatches(base, quote, includeInactive, N, f)
}

func rowsToMatchDataWithCoinsStreaming(rows *sql.Rows, includeInactive bool, f func(*db.MatchDataWithCoins) error) (int, error) {
	defer rows.Close()

	var N int
	for rows.Next() {
		var m db.MatchDataWithCoins
		var status uint8
		var baseRate, quoteRate sql.NullInt64
		var takerSell sql.NullBool
		var takerAddr, makerAddr sql.NullString
		if includeInactive {
			// "active" column SELECTed.
			err := rows.Scan(&m.ID, &m.Active, &takerSell,
				&m.Taker, &m.TakerAcct, &takerAddr,
				&m.Maker, &m.MakerAcct, &makerAddr,
				&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
				&baseRate, &quoteRate, &status,
				&m.MakerSwapCoin, &m.TakerSwapCoin, &m.MakerRedeemCoin, &m.TakerRedeemCoin)
			if err != nil {
				return N, err
			}
		} else {
			// "active" column not SELECTed.
			err := rows.Scan(&m.ID, &takerSell,
				&m.Taker, &m.TakerAcct, &takerAddr,
				&m.Maker, &m.MakerAcct, &makerAddr,
				&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
				&baseRate, &quoteRate, &status,
				&m.MakerSwapCoin, &m.TakerSwapCoin, &m.MakerRedeemCoin, &m.TakerRedeemCoin)
			if err != nil {
				return N, err
			}
			// All are active.
			m.Active = true
		}
		m.Status = order.MatchStatus(status)
		m.TakerSell = takerSell.Bool
		m.TakerAddr = takerAddr.String
		m.MakerAddr = makerAddr.String
		m.BaseRate = uint64(baseRate.Int64)
		m.QuoteRate = uint64(quoteRate.Int64)

		if err := f(&m); err != nil {
			return N, err
		}
		N++
	}

	return N, rows.Err()
}

// AllActiveUserMatches retrieves a MatchData slice for active matches in all
// markets involving the given user. Swaps that have successfully completed or
// failed are not included.
func (a *Archiver) AllActiveUserMatches(aid account.AccountID) ([]*db.MatchData, error) {
	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	var matches []*db.MatchData
	for schema := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, schema)
		mdM, err := activeUserMatches(ctx, a.db, matchesTableName, aid)
		if err != nil {
			return nil, err
		}

		matches = append(matches, mdM...)
	}

	return matches, nil
}

// MatchStatuses retrieves a *db.MatchStatus for every match in matchIDs for
// which there is data, and for which the user is at least one of the parties.
// It is not an error if a match ID in matchIDs does not match, i.e. the
// returned slice need not be the same length as matchIDs.
func (a *Archiver) MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error) {
	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	return matchStatusesByID(ctx, a.db, aid, matchesTableName, matchIDs)

}

func upsertMatch(dbe sqlExecutor, tableName string, match *order.Match) (int64, error) {
	var takerAddr string
	tt := match.Taker.Trade()
	if tt != nil {
		takerAddr = tt.SwapAddress()
	}

	// Cancel orders do not store taker or maker addresses, and are stored with
	// complete status with no active swap negotiation.
	if takerAddr == "" {
		stmt := fmt.Sprintf(internal.UpsertCancelMatch, tableName)
		return sqlExec(dbe, stmt, match.ID(),
			match.Taker.ID(), match.Taker.User(), // taker address remains unset/default
			match.Maker.ID(), match.Maker.User(), // as does maker's since it is not used
			match.Epoch.Idx, match.Epoch.Dur,
			int64(match.Quantity), int64(match.Rate), // quantity and rate may be useful for cancel statistics however
			int8(order.MatchComplete)) // status is complete
	}

	stmt := fmt.Sprintf(internal.UpsertMatch, tableName)
	return sqlExec(dbe, stmt, match.ID(), tt.Sell,
		match.Taker.ID(), match.Taker.User(), takerAddr,
		match.Maker.ID(), match.Maker.User(), match.Maker.Trade().SwapAddress(),
		match.Epoch.Idx, match.Epoch.Dur,
		int64(match.Quantity), int64(match.Rate),
		match.FeeRateBase, match.FeeRateQuote, int8(match.Status))
}

// MatchByID retrieves the match for the given MatchID.
func (a *Archiver) MatchByID(mid order.MatchID, base, quote uint32) (*db.MatchData, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	matchData, err := matchByID(a.db, matchesTableName, mid)
	if errors.Is(err, sql.ErrNoRows) {
		err = db.ArchiveError{Code: db.ErrUnknownMatch}
	}
	return matchData, err
}

func matchByID(dbe sqlQueryer, tableName string, mid order.MatchID) (*db.MatchData, error) {
	var m db.MatchData
	var status uint8
	var baseRate, quoteRate sql.NullInt64
	var takerAddr, makerAddr sql.NullString
	var takerSell sql.NullBool
	stmt := fmt.Sprintf(internal.RetrieveMatchByID, tableName)
	err := dbe.QueryRow(stmt, mid).
		Scan(&m.ID, &m.Active, &takerSell,
			&m.Taker, &m.TakerAcct, &takerAddr,
			&m.Maker, &m.MakerAcct, &makerAddr,
			&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
			&baseRate, &quoteRate, &status)
	if err != nil {
		return nil, err
	}
	m.TakerSell = takerSell.Bool
	m.TakerAddr = takerAddr.String
	m.MakerAddr = makerAddr.String
	m.BaseRate = uint64(baseRate.Int64)
	m.QuoteRate = uint64(quoteRate.Int64)
	m.Status = order.MatchStatus(status)
	return &m, nil
}

// matchStatusesByID retrieves the []*db.MatchStatus for the requested matchIDs.
// See docs for MatchStatuses.
func matchStatusesByID(ctx context.Context, dbe *sql.DB, aid account.AccountID, tableName string, matchIDs []order.MatchID) ([]*db.MatchStatus, error) {
	stmt := fmt.Sprintf(internal.SelectMatchStatuses, tableName)
	pqArr := make(pq.ByteaArray, 0, len(matchIDs))
	for i := range matchIDs {
		pqArr = append(pqArr, matchIDs[i][:])
	}
	rows, err := dbe.QueryContext(ctx, stmt, aid, pqArr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statuses := make([]*db.MatchStatus, 0, len(matchIDs))
	for rows.Next() {
		status := new(db.MatchStatus)
		err := rows.Scan(&status.TakerSell, &status.IsTaker, &status.IsMaker, &status.ID,
			&status.Status, &status.MakerContract, &status.TakerContract, &status.MakerSwap,
			&status.TakerSwap, &status.MakerRedeem, &status.TakerRedeem, &status.Secret, &status.Active)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return statuses, nil
}

// Swap Data
//
// In the swap process, the counterparties are:
// - Initiator or party A on chain X. This is the maker in the DEX.
// - Participant or party B on chain Y. This is the taker in the DEX.
//
// For each match, a successful swap will generate the following data that must
// be stored:
// - 5 client signatures. Both parties sign the data to acknowledge (1) the
//   match ack, and (2) the counterparty's contract script and contract
//   transaction. Plus, the taker acks the makers's redemption transaction.
// - 2 swap contracts and the associated transaction outputs (more generally,
//   coinIDs), one on each party's blockchain.
// - the secret hash from the initiator contract
// - the secret from from the initiator redeem
// - 2 redemption transaction outputs (coinIDs).
//
// The methods for saving this data are defined below in the order in which the
// data is expected from the parties.

// SwapDataFullByID loads an active or inactive match and its settlement data
// from the configured markets. It returns ErrUnknownMatch when no match is found.
func (a *Archiver) SwapDataFullByID(mid order.MatchID) (*db.SwapDataFull, error) {
	for _, mkt := range a.markets {
		md, err := a.MatchByID(mid, mkt.Base, mkt.Quote)
		if err != nil {
			if db.IsErrMatchUnknown(err) {
				continue
			}
			return nil, err
		}
		_, sd, err := a.SwapData(db.MarketMatchID{MatchID: mid, Base: mkt.Base, Quote: mkt.Quote})
		if err != nil {
			return nil, err
		}
		return &db.SwapDataFull{Base: mkt.Base, Quote: mkt.Quote, MatchData: md, SwapData: sd}, nil
	}
	return nil, db.ArchiveError{Code: db.ErrUnknownMatch}
}

// SwapData retrieves the match status and all the SwapData for a match.
func (a *Archiver) SwapData(mid db.MarketMatchID) (order.MatchStatus, *db.SwapData, error) {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return 0, nil, err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(internal.RetrieveSwapData, matchesTableName)

	var sd db.SwapData
	var status uint8
	var contractATime, contractBTime, redeemATime, redeemBTime sql.NullInt64
	err = a.db.QueryRow(stmt, mid).
		Scan(&status,
			&sd.SigMatchAckMaker, &sd.SigMatchAckTaker,
			&sd.MakerSwapAddr, &sd.TakerSwapAddr,
			&sd.ContractACoinID, &sd.ContractA, &contractATime,
			&sd.ContractAAckSig,
			&sd.ContractBCoinID, &sd.ContractB, &contractBTime,
			&sd.ContractBAckSig,
			&sd.RedeemACoinID, &sd.RedeemASecret, &redeemATime,
			&sd.RedeemAAckSig,
			&sd.RedeemBCoinID, &redeemBTime)
	if err != nil {
		return 0, nil, err
	}

	sd.ContractATime = contractATime.Int64
	sd.ContractBTime = contractBTime.Int64
	sd.RedeemATime = redeemATime.Int64
	sd.RedeemBTime = redeemBTime.Int64

	return order.MatchStatus(status), &sd, nil
}

// updateMatchStmtWithExecutor executes stmt on the market's matches table.
// It returns an error unless exactly one row is updated.
func (a *Archiver) updateMatchStmtWithExecutor(dbe sqlExecutor, mid db.MarketMatchID, stmt string, args ...any) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt = fmt.Sprintf(stmt, matchesTableName)
	rowsAffected, err := sqlExec(dbe, stmt, args...)
	if err != nil {
		a.fatalBackendErr(err)
		return err
	}
	if rowsAffected != 1 {
		return fmt.Errorf("updateMatchStmt: updated %d match rows for match %v, expected 1", rowsAffected, mid)
	}
	return nil
}

// ApplyMatchAcksRecordedEvent records match acknowledgement signatures and swap
// addresses in one transaction, preserving any previously recorded addresses.
func (a *Archiver) ApplyMatchAcksRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.MatchAcksRecordedEvent) (*db.EventLogEntry, error) {
	if event == nil {
		return nil, fmt.Errorf("nil match acks recorded event")
	}
	if len(event.Records) == 0 {
		return nil, fmt.Errorf("match_acks_recorded event has no acks")
	}
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}

	return a.applyEventTx(ctx, meta, meshevents.EventKindMatchAcksRecorded, txData, func(tx *sql.Tx) error {
		for _, ack := range event.Records {
			if err := a.saveMatchAck(tx, ack); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *Archiver) saveMatchAck(dbe sqlExecutor, ack meshevents.MatchAckRecord) error {
	mid := db.MarketMatchID{MatchID: ack.MatchID, Base: ack.Base, Quote: ack.Quote}
	sigStmt := internal.SetTakerMatchAckSig
	addrStmt := internal.SetTakerSwapAddr
	if ack.Maker {
		sigStmt = internal.SetMakerMatchAckSig
		addrStmt = internal.SetMakerSwapAddr
	}
	if err := a.updateMatchStmtWithExecutor(dbe, mid, sigStmt, ack.MatchID, ack.Sig); err != nil {
		return fmt.Errorf("saving match ack signature (match id=%v, maker=%v): %w",
			ack.MatchID, ack.Maker, err)
	}
	if ack.Cancel {
		return nil
	}
	if err := a.updateMatchStmtWithExecutor(dbe, mid, addrStmt, ack.MatchID, ack.Address); err != nil {
		return fmt.Errorf("saving match ack address (match id=%v, maker=%v): %w",
			ack.MatchID, ack.Maker, err)
	}
	return nil
}

// ApplySwapContractRecordedEvent records a swap contract and advances
// the match status in one transaction.
func (a *Archiver) ApplySwapContractRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.SwapContractRecordedEvent) (*db.EventLogEntry, error) {
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	return a.applyEventTx(ctx, meta, meshevents.EventKindSwapContractRecorded, txData, func(tx *sql.Tx) error {
		return a.recordSwapContract(tx, event)
	})
}

// recordSwapContract stores the contract data and advances the match status.
func (a *Archiver) recordSwapContract(dbe sqlExecutor, event *meshevents.SwapContractRecordedEvent) error {
	stmt := internal.SetParticipantSwapData
	status := order.TakerSwapCast
	if event.Maker {
		stmt = internal.SetInitiatorSwapData
		status = order.MakerSwapCast
	}
	mid := db.MarketMatchID{MatchID: event.MatchID, Base: event.Base, Quote: event.Quote}
	return a.updateMatchStmtWithExecutor(dbe, mid, stmt, event.MatchID,
		uint8(status), event.CoinID, event.Contract, event.SwapTime)
}

// ApplyAuditAckRecordedEvent records a client's acknowledgement of its
// counterparty's swap contract.
func (a *Archiver) ApplyAuditAckRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.AuditAckRecordedEvent) (*db.EventLogEntry, error) {
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	return a.applyEventTx(ctx, meta, meshevents.EventKindAuditAckRecorded, txData, func(tx *sql.Tx) error {
		stmt := internal.SetParticipantContractAuditSig
		if event.Maker {
			stmt = internal.SetInitiatorContractAuditSig
		}
		mid := db.MarketMatchID{MatchID: event.MatchID, Base: event.Base, Quote: event.Quote}
		return a.updateMatchStmtWithExecutor(tx, mid, stmt, event.MatchID, event.Sig)
	})
}

// ApplySwapRedemptionRecordedEvent records a redemption, advances the match
// status, and records the redeeming user's successful match outcome. It also
// records order completion when the order is executed and has no unsettled
// matches remaining.
func (a *Archiver) ApplySwapRedemptionRecordedEvent(ctx context.Context, meta *db.EventLogMeta, policy *db.ReputationOutcomePolicy, event *meshevents.SwapRedemptionRecordedEvent) (*db.EventLogEntry, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	return a.applyRepEventTx(ctx, meta, event.Kind(), txData, policy, func(tx *sql.Tx, outcomes *reputationOutcomeBatch) error {
		marketSchema, err := a.marketSchema(event.Base, event.Quote)
		if err != nil {
			return err
		}
		matchesTable := fullMatchesTableName(a.dbName, marketSchema)
		match, err := matchByID(tx, matchesTable, event.MatchID)
		if errors.Is(err, sql.ErrNoRows) {
			err = db.ArchiveError{Code: db.ErrUnknownMatch}
		}
		if err != nil {
			return err
		}
		requiredStatus := order.MakerRedeemed
		if event.Maker {
			requiredStatus = order.TakerSwapCast
		}
		if match.Status != requiredStatus {
			return fmt.Errorf("swap redemption recorded event requires status %v, found %v for match %v",
				requiredStatus, match.Status, event.MatchID)
		}
		if err := a.recordRedeemData(tx, event); err != nil {
			return err
		}
		actor, counterparty, actorOrder := match.TakerAcct, match.MakerAcct, match.Taker
		if event.Maker {
			actor, counterparty, actorOrder = match.MakerAcct, match.TakerAcct, match.Maker
		}
		if actor != counterparty {
			outcomes.matches = append(outcomes.matches, &reputationMatchOutcome{
				user:    actor,
				mid:     event.MatchID,
				outcome: db.OutcomeSwapSuccess,
			})
		}
		mid := db.MarketMatchID{MatchID: event.MatchID, Base: event.Base, Quote: event.Quote}
		return a.completeOrderIfSettled(tx, matchesTable, outcomes, mid, actorOrder, actor, event.RedeemTime)
	})
}

// recordRedeemData stores the redemption and advances the match status.
func (a *Archiver) recordRedeemData(dbe sqlExecutor, event *meshevents.SwapRedemptionRecordedEvent) error {
	mid := db.MarketMatchID{MatchID: event.MatchID, Base: event.Base, Quote: event.Quote}
	if event.Maker {
		return a.updateMatchStmtWithExecutor(dbe, mid, internal.SetInitiatorRedeemData,
			event.MatchID, uint8(order.MakerRedeemed), event.CoinID, event.Secret, event.RedeemTime)
	}
	return a.updateMatchStmtWithExecutor(dbe, mid, internal.SetParticipantRedeemData,
		event.MatchID, uint8(order.MatchComplete), event.CoinID, event.RedeemTime)
}

// orderHasUnsettledMatch reports whether an order still has swaps to finish.
// A maker is finished once it redeems; a taker's redemption makes the match
// inactive. Failed matches are inactive as well.
func orderHasUnsettledMatch(dbe sqlQueryer, matchesTable string, oid order.OrderID) (bool, error) {
	stmt := fmt.Sprintf(internal.UnsettledOrderMatchExists, matchesTable, matchesTable)
	var exists bool
	err := dbe.QueryRow(stmt, oid, uint8(order.MakerRedeemed)).Scan(&exists)
	return exists, err
}

// completeOrderIfSettled records the completion time and adds an order reputation
// outcome if the order is executed and none of its matches remain unsettled.
func (a *Archiver) completeOrderIfSettled(dbe sqlQueryExecutor, matchesTable string, outcomes *reputationOutcomeBatch,
	mid db.MarketMatchID, oid order.OrderID, user account.AccountID, completeTimeMS int64) error {
	status, _, _, err := a.orderStatusByID(dbe, oid, mid.Base, mid.Quote)
	if err != nil {
		return err
	}
	if status != orderStatusExecuted {
		return nil
	}
	unsettled, err := orderHasUnsettledMatch(dbe, matchesTable, oid)
	if err != nil {
		return err
	}
	if unsettled {
		return nil
	}
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}
	// Only executed trading orders can complete their swaps.
	table := fullOrderTableName(a.dbName, marketSchema, false)
	stmt := fmt.Sprintf(internal.SetOrderCompleteTime, table)
	rows, err := sqlExec(dbe, stmt, completeTimeMS, oid)
	if err != nil {
		a.fatalBackendErr(err)
		return fmt.Errorf("setting completion time for order %v: %w", oid, err)
	}
	if rows != 1 {
		return db.ArchiveError{
			Code:   db.ErrUnknownOrder,
			Detail: fmt.Sprintf("update count = %d for order %v, expected 1", rows, oid),
		}
	}
	outcomes.orders = append(outcomes.orders, &reputationOrderOutcome{user: user, oid: oid})
	return nil
}

// ApplyRedemptionAckRecordedEvent stores the taker's acknowledgement of the
// maker's redemption. Maker acknowledgements only add an event-log entry.
func (a *Archiver) ApplyRedemptionAckRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.RedemptionAckRecordedEvent) (*db.EventLogEntry, error) {
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	return a.applyEventTx(ctx, meta, event.Kind(), txData, func(tx *sql.Tx) error {
		if event.Maker {
			return nil
		}
		mid := db.MarketMatchID{MatchID: event.MatchID, Base: event.Base, Quote: event.Quote}
		return a.updateMatchStmtWithExecutor(tx, mid, internal.SetParticipantRedeemAckSig, event.MatchID, event.Sig)
	})
}

// ApplyMatchFailedEvent marks a match inactive, records any failure penalty,
// revokes the faulted party's booked order, and completes eligible orders
// belonging to a party that was not at fault.
func (a *Archiver) ApplyMatchFailedEvent(ctx context.Context, meta *db.EventLogMeta, policy *db.ReputationOutcomePolicy, event *meshevents.MatchFailedEvent) (*db.EventLogEntry, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}

	return a.applyRepEventTx(ctx, meta, event.Kind(), txData, policy, func(tx *sql.Tx, outcomes *reputationOutcomeBatch) error {
		marketSchema, err := a.marketSchema(event.Base, event.Quote)
		if err != nil {
			return err
		}
		matchesTable := fullMatchesTableName(a.dbName, marketSchema)
		match, err := matchByID(tx, matchesTable, event.MatchID)
		if errors.Is(err, sql.ErrNoRows) {
			err = db.ArchiveError{Code: db.ErrUnknownMatch}
		}
		if err != nil {
			return err
		}
		if match.Status != event.Status {
			return fmt.Errorf("match_failed requires status %v, found %v for match %v",
				event.Status, match.Status, event.MatchID)
		}

		mid := db.MarketMatchID{MatchID: event.MatchID, Base: event.Base, Quote: event.Quote}
		stmt := internal.SetSwapDone
		if event.Fault == meshevents.MatchFailureNoUserFault {
			stmt = internal.SetSwapDoneForgiven
		}
		if err := a.updateMatchStmtWithExecutor(tx, mid, stmt, event.MatchID); err != nil {
			return err
		}

		makerFault := event.Fault == meshevents.MatchFailureMakerFault
		takerFault := event.Fault == meshevents.MatchFailureTakerFault
		// Self-matches do not receive match-failure penalties.
		if event.Fault != meshevents.MatchFailureNoUserFault && match.MakerAcct != match.TakerAcct {
			faultedUser := match.TakerAcct
			if makerFault {
				faultedUser = match.MakerAcct
			}
			// The status and responsible party determine the missed action.
			var outcome db.Outcome
			switch event.Status {
			case order.NewlyMatched:
				outcome = db.OutcomeNoSwapAsMaker
				if takerFault {
					outcome = db.OutcomeNoAddrAsTaker
				}
			case order.MakerSwapCast:
				outcome = db.OutcomeNoSwapAsTaker
			case order.TakerSwapCast:
				outcome = db.OutcomeNoRedeemAsMaker
			case order.MakerRedeemed:
				outcome = db.OutcomeNoRedeemAsTaker
			}
			outcomes.matches = append(outcomes.matches, &reputationMatchOutcome{
				user:    faultedUser,
				mid:     event.MatchID,
				outcome: outcome,
			})
		}

		// The maker's redemption already handled its order completion.
		if event.Status != order.MakerRedeemed {
			if err := a.updateOrderAfterMatchFailure(tx, matchesTable, outcomes, mid,
				match.Maker, match.MakerAcct, makerFault, event.FailTime); err != nil {
				return err
			}
		}

		return a.updateOrderAfterMatchFailure(tx, matchesTable, outcomes, mid,
			match.Taker, match.TakerAcct, takerFault, event.FailTime)
	})
}

// updateOrderAfterMatchFailure checks for order completion when the owner was
// not at fault. For an owner at fault, it revokes the order if still booked and
// records the generated cancel as a non-penalized order reputation outcome.
func (a *Archiver) updateOrderAfterMatchFailure(dbe sqlQueryExecutor, matchesTable string,
	outcomes *reputationOutcomeBatch, mid db.MarketMatchID, oid order.OrderID,
	user account.AccountID, ownerAtFault bool, failTimeMS int64) error {
	if !ownerAtFault {
		return a.completeOrderIfSettled(dbe, matchesTable, outcomes, mid, oid, user, failTimeMS)
	}
	status, _, _, err := a.orderStatusByID(dbe, oid, mid.Base, mid.Quote)
	if err != nil {
		return err
	}
	if status != orderStatusBooked {
		return nil
	}
	cancelID, err := a.revokeBookedOrderByID(dbe, oid, user, mid.Base, mid.Quote, false, time.UnixMilli(failTimeMS).UTC())
	if err != nil {
		return err
	}
	// Record the revocation under the generated cancel's ID, not the original order's.
	outcomes.orders = append(outcomes.orders, &reputationOrderOutcome{user: user, oid: cancelID})
	return nil
}
