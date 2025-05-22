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
)

const newReputationVersion int16 = 1

var _ db.ReputationArchiver = (*Archiver)(nil)

func (a *Archiver) GetUserReputationData(
	ctx context.Context,
	user account.AccountID,
	pimgSz, matchSz, orderSz int, /* pre-allocation sizes */
) ([]*db.PreimageOutcome, []*db.MatchResult, []*db.OrderOutcome, error) {
	rows, err := a.queries.selectPoints.QueryContext(ctx, user)
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

func (a *Archiver) insertPoints(
	ctx context.Context,
	user account.AccountID,
	link [32]byte,
	outcomeClass db.OutcomeClass,
	outcome db.Outcome,
) (dbID int64, _ error) {
	var oid order.OrderID // need a sql.Scanner
	copy(oid[:], link[:])
	return dbID, a.queries.insertPoints.QueryRowContext(ctx, user, oid, outcomeClass, outcome).Scan(&dbID)
}

func (a *Archiver) AddPreimageOutcome(ctx context.Context, user account.AccountID, oid order.OrderID, miss bool) (*db.PreimageOutcome, error) {
	outcome := db.OutcomePreimageSuccess
	if miss {
		outcome = db.OutcomePreimageMiss
	}
	dbID, err := a.insertPoints(ctx, user, oid, db.OutcomeClassPreimage, outcome)
	if err != nil {
		return nil, err
	}
	return &db.PreimageOutcome{
		DBID:    dbID,
		OrderID: oid,
		Miss:    miss,
	}, nil
}

func (a *Archiver) AddMatchOutcome(ctx context.Context, user account.AccountID, mid order.MatchID, outcome db.Outcome) (*db.MatchResult, error) {
	if outcome < db.OutcomeSwapSuccess || outcome > db.OutcomeNoRedeemAsTaker {
		return nil, fmt.Errorf("invalid outcome for a match: %d", outcome)
	}
	dbID, err := a.insertPoints(ctx, user, mid, db.OutcomeClassMatch, outcome)
	if err != nil {
		return nil, err
	}
	return &db.MatchResult{
		DBID:         dbID,
		MatchID:      mid,
		MatchOutcome: outcome,
	}, nil
}

func (a *Archiver) AddOrderOutcome(ctx context.Context, user account.AccountID, oid order.OrderID, canceled bool) (*db.OrderOutcome, error) {
	outcome := db.OutcomeOrderComplete
	if canceled {
		outcome = db.OutcomeOrderCanceled
	}
	dbID, err := a.insertPoints(ctx, user, oid, db.OutcomeClassOrder, outcome)
	if err != nil {
		return nil, err
	}
	return &db.OrderOutcome{
		DBID:     dbID,
		OrderID:  oid,
		Canceled: canceled,
	}, nil
}

func (a *Archiver) PruneOutcomes(ctx context.Context, user account.AccountID, outcomeClass db.OutcomeClass, fromDBID int64) (err error) {
	_, err = a.queries.prunePoints.ExecContext(ctx, user, outcomeClass, fromDBID)
	return err
}

func (a *Archiver) GetUserReputationVersion(ctx context.Context, user account.AccountID) (ver int16, err error) {
	if err := a.queries.selectReputationVersion.QueryRowContext(ctx, user).Scan(&ver); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// New user.
			return newReputationVersion, nil
		}
		return 0, err
	}
	return ver, nil
}

func (a *Archiver) UpgradeUserReputationV1(
	ctx context.Context, user account.AccountID, pimgs []*db.PreimageOutcome, matches []*db.MatchResult, orders []*db.OrderOutcome, /* Without DB IDs */
) ([]*db.PreimageOutcome, []*db.MatchResult, []*db.OrderOutcome, error) /* With DB IDs */ {
	tx, err := a.db.Begin()
	if err != nil {
		a.fatalBackendErr(err)
		return nil, nil, nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback() // rollback on error
		} else {
			tx.Commit() // commit if all went well
		}
	}()

	stmt, err := tx.Prepare(fmt.Sprintf(internal.InsertPoints, a.tables.points))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error constructing prepared statement for reputation points selection: %w", err)
	}
	defer stmt.Close()
	for _, o := range pimgs {
		outcome := db.OutcomePreimageSuccess
		if o.Miss {
			outcome = db.OutcomePreimageMiss
		}
		if err = stmt.QueryRowContext(ctx, user, o.OrderID, db.OutcomeClassPreimage, outcome).Scan(&o.DBID); err != nil {
			return nil, nil, nil, fmt.Errorf("error inserting preimage row during reputation upgrade: %w", err)
		}
	}
	for _, o := range matches {
		if err = stmt.QueryRowContext(ctx, user, o.MatchID, db.OutcomeClassMatch, o.MatchOutcome).Scan(&o.DBID); err != nil {
			return nil, nil, nil, fmt.Errorf("error inserting match row during reputation upgrade: %w", err)
		}
	}
	for _, o := range orders {
		outcome := db.OutcomeOrderComplete
		if o.Canceled {
			outcome = db.OutcomeOrderCanceled
		}
		if err = stmt.QueryRowContext(ctx, user, o.OrderID, db.OutcomeClassOrder, outcome).Scan(&o.DBID); err != nil {
			return nil, nil, nil, fmt.Errorf("error inserting order row during reputation upgrade: %w", err)
		}
	}
	query := fmt.Sprintf(internal.UpdateReputationVersion, a.tables.accounts)
	if _, err = tx.ExecContext(ctx, query, newReputationVersion, user); err != nil {
		return nil, nil, nil, fmt.Errorf("error updating reputation version: %w", err)
	}
	return pimgs, matches, orders, nil
}

func (a *Archiver) ForgiveUser(ctx context.Context, user account.AccountID) error {
	query := fmt.Sprintf(internal.ForgiveUser, a.tables.points)
	if _, err := a.db.ExecContext(ctx, query, user, db.OutcomeSwapSuccess, db.OutcomePreimageSuccess, db.OutcomeOrderComplete); err != nil {
		return fmt.Errorf("error forgiving user: %w", err)
	}
	return nil
}
