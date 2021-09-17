// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrdex/server/asset"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/hdkeychain"
)

// AddressDeriver generates unique addresses from an extended public key.
type AddressDeriver struct {
	params   *chaincfg.Params
	storeIdx func(uint32) // e.g. callback to a DB update
	xpub     *hdkeychain.ExtendedKey

	mtx  sync.Mutex
	next uint32 // next child index to generate
}

// NewAddressDeriver creates a new AddressDeriver for the provided extended
// public key, HDKeyIndexer, and network parameters. Note that if the source
// wallet has accounts, the extended key should be for an account.
func NewAddressDeriver(xpub string, keyIndexer asset.HDKeyIndexer, chainParams *chaincfg.Params) (*AddressDeriver, error) {
	key, err := hdkeychain.NewKeyFromString(xpub)
	if err != nil {
		return nil, fmt.Errorf("error parsing master pubkey: %w", err)
	}
	if !key.IsForNet(chainParams) {
		return nil, fmt.Errorf("key is for the wrong network, wanted %s", chainParams.Name)
	}
	if key.IsPrivate() {
		return nil, errors.New("private key provided")
	}
	external, _ := getChild(key, 0) // derive from the external branch (not change addresses)
	if external == nil {
		return nil, errors.New("unexpected key derivation error")
	}
	next, err := keyIndexer.KeyIndex(xpub)
	if err != nil {
		return nil, err
	}
	storKey := func(idx uint32) {
		keyIndexer.SetKeyIndex(idx, xpub)
	}
	return &AddressDeriver{
		params:   chainParams,
		storeIdx: storKey,
		xpub:     external,
		next:     next,
	}, nil
}

func getChild(xkey *hdkeychain.ExtendedKey, i uint32) (*hdkeychain.ExtendedKey, uint32) {
	for {
		child, err := xkey.Derive(i) // standard BIP32, not compatible with Child method used in legacy btcwallets
		switch {
		case errors.Is(err, hdkeychain.ErrInvalidChild):
			i++
			continue
		case err == nil:
			return child, i
		default: // Should never happen with a valid xpub.
			return nil, 0
		}
	}
}

// NextAddress retrieves the pkh address for the next pubkey. While this should
// always return a valid address, an empty string may be returned in the event
// of an unexpected internal error.
func (ap *AddressDeriver) NextAddress() string {
	ap.mtx.Lock()
	defer ap.mtx.Unlock()

	child, i := getChild(ap.xpub, ap.next)
	if child == nil {
		return "" // should never happen
	}
	addr, err := child.Address(ap.params)
	if err != nil {
		// Address cannot error presently because NewAddressPubKeyHash only
		// errors if it is given a hash that is not 20 bytes, and Address calls
		// Hash160 first. But be safe in case this changes.
		return ""
	}
	ap.storeIdx(i) // not necessarily ap.next++
	ap.next = i + 1
	hash := addr.Hash160()
	addrP2WPKH, err := btcutil.NewAddressWitnessPubKeyHash(hash[:], ap.params)
	if err != nil {
		// This can only error if hash is not 20 bytes (ripemd160.Size), but we
		// guarantee it. Still, btcutil could change, so return an empty string
		// so caller can treat this as an unsupported asset rather than panic.
		return ""
	}
	return addrP2WPKH.String()
}
