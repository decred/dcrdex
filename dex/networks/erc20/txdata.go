// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package erc20

import (
	"fmt"
	"math/big"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
)

func parseCallData(method string, data []byte, expArgs int) ([]interface{}, error) {
	decoded, err := dexeth.ParseCallData(data, ERC20_ABI)
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != method {
		return nil, fmt.Errorf("expected %v function but got %v", method, decoded.Name)
	}
	if len(decoded.Args) != expArgs {
		return nil, fmt.Errorf("wrong number of arguments. wanted %d, got %d", expArgs, len(decoded.Args))
	}
	return decoded.Args, nil
}

// ParseTransferFromData parses the calldata used to call the transferFrom
// method of an ERC20 contract.
func ParseTransferFromData(data []byte) (sender, recipient common.Address, amount *big.Int, err error) {
	args, err := parseCallData("transferFrom", data, 3)
	if err != nil {
		return common.Address{}, common.Address{}, nil, fmt.Errorf("unable to parse call data: %v", err)
	}

	sender, ok := args[0].(common.Address)
	if !ok {
		return common.Address{}, common.Address{}, nil, fmt.Errorf("expected first arg of type common.Address but got %T", args[0])
	}

	recipient, ok = args[1].(common.Address)
	if !ok {
		return common.Address{}, common.Address{}, nil, fmt.Errorf("expected second arg of type common.Address but got %T", args[1])
	}

	value, ok := args[2].(*big.Int)
	if !ok {
		return common.Address{}, common.Address{}, nil, fmt.Errorf("expected third arg of type *big.Int but got %T", args[2])
	}

	return sender, recipient, value, nil
}

// ParseTransferData parses the calldata used to call the transfer method of an
// ERC20 contract.
func ParseTransferData(data []byte) (common.Address, *big.Int, error) {
	args, err := parseCallData("transfer", data, 2)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("unable to parse call data: %v", err)
	}

	recipient, ok := args[0].(common.Address)
	if !ok {
		return common.Address{}, nil, fmt.Errorf("expected first arg of type common.Address but got %T", args[0])
	}
	value, ok := args[1].(*big.Int)
	if !ok {
		return common.Address{}, nil, fmt.Errorf("expected second arg of type *big.Int but got %T", args[1])
	}

	return recipient, value, nil
}
