// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// GweiFactor is the amount of wei in one gwei.
	GweiFactor = 1e9
	// MaxBlockInterval is the number of seconds since the last header came
	// in over which we consider the chain to be out of sync.
	MaxBlockInterval = 180
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "gwei",
		Conventional: dex.Denomination{
			Unit:             "ETH",
			ConversionFactor: 1e9,
		},
	}

	// BigGweiFactor is the *big.Int form of the GweiFactor.
	BigGweiFactor = big.NewInt(GweiFactor)

	VersionedGases = map[uint32]*Gases{
		0: v0Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Mainnet: common.Address{},
			dex.Simnet:  common.HexToAddress("0x2f68e723b8989ba1c6a9f03e42f33cb7dc9d606f"),
			dex.Testnet: common.Address{},
		},
	}
)

type Gases struct {
	InitGas, AdditionalInitGas, RedeemGas, AdditionalRedeemGas, RefundGas uint64
}

var v0Gases = &Gases{
	// InitGas is the amount of gas needed to initialize a single
	// ethereum swap.
	InitGas: 135000,
	// AdditionalInitGas is the amount of gas needed to initialize
	// additional swaps in the same transaction.
	AdditionalInitGas: 113000,
	// RedeemGas is the amount of gas it costs to redeem a swap.
	RedeemGas: 63000,
	// AdditionalRedeemGas is the amount of gas needed to redeem
	// additional swaps in the same transaction.
	AdditionalRedeemGas: 32000,
	// RefundGas is the amount of gas it costs to refund a swap.
	RefundGas: 43000,
}

// EncodeContractData packs the contract version and the secret hash into a byte
// slice for communicating a swap's identity.
func EncodeContractData(contractVersion uint32, swapKey [SecretHashSize]byte) []byte {
	b := make([]byte, SecretHashSize+4)
	binary.BigEndian.PutUint32(b[:4], contractVersion)
	copy(b[4:], swapKey[:])
	return b
}

// DecodeContractData unpacks the contract version and secret hash.
func DecodeContractData(data []byte) (contractVersion uint32, swapKey [SecretHashSize]byte, err error) {
	if len(data) != SecretHashSize+4 {
		err = errors.New("invalid swap data")
		return
	}
	contractVersion = binary.BigEndian.Uint32(data[:4])
	copy(swapKey[:], data[4:])
	return
}

// InitGas calculates the gas required for a batch of n inits.
func InitGas(n int, contractVer uint32) uint64 {
	if n == 0 {
		return 0
	}
	g, ok := VersionedGases[contractVer]
	if !ok {
		return math.MaxUint64
	}
	return g.InitGas + (uint64(n)-1)*g.AdditionalInitGas
}

// RedeemGas calculates the gas required for a batch of n redemptions.
func RedeemGas(n int, contractVer uint32) uint64 {
	if n == 0 {
		return 0
	}
	g, ok := VersionedGases[contractVer]
	if !ok {
		return math.MaxUint64
	}
	return g.RedeemGas + (uint64(n)-1)*g.AdditionalRedeemGas
}

// RefundGas calculates the gas required for a refund.
func RefundGas(contractVer uint32) uint64 {
	g, ok := VersionedGases[contractVer]
	if !ok {
		return math.MaxUint64
	}
	return g.RefundGas
}

// GweiToWei converts uint64 Gwei to *big.Int Wei.
func GweiToWei(v uint64) *big.Int {
	return new(big.Int).Mul(big.NewInt(int64(v)), BigGweiFactor)
}

// GweiToWei converts *big.Int Wei to uint64 Gwei.
func WeiToGwei(v *big.Int) uint64 {
	return new(big.Int).Div(v, BigGweiFactor).Uint64()
}

// SwapStep is the state of a swap and corresponds to values in the Solidity
// swap contract.
type SwapStep uint8

// Swap states represent the status of a swap. The default state of a swap is
// SSNone. A swap in status SSNone does not exist. SSInitiated indicates that a
// party has initiated the swap and funds have been sent to the contract.
// SSRedeemed indicates a successful swap where the participant was able to
// redeem with the secret hash. SSRefunded indicates a failed swap, where the
// initiating party refunded their coins after the locktime passed. A swap no
// longer changes states after reaching SSRedeemed or SSRefunded.
const (
	// SSNone indicates that the swap is not initiated. This is the default
	// state of a swap.
	SSNone SwapStep = iota
	// SSInitiated indicates that the swap has been initiated.
	SSInitiated
	// SSRedeemed indicates that the swap was initiated and then redeemed.
	// This is one of two possible end states of a swap.
	SSRedeemed
	// SSRefunded indicates that the swap was initiated and then refunded.
	// This is one of two possible end states of a swap.
	SSRefunded
)

// String satisfies the Stringer interface.
func (ss SwapStep) String() string {
	switch ss {
	case SSNone:
		return "none"
	case SSInitiated:
		return "initiated"
	case SSRedeemed:
		return "redeemed"
	case SSRefunded:
		return "refunded"
	}
	return "unknown"
}

// SwapState is the current state of an in-process swap.
type SwapState struct {
	BlockHeight uint64
	LockTime    time.Time
	Secret      [32]byte
	Initiator   common.Address
	Participant common.Address
	Value       uint64
	State       SwapStep
}
