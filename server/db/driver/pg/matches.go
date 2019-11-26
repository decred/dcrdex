package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

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
		var epochID string
		err := rows.Scan(&m.ID, &m.Taker, &m.TakerAcct, &m.TakerAddr,
			&m.Maker, &m.MakerAcct, &m.MakerAddr,
			&epochID, &m.Quantity, &m.Rate, &status)
		if err != nil {
			return nil, err
		}
		m.Epoch.Idx, m.Epoch.Dur, err = splitEpochID(epochID)
		if err != nil {
			return nil, err
		}
		m.Status = order.MatchStatus(status)
		ms = append(ms, &m)
	}
	return ms, nil
}

func splitEpochID(epochID string) (epochIdx uint64, epochDur uint64, err error) {
	strs := strings.Split(epochID, ":")
	if len(strs) != 2 {
		return 0, 0, fmt.Errorf("invalid epochID: %s", epochID)
	}
	epochIdx, err = strconv.ParseUint(strs[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid epochIdx: %s", strs[0])
	}
	epochDur, err = strconv.ParseUint(strs[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid epochDur: %s", strs[1])
	}
	return
}

func upsertMatch(dbe sqlExecutor, tableName string, match *order.Match) (int64, error) {
	stmt := fmt.Sprintf(internal.UpsertMatch, tableName)
	epochID := fmt.Sprintf("%d:%d", match.Epoch.Idx, match.Epoch.Dur)
	return sqlExec(dbe, stmt, match.ID(),
		match.Taker.ID(), match.Taker.User(), match.Taker.SwapAddress(),
		match.Maker.ID(), match.Maker.User(), match.Maker.SwapAddress(),
		epochID, int64(match.Quantity), int64(match.Rate), int8(match.Status),
		match.Sigs.TakerMatch, match.Sigs.MakerMatch,
		match.Sigs.TakerAudit, match.Sigs.MakerAudit,
		match.Sigs.TakerRedeem, match.Sigs.MakerRedeem)
}

// UpdateMatch updates an existing match.
func (a *Archiver) UpdateMatch(match *order.Match) error {
	matchesTableName, err := a.matchTableName(match)
	if err != nil {
		return err
	}
	N, err := upsertMatch(a.db, matchesTableName, match)
	if err != nil {
		return fmt.Errorf("updateMatch: %v", err)
	}
	if N != 1 {
		return fmt.Errorf("updateMatch: updated %d rows, expected 1", N)
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
	if err == sql.ErrNoRows {
		err = db.ArchiveError{Code: db.ErrUnknownMatch}
	}
	return matchData, err
}

func matchByID(dbe *sql.DB, tableName string, mid order.MatchID) (*db.MatchData, error) {
	var m db.MatchData
	var status uint8
	var epochID string
	stmt := fmt.Sprintf(internal.RetrieveMatchByID, tableName)
	err := dbe.QueryRow(stmt, mid).
		Scan(&m.ID, &m.Taker, &m.TakerAcct, &m.TakerAddr,
			&m.Maker, &m.MakerAcct, &m.MakerAddr,
			&epochID, &m.Quantity, &m.Rate, &status,
			&m.Sigs.TakerMatch, &m.Sigs.MakerMatch,
			&m.Sigs.TakerAudit, &m.Sigs.MakerAudit,
			&m.Sigs.TakerRedeem, &m.Sigs.MakerRedeem)
	if err != nil {
		return nil, err
	}
	m.Epoch.Idx, m.Epoch.Dur, err = splitEpochID(epochID)
	if err != nil {
		return nil, err
	}
	m.Status = order.MatchStatus(status)
	return &m, nil
}
