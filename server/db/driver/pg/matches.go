package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

func (a *Archiver) matchTableName(match *order.Match) (string, error) {
	marketSchema, err := a.marketSchema(match.Maker.Base(), match.Maker.Quote())
	if err != nil {
		return "", err
	}
	return fullMatchesTableName(a.dbName, marketSchema), nil
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

	return userMatches(ctx, a.db, matchesTableName, aid)
}

func userMatches(ctx context.Context, dbe *sql.DB, tableName string, aid account.AccountID) ([]*db.MatchData, error) {
	stmt := fmt.Sprintf(internal.RetrieveUserMatches, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, aid)
	if err != nil {
		return nil, err
	}

	var ms []*db.MatchData
	for rows.Next() {
		var m db.MatchData
		var status uint8
		err := rows.Scan(&m.ID, &m.Active,
			&m.Taker, &m.TakerAcct, &m.TakerAddr,
			&m.Maker, &m.MakerAcct, &m.MakerAddr,
			&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate, &status)
		if err != nil {
			return nil, err
		}
		m.Status = order.MatchStatus(status)
		ms = append(ms, &m)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return ms, nil
}

// ActiveMatches retrieves a UserMatch slice for active matches involving the
// given user. Swaps that have successfully completed or failed are not
// included.
func (a *Archiver) ActiveMatches(aid account.AccountID) ([]*order.UserMatch, error) {
	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	var matches []*order.UserMatch
	for m := range a.markets {
		matchesTableName := fullMatchesTableName(a.dbName, m)
		matchesM, err := activeUserMatches(ctx, a.db, matchesTableName, aid)
		if err != nil {
			return nil, err
		}
		matches = append(matches, matchesM...)
	}

	return matches, nil
}

func activeUserMatches(ctx context.Context, dbe *sql.DB, tableName string, aid account.AccountID) ([]*order.UserMatch, error) {
	stmt := fmt.Sprintf(internal.RetrieveActiveUserMatches, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, aid)
	if err != nil {
		return nil, err
	}

	var ms []*order.UserMatch
	for rows.Next() {
		var m db.MatchData
		var status uint8
		err := rows.Scan(&m.ID, &m.Taker, &m.TakerAcct, &m.TakerAddr,
			&m.Maker, &m.MakerAcct, &m.MakerAddr,
			&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate, &status)
		if err != nil {
			return nil, err
		}
		m.Status = order.MatchStatus(status)

		var addr string
		var oid order.OrderID
		var side order.MatchSide
		switch aid {
		case m.TakerAcct:
			addr = m.TakerAddr
			oid = m.Taker
			side = order.Taker
		case m.MakerAcct:
			addr = m.MakerAddr
			oid = m.Maker
			side = order.Maker
		default:
			return nil, fmt.Errorf("loaded match %v not belonging to user %v", m.ID, aid)
		}

		um := &order.UserMatch{
			OrderID:  oid,
			MatchID:  m.ID,
			Quantity: m.Quantity,
			Rate:     m.Rate,
			Address:  addr,
			Side:     side,
			Status:   m.Status,
		}

		ms = append(ms, um)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return ms, nil
}

func upsertMatch(dbe sqlExecutor, tableName string, match *order.Match) (int64, error) {
	var takerAddr string
	tt := match.Taker.Trade()
	if tt != nil {
		takerAddr = tt.SwapAddress()
	}
	stmt := fmt.Sprintf(internal.UpsertMatch, tableName)
	return sqlExec(dbe, stmt, match.ID(),
		match.Taker.ID(), match.Taker.User(), takerAddr,
		match.Maker.ID(), match.Maker.User(), match.Maker.Trade().SwapAddress(),
		match.Epoch.Idx, match.Epoch.Dur,
		int64(match.Quantity), int64(match.Rate), int8(match.Status))
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
	stmt := fmt.Sprintf(internal.RetrieveMatchByID, tableName)
	err := dbe.QueryRow(stmt, mid).
		Scan(&m.ID, &m.Active,
			&m.Taker, &m.TakerAcct, &m.TakerAddr,
			&m.Maker, &m.MakerAcct, &m.MakerAddr,
			&m.Epoch.Idx, &m.Epoch.Dur, &m.Quantity, &m.Rate, &status)
	if err != nil {
		return nil, err
	}
	m.Status = order.MatchStatus(status)
	return &m, nil
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
			&sd.ContractACoinID, &sd.ContractA, &contractATime,
			&sd.ContractAAckSig,
			&sd.ContractBCoinID, &sd.ContractB, &contractBTime,
			&sd.ContractBAckSig,
			&sd.RedeemACoinID, &redeemATime,
			&sd.RedeemAAckSig,
			&sd.RedeemBCoinID, &redeemBTime,
			&sd.RedeemBAckSig)
	if err != nil {
		return 0, nil, err
	}

	sd.ContractATime = contractATime.Int64
	sd.ContractBTime = contractBTime.Int64
	sd.RedeemATime = redeemATime.Int64
	sd.RedeemBTime = redeemBTime.Int64

	return order.MatchStatus(status), &sd, nil
}

// Swap Data
//
// In the swap process, the counterparties are:
// - Initiator or party A on chain X. This is the maker in the DEX.
// - Participant or party B on chain Y. This is the taker in the DEX.
//
// For each match, a successful swap will generate the following data that must
// be stored:
// - 6 client signatures. Both parties sign the data to acknowledge (1) the
//   match ack, (2) the counterparty's contract script and contract transaction,
//   and (3) the counterparty's redemption transaction.
// - 2 swap contracts and the associated transaction outputs (more generally,
//   coinIDs), one on each party's blockchain.
// - 2 redemption transaction outputs (coinIDs).
//
// The methods for saving this data are defined below in the order in which the
// data is expected from the parties.

// Match acknowledgement message signatures.

func (a *Archiver) saveAckSig(mid db.MarketMatchID, sig []byte, sigStmt string) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(sigStmt, matchesTableName)
	N, err := sqlExec(a.db, stmt, mid.MatchID, sig)
	if err != nil { // not just no rows updated
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("saveMatchSig: updated %d match rows for match %v, expected 1", N, mid)
	}
	return nil
}

// SaveMatchAckSigA records the match data acknowledgement signature from swap
// party A (the initiator), which is the maker in the DEX.
func (a *Archiver) SaveMatchAckSigA(mid db.MarketMatchID, sig []byte) error {
	return a.saveAckSig(mid, sig, internal.SetMakerMatchAckSig)
}

// SaveMatchAckSigB records the match data acknowledgement signature from swap
// party B (the participant), which is the taker in the DEX.
func (a *Archiver) SaveMatchAckSigB(mid db.MarketMatchID, sig []byte) error {
	return a.saveAckSig(mid, sig, internal.SetTakerMatchAckSig)
}

// Swap contracts, and counterparty audit acknowledgement signatures.

func (a *Archiver) saveContract(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64, status uint8, contractStmt string) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(contractStmt, matchesTableName)
	N, err := sqlExec(a.db, stmt, mid.MatchID, status, coinID, contract, timestamp)
	if err != nil { // not just no rows updated
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("saveContract: updated %d match rows for match %v, expected 1", N, mid)
	}
	return nil
}

// SaveContractA records party A's swap contract script and the coinID (e.g.
// transaction output) containing the contract on chain X. Note that this
// contract contains the secret hash.
func (a *Archiver) SaveContractA(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64) error {
	return a.saveContract(mid, contract, coinID, timestamp, uint8(order.MakerSwapCast), internal.SetInitiatorSwapData)
}

// SaveAuditAckSigB records party B's signature acknowledging their audit of A's
// swap contract.
func (a *Archiver) SaveAuditAckSigB(mid db.MarketMatchID, sig []byte) error {
	return a.saveAckSig(mid, sig, internal.SetParticipantContractAuditSig)
}

// SaveContractB records party B's swap contract script and the coinID (e.g.
// transaction output) containing the contract on chain Y.
func (a *Archiver) SaveContractB(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64) error {
	return a.saveContract(mid, contract, coinID, timestamp, uint8(order.TakerSwapCast), internal.SetParticipantSwapData)
}

// SaveAuditAckSigA records party A's signature acknowledging their audit of B's
// swap contract.
func (a *Archiver) SaveAuditAckSigA(mid db.MarketMatchID, sig []byte) error {
	return a.saveAckSig(mid, sig, internal.SetInitiatorContractAuditSig)
}

// Redemption transactions, and counterparty acknowledgement signatures.

func (a *Archiver) saveRedeem(mid db.MarketMatchID, coinID []byte, timestamp int64, status uint8, redeemStmt string) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(redeemStmt, matchesTableName)
	N, err := sqlExec(a.db, stmt, mid.MatchID, status, coinID, timestamp)
	if err != nil { // not just no rows updated
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("saveRedeem: updated %d match rows for match %v, expected 1", N, mid)
	}
	return nil
}

func (a *Archiver) saveRedeemAckSig(mid db.MarketMatchID, sig []byte, sigStmt string) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(sigStmt, matchesTableName)
	_, err = a.db.Exec(stmt, mid.MatchID, sig)
	return err
}

// SaveRedeemA records party A's redemption coinID (e.g. transaction output),
// which spends party B's swap contract on chain Y. Note that this transaction
// will contain the secret, which party B extracts.
func (a *Archiver) SaveRedeemA(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
	return a.saveRedeem(mid, coinID, timestamp, uint8(order.MakerRedeemed), internal.SetInitiatorRedeemData)
}

// SaveRedeemAckSigB records party B's signature acknowledging party A's
// redemption, which spent their swap contract on chain Y and revealed the
// secret. Since this may be the final step in match negotiation, the match is
// also flagged as inactive (not the same as archival or even status of
// MatchComplete, which is set by SaveRedeemB) if the initiators's redeem ack
// signature is already set.
func (a *Archiver) SaveRedeemAckSigB(mid db.MarketMatchID, sig []byte) error {
	return a.saveRedeemAckSig(mid, sig, internal.SetParticipantRedeemAckSig)
}

// SaveRedeemB records party B's redemption coinID (e.g. transaction output),
// which spends party A's swap contract on chain X.
func (a *Archiver) SaveRedeemB(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
	return a.saveRedeem(mid, coinID, timestamp, uint8(order.MatchComplete), internal.SetParticipantRedeemData)
}

// SaveRedeemAckSigA records party A's signature acknowledging party B's
// redemption. Since this may be the final step in match negotiation, the match
// is also flagged as inactive (not the same as archival or even status of
// MatchComplete, which is set by SaveRedeemB) if the participant's redeem ack
// signature is already set.
func (a *Archiver) SaveRedeemAckSigA(mid db.MarketMatchID, sig []byte) error {
	return a.saveRedeemAckSig(mid, sig, internal.SetInitiatorRedeemAckSig)
}

// SetMatchInactive flags the match as done/inactive. This is not necessary if
// both SaveRedeemAckSigA and SaveRedeemAckSigB are run for the match since they
// will also flags the match as done when both signatures are stored.
func (a *Archiver) SetMatchInactive(mid db.MarketMatchID) error {
	marketSchema, err := a.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		return err
	}

	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	N, err := sqlExec(a.db, fmt.Sprintf(internal.SetSwapDone, matchesTableName), mid.MatchID)
	if err != nil { // not just no rows updated
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("SetMatchInactive: updated %d match rows for match %v, expected 1", N, mid)
	}
	return nil
}
