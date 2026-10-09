// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"reflect"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

func TestMatchAcksRecordedEventRoundTrip(t *testing.T) {
	var mid, cancelMID order.MatchID
	mid[0] = 0x01
	cancelMID[0] = 0x02
	event := &MatchAcksRecordedEvent{
		AckTime: 1670000000123,
		Records: []MatchAckRecord{{
			MatchID: mid,
			Base:    42,
			Quote:   0,
			Maker:   true,
			Sig:     dex.Bytes{0x0a, 0x0b},
			Address: "swap-addr",
		}, {
			MatchID: cancelMID,
			Base:    42,
			Quote:   0,
			Cancel:  true,
			Sig:     dex.Bytes{0x0c},
		}},
	}

	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	got, err := DecodeMatchAcksRecordedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !reflect.DeepEqual(got, event) {
		t.Fatalf("round trip mismatch.\nwant: %#v\n got: %#v", event, got)
	}
}

func TestMatchAcksRecordedEventValidate(t *testing.T) {
	var mid order.MatchID
	mid[0] = 0x01
	tradeRecord := MatchAckRecord{
		MatchID: mid,
		Base:    42,
		Maker:   true,
		Sig:     dex.Bytes{0x0a},
		Address: "swap-addr",
	}
	cancelRecord := MatchAckRecord{
		MatchID: mid,
		Base:    42,
		Cancel:  true,
		Sig:     dex.Bytes{0x0b},
	}
	noSig := tradeRecord
	noSig.Sig = nil
	noAddr := tradeRecord
	noAddr.Address = ""

	tests := []struct {
		name    string
		event   *MatchAcksRecordedEvent
		wantErr bool
	}{
		{"ok", &MatchAcksRecordedEvent{AckTime: 1, Records: []MatchAckRecord{tradeRecord, cancelRecord}}, false},
		{"no ack time", &MatchAcksRecordedEvent{Records: []MatchAckRecord{tradeRecord}}, true},
		{"no records", &MatchAcksRecordedEvent{AckTime: 1}, true},
		{"empty sig", &MatchAcksRecordedEvent{AckTime: 1, Records: []MatchAckRecord{noSig}}, true},
		{"trade without address", &MatchAcksRecordedEvent{AckTime: 1, Records: []MatchAckRecord{noAddr}}, true},
		{"cancel without address ok", &MatchAcksRecordedEvent{AckTime: 1, Records: []MatchAckRecord{cancelRecord}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
