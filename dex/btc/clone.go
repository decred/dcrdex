// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg"
)

// ReadCloneParams translates a network parameters struct. This entire module
// may be usable by a BTC clone if certain conditions are met. Many BTC clones
// have btcd forks with their own Go config files. See also CloneParams.
func ReadCloneParams(cloneParams interface{}) (*chaincfg.Params, error) {
	p := new(chaincfg.Params)
	cloneJSON, err := json.Marshal(cloneParams)
	if err != nil {
		return nil, fmt.Errorf("error marshaling network params: %v", err)
	}
	err = json.Unmarshal(cloneJSON, &p)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling network params: %v", err)
	}
	return p, nil
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
}
