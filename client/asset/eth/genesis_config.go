package eth

import (
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	ethCore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
)

// chainConfig returns chain config for Ethereum and Ethereum compatible chains
// (Polygon).
// NOTE: Other consensus engines (bor) are not considered in ethconfig.Config
// but this is not an issue because there have not been an obvious use for it.
func chainConfig(chainID int64, network dex.Network) (c ethconfig.Config, err error) {
	if network == dex.Simnet {
		c.Genesis, err = readSimnetGenesisFile(chainID)
		if err != nil {
			return c, fmt.Errorf("readSimnetGenesisFile error: %w", err)
		}
		c.NetworkId = c.Genesis.Config.ChainID.Uint64()
		return c, nil
	}

	cfg := ethconfig.Defaults
	switch chainID {
	// Ethereum
	case dexeth.TestnetChainID:
		cfg.Genesis = ethCore.DefaultGoerliGenesisBlock()
	case dexeth.MainnetChainID:
		cfg.Genesis = ethCore.DefaultGenesisBlock()

	// Polygon
	case dexpolygon.MainnetChainID:
		cfg.Genesis = dexpolygon.DefaultBorMainnetGenesisBlock()
	case dexpolygon.TestnetChainID:
		cfg.Genesis = dexpolygon.DefaultMumbaiGenesisBlock()
	default:
		return c, fmt.Errorf("unknown chain ID: %d", chainID)
	}

	cfg.NetworkId = cfg.Genesis.Config.ChainID.Uint64()

	return cfg, nil
}

// readSimnetGenesisFile reads the simnet genesis file for the wallet with the
// specified chainID.
func readSimnetGenesisFile(chainID int64) (*ethCore.Genesis, error) {
	dataDir, err := simnetDataDir(chainID)
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
