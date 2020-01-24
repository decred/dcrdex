// +build pgonline

package pg

import (
	"encoding/hex"
	"testing"

	"decred.org/dcrdex/server/account"
)

var tPubKey = []byte{
	0x02, 0x04, 0x98, 0x8a, 0x49, 0x8d, 0x5d, 0x19, 0x51, 0x4b, 0x21, 0x7e, 0x87,
	0x2b, 0x4d, 0xbd, 0x1c, 0xf0, 0x71, 0xd3, 0x65, 0xc4, 0x87, 0x9e, 0x64, 0xed,
	0x59, 0x19, 0x88, 0x1c, 0x97, 0xeb, 0x19,
}

var tAcctID = account.AccountID{
	0x0a, 0x99, 0x12, 0x20, 0x5b, 0x2c, 0xba, 0xb0, 0xc2, 0x5c, 0x2d, 0xe3, 0x0b,
	0xda, 0x90, 0x74, 0xde, 0x0a, 0xe2, 0x3b, 0x06, 0x54, 0x89, 0xa9, 0x91, 0x99,
	0xba, 0xd7, 0x63, 0xf1, 0x02, 0xcc,
}

func tNewAccount(t *testing.T) *account.Account {
	acct, err := account.NewAccountFromPubKey(tPubKey)
	if err != nil {
		t.Fatalf("error creating account from pubkey: %v", err)
	}
	if acct.ID != tAcctID {
		t.Fatalf("unexpected account ID. wanted %x, got %x", tAcctID, acct.ID)
	}
	return acct
}

func TestAccounts(t *testing.T) {
	tCoinID, _ := hex.DecodeString("6e515ff861f2016fd0da2f3eccdf8290c03a9d116bfba2f6729e648bdc6e5aed00000005")
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	acct := tNewAccount(t)

	regAddr, err := archie.CreateAccount(acct)
	if err != nil {
		t.Fatalf("error creating account: %v", err)
	}
	t.Logf("created registration address: %s", regAddr)

	checkAddr, err := archie.AccountRegAddr(tAcctID)
	if err != nil {
		t.Fatalf("error getting registration address: %v", err)
	}

	if checkAddr != regAddr {
		t.Fatalf("unexpected address retrieved from the DB. wanted %s, got %s",
			regAddr, checkAddr)
	}

	// Get the account. It should be unpaid.
	acct, paid, _ := archie.Account(tAcctID)
	if paid {
		t.Fatalf("account marked as paid before setting tx details")
	}

	// Pay the registration fee.
	err = archie.PayAccount(tAcctID, tCoinID)
	if err != nil {
		t.Fatalf("error setting registration fee payment details: %v", err)
	}

	// The account should not be marked paid.
	_, paid, open := archie.Account(tAcctID)
	if !paid {
		t.Fatalf("account not marked as paid after setting reg tx details")
	}
	if !open {
		t.Fatalf("newly paid account marked as closed")
	}

	// Close the account for failure to complete a swap.
	archie.CloseAccount(tAcctID, account.FailureToAct)
	_, _, open = archie.Account(tAcctID)
	if open {
		t.Fatalf("closed account still marked as open")
	}
}

func TestWrongAccount(t *testing.T) {
	tCoinID, _ := hex.DecodeString("6e515ff861f2016fd0da2f3eccdf8290c03a9d116bfba2f6729e648bdc6e5aed00000005")
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	acct := tNewAccount(t)

	_, err := archie.AccountRegAddr(tAcctID)
	if err == nil {
		t.Fatalf("no error fetching registration address for unknown account")
	}

	acct, paid, open := archie.Account(tAcctID)
	if acct != nil {
		t.Fatalf("account retrieved for unknown account ID")
	}
	if paid {
		t.Fatalf("unknown account marked as paid")
	}
	if open {
		t.Fatalf("unknown account marked as open")
	}

	err = archie.PayAccount(tAcctID, tCoinID)
	if err == nil {
		t.Fatalf("no error paying registration fee for unknown account")
	}
}
