// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
)

func TestMeshClientEndpoint(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "rpc.cert")
	cert := []byte("cert")
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	missingCert := filepath.Join(t.TempDir(), "missing.cert")
	tests := []struct {
		name     string
		address  string
		noTLS    bool
		certPath string
		wantHost string
		wantCert []byte
		wantErr  bool
	}{
		{
			name: "TLS hostname", address: "DEX.EXAMPLE.COM:7232",
			certPath: certPath, wantHost: "DEX.EXAMPLE.COM:7232", wantCert: cert,
		},
		{
			name: "onion URL", address: "wss://ABC.ONION/ws",
			certPath: missingCert, wantHost: "wss://ABC.ONION/ws",
		},
		{
			name: "plaintext", address: "dex.example.com:7232", noTLS: true,
			certPath: missingCert, wantHost: "dex.example.com:7232",
		},
		{
			name: "URL with whitespace", address: " wss://dex.example.com/ws ",
			certPath: certPath, wantHost: "wss://dex.example.com/ws", wantCert: cert,
		},
		{
			name: "onion substring in ordinary hostname", address: "dex.onion.example.com:7232",
			certPath: certPath, wantHost: "dex.onion.example.com:7232", wantCert: cert,
		},
		{
			name: "missing certificate", address: "dex.example.com:7232",
			certPath: missingCert, wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, gotCert, err := meshClientEndpoint(tt.address, tt.noTLS, tt.certPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("meshClientEndpoint error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if host != tt.wantHost || !bytes.Equal(gotCert, tt.wantCert) {
				t.Fatalf("endpoint = %q/%x, want %q/%x", host, gotCert, tt.wantHost, tt.wantCert)
			}
		})
	}
}

func TestPublishMeshClientEndpoints(t *testing.T) {
	own := &msgjson.MeshEndpoint{Host: "self.example:7232", Cert: []byte{4, 5, 6}}
	peer := &msgjson.MeshEndpoint{Host: "peer.example:7232", Cert: []byte{1, 2, 3}}
	changedPeer := &msgjson.MeshEndpoint{Host: peer.Host, Cert: []byte{7, 8, 9}}
	both := []*msgjson.MeshEndpoint{own, peer}
	tests := []struct {
		name       string
		previous   []*msgjson.MeshEndpoint
		peer       *msgjson.MeshEndpoint
		want       []*msgjson.MeshEndpoint
		wantNotify bool
	}{
		{
			name: "publish both endpoints", peer: peer,
			want: both, wantNotify: true,
		},
		{
			name: "unchanged endpoints", previous: both, peer: peer,
			want: both,
		},
		{
			name: "changed peer certificate", previous: both, peer: changedPeer,
			want: []*msgjson.MeshEndpoint{own, changedPeer}, wantNotify: true,
		},
		{
			name: "duplicate peer address", previous: both, peer: own,
			want: []*msgjson.MeshEndpoint{own}, wantNotify: true,
		},
		{
			name: "peer removed", previous: both, peer: &msgjson.MeshEndpoint{},
			want: []*msgjson.MeshEndpoint{own}, wantNotify: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var broadcasts []*msgjson.Message
			dm := &DEX{
				configResp: &configResponse{
					configMsg: &msgjson.ConfigResult{MeshEndpoints: tt.previous},
				},
				broadcast: func(msg *msgjson.Message) {
					broadcasts = append(broadcasts, msg)
				},
			}
			dm.configResp.remarshal()
			dm.publishMeshClientEndpoints(own.Host, own.Cert, tt.peer.Host, tt.peer.Cert)

			if got := dm.configResp.configMsg.MeshEndpoints; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("stored endpoints = %+v, want %+v", got, tt.want)
			}
			var config msgjson.ConfigResult
			if err := json.Unmarshal(dm.configResp.configEnc, &config); err != nil {
				t.Fatalf("config unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(config.MeshEndpoints, tt.want) {
				t.Fatalf("encoded endpoints = %+v, want %+v", config.MeshEndpoints, tt.want)
			}

			wantBroadcasts := 0
			if tt.wantNotify {
				wantBroadcasts = 1
			}
			if len(broadcasts) != wantBroadcasts {
				t.Fatalf("broadcast count = %d, want %d", len(broadcasts), wantBroadcasts)
			}
			if !tt.wantNotify {
				return
			}
			msg := broadcasts[0]
			if msg.Type != msgjson.Notification || msg.Route != msgjson.MeshEndpointsRoute {
				t.Fatalf("broadcast type/route = %d/%q, want notification/%q", msg.Type, msg.Route, msgjson.MeshEndpointsRoute)
			}
			var note msgjson.MeshEndpointsNotification
			if err := msg.Unmarshal(&note); err != nil {
				t.Fatalf("notification unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(note.MeshEndpoints, tt.want) {
				t.Fatalf("notification endpoints = %+v, want %+v", note.MeshEndpoints, tt.want)
			}
		})
	}
}
