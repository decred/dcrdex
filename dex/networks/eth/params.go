// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"time"

	"decred.org/dcrdex/dex"
	v0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
)

const (
	// GweiFactor is the amount of wei in one gwei.
	GweiFactor = 1e9
	// MaxBlockInterval is the number of seconds since the last header came
	// in over which we consider the chain to be out of sync.
	MaxBlockInterval = 180
	EthBipID         = 60
	MinGasTipCap     = 2 //gwei
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "gwei",
		Conventional: dex.Denomination{
			Unit:             "ETH",
			ConversionFactor: 1e9,
		},
	}

	VersionedGases = map[uint32]*Gases{
		0: v0Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Mainnet: common.Address{},
			dex.Simnet:  common.HexToAddress("0x2f68e723b8989ba1c6a9f03e42f33cb7dc9d606f"),
			dex.Testnet: common.HexToAddress("0xa483b6166dA8Da6748B29Af35f96C4F9388c456C"),
		},
	}
)

var v0Gases = &Gases{
	Swap:      135000,
	SwapAdd:   113000,
	Redeem:    63000,
	RedeemAdd: 32000,
	Refund:    43000,
}

// LoadGenesisFile loads a Genesis config from a json file.
func LoadGenesisFile(genesisFile string) (*core.Genesis, error) {
	fid, err := os.Open(genesisFile)
	if err != nil {
		return nil, err
	}
	defer fid.Close()

	var genesis core.Genesis
	err = json.NewDecoder(fid).Decode(&genesis)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal simnet genesis: %v", err)
	}
	return &genesis, nil
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
	return g.SwapN(n)
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
	return g.RedeemN(n)
}

// RefundGas calculates the gas required for a refund.
func RefundGas(contractVer uint32) uint64 {
	g, ok := VersionedGases[contractVer]
	if !ok {
		return math.MaxUint64
	}
	return g.Refund
}

// GweiToWei converts uint64 Gwei to *big.Int Wei.
func GweiToWei(v uint64) *big.Int {
	return new(big.Int).Mul(big.NewInt(int64(v)), big.NewInt(GweiFactor))
}

// WeiToGwei converts *big.Int Wei to uint64 Gwei. If v is determined to be
// unsuitable for a uint64, zero is returned.
func WeiToGwei(v *big.Int) uint64 {
	vGwei := new(big.Int).Div(v, big.NewInt(GweiFactor))
	if vGwei.IsUint64() {
		return vGwei.Uint64()
	}
	return 0
}

// WeiToGweiUint64 converts a *big.Int in wei (1e18 unit) to gwei (1e9 unit) as
// a uint64. Errors if the amount of gwei is too big to fit fully into a uint64.
func WeiToGweiUint64(wei *big.Int) (uint64, error) {
	if wei.Cmp(new(big.Int)) == -1 {
		return 0, fmt.Errorf("wei must be non-negative")
	}
	gweiFactorBig := big.NewInt(GweiFactor)
	gwei := new(big.Int).Div(wei, gweiFactorBig)
	if !gwei.IsUint64() {
		return 0, fmt.Errorf("suggest gas price %v gwei is too big for a uint64", wei)
	}
	return gwei.Uint64(), nil
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

// Initiation is the data used to initiate a swap.
type Initiation struct {
	LockTime    time.Time
	SecretHash  [32]byte
	Participant common.Address
	Value       uint64 // gwei
}

// Redemption is the data used to redeem a swap.
type Redemption struct {
	Secret     [32]byte
	SecretHash [32]byte
}

var testTokenID, _ = dex.BipSymbolID("dextt.eth")

type Gases struct {
	// Approve is the amount of gas needed to approve the swap contract for
	// transferring tokens.
	Approve uint64 `json:"approve"`
	// Transfer is the amount of gas needed to transfer tokens.
	Transfer uint64 `json:"transfer"`
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
}

// SwapN calculates the gas needed to initiate n swaps.
func (g *Gases) SwapN(n int) uint64 {
	if n <= 0 {
		return 0
	}
	return g.Swap + g.SwapAdd*(uint64(n)-1)
}

// RedeemN calculates the gas needed to redeem n swaps.
func (g *Gases) RedeemN(n int) uint64 {
	if n <= 0 {
		return 0
	}
	return g.Redeem + g.RedeemAdd*(uint64(n)-1)
}

// SwapStateFromV0 converts a v0.ETHSwapSwap to a *SwapState.
func SwapStateFromV0(state *v0.ETHSwapSwap) *SwapState {
	var blockTime int64
	if state.RefundBlockTimestamp.IsInt64() {
		blockTime = state.RefundBlockTimestamp.Int64()
	}
	return &SwapState{
		BlockHeight: state.InitBlockNumber.Uint64(),
		LockTime:    time.Unix(blockTime, 0),
		Secret:      state.Secret,
		Initiator:   state.Initiator,
		Participant: state.Participant,
		Value:       WeiToGwei(state.Value),
		State:       SwapStep(state.State),
	}
}
