// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/lib/pq"
)

func (a *Archiver) matchTableName(match *order.Match) (string, error) {
	marketSchema, err := a.marketSchema(match.Maker.Base(), match.Maker.Quote())
	if err != nil {
		return "", err
	}
	return fullMatchesTableName(a.dbName, marketSchema), nil
}

// ForgiveMatchFail marks the specified match as forgiven. Since this is an
// administrative function, the burden is on the operator to ensure the match
// can actually be forgiven (inactive, not already forgiven, and not in
// MatchComplete status).
func (a *Archiver) ForgiveMatchFail(mid order.MatchID) (bool, error) {
	for m := range a.markets {
		stmt := fmt.Sprintf(internal.ForgiveMatchFail, fullMatchesTableName(a.dbName, m))
		N, err := sqlExec(a.db, stmt, mid)
		if err != nil { // not just no rows updated
			return false, err
		}
		if N == 1 {
			return true, nil
		} // N > 1 cannot happen since matchid is the primary key
		// N==0 could also mean it was not eligible to forgive, but just keep going
	}
	return false, nil
}

// ActiveSwaps loads the full details for all active swaps across all markets.
func (a *Archiver) ActiveSwaps() ([]*db.SwapDataFull, error) {
	var sd []*db.SwapDataFull

	for m, mkt := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, m)
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

	for m, mkt := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, m)
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		matchOutcomes, err := completedAndAtFaultMatches(ctx, a.db, matchesTableName, aid, lastN, mkt.Base, mkt.Quote)
		cancel()
		if err != nil {
			return nil, err
		}

		outcomes = append(outcomes, matchOutcomes...)
	}

	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[j].Time < outcomes[i].Time // descending
	})
	if len(outcomes) > lastN {
		outcomes = outcomes[:lastN]
	}
	return outcomes, nil
}

func completedAndAtFaultMatches(ctx context.Context, dbe *sql.DB, tableName string,
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

// UserMatches retrieves all matches involving a user on the given market.
// TODO: consider a time limited version of this to retrieve recent matches.
func (a *Archiver) UserMatches(aid account.AccountID, base, quote uint32) ([]*db.MatchData, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	return userMatches(ctx, a.db, matchesTableName, aid, true)
}

func userMatches(ctx context.Context, dbe *sql.DB, tableName string, aid account.AccountID, includeInactive bool) ([]*db.MatchData, error) {
	query := internal.RetrieveActiveUserMatches
	if includeInactive {
		query = internal.RetrieveUserMatches
	}
	stmt := fmt.Sprintf(query, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, aid)
	if err != nil {
		return nil, err
	}
	return rowsToMatchData(rows, includeInactive)
}

// MarketMatches retrieves all active matches for a market. If includeInactive,
// all matches are returned.
func (a *Archiver) MarketMatches(base, quote uint32, includeInactive bool) ([]*db.MatchData, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	query := internal.RetrieveActiveMarketMatches
	if includeInactive {
		query = internal.RetrieveMarketMatches
	}
	stmt := fmt.Sprintf(query, matchesTableName)
	rows, err := a.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, err
	}
	return rowsToMatchData(rows, includeInactive)
}

func rowsToMatchData(rows *sql.Rows, includeInactive bool) ([]*db.MatchData, error) {
	defer rows.Close()

	var (
		ms  []*db.MatchData
		err error
	)
	for rows.Next() {
		var m db.MatchData
		var status uint8
		var baseRate, quoteRate sql.NullInt64
		var takerSell sql.NullBool
		var takerAddr, makerAddr sql.NullString
		if includeInactive {
			// "active" column SELECTed.
			err = rows.Scan(&m.ID, &m.Active, &takerSell,
				&m.Taker, &m.TakerAcct, &takerAddr,
				&m.Maker, &m.MakerAcct, &makerAddr,
				&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
				&baseRate, &quoteRate, &status)
			if err != nil {
				return nil, err
			}
		} else {
			// "active" column not SELECTed.
			err = rows.Scan(&m.ID, &takerSell,
				&m.Taker, &m.TakerAcct, &takerAddr,
				&m.Maker, &m.MakerAcct, &makerAddr,
				&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate,
				&baseRate, &quoteRate, &status)
			if err != nil {
				return nil, err
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

		ms = append(ms, &m)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return ms, nil
}

// AllActiveUserMatches retrieves a MatchData slice for active matches in all
// markets involving the given user. Swaps that have successfully completed or
// failed are not included.
func (a *Archiver) AllActiveUserMatches(aid account.AccountID) ([]*db.MatchData, error) {
	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	var matches []*db.MatchData
	for m := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, m)
		mdM, err := userMatches(ctx, a.db, matchesTableName, aid, false)
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

// InsertMatch updates an existing match.
func (a *Archiver) InsertMatch(match *order.Match) error {
	matchesTableName, err := a.matchTableName(match)
	if err != nil {
		return err
	}
	N, err := upsertMatch(a.db, matchesTableName, match)
	if err != nil {
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("upsertMatch: updated %d rows, expected 1", N)
	}
	return nil
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

func matchByID(dbe *sql.DB, tableName string, mid order.MatchID) (*db.MatchData, error) {
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
		err := rows.Scan(&status.ID, &status.Status, &status.MakerContract,
			&status.TakerContract, &status.MakerSwap, &status.TakerSwap,
			&status.MakerRedeem, &status.TakerRedeem, &status.Secret, &status.Active)
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

// updateMatchStmt executes a SQL statement with the provided arguments,
// choosing the market's matches table from the MarketMatchID. Exactly 1 table
// row must be updated, otherwise an error is returned.
func (a *Archiver) updateMatchStmt(mid db.MarketMatchID, stmt string, args ...interface{}) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt = fmt.Sprintf(stmt, matchesTableName)
	N, err := sqlExec(a.db, stmt, args...)
	if err != nil { // not just no rows updated
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("updateMatchStmt: updated %d match rows for match %v, expected 1", N, mid)
	}
	return nil
}

// Match acknowledgement message signatures.

// SaveMatchAckSigA records the match data acknowledgement signature from swap
// party A (the initiator), which is the maker in the DEX.
func (a *Archiver) SaveMatchAckSigA(mid db.MarketMatchID, sig []byte) error {
	return a.updateMatchStmt(mid, internal.SetMakerMatchAckSig,
		mid.MatchID, sig)
}

// SaveMatchAckSigB records the match data acknowledgement signature from swap
// party B (the participant), which is the taker in the DEX.
func (a *Archiver) SaveMatchAckSigB(mid db.MarketMatchID, sig []byte) error {
	return a.updateMatchStmt(mid, internal.SetTakerMatchAckSig,
		mid.MatchID, sig)
}

// Swap contracts, and counterparty audit acknowledgement signatures.

// SaveContractA records party A's swap contract script and the coinID (e.g.
// transaction output) containing the contract on chain X. Note that this
// contract contains the secret hash.
func (a *Archiver) SaveContractA(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64) error {
	return a.updateMatchStmt(mid, internal.SetInitiatorSwapData,
		mid.MatchID, uint8(order.MakerSwapCast), coinID, contract, timestamp)
}

// SaveAuditAckSigB records party B's signature acknowledging their audit of A's
// swap contract.
func (a *Archiver) SaveAuditAckSigB(mid db.MarketMatchID, sig []byte) error {
	return a.updateMatchStmt(mid, internal.SetParticipantContractAuditSig,
		mid.MatchID, sig)
}

// SaveContractB records party B's swap contract script and the coinID (e.g.
// transaction output) containing the contract on chain Y.
func (a *Archiver) SaveContractB(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64) error {
	return a.updateMatchStmt(mid, internal.SetParticipantSwapData,
		mid.MatchID, uint8(order.TakerSwapCast), coinID, contract, timestamp)
}

// SaveAuditAckSigA records party A's signature acknowledging their audit of B's
// swap contract.
func (a *Archiver) SaveAuditAckSigA(mid db.MarketMatchID, sig []byte) error {
	return a.updateMatchStmt(mid, internal.SetInitiatorContractAuditSig,
		mid.MatchID, sig)
}

// Redemption transactions, and counterparty acknowledgement signatures.

// SaveRedeemA records party A's redemption coinID (e.g. transaction output),
// which spends party B's swap contract on chain Y, and the secret revealed by
// the signature script of the input spending the contract. Note that this
// transaction will contain the secret, which party B extracts.
func (a *Archiver) SaveRedeemA(mid db.MarketMatchID, coinID, secret []byte, timestamp int64) error {
	return a.updateMatchStmt(mid, internal.SetInitiatorRedeemData,
		mid.MatchID, uint8(order.MakerRedeemed), coinID, secret, timestamp)
}

// SaveRedeemAckSigB records party B's signature acknowledging party A's
// redemption, which spent their swap contract on chain Y and revealed the
// secret. Since this may be the final step in match negotiation, the match is
// also flagged as inactive (not the same as archival or even status of
// MatchComplete, which is set by SaveRedeemB) if the initiators's redeem ack
// signature is already set.
func (a *Archiver) SaveRedeemAckSigB(mid db.MarketMatchID, sig []byte) error {
	return a.updateMatchStmt(mid, internal.SetParticipantRedeemAckSig,
		mid.MatchID, sig)
}

// SaveRedeemB records party B's redemption coinID (e.g. transaction output),
// which spends party A's swap contract on chain X.
func (a *Archiver) SaveRedeemB(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
	return a.updateMatchStmt(mid, internal.SetParticipantRedeemData,
		mid.MatchID, uint8(order.MatchComplete), coinID, timestamp)
}

// SetMatchInactive flags the match as done/inactive. This is not necessary if
// SaveRedeemAckSigB is run for the match since it will flag the match as done.
func (a *Archiver) SetMatchInactive(mid db.MarketMatchID) error {
	return a.updateMatchStmt(mid, internal.SetSwapDone, mid.MatchID)
}
