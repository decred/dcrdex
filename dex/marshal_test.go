// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import "testing"

func TestBytes_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		encHex  string
		wantErr bool
	}{
		{
			name:    "ok",
			encHex:  `"0f0e"`,
			wantErr: false,
		},
		{
			name:    "odd, 1",
			encHex:  `"f"`,
			wantErr: true,
		},
		{
			name:    "odd, 3",
			encHex:  `"fff"`,
			wantErr: true,
		},
		{
			name:    "bad hex",
			encHex:  `"adsf"`, // s not valid hex
			wantErr: true,
		},
		{
			name:    "too short",
			encHex:  `2`,
			wantErr: true,
		},
		{
			name:    "ok empty",
			encHex:  `""`,
			wantErr: false,
		},
		{
			name: "not quoted (also invalid hex to demo error printing)",
			encHex: `abc
			abc`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := new(Bytes)
			err := b.UnmarshalJSON([]byte(tt.encHex))
			if (err != nil) != tt.wantErr {
				t.Errorf("Bytes.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
