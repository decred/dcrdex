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
