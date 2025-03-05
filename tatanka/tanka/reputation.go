// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tanka

import (
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/crypto/blake256"
)

const (
	MaxSubScore          = 256
	MaxReputationEntries = 100
	TierIncrement        = 20
	MaxAggregateScore    = MaxReputationEntries * MaxSubScore
	EpochLength          = time.Second * 15
)

type Reputation struct {
	Score int64
	Depth uint64
}

type Bond struct {
	PeerID     PeerID    `json:"peerID"`
	AssetID    uint32    `json:"assetID"`
	CoinID     dex.Bytes `json:"coinID"`
	Strength   uint64    `json:"strength"`
	Expiration time.Time `json:"expiration"`
	// TODO (buck): Switch to Maturation.
	Maturation time.Time `json:"maturation"`
}

func (bond *Bond) ID() ID32 {
	buf := make([]byte, PeerIDLength+4 /* asset ID */ +len(bond.CoinID))
	copy(buf[:PeerIDLength], bond.PeerID[:])
	copy(buf[PeerIDLength:PeerIDLength+4], encode.Uint32Bytes(bond.AssetID))
	copy(buf[PeerIDLength+4:], bond.CoinID[:])
	return blake256.Sum256(buf)

}

type HTLCAudit struct{}
