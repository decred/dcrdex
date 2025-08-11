package eth

import (
	"strconv"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbase "decred.org/dcrdex/dex/networks/base"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"github.com/ethereum/go-ethereum/common"
)

func regToken(tokens map[uint32]*dexeth.Token, tokenID uint32, desc string, nets ...dex.Network) {
	token, found := tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	netAddrs := make(map[dex.Network]string)
	netVersions := make(map[dex.Network][]uint32, 3)
	for net, netToken := range token.NetTokens {
		netAddrs[net] = netToken.Address.String()
		netVersions[net] = make([]uint32, 0, 1)
		for ver := range netToken.SwapContracts {
			netVersions[net] = append(netVersions[net], ver)
		}
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        walletTypeToken,
		Tab:         "Polygon token",
		Description: desc,
	}, netAddrs, netVersions)
}

func registerTokens() {
	asset.Register(966, &Driver{})
	regToken(dexpolygon.Tokens, 966001, "The USDC Ethereum ERC20 token.", dex.Mainnet)
	regToken(dexpolygon.Tokens, 966004, "The USDT Ethereum ERC20 token.", dex.Mainnet)
	regToken(dexpolygon.Tokens, 966003, "Wrapped BTC.", dex.Mainnet)
	regToken(dexpolygon.Tokens, 966002, "Wrapped ETH.", dex.Mainnet)
	asset.Register(8453, &Driver{})
	regToken(dexbase.Tokens, 61000, "The USDC Base ERC20 token.", dex.Mainnet)
}

func TestAcrossAssetToID(t *testing.T) {
	registerTokens()
	asset.SetNetwork(dex.Mainnet)

	expectedMappings := map[uint32]*acrossAsset{
		60: {
			chainID: 1,
			symbol:  "WETH",
			address: common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
		},
		60001: {
			chainID: 1,
			symbol:  "USDC",
			address: common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		},
		60002: {
			chainID: 1,
			symbol:  "USDT",
			address: common.HexToAddress("0xdac17f958d2ee523a2206206994597c13d831ec7"),
		},
		966001: {
			chainID: 137,
			symbol:  "USDC",
			address: common.HexToAddress("0x3c499c542cef5e3811e1192ce70d8cc03d5c3359"),
		},
		966002: {
			chainID: 137,
			symbol:  "WETH",
			address: common.HexToAddress("0x7ceb23fd6bc0add59e62ac25578270cff1b9f619"),
		},
		966003: {
			chainID: 137,
			symbol:  "WBTC",
			address: common.HexToAddress("0x1bfd67037b42cf73acf2047067bd4f2c47d9bfd6"),
		},
		966004: {
			chainID: 137,
			symbol:  "USDT",
			address: common.HexToAddress("0xc2132D05D31c914a87C6611C10748AEb04B58e8F"),
		},
		8453: {
			chainID: 8453,
			symbol:  "WETH",
			address: common.HexToAddress("0x4200000000000000000000000000000000000006"),
		},
		61000: {
			chainID: 8453,
			symbol:  "USDC",
			address: common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
		},
	}

	// Test asset ID to across asset conversion
	for assetID, expectedAcross := range expectedMappings {
		acrossAsset := assetIDToAcrossAsset(dex.Mainnet, assetID)

		if acrossAsset == nil {
			t.Errorf("Expected assetIDToAcrossAsset to return non-nil for asset %d", assetID)
			continue
		}

		if acrossAsset.chainID != expectedAcross.chainID {
			t.Errorf("Asset %d: Expected chainID %d, got %d", assetID, expectedAcross.chainID, acrossAsset.chainID)
		}

		if acrossAsset.symbol != expectedAcross.symbol {
			t.Errorf("Asset %d: Expected symbol %s, got %s", assetID, expectedAcross.symbol, acrossAsset.symbol)
		}

		if acrossAsset.address != expectedAcross.address {
			t.Errorf("Asset %d: Expected address %s, got %s", assetID, expectedAcross.address.Hex(), acrossAsset.address.Hex())
		}
	}

	// Test across asset to asset ID conversion (reverse direction)
	for expectedAssetID, acrossAsset := range expectedMappings {
		assetID, ok := acrossAssetToAssetID(dex.Mainnet, acrossAsset.symbol, acrossAsset.chainID)

		if !ok {
			t.Errorf("Expected acrossAssetToAssetID to succeed for %+v", acrossAsset)
			continue
		}

		if assetID != expectedAssetID {
			t.Errorf("Expected asset ID %d, got %d", expectedAssetID, assetID)
		}
	}

	// Test round-trip conversions
	for assetID := range expectedMappings {
		acrossAsset := assetIDToAcrossAsset(dex.Mainnet, assetID)
		if acrossAsset == nil {
			t.Errorf("Forward conversion failed for asset ID %d", assetID)
			continue
		}

		convertedAssetID, ok := acrossAssetToAssetID(dex.Mainnet, acrossAsset.symbol, acrossAsset.chainID)
		if !ok {
			t.Errorf("Reverse conversion failed for across asset %+v", acrossAsset)
			continue
		}

		if convertedAssetID != assetID {
			t.Errorf("Round-trip failed: expected asset ID %d, got %d", assetID, convertedAssetID)
		}
	}
}
