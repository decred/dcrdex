// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package bch

import (
	"fmt"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
	bchchaincfg "github.com/gcash/bchd/chaincfg"
	"github.com/gcash/bchutil"
)

// RecodeCashAddress takes a BTC base-58 encoded address and converts it into a
// Cash Address.
func RecodeCashAddress(addr string, net *chaincfg.Params) (string, error) {
	btcAddr, err := btcutil.DecodeAddress(addr, net)
	if err != nil {
		return "", err
	}

	var bchAddr bchutil.Address
	switch at := btcAddr.(type) {
	case *btcutil.AddressPubKeyHash:
		bchAddr, err = bchutil.NewAddressPubKeyHash(btcAddr.ScriptAddress(), convertParams(net))
	case *btcutil.AddressScriptHash:
		bchAddr, err = bchutil.NewAddressScriptHashFromHash(btcAddr.ScriptAddress(), convertParams(net))
	case *btcutil.AddressPubKey:
		bchAddr, err = bchutil.NewAddressPubKey(btcAddr.ScriptAddress(), convertParams(net))
	default:
		return "", fmt.Errorf("unsupported address type %T", at)
	}

	if err != nil {
		return "", err
	}

	return withPrefix(bchAddr, net), nil
}

// DecodeCashAddress decodes a Cash Address string into a btcutil.Address
// that the BTC backend can use internally.
func DecodeCashAddress(addr string, net *chaincfg.Params) (btcutil.Address, error) {
	bchAddr, err := bchutil.DecodeAddress(addr, convertParams(net))
	if err != nil {
		return nil, fmt.Errorf("error decoding CashAddr address: %v", err)
	}

	switch at := bchAddr.(type) {
	// From what I can tell, the legacy address formats are probably
	// unnecessary, but I'd hate to be wrong.
	case *bchutil.AddressPubKeyHash, *bchutil.LegacyAddressPubKeyHash:
		return btcutil.NewAddressPubKeyHash(bchAddr.ScriptAddress(), net)
	case *bchutil.AddressScriptHash, *bchutil.LegacyAddressScriptHash:
		return btcutil.NewAddressScriptHashFromHash(bchAddr.ScriptAddress(), net)
	case *bchutil.AddressPubKey:
		return btcutil.NewAddressPubKey(bchAddr.ScriptAddress(), net)
	default:
		return nil, fmt.Errorf("unsupported address type %T", at)
	}
}

// convertParams converts the btcd/*chaincfg.Params to a bchd/*chaincfg.Params.
func convertParams(btcParams *chaincfg.Params) *bchchaincfg.Params {
	switch btcParams.Net {
	case MainNetParams.Net:
		return &bchchaincfg.MainNetParams
	case TestNet3Params.Net:
		return &bchchaincfg.TestNet3Params
	case RegressionNetParams.Net:
		return &bchchaincfg.RegressionNetParams
	}
	panic(fmt.Sprintf("unknown network for %s chain: %v", btcParams.Name, btcParams.Net))
}

// withPrefix adds the Bech32 prefix to the bchutil.Address, since the stringers
// don't, for some reason.
func withPrefix(bchAddr bchutil.Address, net *chaincfg.Params) string {
	switch bchAddr.(type) {
	case *bchutil.AddressPubKeyHash, *bchutil.AddressScriptHash:
		return net.Bech32HRPSegwit + ":" + bchAddr.String()
	}
	// Must be a pubkey address, which gets no prefix.
	return bchAddr.String()
}
