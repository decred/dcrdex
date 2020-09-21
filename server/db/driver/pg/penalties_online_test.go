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
			Strikes:    1,
			Details:    "details",
		},
	}
	penaltyTenSec := &db.Penalty{
		Penalty: &msgjson.Penalty{
			AccountID:  acctID,
			BrokenRule: 1,
			Time:       nowInMS,
			Duration:   tenSecs,
			Strikes:    2,
			Details:    "details",
		},
	}
	penaltyTenSecDiffUser := &db.Penalty{
		Penalty: &msgjson.Penalty{
			AccountID:  anotherAcctID,
			BrokenRule: 1,
			Time:       nowInMS,
			Duration:   tenSecs,
			Strikes:    1,
			Details:    "details",
		},
	}
	// Add penalties.
	idTwoSec, err := archie.InsertPenalty(penaltyTwoSec)
	if err != nil {
		t.Fatal(err)
	}
	idTenSec, err := archie.InsertPenalty(penaltyTenSec)
	if err != nil {
		t.Fatal(err)
	}
	_, err = archie.InsertPenalty(penaltyTenSecDiffUser)
	if err != nil {
		t.Fatal(err)
	}
	// Check penalties. Strike threshold is four, with total three strikes.
	penalties, bannedUntil, err := archie.Penalties(acctID, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	zeroTime := time.Time{}
	// bannedUntil is zero because we are under the strike threshold.
	if bannedUntil != zeroTime {
		t.Fatal("banned until should be zero")
	}
	// Strike threshold of three.
	penalties, bannedUntil, err = archie.Penalties(acctID, 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Banned until the earliest strike expires.
	if bannedUntil != time.Unix(int64(penaltyTwoSec.Time+penaltyTwoSec.Duration), 0) {
		t.Fatal("banned until should be the lower time")
	}
	// Strike threshold of two.
	penalties, bannedUntil, err = archie.Penalties(acctID, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Because the ten second ban has a weight of two strikes, it is in
	// effect at strike threshold of 2.
	if bannedUntil != time.Unix(int64(penaltyTenSec.Time+penaltyTenSec.Duration), 0) {
		t.Fatal("banned until should be the higher time")
	}
	// Strike threshold of one.
	penalties, bannedUntil, err = archie.Penalties(acctID, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Ten second ban is still in effect whith threshold of 1.
	if bannedUntil != time.Unix(int64(penaltyTenSec.Time+penaltyTenSec.Duration), 0) {
		t.Fatal("banned until should be the higher time")
	}
	// Strike threshold of zero.
	penalties, bannedUntil, err = archie.Penalties(acctID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Zero theshold results in zero ban time.
	if bannedUntil != zeroTime {
		t.Fatal("banned until should be zero for theshold zero")
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
	penalties, _, err = archie.Penalties(acctID, 0, false)
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
	penalties, _, err = archie.Penalties(acctID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 0 {
		t.Fatal("wrong number of penalties")
	}
	// They should still show up for all penalties.
	penalties, _, err = archie.Penalties(acctID, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 2 {
		t.Fatal("wrong number of penalties")
	}
	// Add two more.
	_, err = archie.InsertPenalty(penaltyTenSec)
	if err != nil {
		t.Fatal(err)
	}
	_, err = archie.InsertPenalty(penaltyTenSec)
	if err != nil {
		t.Fatal(err)
	}
	// They are currently in effect.
	penalties, _, err = archie.Penalties(acctID, 0, false)
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
	penalties, _, err = archie.Penalties(acctID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(penalties) != 0 {
		t.Fatal("wrong number of penalties")
	}
	// The other user's penalty is still in effect.
	penalties, _, err = archie.Penalties(anotherAcctID, 0, false)
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
