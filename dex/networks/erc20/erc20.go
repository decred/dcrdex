// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package erc20

import (
	"fmt"
	"strings"

	v0 "decred.org/dcrdex/dex/networks/erc20/contracts/v0"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

func parseABI(abiStr string) *abi.ABI {
	erc20ABI, err := abi.JSON(strings.NewReader(abiStr))
	if err != nil {
		panic(fmt.Sprintf("failed to parse erc20 abi: %v", err))
	}
	return &erc20ABI
}

var ERC20ABI = parseABI(IERC20MetaData.ABI)
var ERC20SwapABIV0 = parseABI(v0.ERC20SwapMetaData.ABI)
