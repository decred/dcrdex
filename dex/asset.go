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
	lockTimeTaker          = 24 * time.Hour
	lockTimeMaker          = 48 * time.Hour
)

var (
	// These variables are defined to enable setting custom locktime values that
	// will be used in place of the `lockTimeTaker` and `lockTimeMaker` constants
	// defined above, _when_ running on a test network (testnet or regtest).
	// Values for both variables may be set at build time using linker flags e.g:
	// go build -ldflags "-X 'decred.org/dcrdex/dex.testLockTimeTaker=10m' \
	// -X 'decred.org/dcrdex/dex.testLockTimeMaker=20m'"
	// Same values should be set when building server and client binaries.
	testLockTimeTaker string
	testLockTimeMaker string
)

// LockTimeTaker returns the taker locktime value that should be used by both
// client and server for the specified network. Mainnet uses a constant value
// while test networks support setting a custom value during build.
func LockTimeTaker(network Network) time.Duration {
	if network == Mainnet || testLockTimeTaker == "" {
		return lockTimeTaker
	}
	testLockTime, _ := time.ParseDuration(testLockTimeTaker)
	if testLockTime.Seconds() == 0 {
		// Use default value if invalid or zero locktime is specifed.
		return lockTimeTaker
	}
	return testLockTime
}

// LockTimeMaker returns the taker locktime value that should be used by both
// client and server for the specified network. Mainnet uses a constant value
// while test networks support setting a custom value during build.
func LockTimeMaker(network Network) time.Duration {
	if network == Mainnet || testLockTimeMaker == "" {
		return lockTimeMaker
	}
	testLockTime, _ := time.ParseDuration(testLockTimeMaker)
	if testLockTime.Seconds() == 0 {
		// Use default value if invalid or zero locktime is specifed.
		return lockTimeMaker
	}
	return testLockTime
}

// Network flags passed to asset backends to signify which network to use.
type Network uint8

const (
	Mainnet Network = iota
	Testnet
	Regtest
)

// The DEX recognizes only three networks. Simnet is an alias of Regtest.
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
