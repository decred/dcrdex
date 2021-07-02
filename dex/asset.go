// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	UnsupportedScriptError = ErrorKind("unsupported script type")

	defaultLockTimeTaker = 8 * time.Hour
	defaultlockTimeMaker = 20 * time.Hour

	secondsPerMinute  int64 = 60
	secondsPerDay           = 24 * 60 * secondsPerMinute
	BondExpiryMainnet       = 30 * secondsPerDay    // 30 days
	BondExpiryTestnet       = 90 * secondsPerMinute // 90 minutes
	BondExpirySimnet        = 4 * secondsPerMinute  // 4 minutes
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

// BondExpiry returns the bond expiry duration in seconds for a given network.
// Once APIVersion reaches BondAPIVersion, clients should use this compiled
// helper function. Until then, bonds are considered experimental and the
// current value should be referenced from config response.
func BondExpiry(net Network) int64 {
	switch net {
	case Mainnet:
		return BondExpiryMainnet
	case Testnet:
		return BondExpiryTestnet
	default: // Regtest, Simnet, other
		return BondExpirySimnet
	}
}

// Network flags passed to asset backends to signify which network to use.
type Network uint8

const (
	Mainnet Network = iota
	Testnet
	Regtest
)

// Simnet is an alias of Regtest.
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
	ID           uint32   `json:"id"`
	Symbol       string   `json:"symbol"`
	Version      uint32   `json:"version"`
	MaxFeeRate   uint64   `json:"maxFeeRate"`
	SwapSize     uint64   `json:"swapSize"`
	SwapSizeBase uint64   `json:"swapSizeBase"`         // = SwapSize for account-based assets
	RedeemSize   uint64   `json:"redeemSize,omitempty"` // Account-based assets only
	SwapConf     uint32   `json:"swapConf"`
	UnitInfo     UnitInfo `json:"unitInfo"`
}

// Denomination is a unit and its conversion factor.
type Denomination struct {
	Unit             string `json:"unit"`
	ConversionFactor uint64 `json:"conversionFactor"`
}

// UnitInfo conveys information about the units and available denominations for
// an asset.
type UnitInfo struct {
	// AtomicUnit is the name associated with the asset's integral unit of
	// measure, e.g. satoshis, atoms, gwei (for DEX purposes).
	AtomicUnit string `json:"atomicUnit"`
	// Conventional is the conventionally-used denomination.
	Conventional Denomination `json:"conventional"`
	// Alternatives lists additionally available Denominations, and can be
	// empty.
	Alternatives []Denomination `json:"denominations"`
}

// ConventionalString converts the quantity to conventional units, and returns
// the formatted float string.
func (ui *UnitInfo) ConventionalString(v uint64) string {
	c := ui.Conventional.ConversionFactor
	prec := int(math.Round(math.Log10(float64(c)))) // Assumes integer powers of 10
	return strconv.FormatFloat(float64(v)/float64(c), 'f', prec, 64)
}

// Token is a generic representation of a token-type asset.
type Token struct {
	// ParentID is the asset ID of the token's parent asset.
	ParentID uint32 `json:"parentID"`
	// Name is the display name of the token asset.
	Name string `json:"name"`
	// UnitInfo is the UnitInfo for the token.
	UnitInfo UnitInfo `json:"unitInfo"`
}

// IntDivUp divides two integers, rounding up, without using floating point
// conversion. This will panic if the denominator is zero, just like regular
// integer division. This function should be used instead of ad hoc solutions or
// being lazy with floating point math.
func IntDivUp(val, div int64) int64 {
	// https://github.com/rust-lang/rust/blob/343889b7234bf786e2bc673029467052f22fca08/library/core/src/num/uint_macros.rs#L2061
	q, rem := val/div, val%div
	if (rem > 0 && div > 0) || (rem < 0 && div < 0) {
		q++
	}
	return q
	// return (val + div - 1) / div
}
