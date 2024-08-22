// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
)

// CompatibilityData is some addresses and hashes used for validating an RPC
// APIs compatibility with trading.
type CompatibilityData struct {
	Addr      common.Address
	TokenAddr common.Address
	TxHash    common.Hash
	BlockHash common.Hash
}

var (
	mainnetCompatibilityData = CompatibilityData{
		// Vitalik's address from https://twitter.com/VitalikButerin/status/1050126908589887488
		Addr:      common.HexToAddress("0xab5801a7d398351b8be11c439e05c5b3259aec9b"),
		TokenAddr: common.HexToAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"),
		TxHash:    common.HexToHash("0xea1a717af9fad5702f189d6f760bb9a5d6861b4ee915976fe7732c0c95cd8a0e"),
		BlockHash: common.HexToHash("0x44ebd6f66b4fd546bccdd700869f6a433ef9a47e296a594fa474228f86eeb353"),
	}

	testnetCompatibilityData = CompatibilityData{
		Addr:      common.HexToAddress("0x1268ad189526ac0b386faf06effc46779c340ee6"),
		TokenAddr: common.HexToAddress("0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238"), // USDC
		TxHash:    common.HexToHash("0x854860598c69e9b9e259fe74ca88610752428541e747180e0d40a4409d036f02"),
		BlockHash: common.HexToHash("0x7f3cd2786bc82872ffb55eface91a0afb927a9868adbb16a8d7939fef7385772"),
	}
)

// NetworkCompatibilityData returns the CompatibilityData for the specified
// network. If using simnet, make sure the simnet harness is running.
func NetworkCompatibilityData(net dex.Network) (c CompatibilityData, err error) {
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
	return CompatibilityData{
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

	return filepath.Join(u.HomeDir, "dextest", "eth"), nil
}

// ETHConfig returns the ETH protocol configuration for the specified network.
func ETHConfig(net dex.Network) (c ethconfig.Config, err error) {
	c = ethconfig.Defaults
	switch net {
	// Ethereum
	case dex.Mainnet:
		c.Genesis = ethcore.DefaultGenesisBlock()
	case dex.Testnet:
		c.Genesis = ethcore.DefaultSepoliaGenesisBlock()
	case dex.Simnet:
		c.Genesis, err = readSimnetGenesisFile()
		if err != nil {
			return c, fmt.Errorf("readSimnetGenesisFile error: %w", err)
		}
	default:
		return c, fmt.Errorf("unknown network %d", net)

	}
	c.NetworkId = c.Genesis.Config.ChainID.Uint64()
	return
}

// ChainConfig returns the core configuration for the blockchain.
func ChainConfig(net dex.Network) (c *params.ChainConfig, err error) {
	cfg, err := ETHConfig(net)
	if err != nil {
		return nil, err
	}
	return cfg.Genesis.Config, nil
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
