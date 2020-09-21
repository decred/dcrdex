// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

// InsertPenalty adds penalty to the penalties table. The passed ID and Forgiven
// fields are ignored. The db decided id is returned.
func (a *Archiver) InsertPenalty(penalty *db.Penalty) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertPenalty, penaltiesTableName)
	var id int64
	err := a.db.QueryRow(stmt, penalty.AccountID, penalty.BrokenRule,
		int64(penalty.Time), penalty.Duration, penalty.Strikes, penalty.Details).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
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
// Forgiven and expired penalties are not included unless all is true. If the
// user has enough strikes to bring them to the strike threshold, the latest
// expiration time that brings them under the strike threshold is returned. If
// not, or the strike threshold is 0, zero time is returned.
func (a *Archiver) Penalties(aid account.AccountID, strikeThreshold int, all bool) ([]*db.Penalty, time.Time, error) {
	var (
		pens     []*db.Penalty
		err      error
		zeroTime = time.Time{}
	)
	if all {
		stmt := fmt.Sprintf(internal.SelectAllPenalties, penaltiesTableName)
		pens, err = penalties(a.db, stmt, aid)
		if err != nil {
			return nil, zeroTime, err
		}
	} else {
		stmt := fmt.Sprintf(internal.SelectPenalties, penaltiesTableName)
		pens, err = penalties(a.db, stmt, aid, encode.UnixMilli(time.Now()))
		if err != nil {
			return nil, zeroTime, err
		}
	}
	if strikeThreshold == 0 {
		return pens, zeroTime, nil
	}
	strikes := 0
	times := make([]uint64, 0)
	for _, p := range pens {
		strikes += int(p.Strikes)
		// Add one entry to times per strike.
		for i := 0; i < int(p.Strikes); i++ {
			times = append(times, p.Time+p.Duration)
		}
	}
	if strikes < strikeThreshold {
		return pens, zeroTime, nil
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	// Ban time is until the last time within the strike threshold.
	bannedUntil := times[strikes-strikeThreshold]
	return pens, encode.UnixTimeMilli(int64(bannedUntil)), nil
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

		err = rows.Scan(&p.ID, &p.AccountID, &p.BrokenRule, &t, &p.Duration, &p.Strikes, &details, &p.Forgiven)
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
