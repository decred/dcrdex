// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import "github.com/decred/slog"

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
