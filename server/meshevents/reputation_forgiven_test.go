// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"bytes"
	"testing"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

func TestReputationForgivenEventTxDataIncludesScopeAndMatch(t *testing.T) {
	var acct account.AccountID
	acct[0] = 0x01
	var mid order.MatchID
	mid[0] = 0x02
	event := &ReputationForgivenEvent{
		AccountID: acct,
		Scope:     ReputationForgivenessScopeMatch,
		MatchID:   &mid,
	}

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
	if len(pushes) != 3 {
		t.Fatalf("push count = %d, want 3", len(pushes))
	}
	if !bytes.Equal(pushes[0], acct[:]) {
		t.Fatalf("account push = %x, want %x", pushes[0], acct[:])
	}
	if !bytes.Equal(pushes[1], []byte{byte(ReputationForgivenessScopeMatch)}) {
		t.Fatalf("scope push = %x, want %x", pushes[1], []byte{byte(ReputationForgivenessScopeMatch)})
	}
	if !bytes.Equal(pushes[2], mid[:]) {
		t.Fatalf("match id push = %x, want %x", pushes[2], mid[:])
	}
}

func TestReputationForgivenEventValidate(t *testing.T) {
	var acct account.AccountID
	acct[0] = 0x01
	var mid order.MatchID
	mid[0] = 0x02
	tests := []struct {
		name    string
		event   *ReputationForgivenEvent
		wantErr bool
	}{
		{"user ok", &ReputationForgivenEvent{AccountID: acct, Scope: ReputationForgivenessScopeUser}, false},
		{"user with match", &ReputationForgivenEvent{AccountID: acct, Scope: ReputationForgivenessScopeUser, MatchID: &mid}, true},
		{"match ok", &ReputationForgivenEvent{AccountID: acct, Scope: ReputationForgivenessScopeMatch, MatchID: &mid}, false},
		{"match missing id", &ReputationForgivenEvent{AccountID: acct, Scope: ReputationForgivenessScopeMatch}, true},
		{"zero account", &ReputationForgivenEvent{Scope: ReputationForgivenessScopeUser}, true},
		{"invalid scope", &ReputationForgivenEvent{AccountID: acct, Scope: ReputationForgivenessScopeInvalid}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReputationForgivenEventEncodeDecode(t *testing.T) {
	var acct account.AccountID
	acct[0] = 0x07
	var mid order.MatchID
	mid[0] = 0x09
	event := &ReputationForgivenEvent{
		AccountID: acct,
		Scope:     ReputationForgivenessScopeMatch,
		MatchID:   &mid,
	}
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	got, err := DecodeReputationForgivenEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.AccountID != acct || got.Scope != ReputationForgivenessScopeMatch || got.Match() != mid {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
