// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

// InsertPenalty adds penalty to the penalties table. The passed ID and Forgiven
// fields are ignored. The db decided id is returned.
func (a *Archiver) InsertPenalty(penalty *db.Penalty) (id int64) {
	stmt := fmt.Sprintf(internal.InsertPenalty, penaltiesTableName)
	if err := a.db.QueryRow(stmt, penalty.AccountID, penalty.BrokenRule,
		int64(penalty.Time), penalty.Duration, penalty.Details).Scan(&id); err != nil {
		a.fatalBackendErr(err)
		return 0
	}
	return id
}

// ForgivePenalties forgives all penalties currenty held by a user.
func (a *Archiver) ForgivePenalties(aid account.AccountID) error {
	stmt := fmt.Sprintf(internal.ForgivePenalties, penaltiesTableName)
	_, err := a.db.Exec(stmt, aid, encode.UnixMilli(time.Now()))
	return err
}

// ForgivePenalty forgives a penalty.
func (a *Archiver) ForgivePenalty(id int64) error {
	stmt := fmt.Sprintf(internal.ForgivePenalty, penaltiesTableName)
	_, err := a.db.Exec(stmt, id)
	return err
}

// Penalties returns penalties that are currently in effect for a user.
// Forgiven and expired penalties are not included.
func (a *Archiver) Penalties(aid account.AccountID) ([]*db.Penalty, error) {
	stmt := fmt.Sprintf(internal.SelectPenalties, penaltiesTableName)
	return penalties(a.db, stmt, aid, encode.UnixMilli(time.Now()))
}

// AllPenalties returns all penalties for a user, even those that are forgiven
// or expired.
func (a *Archiver) AllPenalties(aid account.AccountID) ([]*db.Penalty, error) {
	stmt := fmt.Sprintf(internal.SelectAllPenalties, penaltiesTableName)
	return penalties(a.db, stmt, aid)
}

// penalties returns a slice of penalties for a user, depending on the passed
// statement and arguments.
func penalties(dbe *sql.DB, stmt string, args ...interface{}) ([]*db.Penalty, error) {
	rows, err := dbe.Query(stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var (
		penalties []*db.Penalty
		details   sql.NullString
		t         int64
	)
	for rows.Next() {
		p := &db.Penalty{Penalty: new(msgjson.Penalty)}

		err = rows.Scan(&p.ID, &p.AccountID, &p.BrokenRule, &t, &p.Duration, &details, &p.Forgiven)
		if err != nil {
			return nil, err
		}
		p.Time = uint64(t)
		p.Details = details.String
		penalties = append(penalties, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return penalties, nil
}
