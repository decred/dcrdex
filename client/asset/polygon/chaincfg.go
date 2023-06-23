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
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
)

var (
	mainnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0x5973918275C01F50555d44e92c9d9b353CaDAD54"),
		TokenAddr: common.HexToAddress("0x2791bca1f2de4661ed88a30c99a7a9449aa84174"), // usdc
		TxHash:    common.HexToHash("0xc388210f83679f9841e34fb3cdee0294f885846de3e01211e50f77d508a0d6ec"),
		BlockHash: common.HexToHash("0xa603d7354686269521e8d561d6ffa4aa92aad80e01f3b4cc9745fdb54342f85b"),
	}

	testnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("C26880A0AF2EA0c7E8130e6EC47Af756465452E8"),
		TokenAddr: common.HexToAddress("0x1e833c55267ba4a78bb6e414acda36569d3c78d9"), // maybe usdt
		TxHash:    common.HexToHash("0xc592ac8975a58bc7ad48381f9a05c07a53a67b2a4448ad821ed7ef2dcd1a878a"),
		BlockHash: common.HexToHash("0x5a2d26b5bd9d1995c25e211379671bce893befa31cf2a9704ff89f8682b3c6cf"),
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
		return c, nil
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
		tContractFile  = filepath.Join(tDir, "test_token_contract_address.txt")
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

// ChainConfig returns chain configuration for the specified network.
func ChainConfig(net dex.Network) (c ethconfig.Config, err error) {
	// There are some minor differences between ethconfig.Default and the
	// polygon Defaults. The Miner settings are slightly different. There are
	// some new fields added in Polygon. I don't think any of them are
	// significant for us.
	c = ethconfig.Defaults
	c.RPCTxFeeCap = 5

	switch net {
	// Ethereum
	case dex.Testnet:
		c.Genesis = dexpolygon.DefaultMumbaiGenesisBlock()
	case dex.Mainnet:
		c.Genesis = dexpolygon.DefaultBorMainnetGenesisBlock()
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

// ChainGenesis returns the genesis parameters for the specified network.
func ChainGenesis(net dex.Network) (c *params.ChainConfig, err error) {
	cfg, err := ChainConfig(net)
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
