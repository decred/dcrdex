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

type reputationPreimageOutcome struct {
	user account.AccountID
	oid  order.OrderID
	miss bool
}

type reputationOrderOutcome struct {
	user            account.AccountID
	oid             order.OrderID
	penalizedCancel bool
}

// reputationOutcomeBatch contains the reputation outcomes to record for one event.
type reputationOutcomeBatch struct {
	preimages []*reputationPreimageOutcome
	orders    []*reputationOrderOutcome
}

// reputationClassKey identifies the account and outcome class to prune.
type reputationClassKey struct {
	user  account.AccountID
	class db.OutcomeClass
}

// applyRepEventTx applies an event and records its reputation outcomes in
// the same transaction. It prunes older outcomes to the configured limits
// and notifies the reputation listener if the transaction committed or
// its commit outcome is unknown.
//
// apply updates the event's database state and adds reputation outcomes
// to the batch. Those outcomes are written after apply returns successfully.
func (a *Archiver) applyRepEventTx(ctx context.Context, meta *db.EventLogMeta, kind string, txData []byte,
	policy *db.ReputationOutcomePolicy, apply func(*sql.Tx, *reputationOutcomeBatch) error) (*db.EventLogEntry, error) {

	batch := new(reputationOutcomeBatch)
	logEntry, err := a.applyEventTx(ctx, meta, kind, txData, func(tx *sql.Tx) error {
		if err := apply(tx, batch); err != nil {
			return err
		}
		return a.storeReputationOutcomeBatch(ctx, tx, policy, batch)
	})
	a.notifyRepInputsOnCommit(err, reputationBatchAccounts(batch)...)
	return logEntry, err
}

// storeReputationOutcomeBatch writes and prunes outcome rows for the batch.
func (a *Archiver) storeReputationOutcomeBatch(
	ctx context.Context,
	tx *sql.Tx,
	policy *db.ReputationOutcomePolicy,
	batch *reputationOutcomeBatch,
) error {
	keys := reputationClassKeys(batch)
	if len(keys) == 0 {
		return nil
	}
	// Guard against programmer error: a missing limit would prune all outcomes
	// for the account and class.
	for _, key := range keys {
		if outcomeRetentionLimit(policy, key.class) <= 0 {
			return fmt.Errorf("missing reputation outcome limit for class %d", key.class)
		}
	}
	handleDBError := func(err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		a.fatalBackendErr(err)
		return err
	}
	for _, update := range batch.preimages {
		outcome := db.OutcomePreimageSuccess
		if update.miss {
			outcome = db.OutcomePreimageMiss
		}
		if err := a.insertPoints(ctx, tx, update.user, update.oid, db.OutcomeClassPreimage, outcome); err != nil {
			return handleDBError(err)
		}
	}
	for _, update := range batch.orders {
		outcome := db.OutcomeOrderComplete
		if update.penalizedCancel {
			outcome = db.OutcomeOrderCanceled
		}
		if err := a.insertPoints(ctx, tx, update.user, update.oid, db.OutcomeClassOrder, outcome); err != nil {
			return handleDBError(err)
		}
	}

	// Prune once per account and outcome class after inserting the whole batch.
	pruneStmt := fmt.Sprintf(internal.PrunePointsPastLimit, a.tables.points)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, pruneStmt, key.user, key.class, outcomeRetentionLimit(policy, key.class)); err != nil {
			return handleDBError(err)
		}
	}
	return nil
}

func (a *Archiver) insertPoints(ctx context.Context, tx *sql.Tx, user account.AccountID, link [32]byte,
	class db.OutcomeClass, outcome db.Outcome) error {
	stmt := fmt.Sprintf(internal.InsertPoints, a.tables.points)
	_, err := tx.ExecContext(ctx, stmt, user, link[:], class, outcome)
	return err
}

func outcomeRetentionLimit(policy *db.ReputationOutcomePolicy, class db.OutcomeClass) int {
	if policy == nil {
		return 0
	}
	switch class {
	case db.OutcomeClassPreimage:
		return policy.PreimageLimit
	case db.OutcomeClassOrder:
		return policy.OrderLimit
	default:
		return 0
	}
}

func reputationClassKeys(batch *reputationOutcomeBatch) []reputationClassKey {
	if batch == nil {
		return nil
	}
	keys := make(map[reputationClassKey]struct{})
	for _, update := range batch.preimages {
		if update != nil {
			keys[reputationClassKey{user: update.user, class: db.OutcomeClassPreimage}] = struct{}{}
		}
	}
	for _, update := range batch.orders {
		if update != nil {
			keys[reputationClassKey{user: update.user, class: db.OutcomeClassOrder}] = struct{}{}
		}
	}
	distinct := make([]reputationClassKey, 0, len(keys))
	for key := range keys {
		distinct = append(distinct, key)
	}
	return distinct
}

// reputationBatchAccounts returns each account represented in the batch once,
// even if it has multiple outcomes or outcomes in different classes.
func reputationBatchAccounts(batch *reputationOutcomeBatch) []account.AccountID {
	keys := reputationClassKeys(batch)
	seen := make(map[account.AccountID]struct{}, len(keys))
	users := make([]account.AccountID, 0, len(keys))
	for _, key := range keys {
		if _, found := seen[key.user]; found {
			continue
		}
		seen[key.user] = struct{}{}
		users = append(users, key.user)
	}
	return users
}
