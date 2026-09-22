// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"encoding/gob"
	"math"
	"strings"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
	"github.com/lib/pq"
)

// TestLifecycleForConfiguredMarkets: lifecycle uses market names, not schema keys.
func TestLifecycleForConfiguredMarkets(t *testing.T) {
	if got := lifecycleForConfiguredMarkets(nil); got != "FALSE" {
		t.Fatalf("empty markets: got %q, want FALSE", got)
	}

	markets := map[string]*dex.MarketInfo{
		"dcrTKNbtc": {Name: "dcr.btc"},
		"btc_ltc":   {Name: "btc_ltc"},
	}
	got := lifecycleForConfiguredMarkets(markets)
	want := "market IN (" + pq.QuoteLiteral("btc_ltc") + ", " + pq.QuoteLiteral("dcr.btc") + ")"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// snapshotBytes encodes a test snapshot using the format accepted by LoadSnapshot.
func snapshotBytes(t *testing.T, snapshot *pgSnapshot) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(snapshot); err != nil {
		t.Fatalf("encode test snapshot: %v", err)
	}
	return encoded.Bytes()
}

func TestValidateSnapshotTableSet(t *testing.T) {
	expected := []snapshotTable{
		{schema: publicSchema, table: accountsTableName},
		{schema: publicSchema, table: bondsTableName},
		{schema: "mkt", table: ordersActiveTableName},
	}
	for _, tt := range []struct {
		name    string
		keys    []string
		wantErr bool
	}{
		{"exact", []string{"public.accounts", "public.bonds", "mkt.orders_active"}, false},
		{"reordered", []string{"mkt.orders_active", "public.bonds", "public.accounts"}, false},
		{"extra", []string{"public.accounts", "public.bonds", "mkt.orders_active", "public.markets"}, true},
		{"missing", []string{"public.accounts", "public.bonds"}, true},
		{"duplicate", []string{"public.accounts", "public.bonds", "mkt.orders_active", "public.accounts"}, true},
		{"duplicate replaces required table", []string{"public.accounts", "public.bonds", "public.accounts"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dumps := make([]snapshotTableDump, len(tt.keys))
			for i, key := range tt.keys {
				schema, table, _ := strings.Cut(key, ".")
				dumps[i] = snapshotTableDump{Schema: schema, Table: table}
			}
			err := validateSnapshotTableSet(dumps, expected)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSnapshotTableSet error = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodePGSnapshot(t *testing.T) {
	tip := bytes.Repeat([]byte{0x01}, db.EventLogTipHashSize)
	for _, tt := range []struct {
		name    string
		seq     uint64
		tip     []byte
		wantErr bool
	}{
		{"zero frontier", 0, nil, false},
		{"empty hash", 0, []byte{}, false},
		{"valid frontier", 1, tip, false},
		{"largest sequence", math.MaxInt64, tip, false},
		{"sequence overflow", uint64(math.MaxInt64) + 1, tip, true},
		{"hash without sequence", 0, tip, true},
		{"missing hash", 1, nil, true},
		{"short hash", 1, tip[:len(tip)-1], true},
		{"long hash", 1, bytes.Repeat([]byte{0x01}, db.EventLogTipHashSize+1), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			encoded := snapshotBytes(t, &pgSnapshot{FrontierSeq: tt.seq, FrontierTipHash: tt.tip})
			snapshot, err := decodePGSnapshot(bytes.NewReader(encoded))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodePGSnapshot error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if snapshot != nil {
					t.Fatal("invalid snapshot returned without rejection")
				}
				return
			}
			if snapshot.FrontierSeq != tt.seq || !bytes.Equal(snapshot.FrontierTipHash, tt.tip) {
				t.Fatalf("decoded frontier = %v, want sequence %d and hash %x", snapshot.frontier(), tt.seq, tt.tip)
			}
		})
	}
	t.Run("invalid encoding", func(t *testing.T) {
		if snapshot, err := decodePGSnapshot(strings.NewReader("not a snapshot")); err == nil || snapshot != nil {
			t.Fatalf("decodePGSnapshot = (%+v, %v), want rejection", snapshot, err)
		}
	})
}
