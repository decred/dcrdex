// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"time"

	"decred.org/dcrdex/dex"
	v0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
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

// These are the chain IDs of the various Ethereum network supported.
const (
	MainnetChainID = 1
	TestnetChainID = 11155111 // Sepolia
	SimnetChainID  = 1337     // see dex/testing/eth/harness.sh
)

var (
	// ChainIDs is a map of the network name to it's chain ID.
	ChainIDs = map[dex.Network]int64{
		dex.Mainnet: MainnetChainID,
		dex.Testnet: TestnetChainID,
		dex.Simnet:  SimnetChainID,
	}

	UnitInfo = dex.UnitInfo{
		AtomicUnit: "gwei",
		Conventional: dex.Denomination{
			Unit:             "ETH",
			ConversionFactor: 1e9,
		},
		Alternatives: []dex.Denomination{
			{
				Unit:             "Szabos",
				ConversionFactor: 1e6,
			},
			{
				Unit:             "Finneys",
				ConversionFactor: 1e3,
			},
		},
		FeeRateDenom: "gas",
	}

	VersionedGases = map[uint32]*Gases{
		0: v0Gases,
		1: v1Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Mainnet: common.HexToAddress("0x8C17e4968B6903E1601be82Ca989c5B5E2c7b400"),
			dex.Testnet: common.HexToAddress("0x73bc803A2604b2c58B8680c3CE1b14489842EF16"), // tx 0xb24b44beebc0e34fa57bd9f08f9aaf70f40c654f3ddbe0b15dd942ee23ce02f4
			dex.Simnet:  common.HexToAddress("0x2f68e723b8989ba1c6a9f03e42f33cb7dc9d606f"),
		},
		1: {
			dex.Mainnet: common.HexToAddress("0xa958d5B8a3a29E3f5f41742Fbb939A0dd93EB418"), // tx 0x4adf0314237c454acee1f8d33e97f84126af612245cad0794471693f0906610e
			dex.Testnet: common.HexToAddress("0x9CDe3c347021F0AA63E2780dAD867B5949c5E083"), // tx 0x90f18e70121598a48fc49a5d5b0328358eb34441e2c5dee439dda2dfc7bf3dd8
			dex.Simnet:  common.HexToAddress("0x2f68e723b8989ba1c6a9f03e42f33cb7dc9d606f"),
		},
	}

	MultiBalanceAddresses = map[dex.Network]common.Address{
		dex.Mainnet: common.HexToAddress("0x73bc803A2604b2c58B8680c3CE1b14489842EF16"), // tx 0xaf6cb861578c0ded0750397d7e044a7dd86c94aa47211d02188e146a2424dda4
		dex.Testnet: common.HexToAddress("0x8Bd6F6dBe69588D94953EE289Fd3E1db3e8dB43D"), // tx 0x46a416344927a8d1f33865374e9b9e824249980da8d34f6c3214a1ee036ca5fe
	}
)

var v0Gases = &Gases{
	Swap:      174500, // 134,500 actual -- https://goerli.etherscan.io/tx/0xa17b6edeaf79791b5fc9232dc05a56d43f3a67845f3248e763b77162fae9b181, verified on mainnet
	SwapAdd:   146400, // 112,600 actual (247,100 for 2) -- https://goerli.etherscan.io/tx/0xa4fc65b8001bf8c44f1079b3d97adf42eb1097658e360b9033596253b0cbbd04, verified on mainnet
	Redeem:    78600,  // 60,456 actual -- https://goerli.etherscan.io/tx/0x5b22c48052df4a8ecd03a31b62e5015e6afe18c9ffb05e6cdd77396dfc3ca917, verified on mainnet
	RedeemAdd: 41000,  // 31,672 actual (92,083 for 2, 123,724 for 3) -- https://goerli.etherscan.io/tx/0xae424cc9b0d43bf934112245cb74ab9eca9c2611eabcd6257b6ec258b071c1e6, https://goerli.etherscan.io/tx/0x7ba7cb945da108d39a5a0ac580d4841c4017a32cd0e244f26845c6ed501d2475, verified on mainnet
	Refund:    57000,  // 43,014 actual -- https://goerli.etherscan.io/tx/0x586ed4cb7dab043f98d4cc08930d9eb291b0052d140d949b20232ceb6ad15f25
}

var v1Gases = &Gases{
	// First swap used 48801 gas Recommended Gases.Swap = 63441
	Swap: 63_441,
	// 	4 additional swaps averaged 26695 gas each. Recommended Gases.SwapAdd = 34703
	// 	[48801 75511 102209 128895 155582]
	SwapAdd: 34_703,
	// First redeem used 40032 gas. Recommended Gases.Redeem = 52041
	Redeem: 52_041,
	// 	4 additional redeems averaged 10950 gas each. recommended Gases.RedeemAdd = 14235
	// 	[40032 50996 61949 72890 83832]
	RedeemAdd: 14_235,
	// *** Compare expected Swap + Redeem = 88k with UniSwap v2: 102k, v3: 127k
	// *** A 1-match order is cheaper than UniSwap.
	// Average of 5 refunds: 40390. Recommended Gases.Refund = 52507
	// 	[40381 40393 40393 40393 40393]
	Refund: 52_507,
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

// EncodeContractData packs the contract version and the locator into a byte
// slice for communicating a swap's identity.
func EncodeContractData(contractVersion uint32, locator []byte) []byte {
	b := make([]byte, len(locator)+4)
	binary.BigEndian.PutUint32(b[:4], contractVersion)
	copy(b[4:], locator[:])
	return b
}

func DecodeContractDataV0(data []byte) (secretHash [32]byte, err error) {
	contractVer, secretHashB, err := DecodeContractData(data)
	if err != nil {
		return secretHash, err
	}
	if contractVer != 0 {
		return secretHash, errors.New("not contract version 0")
	}
	copy(secretHash[:], secretHashB)
	return
}

// DecodeContractData unpacks the contract version and the locator.
func DecodeContractData(data []byte) (contractVersion uint32, locator []byte, err error) {
	if len(data) < 4 {
		err = errors.New("invalid short encoding")
		return
	}
	locator = data[4:]
	contractVersion = binary.BigEndian.Uint32(data[:4])
	switch contractVersion {
	case 0:
		if len(locator) != SecretHashSize {
			err = fmt.Errorf("v0 locator is too small. expected %d, got %d", SecretHashSize, len(locator))
			return
		}
	case 1:
		if len(locator) != LocatorV1Length {
			err = fmt.Errorf("v1 locator is too small. expected %d, got %d", LocatorV1Length, len(locator))
			return
		}
	default:
		err = fmt.Errorf("unkown contract version %d", contractVersion)
	}
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

var (
	// add before diving by gweiFactorBig to take the ceiling.
	gweiCeilAddend = big.NewInt(GweiFactor - 1)
	gweiFactorBig  = big.NewInt(GweiFactor)
)

// GweiToWei converts uint64 Gwei to *big.Int Wei.
func GweiToWei(v uint64) *big.Int {
	return new(big.Int).Mul(big.NewInt(int64(v)), gweiFactorBig)
}

// WeiToGweiFloor converts *big.Int Wei to uint64 Gwei. If v is determined to be
// unsuitable for a uint64, zero is returned. For values that are not even
// multiples of 1 gwei, this function returns the floor.
func WeiToGwei(v *big.Int) uint64 {
	vGwei := new(big.Int).Div(v, gweiFactorBig)
	if vGwei.IsUint64() {
		return vGwei.Uint64()
	}
	return 0
}

// WeiToGweiCeil converts *big.Int Wei to uint64 Gwei. If v is determined to be
// unsuitable for a uint64, zero is returned. For values that are not even
// multiples of 1 gwei, this function returns the ceiling. In general,
// WeiToWeiCeil should be used with gwei-unit fee rates are generated or
// validated and WeiToGwei should be used with balances and values.
func WeiToGweiCeil(v *big.Int) uint64 {
	vGwei := new(big.Int).Div(new(big.Int).Add(v, gweiCeilAddend), big.NewInt(GweiFactor))
	if vGwei.IsUint64() {
		return vGwei.Uint64()
	}
	return 0
}

// WeiToGweiSafe converts a *big.Int in wei (1e18 unit) to gwei (1e9 unit) as
// a uint64. Errors if the amount of gwei is too big to fit fully into a uint64.
// For values that are not even multiples of 1 gwei, this function returns the
// ceiling. As such, WeiToGweiSafe is more suitable for validating or generating
// fee rates. If balance or value validation is the goal, use truncation e.g.
// WeiToGwei.
func WeiToGweiSafe(wei *big.Int) (uint64, error) {
	if wei.Cmp(new(big.Int)) == -1 {
		return 0, fmt.Errorf("wei must be non-negative")
	}
	gwei := new(big.Int).Div(new(big.Int).Add(wei, gweiCeilAddend), gweiFactorBig)
	if !gwei.IsUint64() {
		return 0, fmt.Errorf("%v gwei is too big for a uint64", gwei)
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

// SwapVector is immutable contract data.
type SwapVector struct {
	From       common.Address
	To         common.Address
	Value      *big.Int
	SecretHash [32]byte
	LockTime   uint64 // seconds
}

// Locator encodes a version 1 locator for the SwapVector.
func (v *SwapVector) Locator() []byte {
	locator := make([]byte, LocatorV1Length)
	copy(locator[0:20], v.From[:])
	copy(locator[20:40], v.To[:])
	v.Value.FillBytes(locator[40:72])
	copy(locator[72:104], v.SecretHash[:])
	binary.BigEndian.PutUint64(locator[104:112], v.LockTime)
	return locator
}

func (v *SwapVector) String() string {
	return fmt.Sprintf("{ from = %s, to = %s, value = %d, secret hash = %s, locktime = %s }",
		v.From, v.To, v.Value, hex.EncodeToString(v.SecretHash[:]), time.UnixMilli(int64(v.LockTime)))
}

func CompareVectors(v1, v2 *SwapVector) bool {
	// Check vector equivalence.
	return v1.Value.Cmp(v2.Value) == 0 && v1.To == v2.To && v1.From == v2.From &&
		v1.LockTime == v2.LockTime && v1.SecretHash == v2.SecretHash
}

// SwapStatus is the contract data that specifies the current contract state.
type SwapStatus struct {
	BlockHeight uint64
	Secret      [32]byte
	Step        SwapStep
}

// SwapState is the current state of an in-process swap, as stored on-chain by
// the v0 contract.
type SwapState struct {
	BlockHeight uint64
	LockTime    time.Time
	Secret      [32]byte
	Initiator   common.Address
	Participant common.Address
	Value       *big.Int
	State       SwapStep
}

// SwapStateFromV0 converts a version 0 contract *ETHSwapSwap to the generalized
// *SwapState type.
func SwapStateFromV0(state *v0.ETHSwapSwap) *SwapState {
	return &SwapState{
		BlockHeight: state.InitBlockNumber.Uint64(),
		LockTime:    time.Unix(state.RefundBlockTimestamp.Int64(), 0),
		Secret:      state.Secret,
		Initiator:   state.Initiator,
		Participant: state.Participant,
		Value:       state.Value,
		State:       SwapStep(state.State),
	}
}

// Initiation is the data used to initiate a swap.
type Initiation struct {
	LockTime    time.Time
	SecretHash  [32]byte
	Participant common.Address
	Value       *big.Int
}

// Redemption is the data used to redeem a swap.
type Redemption struct {
	Secret     [32]byte
	SecretHash [32]byte
}

var (
	usdcTokenID, _  = dex.BipSymbolID("usdc.eth")
	usdtTokenID, _  = dex.BipSymbolID("usdt.eth")
	maticTokenID, _ = dex.BipSymbolID("matic.eth") // old matic 0x7d1afa7b718fb893db30a3abc0cfc608aacfebb0
)

// Gases lists the expected gas required for various DEX and wallet operations.
type Gases struct {
	// Approve is the amount of gas needed to approve the swap contract for
	// transferring tokens. The first approval for an address uses more gas than
	// subsequent approvals for the same address.
	Approve uint64 `json:"approve"`
	// Transfer is the amount of gas needed to transfer tokens. The first
	// transfer to an address uses more gas than subsequent transfers to the
	// same address.
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

func ParseV0Locator(locator []byte) (secretHash [32]byte, err error) {
	if len(locator) == SecretHashSize {
		copy(secretHash[:], locator)
	} else {
		err = fmt.Errorf("wrong v0 locator length. wanted %d, got %d", SecretHashSize, len(locator))
	}
	return
}

// LocatorV1Length = from 20 + to 20 + value 32 + secretHash 32 +
// lockTime 8 = 112 bytes
const LocatorV1Length = 112

func ParseV1Locator(locator []byte) (v *SwapVector, err error) {
	// from 20 + to 20 + value 8 + secretHash 32 + lockTime 8
	if len(locator) == LocatorV1Length {
		v = &SwapVector{
			From:     common.BytesToAddress(locator[:20]),
			To:       common.BytesToAddress(locator[20:40]),
			Value:    new(big.Int).SetBytes(locator[40:72]),
			LockTime: binary.BigEndian.Uint64(locator[104:112]),
		}
		copy(v.SecretHash[:], locator[72:104])
	} else {
		err = fmt.Errorf("wrong v1 locator length. wanted %d, got %d", LocatorV1Length, len(locator))
	}
	return
}

func SwapVectorToAbigen(v *SwapVector) swapv1.ETHSwapVector {
	return swapv1.ETHSwapVector{
		SecretHash:      v.SecretHash,
		Initiator:       v.From,
		RefundTimestamp: v.LockTime,
		Participant:     v.To,
		Value:           v.Value,
	}
}

// ProtocolVersion assists in mapping the dex.Asset.Version to a contract
// version.
type ProtocolVersion uint32

const (
	ProtocolVersionZero ProtocolVersion = iota
	ProtocolVersionV1Contracts
)

func (v ProtocolVersion) ContractVersion() uint32 {
	switch v {
	case ProtocolVersionZero:
		return 0
	case ProtocolVersionV1Contracts:
		return 1
	default:
		return ContractVersionUnknown
	}
}

var (
	// ContractVersionERC20 is passed as the contract version when calling
	// ERC20 contract methods.
	ContractVersionERC20   = ^uint32(0)
	ContractVersionNewest  = ContractVersionERC20 // same thing
	ContractVersionUnknown = ContractVersionERC20 - 1
)
