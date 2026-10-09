// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"strings"
	"testing"
)

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

func Test_validateMeshOptions(t *testing.T) {
	tests := []struct {
		name    string
		cfg     flagsData
		errWant string // substring of the expected error, empty for no error
	}{
		{
			name: "no mesh options",
		},
		{
			name: "full mesh config with surrounding whitespace",
			cfg: flagsData{
				MeshPeerAddr: "  127.0.0.1:7232 \t",
				MeshListen:   "  127.0.0.1:7233 \t",
				ClientAddr:   "  dex.example.com:7232 \t",
				MeshPeerCert: "  /path/rpc.cert \t",
			},
		},
		{
			name: "tls mesh peer without cert",
			cfg: flagsData{
				MeshPeerAddr: "127.0.0.1:7232",
				MeshListen:   "127.0.0.1:7233",
				ClientAddr:   "dex.example.com:7232",
			},
			errWant: "meshpeercert is required for a TLS mesh peer",
		},
		{
			name: "explicit wss mesh peer without cert",
			cfg: flagsData{
				MeshPeerAddr: "wss://127.0.0.1:7232",
				MeshListen:   "127.0.0.1:7233",
				ClientAddr:   "dex.example.com:7232",
			},
			errWant: "meshpeercert is required for a TLS mesh peer",
		},
		{
			name: "plaintext ws mesh peer needs no cert",
			cfg: flagsData{
				MeshPeerAddr: "ws://127.0.0.1:7232",
				MeshListen:   "127.0.0.1:7233",
				ClientAddr:   "dex.example.com:7232",
			},
		},
		{
			name:    "meshlisten without meshpeer",
			cfg:     flagsData{MeshListen: "127.0.0.1:7233"},
			errWant: "meshlisten set but meshpeer is not",
		},
		{
			name:    "clientaddr without meshpeer",
			cfg:     flagsData{ClientAddr: "dex.example.com:7232"},
			errWant: "clientaddr set but meshpeer is not",
		},
		{
			name:    "meshpeercert without meshpeer",
			cfg:     flagsData{MeshPeerCert: "/path/rpc.cert"},
			errWant: "meshpeercert set but meshpeer is not",
		},
		{
			name:    "meshforkreset without meshpeer",
			cfg:     flagsData{MeshForkReset: "41:deadbeef"},
			errWant: "meshforkreset set but meshpeer is not",
		},
		{
			name: "listen and clientaddr without meshpeer",
			cfg: flagsData{
				MeshListen: "127.0.0.1:7233",
				ClientAddr: "dex.example.com:7232",
			},
			errWant: "meshlisten, clientaddr set but meshpeer is not",
		},
		{
			name:    "whitespace-only meshpeer with meshlisten",
			cfg:     flagsData{MeshPeerAddr: "  ", MeshListen: "127.0.0.1:7233"},
			errWant: "meshlisten set but meshpeer is not",
		},
		{
			name: "whitespace-only meshforkreset counts as unset",
			cfg:  flagsData{MeshForkReset: "  "},
		},
		{
			name: "blank certificate",
			cfg: flagsData{
				MeshPeerAddr: "127.0.0.1:7232",
				MeshListen:   "127.0.0.1:7233",
				ClientAddr:   "dex.example.com:7232",
				MeshPeerCert: "  ",
			},
			errWant: "meshpeercert is required for a TLS mesh peer",
		},
		{
			name:    "meshpeer without meshlisten",
			cfg:     flagsData{MeshPeerAddr: "127.0.0.1:7232", ClientAddr: "dex.example.com:7232", MeshPeerCert: "/path/rpc.cert"},
			errWant: "meshpeer requires meshlisten",
		},
		{
			name:    "meshpeer without clientaddr",
			cfg:     flagsData{MeshPeerAddr: "127.0.0.1:7232", MeshListen: "127.0.0.1:7233", MeshPeerCert: "/path/rpc.cert"},
			errWant: "meshpeer requires clientaddr",
		},
		{
			name:    "meshpeer without meshlisten or clientaddr",
			cfg:     flagsData{MeshPeerAddr: "127.0.0.1:7232", MeshPeerCert: "/path/rpc.cert"},
			errWant: "meshpeer requires meshlisten, clientaddr",
		},
		{
			name: "noresumeswaps with meshpeer",
			cfg: flagsData{
				MeshPeerAddr:  "127.0.0.1:7232",
				MeshListen:    "127.0.0.1:7233",
				ClientAddr:    "dex.example.com:7232",
				MeshPeerCert:  "/path/rpc.cert",
				NoResumeSwaps: true,
			},
			errWant: "noresumeswaps cannot be used with meshpeer",
		},
		{
			name: "noresumeswaps without meshpeer",
			cfg:  flagsData{NoResumeSwaps: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			err := validateMeshOptions(&cfg)
			if tt.errWant == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for name, value := range map[string]struct{ got, original string }{
					"meshpeer":     {cfg.MeshPeerAddr, tt.cfg.MeshPeerAddr},
					"meshlisten":   {cfg.MeshListen, tt.cfg.MeshListen},
					"clientaddr":   {cfg.ClientAddr, tt.cfg.ClientAddr},
					"meshpeercert": {cfg.MeshPeerCert, tt.cfg.MeshPeerCert},
				} {
					if want := strings.TrimSpace(value.original); value.got != want {
						t.Errorf("%s = %q, want %q", name, value.got, want)
					}
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errWant)
			}
			if !strings.Contains(err.Error(), tt.errWant) {
				t.Fatalf("error %q does not contain %q", err, tt.errWant)
			}
		})
	}
}
