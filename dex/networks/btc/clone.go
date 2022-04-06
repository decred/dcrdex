// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

// AddressDecoder decodes a string address to a btcutil.Address.
type AddressDecoder func(addr string, net *chaincfg.Params) (btcutil.Address, error)

// ReadCloneParams translates a CloneParams into a btcsuite chaincfg.Params.
func ReadCloneParams(cloneParams *CloneParams) *chaincfg.Params {
	return &chaincfg.Params{
		Name:             cloneParams.Name,
		PubKeyHashAddrID: cloneParams.PubKeyHashAddrID,
		ScriptHashAddrID: cloneParams.ScriptHashAddrID,
		Bech32HRPSegwit:  cloneParams.Bech32HRPSegwit,
		CoinbaseMaturity: cloneParams.CoinbaseMaturity,
		Net:              wire.BitcoinNet(cloneParams.Net),
		HDPrivateKeyID:   cloneParams.HDPrivateKeyID,
		HDPublicKeyID:    cloneParams.HDPublicKeyID,
	}
}

// CloneParams are the parameters needed by BTC-clone-based Backend and
// ExchangeWallet implementations. Pass a *CloneParams to ReadCloneParams to
// create a *chaincfg.Params for server/asset/btc.NewBTCClone and
// client/asset/btc.BTCCloneWallet.
type CloneParams struct {
	// Name defines a human-readable identifier for the network. e.g. "mainnet",
	// "testnet4", or "regtest"
	Name string
	// PubKeyHashAddrID: Net ID byte for a pubkey-hash address
	PubKeyHashAddrID byte
	// ScriptHashAddrID: Net ID byte for a script-hash address
	ScriptHashAddrID byte
	// Bech32HRPSegwit: Human-readable part for Bech32 encoded segwit addresses,
	// as defined in BIP 173.
	Bech32HRPSegwit string
	// CoinbaseMaturity: The number of confirmations before a transaction
	// spending a coinbase input can be spent.
	CoinbaseMaturity uint16
	// Net is the network identifier, e.g. wire.BitcoinNet.
	Net uint32
	// HDPrivateKeyID and HDPublicKeyID are the BIP32 hierarchical deterministic
	// extended key magic sequences. They are ONLY required if there is a need
	// to derive addresses from an extended key, such as if the asset is being
	// used by the server with the hdkeychain package to create fee addresses.
	// These are not required by the client.
	HDPrivateKeyID [4]byte
	HDPublicKeyID  [4]byte
}
