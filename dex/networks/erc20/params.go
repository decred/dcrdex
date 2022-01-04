// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package erc20

import (
	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
)

var (
	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Mainnet: common.Address{},
			dex.Simnet:  common.HexToAddress("0x6b4368d3E41a60e20FF8539C843B3CDB38C8A507"),
			dex.Testnet: common.Address{},
		},
	}
)
