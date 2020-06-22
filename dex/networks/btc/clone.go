// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

// ReadCloneParams translates a CloneParams into a btcsuite chaincfg.Params.
func ReadCloneParams(cloneParams *CloneParams) *chaincfg.Params {
	return &chaincfg.Params{
		PubKeyHashAddrID: cloneParams.PubKeyHashAddrID,
		ScriptHashAddrID: cloneParams.ScriptHashAddrID,
		Bech32HRPSegwit:  cloneParams.Bech32HRPSegwit,
		CoinbaseMaturity: cloneParams.CoinbaseMaturity,
		Net:              wire.BitcoinNet(cloneParams.Net),
	}
}

// CloneParams are the parameters needed by BTC-clone-based Backend and
// ExchangeWallet implementations. Pass a *CloneParams to ReadCloneParams to
// create a *chaincfg.Params for server/asset/btc.NewBTCClone and
// client/asset/btc.BTCCloneWallet.
type CloneParams struct {
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
}
