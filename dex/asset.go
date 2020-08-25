// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"strings"
	"time"
)

const (
	UnsupportedScriptError = ErrorKind("unsupported script type")

	defaultLockTimeTaker = 8 * time.Hour
	defaultlockTimeMaker = 20 * time.Hour
)

var (
	// These string variables are defined to enable setting custom locktime values
	// that may be used instead of `DefaultLockTimeTaker` and `DefaultLockTimeMaker`
	// IF running on a test network (testnet or regtest) AND both values are set to
	// valid duration strings.
	// Values for both variables may be set at build time using linker flags e.g:
	// go build -ldflags "-X 'decred.org/dcrdex/dex.testLockTimeTaker=10m' \
	// -X 'decred.org/dcrdex/dex.testLockTimeMaker=20m'"
	// Same values should be set when building server and client binaries.
	testLockTimeTaker string
	testLockTimeMaker string

	testLockTime struct {
		taker time.Duration
		maker time.Duration
	}
)

// Set test locktime values on init. Panics if invalid or 0-second duration
// strings are provided.
func init() {
	if testLockTimeTaker == "" && testLockTimeMaker == "" {
		testLockTime.taker, testLockTime.maker = defaultLockTimeTaker, defaultlockTimeMaker
		return
	}
	testLockTime.taker, _ = time.ParseDuration(testLockTimeTaker)
	if testLockTime.taker.Seconds() == 0 {
		panic(fmt.Sprintf("invalid value for testLockTimeTaker: %q", testLockTimeTaker))
	}
	testLockTime.maker, _ = time.ParseDuration(testLockTimeMaker)
	if testLockTime.maker.Seconds() == 0 {
		panic(fmt.Sprintf("invalid value for testLockTimeMaker: %q", testLockTimeMaker))
	}
}

// LockTimeTaker returns the taker locktime value that should be used by both
// client and server for the specified network. Mainnet uses a constant value
// while test networks support setting a custom value during build.
func LockTimeTaker(network Network) time.Duration {
	if network == Mainnet {
		return defaultLockTimeTaker
	}
	return testLockTime.taker
}

// LockTimeMaker returns the maker locktime value that should be used by both
// client and server for the specified network. Mainnet uses a constant value
// while test networks support setting a custom value during build.
func LockTimeMaker(network Network) time.Duration {
	if network == Mainnet {
		return defaultlockTimeMaker
	}
	return testLockTime.maker
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
	ID           uint32 `json:"id"`
	Symbol       string `json:"symbol"`
	LotSize      uint64 `json:"lotSize"`
	RateStep     uint64 `json:"rateStep"`
	MaxFeeRate   uint64 `json:"maxFeeRate"`
	SwapSize     uint64 `json:"swapSize"`
	SwapSizeBase uint64 `json:"swapSizeBase"`
	SwapConf     uint32 `json:"swapConf"`
}
