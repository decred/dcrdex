// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"bytes"
	"reflect"
	"testing"

	"decred.org/dcrdex/dex/encode"
)

func testPrepaidBonds() []*PrepaidBond {
	return []*PrepaidBond{
		{CoinID: []byte{0x01, 0x02, 0x03, 0x04}, Strength: 2, LockTime: 1730000000},
		{CoinID: []byte{0x05, 0x06, 0x07, 0x08}, Strength: 5, LockTime: 1740000000},
	}
}

func TestPrepaidBondsCreatedEventEncodeDecode(t *testing.T) {
	bonds := testPrepaidBonds()
	event := &PrepaidBondsCreatedEvent{Bonds: bonds}
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	const wantJSON = `{"bonds":[{"CoinID":"AQIDBA==","Strength":2,"LockTime":1730000000},{"CoinID":"BQYHCA==","Strength":5,"LockTime":1740000000}]}`
	if string(payload) != wantJSON {
		t.Fatalf("encoded event:\n got  %s\n want %s", payload, wantJSON)
	}

	got, err := DecodePrepaidBondsCreatedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(got.Bonds) != len(bonds) {
		t.Fatalf("bond count = %d, want %d", len(got.Bonds), len(bonds))
	}
	for i, bond := range bonds {
		if !reflect.DeepEqual(got.Bonds[i], bond) {
			t.Fatalf("bond %d round trip mismatch: %+v != %+v", i, got.Bonds[i], bond)
		}
	}
}

func TestPrepaidBondsCreatedEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		event   *PrepaidBondsCreatedEvent
		wantErr bool
	}{
		{"ok", &PrepaidBondsCreatedEvent{Bonds: testPrepaidBonds()}, false},
		{"empty list ok", &PrepaidBondsCreatedEvent{Bonds: []*PrepaidBond{}}, false},
		{"nil list", &PrepaidBondsCreatedEvent{}, true},
		{"nil bond", &PrepaidBondsCreatedEvent{Bonds: []*PrepaidBond{nil}}, true},
		{"empty coin id", &PrepaidBondsCreatedEvent{Bonds: []*PrepaidBond{{Strength: 1, LockTime: 1}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrepaidBondsCreatedEventTxData(t *testing.T) {
	bonds := testPrepaidBonds()
	event := &PrepaidBondsCreatedEvent{Bonds: bonds}
	txData, err := event.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	ver, pushes, err := encode.DecodeBlob(txData)
	if err != nil {
		t.Fatalf("DecodeBlob error: %v", err)
	}
	if ver != 0 {
		t.Fatalf("tx data version = %d, want 0", ver)
	}
	if len(pushes) != len(bonds) {
		t.Fatalf("push count = %d, want %d", len(pushes), len(bonds))
	}
	for i, bond := range bonds {
		bondVer, bondPushes, err := encode.DecodeBlob(pushes[i])
		if err != nil {
			t.Fatalf("bond %d DecodeBlob error: %v", i, err)
		}
		if bondVer != 0 {
			t.Fatalf("bond %d tx data version = %d, want 0", i, bondVer)
		}
		if len(bondPushes) != 3 {
			t.Fatalf("bond %d push count = %d, want 3", i, len(bondPushes))
		}
		if !bytes.Equal(bondPushes[0], bond.CoinID) {
			t.Fatalf("bond %d coin id push = %x, want %x", i, bondPushes[0], bond.CoinID)
		}
		if !bytes.Equal(bondPushes[1], encode.Uint32Bytes(bond.Strength)) {
			t.Fatalf("bond %d strength push = %x", i, bondPushes[1])
		}
		if !bytes.Equal(bondPushes[2], encode.Uint64Bytes(uint64(bond.LockTime))) {
			t.Fatalf("bond %d lock time push = %x", i, bondPushes[2])
		}
	}
}
