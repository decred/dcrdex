// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func testBondPostedAccount(t *testing.T) *account.Account {
	t.Helper()
	privBytes := make([]byte, 32)
	privBytes[31] = 0x01
	priv := secp256k1.PrivKeyFromBytes(privBytes)
	acct, err := account.NewAccountFromPubKey(priv.PubKey().SerializeCompressed())
	if err != nil {
		t.Fatalf("NewAccountFromPubKey error: %v", err)
	}
	return acct
}

func testPostedBond() *Bond {
	return &Bond{
		Version:  1,
		AssetID:  42,
		CoinID:   []byte{0xde, 0xad, 0xbe, 0xef},
		Amount:   123456789,
		Strength: 3,
		LockTime: 1720000000,
	}
}

func TestBondPostedEventEncodeDecode(t *testing.T) {
	acct := testBondPostedAccount(t)
	bond := testPostedBond()
	event := NewBondPostedEvent(acct, bond)
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	const wantJSON = `{"account":{"accountid":"6a84ea2917ead0d8f6c1460231898e214e4517870a850e972000443f650efbc4","pubkey":"0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"},"bond":{"Version":1,"AssetID":42,"CoinID":"3q2+7w==","Amount":123456789,"Strength":3,"LockTime":1720000000}}`
	if string(payload) != wantJSON {
		t.Fatalf("encoded event:\n got  %s\n want %s", payload, wantJSON)
	}

	got, err := DecodeBondPostedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.Account.AccountID != acct.ID {
		t.Fatalf("account id round trip mismatch: %v != %v", got.Account.AccountID, acct.ID)
	}
	if !bytes.Equal(got.Account.Pubkey, acct.PubKey.SerializeCompressed()) {
		t.Fatalf("pubkey round trip mismatch")
	}
	if !reflect.DeepEqual(got.Bond, bond) {
		t.Fatalf("bond round trip mismatch: %+v != %+v", got.Bond, bond)
	}
	parsed, err := got.PostedAccount()
	if err != nil {
		t.Fatalf("PostedAccount error: %v", err)
	}
	if parsed.ID != acct.ID {
		t.Fatalf("parsed account id mismatch: %v != %v", parsed.ID, acct.ID)
	}

	txData, err := got.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	const wantTxData = "00206a84ea2917ead0d8f6c1460231898e214e4517870a850e972000443f650efbc4210279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f817982500020001040000002a04deadbeef0800000000075bcd150400000003080000000066851e00"
	if hex.EncodeToString(txData) != wantTxData {
		t.Fatalf("transaction data = %x, want %s", txData, wantTxData)
	}
}

func TestBondPostedEventValidate(t *testing.T) {
	acct := testBondPostedAccount(t)
	goodAcct := &BondPostedAccount{
		AccountID: acct.ID,
		Pubkey:    acct.PubKey.SerializeCompressed(),
	}
	var wrongID account.AccountID
	wrongID[0] = 0xff
	tests := []struct {
		name    string
		event   *BondPostedEvent
		wantErr bool
	}{
		{"ok", &BondPostedEvent{Account: goodAcct, Bond: testPostedBond()}, false},
		{"nil account", &BondPostedEvent{Bond: testPostedBond()}, true},
		{"nil bond", &BondPostedEvent{Account: goodAcct}, true},
		{"bad pubkey", &BondPostedEvent{Account: &BondPostedAccount{
			AccountID: acct.ID,
			Pubkey:    []byte{0x02, 0x01},
		}, Bond: testPostedBond()}, true},
		{"account id mismatch", &BondPostedEvent{Account: &BondPostedAccount{
			AccountID: wrongID,
			Pubkey:    acct.PubKey.SerializeCompressed(),
		}, Bond: testPostedBond()}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
