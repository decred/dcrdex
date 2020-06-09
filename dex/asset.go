// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"strings"
	"time"
)

type Error string

func (err Error) Error() string { return string(err) }

const (
	UnsupportedScriptError = Error("unsupported script type")
	LockTimeTaker          = 24 * time.Hour
	LockTimeMaker          = 48 * time.Hour
)

// Network flags passed to asset backends to signify which network to use.
type Network uint8

const (
	Mainnet Network = iota
	Testnet
	Regtest
)

// The DEX recognizes only three networks. Simnet is a alias of Regtest.
const Simnet = Regtest

// String returns the string representation of a Network.
func (n Network) String() string {
	switch n {
	case Mainnet:
		return "mainnet"
	case Testnet:
		return "testnet"
	case Simnet:
		return "simnet"
	}
	return ""
}

// NetFromString returns the Network for the given network name.
func NetFromString(net string) (Network, error) {
	switch strings.ToLower(net) {
	case "mainnet":
		return Mainnet, nil
	case "testnet":
		return Testnet, nil
	case "regtest", "regnet", "simnet":
		return Simnet, nil
	}
	return 255, fmt.Errorf("unknown network %s", net)
}

// Asset is the configurable asset variables.
type Asset struct {
	ID       uint32 `json:"id"`
	Symbol   string `json:"symbol"`
	LotSize  uint64 `json:"lotSize"`
	RateStep uint64 `json:"rateStep"`
	FeeRate  uint64 `json:"feeRate"`
	SwapSize uint64 `json:"swapSize"`
	SwapConf uint32 `json:"swapConf"`
	FundConf uint32 `json:"fundConf"`
}
