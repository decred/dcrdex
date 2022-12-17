// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"testing"
)

func TestBipIDSymbol(t *testing.T) {
	tests := []struct {
		name string
		idx  uint32
		want string
	}{
		{
			name: "ok",
			idx:  42,
			want: "dcr",
		},
		{
			name: "missing",
			idx:  111111111,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BipIDSymbol(tt.idx); got != tt.want {
				t.Errorf("BipIDSymbol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBipSymbolID(t *testing.T) {
	tests := []struct {
		name   string
		symbol string
		want   uint32
		want1  bool
	}{
		{
			name:   "ok",
			symbol: "dcr",
			want:   42,
			want1:  true,
		},
		{
			name:   "ok, again with pre-computed map",
			symbol: "dcr",
			want:   42,
			want1:  true,
		},
		{
			name:   "missing",
			symbol: "blahblahfake",
			want:   0,
			want1:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := BipSymbolID(tt.symbol)
			if got != tt.want {
				t.Errorf("BipSymbolID() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("BipSymbolID() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestBipTokenSymbolNetworks(t *testing.T) {
	tests := []struct {
		name   string
		symbol string
		want   map[string]uint32
		want1  bool
	}{
		{
			name:   "ok",
			symbol: "dextt",
			want: map[string]uint32{
				"eth": 60000,
			},
			want1: true,
		},
		{
			name:   "ok, again with pre-computed map",
			symbol: "dextt",
			want: map[string]uint32{
				"eth": 60000,
			},
			want1: true,
		},
		{
			name:   "missing",
			symbol: "blahblahfake",
			want:   nil,
			want1:  false,
		},
	}
	for _, tt := range tests {
		got, got1 := BipTokenSymbolNetworks(tt.symbol)
		if tt.want1 != got1 {
			t.Fatalf("%s: expected found %v, but got %v", tt.name, tt.want1, got1)
		}
		if len(tt.want) != len(got) {
			t.Fatalf("%s: expected num networks %d but got %d", tt.name, len(tt.want), len(got))
		}
		for net, id := range tt.want {
			if got[net] != id {
				t.Fatalf("%s: id did not match for netowrk %s", tt.name, net)
			}
		}
	}
}
