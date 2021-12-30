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

// Gases is a the gas required for various operations.
type Gases struct {
	// Swap is the amount of gas needed to initialize a single ethereum swap.
	Swap uint64 `json:"swap"`
	// SwapAdd is the amount of gas needed to initialize additional swaps in
	// the same transaction.
	SwapAdd uint64 `json:"swapAdd"`
	// Redeem is the amount of gas it costs to redeem a swap.
	Redeem uint64 `json:"redeem"`
	// RedeemAdd is the amount of gas needed to redeem additional swaps in the
	// same transaction.
	RedeemAdd uint64 `json:"redeemAdd"`
	// Refund is the amount of gas needed to refund a swap.
	Refund uint64 `json:"refund"`
	// Approve is the amount of gas needed to approve the swap contract for
	// transfering tokens.
	Approve uint64 `json:"approve"`
	// Transfer is the amount of gas needed to transfer tokens.
	Transfer uint64 `json:"transfer"`
}

func (g *Gases) SwapN(n int) uint64 {
	if n <= 0 {
		return 0
	}
	return g.Swap + g.SwapAdd*(uint64(n)-1)
}

func (g *Gases) RedeemN(n int) uint64 {
	if n <= 0 {
		return 0
	}
	return g.Redeem + g.RedeemAdd*(uint64(n)-1)
}

// Token is a generic representation of a token-type asset.
type Token struct {
	ParentID     uint32                      `json:"parentID"`
	Name         string                      `json:"name"`
	UnitInfo     UnitInfo                    `json:"unitInfo"`
	Gas          Gases                       `json:"gas"`
	NetAddresses map[Network]*TokenAddresses `json:"netAddrs"`
}

// TokenAddresses are the addresses associated with the token and it's versioned
// swap contracts.
type TokenAddresses struct {
	Address       [20]byte            `json:"address"` // Will marshal as array of ints [0, 254, 12, ...]
	SwapContracts map[uint32][20]byte `json:"swapContracts"`
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
