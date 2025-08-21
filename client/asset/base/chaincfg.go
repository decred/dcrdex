// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package base

import (
	"errors"
	"fmt"
	"math/big"

	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexbase "decred.org/dcrdex/dex/networks/base"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

var (
	// Random data from block 33188099
	mainnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0x41e263cd1358A97908d47c26dADca2750b1E67f3"),
		TokenAddr: common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"), // usdc
		TxHash:    common.HexToHash("0xc853b7158f29aaaf6727d026d386888b4f42d6fd7a01ca1fad0e82d57c9f6509"),
		BlockHash: common.HexToHash("0x78a12e0b4235b61d0eae276bb33d2298ad886d48145b850999b156948297c7eb"),
	}

	// Random data from block 28699037
	testnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0xfac1636039e9b9A99Be1D3E7221f9A1e56eB4D1E"),
		TokenAddr: common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e"), // usdc
		TxHash:    common.HexToHash("0x63967629e8db177a098f8ad2b693dcfd1b5aa79759dc90dd3c06c365f137cfcb"),
		BlockHash: common.HexToHash("0x2637e321b51b58a1a873111de873aa80a3e12e3263a67a4a1f2968ba2d3f94a7"),
	}
)

// NetworkCompatibilityData returns the CompatibilityData for the specified
// network. If using simnet, make sure the simnet harness is running.
func NetworkCompatibilityData(net dex.Network) (c eth.CompatibilityData, err error) {
	switch net {
	case dex.Mainnet:
		return mainnetCompatibilityData, nil
	case dex.Testnet:
		return testnetCompatibilityData, nil
	case dex.Simnet:
		// TODO: Add simnet.
		return c, errors.New("simnet not implemented yet")
	default:
		return c, fmt.Errorf("No compatibility data for network # %d", net)
	}
}

// ChainConfig returns the core configuration for the blockchain.
func ChainConfig(net dex.Network) (c *params.ChainConfig, err error) {
	c = new(params.ChainConfig)
	c.ChainID = big.NewInt(dexbase.ChainIDs[net])
	return
}
