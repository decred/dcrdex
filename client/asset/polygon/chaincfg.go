// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
)

var (
	mainnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0x5973918275C01F50555d44e92c9d9b353CaDAD54"),
		TokenAddr: common.HexToAddress("0x2791bca1f2de4661ed88a30c99a7a9449aa84174"), // usdc
		TxHash:    common.HexToHash("0xa93416441cf73752767d145364d1bed5b76ed0b0dc531fa7127e63b277e9f537"),
		BlockHash: common.HexToHash("0xc2d5c6ffae606a71a05160d0457825b01d9747737381761aa88e9a5b7b3da743"),
	}

	testnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0x248528f5A2C3731fb598E8cc1dc5dB5f997E74BC"),
		TokenAddr: common.HexToAddress("0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582"), // usdc
		TxHash:    common.HexToHash("0xebf36a0c1ccbddc4b1670766278284789bf6cbebd6447b26fe45ade2ee5d4fbf"),
		BlockHash: common.HexToHash("0x98d056da91e376570dc1ab04bc9b13e3c1806961460a29c6268455b9c449b14b"),
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
	default:
		return c, fmt.Errorf("no compatibility data for network # %d", net)
	}
	// simnet
	tDir, err := simnetDataDir()
	if err != nil {
		return
	}

	addr := common.HexToAddress("18d65fb8d60c1199bb1ad381be47aa692b482605")
	var (
		tTxHashFile    = filepath.Join(tDir, "test_tx_hash.txt")
		tBlockHashFile = filepath.Join(tDir, "test_block10_hash.txt")
		tContractFile  = filepath.Join(tDir, "test_usdc_contract_address.txt")
	)
	readIt := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("Problem reading simnet testing file %q: %v", path, err))
		}
		return strings.TrimSpace(string(b)) // mainly the trailing "\r\n"
	}
	return eth.CompatibilityData{
		Addr:      addr,
		TokenAddr: common.HexToAddress(readIt(tContractFile)),
		TxHash:    common.HexToHash(readIt(tTxHashFile)),
		BlockHash: common.HexToHash(readIt(tBlockHashFile)),
	}, nil
}

// simnetDataDir returns the data directory for Ethereum simnet.
func simnetDataDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("error getting current user: %w", err)
	}

	return filepath.Join(u.HomeDir, "dextest", "polygon"), nil
}

// ChainConfig returns the core configuration for the blockchain.
func ChainConfig(net dex.Network) (c *params.ChainConfig, err error) {
	switch net {
	case dex.Mainnet:
		return dexpolygon.BorMainnetChainConfig, nil
	case dex.Testnet:
		return dexpolygon.AmoyChainConfig, nil
	case dex.Simnet:
	default:
		return c, fmt.Errorf("unknown network %d", net)
	}
	// simnet
	g, err := readSimnetGenesisFile()
	if err != nil {
		return c, fmt.Errorf("readSimnetGenesisFile error: %w", err)
	}
	return g.Config, nil
}

// readSimnetGenesisFile reads the simnet genesis file.
func readSimnetGenesisFile() (*ethcore.Genesis, error) {
	dataDir, err := simnetDataDir()
	if err != nil {
		return nil, err
	}

	genesisFile := filepath.Join(dataDir, "genesis.json")
	genesisCfg, err := dexeth.LoadGenesisFile(genesisFile)
	if err != nil {
		return nil, fmt.Errorf("error reading genesis file: %v", err)
	}

	return genesisCfg, nil
}
