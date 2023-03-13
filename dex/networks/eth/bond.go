package eth

import (
	"fmt"
	"math/big"
	"strings"

	"decred.org/dcrdex/dex"
	bondContract "decred.org/dcrdex/dex/networks/eth/bondcontracts/v0"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

var ETHBondAddress map[dex.Network]common.Address = map[dex.Network]common.Address{
	dex.Simnet: common.HexToAddress("0x6b4368d3e41a60e20ff8539c843b3cdb38c8a507"),
}

const (
	// actual = 115460
	NewBondGas uint64 = 200000

	// actual = 59451
	UpdateBondGas uint64 = 400000

	// actual = 39003, but this includes refund for clearing storage data. If
	// even 50000 is set for the gas limit, the tx will fail.
	RefundBondGas uint64 = 200000
)

var BondABI = initBondABI()

func initBondABI() *abi.ABI {
	abi, err := abi.JSON(strings.NewReader(bondContract.ETHBondABI))
	if err != nil {
		panic(err)
	}
	return &abi
}

func PackCreateBondTx(accountID [32]byte, bondID [32]byte, locktime uint64) ([]byte, error) {
	return BondABI.Pack("createBond", accountID, bondID, locktime)
}

func ParseCreateBondTx(data []byte) (accountID [32]byte, id [32]byte, locktime uint64, err error) {
	decoded, err := ParseCallData(data, BondABI)
	if err != nil {
		return [32]byte{}, [32]byte{}, 0, err
	}

	if decoded.Name != "createBond" {
		return [32]byte{}, [32]byte{}, 0, fmt.Errorf("expected method 'newBond', got '%s'", decoded.Name)
	}

	args := decoded.inputs

	const numArgs = 3
	if len(args) != numArgs {
		return [32]byte{}, [32]byte{}, 0, fmt.Errorf("expected %d arguments, got %d", numArgs, len(args))
	}

	accountID, ok := args[0].value.([32]byte)
	if !ok {
		return [32]byte{}, [32]byte{}, 0, fmt.Errorf("expected [32]byte for accountID, got %T", args[0].value)
	}

	id, ok = args[1].value.([32]byte)
	if !ok {
		return [32]byte{}, [32]byte{}, 0, fmt.Errorf("expected [32]byte for id, got %T", args[1].value)
	}

	locktime, ok = args[2].value.(uint64)
	if !ok {
		return [32]byte{}, [32]byte{}, 0, fmt.Errorf("expected uint64 for locktime, got %T", args[2].value)
	}

	return accountID, id, locktime, nil
}

func PackUpdateBondsTx(accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64) ([]byte, error) {
	return BondABI.Pack("updateBonds", accountID, bondsToUpdate, newBondIDs, value, locktime)
}

func ParseUpdateBondsTx(data []byte) (accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64, err error) {
	decoded, err := ParseCallData(data, BondABI)
	if err != nil {
		return [32]byte{}, nil, nil, nil, 0, err
	}

	if decoded.Name != "updateBonds" {
		return [32]byte{}, nil, nil, nil, 0, err
	}

	args := decoded.inputs

	const numArgs = 5
	if len(args) != numArgs {
		return [32]byte{}, nil, nil, nil, 0, err
	}

	accountID, ok := args[0].value.([32]byte)
	if !ok {
		return [32]byte{}, nil, nil, nil, 0, fmt.Errorf("expected [32]byte for value, got %T", args[0].value)
	}

	bondsToUpdate, ok = args[1].value.([][32]byte)
	if !ok {
		return [32]byte{}, nil, nil, nil, 0, fmt.Errorf("expected [][32]byte for value, got %T", args[1].value)
	}

	newBondIDs, ok = args[2].value.([][32]byte)
	if !ok {
		return [32]byte{}, nil, nil, nil, 0, fmt.Errorf("expected [][32]byte for value, got %T", args[2].value)
	}

	value, ok = args[3].value.(*big.Int)
	if !ok {
		return [32]byte{}, nil, nil, nil, 0, fmt.Errorf("expected *big.Int for value, got %T", args[3].value)
	}

	locktime, ok = args[4].value.(uint64)
	if !ok {
		return [32]byte{}, nil, nil, nil, 0, fmt.Errorf("expected uint64 for value, got %T", args[4].value)
	}

	return accountID, bondsToUpdate, newBondIDs, value, locktime, nil
}

func PackRefundBondTx(accountID [32]byte, bondID [32]byte) ([]byte, error) {
	return BondABI.Pack("refundBond", accountID, bondID)
}

func ParseRefundBondTx(data []byte) (accountID [32]byte, bondID [32]byte, err error) {
	decoded, err := ParseCallData(data, BondABI)
	if err != nil {
		return [32]byte{}, [32]byte{}, err
	}

	if decoded.Name != "refundBond" {
		return [32]byte{}, [32]byte{}, fmt.Errorf("expected method 'refundBond', got '%s'", decoded.Name)
	}

	args := decoded.inputs

	const numArgs = 2
	if len(args) != numArgs {
		return [32]byte{}, [32]byte{}, fmt.Errorf("expected %d arguments, got %d", numArgs, len(args))
	}

	accountID, ok := args[0].value.([32]byte)
	if !ok {
		return [32]byte{}, [32]byte{}, fmt.Errorf("expected [32]byte for accountID, got %T", args[0].value)
	}

	bondID, ok = args[1].value.([32]byte)
	if !ok {
		return [32]byte{}, [32]byte{}, fmt.Errorf("expected [32]byte for bondID, got %T", args[1].value)
	}

	return accountID, bondID, nil
}
