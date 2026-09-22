// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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
	"decred.org/dcrdex/server/meshevents"
)

var _ db.ReputationArchiver = (*Archiver)(nil)

func (a *Archiver) GetUserReputationData(
	ctx context.Context,
	user account.AccountID,
	pimgSz, matchSz, orderSz int,
) ([]*db.PreimageOutcome, []*db.MatchResult, []*db.OrderOutcome, error) {
	return getUserReputationData(ctx, a.queries.selectPoints, user, pimgSz, matchSz, orderSz)
}

// getUserReputationData returns the latest outcomes for each class, ordered by ID.
func getUserReputationData(ctx context.Context, stmt *sql.Stmt, user account.AccountID, pimgSz, matchSz, orderSz int) ([]*db.PreimageOutcome, []*db.MatchResult, []*db.OrderOutcome, error) {
	rows, err := stmt.QueryContext(ctx, user)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error querying reputation points: %w", err)
	}
	defer rows.Close()

	pimgs := make([]*db.PreimageOutcome, 0, pimgSz)
	matches := make([]*db.MatchResult, 0, matchSz)
	orders := make([]*db.OrderOutcome, 0, orderSz)

	for rows.Next() {
		var dbID int64
		var link order.OrderID
		var outcomeClass db.OutcomeClass
		var outcome db.Outcome
		if err := rows.Scan(&dbID, &link, &outcomeClass, &outcome); err != nil {
			return nil, nil, nil, fmt.Errorf("error scanning points row: %w", err)
		}
		switch outcomeClass {
		case db.OutcomeClassPreimage:
			pimgs = append(pimgs, &db.PreimageOutcome{
				DBID:    dbID,
				OrderID: link,
				Miss:    outcome == db.OutcomePreimageMiss,
			})
		case db.OutcomeClassMatch:
			var mid order.MatchID
			copy(mid[:], link[:])
			matches = append(matches, &db.MatchResult{
				DBID:         dbID,
				MatchID:      mid,
				MatchOutcome: outcome,
			})
		case db.OutcomeClassOrder:
			orders = append(orders, &db.OrderOutcome{
				DBID:     dbID,
				OrderID:  link,
				Canceled: outcome == db.OutcomeOrderCanceled,
			})
		}
	}
	if rows.Err() != nil {
		return nil, nil, nil, fmt.Errorf("error iterating points rows: %w", rows.Err())
	}

	if len(pimgs) > pimgSz {
		pimgs = pimgs[len(pimgs)-pimgSz:]
	}
	if len(matches) > matchSz {
		matches = matches[len(matches)-matchSz:]
	}
	if len(orders) > orderSz {
		orders = orders[len(orders)-orderSz:]
	}
	return pimgs, matches, orders, nil
}

// applyUserForgivenessTx deletes the account's non-success outcomes.
// It reports whether any rows were deleted.
func (a *Archiver) applyUserForgivenessTx(ctx context.Context, tx *sql.Tx, accountID account.AccountID) (forgiven bool, err error) {
	stmt := fmt.Sprintf(internal.ForgiveUser, a.tables.points)
	res, err := tx.ExecContext(ctx, stmt, accountID, db.OutcomeSwapSuccess, db.OutcomePreimageSuccess, db.OutcomeOrderComplete)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// applyMatchForgivenessTx marks an inactive match as forgiven and deletes
// the account's failure outcomes for that match. It reports whether a match
// row was updated, including a match that was already forgiven.
func (a *Archiver) applyMatchForgivenessTx(ctx context.Context, tx *sql.Tx, accountID account.AccountID, matchID order.MatchID) (forgiven bool, err error) {
	for schema := range a.markets {
		stmt := fmt.Sprintf(internal.ForgiveMatchFail, fullMatchesTableName(a.dbName, schema))
		res, err := tx.ExecContext(ctx, stmt, matchID)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if n > 0 { // at most one market has the match, matchid is the primary key
			forgiven = true
			break
		}
	}
	if !forgiven {
		return false, nil
	}

	stmt := fmt.Sprintf(internal.ForgiveMatchFailures, a.tables.points)
	var link order.OrderID // need a sql driver Valuer
	copy(link[:], matchID[:])
	if _, err := tx.ExecContext(ctx, stmt, accountID, link, db.OutcomeClassMatch, db.OutcomeSwapSuccess); err != nil {
		return false, err
	}
	return true, nil
}

// ApplyReputationForgivenEvent removes penalty outcomes for an account or a
// specific match, marking the match forgiven when applicable. It commits these
// changes together with the event log entry.
func (a *Archiver) ApplyReputationForgivenEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.ReputationForgivenEvent) (*db.ReputationForgivenResult, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	accountID := event.AccountID
	matchID := event.Match()

	var forgiven bool
	logEntry, err := a.applyEventTx(ctx, meta, meshevents.EventKindReputationForgiven, txData, func(tx *sql.Tx) error {
		var err error
		switch event.Scope {
		case meshevents.ReputationForgivenessScopeUser:
			forgiven, err = a.applyUserForgivenessTx(ctx, tx, accountID)
		case meshevents.ReputationForgivenessScopeMatch:
			forgiven, err = a.applyMatchForgivenessTx(ctx, tx, accountID, matchID)
		default:
			err = fmt.Errorf("invalid reputation forgiveness scope %d", event.Scope)
		}
		return err
	})
	a.notifyRepInputsOnCommit(err, accountID)
	if err != nil {
		return nil, err
	}

	return &db.ReputationForgivenResult{
		Forgiven: forgiven,
		Log:      logEntry,
	}, nil
}

// SetReputationInputsListener registers a callback for changes to the data used
// to calculate reputation. It panics if a listener is already registered.
func (a *Archiver) SetReputationInputsListener(listener func(users ...account.AccountID)) {
	a.repListenerMtx.Lock()
	defer a.repListenerMtx.Unlock()
	if a.repListener != nil {
		panic("reputation inputs listener already registered")
	}
	a.repListener = listener
}

func commitMayHaveLanded(err error) bool {
	return err == nil || errors.As(err, new(*db.EventCommitUnknownError))
}

// notifyRepInputsOnCommit notifies the listener after a successful commit or
// when the commit outcome is unknown, so cached reputation can be invalidated.
func (a *Archiver) notifyRepInputsOnCommit(err error, users ...account.AccountID) {
	if len(users) == 0 || !commitMayHaveLanded(err) {
		return
	}
	a.repListenerMtx.RLock()
	listener := a.repListener
	a.repListenerMtx.RUnlock()
	if listener != nil {
		listener(users...)
	}
}
