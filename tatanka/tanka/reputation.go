// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tanka

import (
	"time"

	"decred.org/dcrdex/dex"
)

const (
	MaxReputationEntries = 100
	TierIncrement        = 20
	MaxScore             = MaxReputationEntries
	EpochLength          = time.Second * 15
)

type Reputation struct {
	Score  int16
	Points []int8
}

type Bond struct {
	PeerID     PeerID    `json:"peerID"`
	AssetID    uint32    `json:"assetID"`
	CoinID     dex.Bytes `json:"coinID"`
	Strength   uint64    `json:"strength"`
	Expiration time.Time `json:"expiration"`
}

type HTLCAudit struct{}
