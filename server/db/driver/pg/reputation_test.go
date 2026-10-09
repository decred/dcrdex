// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"errors"
	"testing"

	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

func TestNotifyRepInputsOnCommit(t *testing.T) {
	archiver := new(Archiver)
	user := randomAccountID()

	archiver.notifyRepInputsOnCommit(nil, user) // nil listener: no-op

	var got [][]account.AccountID
	archiver.SetReputationInputsListener(func(users ...account.AccountID) {
		got = append(got, users)
	})

	archiver.notifyRepInputsOnCommit(nil, user)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0] != user {
		t.Fatalf("committed notify = %v, want [[%v]]", got, user)
	}

	// Commit-unknown still notifies (safe over-invalidation).
	archiver.notifyRepInputsOnCommit(&db.EventCommitUnknownError{Err: errors.New("conn lost")}, user)
	if len(got) != 2 {
		t.Fatalf("commit-unknown notify count = %d, want 2", len(got))
	}

	archiver.notifyRepInputsOnCommit(errors.New("apply failed"), user) // definite failure
	archiver.notifyRepInputsOnCommit(nil)                              // no users
	if len(got) != 2 {
		t.Fatalf("notify count = %d after error/empty calls, want 2", len(got))
	}
}

func TestSetReputationInputsListenerOnce(t *testing.T) {
	archiver := new(Archiver)
	archiver.SetReputationInputsListener(func(...account.AccountID) {})
	defer func() {
		if recover() == nil {
			t.Fatal("second SetReputationInputsListener did not panic")
		}
	}()
	archiver.SetReputationInputsListener(func(...account.AccountID) {})
}
