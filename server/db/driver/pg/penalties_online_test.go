// +build pgonline

package pg

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"github.com/davecgh/go-spew/spew"
)

func TestPenalties(t *testing.T) {
	// Clear penalties table before testing.
	truncatePenalties := `TRUNCATE %s;`
	stmt := fmt.Sprintf(truncatePenalties, penaltiesTableName)
	if _, err := sqlExec(archie.db, stmt); err != nil {
		t.Fatalf("error truncating penalties: %v", err)
	}

	acctID := account.AccountID{
		0x0a, 0x99, 0x12, 0x20, 0x5b, 0x2c, 0xba, 0xb0, 0xc2, 0x5c, 0x2d, 0xe3, 0x0b,
		0xda, 0x90, 0x74, 0xde, 0x0a, 0xe2, 0x3b, 0x06, 0x54, 0x89, 0xa9, 0x91, 0x99,
		0xba, 0xd7, 0x63, 0xf1, 0x02, 0xcc,
	}
	anotherAcctID := account.AccountID{
		0x0b, 0x99, 0x12, 0x20, 0x5b, 0x2c, 0xba, 0xb0, 0xc2, 0x5c, 0x2d, 0xe3, 0x0b,
		0xda, 0x90, 0x74, 0xde, 0x0a, 0xe2, 0x3b, 0x06, 0x54, 0x89, 0xa9, 0x91, 0x99,
		0xba, 0xd7, 0x63, 0xf1, 0x02, 0xcc,
	}
	twoSecs := uint64((time.Second * 2).Milliseconds())
	tenSecs := uint64((time.Second * 10).Milliseconds())
	nowInMS := encode.UnixMilliU(time.Now())
	penaltyTwoSec := &db.Penalty{
		Penalty: &msgjson.Penalty{
			AccountID:  acctID,
			BrokenRule: 1,
			Time:       nowInMS,
			Duration:   twoSecs,
			Details:    "details",
		},
	}
	penaltyTenSec := &db.Penalty{
		Penalty: &msgjson.Penalty{
			AccountID:  acctID,
			BrokenRule: 1,
			Time:       nowInMS,
			Duration:   tenSecs,
			Details:    "details",
		},
	}
	penaltyTenSecDiffUser := &db.Penalty{
		Penalty: &msgjson.Penalty{
			AccountID:  anotherAcctID,
			BrokenRule: 1,
			Time:       nowInMS,
			Duration:   tenSecs,
			Details:    "details",
		},
	}
	// Add penalties.
	idTwoSec := archie.InsertPenalty(penaltyTwoSec)
	idTenSec := archie.InsertPenalty(penaltyTenSec)
	archie.InsertPenalty(penaltyTenSecDiffUser)
	// Check penalties
	penalties, err := archie.Penalties(acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// IDs were added upon inserting.
	penaltyTwoSec.ID = idTwoSec
	penaltyTenSec.ID = idTenSec
	if !reflect.DeepEqual(penalties[0], penaltyTwoSec) {
		t.Fatalf("first penalty not equal: expected %v got %v", spew.Sdump(penaltyTwoSec), spew.Sdump(penalties[0]))
	}
	if !reflect.DeepEqual(penalties[1], penaltyTenSec) {
		t.Fatalf("second penalty not equal: expected %v got %v", spew.Sdump(penaltyTenSec), spew.Sdump(penalties[1]))
	}
	// Wait the duration of the first penalty.
	time.Sleep(time.Second*2 + time.Millisecond)
	// Check penalties. The expired penalty should not be returned.
	penalties, err = archie.Penalties(acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 1 {
		t.Fatal("wrong number of penalties")
	}
	if !reflect.DeepEqual(penalties[0], penaltyTenSec) {
		t.Fatalf("first penalty not equal: expected %v got %v", spew.Sdump(penaltyTenSec), spew.Sdump(penalties[0]))
	}
	// Forgive the active penalty.
	if err := archie.ForgivePenalty(idTenSec); err != nil {
		t.Fatal(err)
	}
	// Check penalties. One is expired and the other forgiven, so none
	// should be returned.
	penalties, err = archie.Penalties(acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 0 {
		t.Fatal("wrong number of penalties")
	}
	// They should still show up for AllPenalties.
	penalties, err = archie.AllPenalties(acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Add two more.
	archie.InsertPenalty(penaltyTenSec)
	archie.InsertPenalty(penaltyTenSec)
	// They are currently in effect.
	penalties, err = archie.Penalties(acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Forgive all penalties for this user.
	if err := archie.ForgivePenalties(acctID); err != nil {
		t.Fatal(err)
	}
	// Check that they are forgiven.
	penalties, err = archie.Penalties(acctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 0 {
		t.Fatal("wrong number of penalties")
	}
	// The other user's penalty is still in effect.
	penalties, err = archie.Penalties(anotherAcctID)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 1 {
		t.Fatal("wrong number of penalties")
	}
	penaltyTenSecDiffUser.ID = penalties[0].ID
	if !reflect.DeepEqual(penalties[0], penaltyTenSecDiffUser) {
		t.Fatalf("second penalty not equal: expected %v got %v", spew.Sdump(penaltyTenSecDiffUser), spew.Sdump(penalties[0]))
	}
}
