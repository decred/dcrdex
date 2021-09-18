// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
)

// AddressDeriver generates unique addresses from an extended public key.
type AddressDeriver struct {
	addrParams dcrutil.AddressParams
	storeIdx   func(uint32) error // e.g. callback to a DB update
	xpub       *hdkeychain.ExtendedKey

	mtx  sync.Mutex
	next uint32 // next child index to generate
}

type NetParams interface {
	hdkeychain.NetworkParams
	dcrutil.AddressParams
}

// NewAddressDeriver creates a new AddressDeriver for the provided extended
// public key for an account, and KeyIndexer, using the provided network
// parameters (e.g. chaincfg.MainNetParams()).
func NewAddressDeriver(xpub string, keyIndexer asset.KeyIndexer, params NetParams) (*AddressDeriver, uint32, error) {
	key, err := hdkeychain.NewKeyFromString(xpub, params)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing master pubkey: %w", err)
	}
	if key.IsPrivate() {
		return nil, 0, errors.New("private key provided")
	}
	external, _, err := getChild(key, 0) // derive from the external branch (not change addresses)
	if err != nil {
		return nil, 0, fmt.Errorf("unexpected key derivation error: %w", err)
	}
	next, err := keyIndexer.KeyIndex(xpub)
	if err != nil {
		return nil, 0, err
	}
	storKey := func(idx uint32) error {
		return keyIndexer.SetKeyIndex(idx, xpub)
	}
	return &AddressDeriver{
		addrParams: params,
		storeIdx:   storKey,
		xpub:       external,
		next:       next,
	}, next, nil
}

func getChild(xkey *hdkeychain.ExtendedKey, i uint32) (*hdkeychain.ExtendedKey, uint32, error) {
	for {
		child, err := xkey.Child(i)
		switch {
		case errors.Is(err, hdkeychain.ErrInvalidChild):
			i++
			continue
		case err == nil:
			return child, i, nil
		default: // Should never happen with a valid xkey.
			return nil, 0, err
		}
	}
}

// NextAddress retrieves the pkh address for the next pubkey. While this should
// always return a valid address, an empty string may be returned in the event
// of an unexpected internal error.
func (ap *AddressDeriver) NextAddress() (string, error) {
	ap.mtx.Lock()
	defer ap.mtx.Unlock()

	child, i, err := getChild(ap.xpub, ap.next)
	if child == nil {
		return "", err // should never happen
	}
	hash := dcrutil.Hash160(child.SerializedPubKey())
	if err = ap.storeIdx(i); err != nil {
		return "", err
	}
	ap.next = i + 1 // not necessarily ap.next++

	addr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(hash, ap.addrParams)
	if err != nil {
		// This can only error if hash is not 20 bytes (ripemd160.Size), but we
		// guarantee it. Still, stdaddr could change, so return an empty string
		// so caller can treat this as an unsupported asset rather than panic.
		return "", err
	}
	return addr.String(), nil
}
