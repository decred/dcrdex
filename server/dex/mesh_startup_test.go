// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"decred.org/dcrdex/server/db"
)

type startupStorage struct {
	db.DEXArchivist
	frontier    *db.EventLogPosition
	empty       bool
	frontierErr error
	stateErr    error
	wipeErr     error
	wipeCalls   int
}

func (s *startupStorage) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	return s.frontier, s.frontierErr
}

func (s *startupStorage) HasNoEventSourcedState(context.Context) (bool, error) {
	return s.empty, s.stateErr
}

func (s *startupStorage) WipeEventSourcedState(context.Context) error {
	s.wipeCalls++
	return s.wipeErr
}

func TestMeshStartupMaintenance(t *testing.T) {
	frontier := &db.EventLogPosition{Seq: 41, TipHash: []byte{1, 2, 3, 4, 5, 6, 7, 8}}
	const token = "41:0102030405060708"
	peer := &MeshConfig{PeerAddr: "peer.example:7233"}
	storageErr := errors.New("storage failure")
	tests := []struct {
		name      string
		storage   startupStorage
		meshCfg   *MeshConfig
		token     string
		wantErr   string
		wantWipes int
	}{
		{
			name:    "existing history without reset",
			storage: startupStorage{frontier: frontier},
		},
		{
			name:    "empty database",
			storage: startupStorage{frontier: &db.EventLogPosition{}, empty: true},
		},
		{
			name:    "state without event log",
			storage: startupStorage{frontier: &db.EventLogPosition{}},
			meshCfg: peer, token: token,
			wantErr: "event-sourced state but its event log is empty",
		},
		{
			name:    "mismatched reset token",
			storage: startupStorage{frontier: frontier},
			meshCfg: peer, token: "40:0102030405060708",
			wantErr: "does not match the current frontier",
		},
		{
			name:    "reset without mesh configuration",
			storage: startupStorage{frontier: frontier}, token: token,
			wantErr: "requires a configured mesh peer",
		},
		{
			name:    "reset without peer address",
			storage: startupStorage{frontier: frontier},
			meshCfg: &MeshConfig{}, token: token,
			wantErr: "requires a configured mesh peer",
		},
		{
			name:    "valid reset",
			storage: startupStorage{frontier: frontier},
			meshCfg: peer, token: token, wantWipes: 1,
		},
		{
			name:    "frontier lookup failure",
			storage: startupStorage{frontierErr: storageErr},
			meshCfg: peer, token: token, wantErr: "event log frontier: storage failure",
		},
		{
			name:    "state check failure",
			storage: startupStorage{frontier: &db.EventLogPosition{}, stateErr: storageErr},
			meshCfg: peer, token: token, wantErr: "event-sourced state check: storage failure",
		},
		{
			name:    "wipe failure",
			storage: startupStorage{frontier: frontier, wipeErr: storageErr},
			meshCfg: peer, token: token, wantWipes: 1, wantErr: "wipe failed: storage failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &DexConf{MeshCfg: tt.meshCfg, MeshForkReset: tt.token}
			err := meshStartupMaintenance(context.Background(), cfg, &tt.storage)
			if tt.storage.wipeCalls != tt.wantWipes {
				t.Fatalf("wipe calls = %d, want %d", tt.storage.wipeCalls, tt.wantWipes)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
