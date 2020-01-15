// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"strings"

	"github.com/decred/slog"
)

type Error string

func (err Error) Error() string { return string(err) }

const (
	UnsupportedScriptError = Error("unsupported script type")
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

// Every backend constructor will accept a Logger. All logging should take place
// through the provided logger.
type Logger = slog.Logger

// Asset is the configurable asset variables.
type Asset struct {
	ID       uint32
	Symbol   string
	LotSize  uint64
	RateStep uint64
	FeeRate  uint64
	SwapSize uint64
	SwapConf uint32
	FundConf uint32
}
