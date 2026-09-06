// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CompatAsset is an asset setting that must be identical on both peers.
type CompatAsset struct {
	ID         uint32 `json:"id"`
	Symbol     string `json:"symbol"`
	MaxFeeRate uint64 `json:"maxFeeRate"`
	SwapConf   uint32 `json:"swapConf"`
	RegFee     uint64 `json:"regFee"`
	RegConfs   uint32 `json:"regConfs"`
	RegXPub    string `json:"regXPub,omitempty"`
	BondAmt    uint64 `json:"bondAmt"`
	BondConfs  uint32 `json:"bondConfs"`
}

// CompatMarket is a market setting that must be identical on both peers.
type CompatMarket struct {
	Name                   string  `json:"name"`
	Base                   uint32  `json:"base"`
	Quote                  uint32  `json:"quote"`
	LotSize                uint64  `json:"lotSize"`
	ParcelSize             uint32  `json:"parcelSize"`
	RateStep               uint64  `json:"rateStep"`
	EpochDuration          uint64  `json:"epochDuration"`
	MarketBuyBuffer        float64 `json:"marketBuyBuffer"`
	MaxUserCancelsPerEpoch uint32  `json:"maxUserCancelsPerEpoch"`
}

// meshProtocolVersion identifies the mesh wire protocol.
const meshProtocolVersion uint32 = 0

// CompatConfig is the server config that must be identical on both peers.
type CompatConfig struct {
	Network            string `json:"network"`
	APIVersion         uint16 `json:"apiVersion"`
	EventSchemaVersion uint32 `json:"eventSchemaVersion"`
	// MeshProtocolVersion identifies the mesh wire protocol. NewCompatSnapshot
	// pins it to this package's constant; any caller-supplied value will error
	// in NewCompatSnapshot.
	MeshProtocolVersion uint32         `json:"meshProtocolVersion"`
	BroadcastTimeoutMS  uint64         `json:"broadcastTimeoutMs"`
	TxWaitExpirationMS  uint64         `json:"txWaitExpirationMs"`
	CancelThreshold     float64        `json:"cancelThreshold"`
	FreeCancels         bool           `json:"freeCancels"`
	PenaltyThreshold    uint32         `json:"penaltyThreshold"`
	Assets              []CompatAsset  `json:"assets"`
	Markets             []CompatMarket `json:"markets"`
}

// CompatSnapshot is the hashed CompatConfig compared during handshake.
type CompatSnapshot struct {
	Config *CompatConfig
	Hash   [32]byte
}

// NewCompatSnapshot normalizes and hashes cfg for handshake.
func NewCompatSnapshot(cfg CompatConfig) (*CompatSnapshot, error) {
	if cfg.MeshProtocolVersion != 0 {
		return nil, fmt.Errorf("mesh protocol version must be 0")
	}
	normalized := normalizeCompatConfig(cfg)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal compatibility config: %w", err)
	}
	return &CompatSnapshot{
		Config: &normalized,
		Hash:   sha256.Sum256(encoded),
	}, nil
}

// HashString returns the compatibility hash as hex for logging.
func (s *CompatSnapshot) HashString() string {
	if s == nil {
		return ""
	}
	return hex.EncodeToString(s.Hash[:])
}

func normalizeCompatConfig(cfg CompatConfig) CompatConfig {
	normalized := CompatConfig{
		Network:             normalizeName(cfg.Network),
		APIVersion:          cfg.APIVersion,
		EventSchemaVersion:  cfg.EventSchemaVersion,
		MeshProtocolVersion: meshProtocolVersion,
		BroadcastTimeoutMS:  cfg.BroadcastTimeoutMS,
		TxWaitExpirationMS:  cfg.TxWaitExpirationMS,
		CancelThreshold:     cfg.CancelThreshold,
		FreeCancels:         cfg.FreeCancels,
		PenaltyThreshold:    cfg.PenaltyThreshold,
		Assets:              make([]CompatAsset, len(cfg.Assets)),
		Markets:             make([]CompatMarket, len(cfg.Markets)),
	}

	for i, a := range cfg.Assets {
		a.Symbol = normalizeName(a.Symbol)
		normalized.Assets[i] = a
	}
	for i, m := range cfg.Markets {
		m.Name = normalizeName(m.Name)
		normalized.Markets[i] = m
	}

	sort.Slice(normalized.Assets, func(i, j int) bool {
		if normalized.Assets[i].ID == normalized.Assets[j].ID {
			return normalized.Assets[i].Symbol < normalized.Assets[j].Symbol
		}
		return normalized.Assets[i].ID < normalized.Assets[j].ID
	})
	sort.Slice(normalized.Markets, func(i, j int) bool {
		return normalized.Markets[i].Name < normalized.Markets[j].Name
	})

	return normalized
}

func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
