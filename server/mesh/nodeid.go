// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	nodeIDDirName  = "mesh"
	nodeIDFileName = "nodeid"
	nodeIDSize     = 16
)

// loadOrCreateNodeID loads a persisted node ID from the DEX data directory or
// creates and stores a new one if none exists yet.
func loadOrCreateNodeID(dataDir string) (string, error) {
	meshDir := filepath.Join(dataDir, nodeIDDirName)
	if err := os.MkdirAll(meshDir, 0700); err != nil {
		return "", fmt.Errorf("create mesh dir: %w", err)
	}

	nodeIDPath := filepath.Join(meshDir, nodeIDFileName)
	if nodeIDB, err := os.ReadFile(nodeIDPath); err == nil {
		nodeID := strings.TrimSpace(string(nodeIDB))
		if err := validateNodeID(nodeID); err != nil {
			return "", fmt.Errorf("invalid stored node ID in %s: %w", nodeIDPath, err)
		}
		return nodeID, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read node ID file: %w", err)
	}

	nodeID, err := newNodeID()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(nodeIDPath, []byte(nodeID+"\n"), 0600); err != nil {
		return "", fmt.Errorf("write node ID file: %w", err)
	}
	return nodeID, nil
}

// newNodeID returns a new random node ID of nodeIDSize bytes, hex encoded.
func newNodeID() (string, error) {
	b := make([]byte, nodeIDSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate node ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// validateNodeID checks a node ID from the data directory or from a peer.
func validateNodeID(nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("empty node ID")
	}
	if strings.ContainsAny(nodeID, " \t\r\n") {
		return fmt.Errorf("node ID may not contain whitespace")
	}
	return nil
}
