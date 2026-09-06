// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateNodeID(t *testing.T) {
	dataDir := t.TempDir()

	nodeID1, err := loadOrCreateNodeID(dataDir)
	if err != nil {
		t.Fatalf("loadOrCreateNodeID error: %v", err)
	}
	if nodeID1 == "" {
		t.Fatalf("empty node ID")
	}

	nodeID2, err := loadOrCreateNodeID(dataDir)
	if err != nil {
		t.Fatalf("loadOrCreateNodeID second call error: %v", err)
	}
	if nodeID1 != nodeID2 {
		t.Fatalf("node ID not persisted: %q != %q", nodeID1, nodeID2)
	}
}

func TestLoadOrCreateNodeIDRejectsInvalidStoredValue(t *testing.T) {
	dataDir := t.TempDir()
	meshDir := filepath.Join(dataDir, nodeIDDirName)
	if err := os.MkdirAll(meshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meshDir, nodeIDFileName), []byte("bad id\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadOrCreateNodeID(dataDir); err == nil {
		t.Fatalf("expected error for invalid stored node ID")
	}
}
