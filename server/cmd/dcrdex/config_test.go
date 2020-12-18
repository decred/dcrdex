// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import "testing"

const (
	defaultHost = "127.0.0.1"
	defaultPort = "17232"
)

func Test_normalizeNetworkAddress(t *testing.T) {
	tests := []struct {
		listen  string
		want    string
		wantErr bool
	}{
		{
			listen: "[::1]",
			want:   "[::1]:17232",
		},
		{
			listen: "[::]:",
			want:   "[::]:17232",
		},
		{
			listen: "",
			want:   "127.0.0.1:17232",
		},
		{
			listen: "127.0.0.2",
			want:   "127.0.0.2:17232",
		},
		{
			listen: ":7222",
			want:   "127.0.0.1:7222",
		},
	}
	for _, tt := range tests {
		t.Run(tt.listen, func(t *testing.T) {
			got, err := normalizeNetworkAddress(tt.listen, defaultHost, defaultPort)
			if (err != nil) != tt.wantErr {
				t.Errorf("normalizeNetworkAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("normalizeNetworkAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}
