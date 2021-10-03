// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrdex/server/asset"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/hdkeychain"
)

// AddressDeriver generates unique addresses from an extended public key.
type AddressDeriver struct {
	params   *chaincfg.Params
	storeIdx func(uint32) error // e.g. callback to a DB update
	xpub     *hdkeychain.ExtendedKey

	mtx  sync.Mutex
	next uint32 // next child index to generate
}

// NewAddressDeriver creates a new AddressDeriver for the provided extended
// public key, KeyIndexer, and network parameters. Note that if the source
// wallet has accounts (e.g. BIP44), the extended key should be for an account.
// Since AddressDeriver returns P2WPKH addresses, the extended public key should
// be for a P2WPKH derivation path (e.g. zpub for mainnet, and vpub for regnet
// and testnet). However, if a key for a different address encoding is provided
// that is otherwise valid for the specified network, the error message will
// suggest the equivalent extended key for P2WPKH addresses, but the operator
// must be certain they can redeem payments to P2WPKH addresses. This is done
// instead of returning different encodings depending on the extended key format
// to prevent accidental reuse of keys.
//
// Refs:
// https://github.com/bitcoin/bips/blob/master/bip-0084.mediawiki#extended-key-version
// https://github.com/satoshilabs/slips/blob/master/slip-0132.md
//
// See TestAddressDeriver to double check your own keys and the first addresses.
func NewAddressDeriver(xpub string, keyIndexer asset.KeyIndexer, chainParams *chaincfg.Params) (*AddressDeriver, uint32, error) {
	key, err := hdkeychain.NewKeyFromString(xpub)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing master pubkey: %w", err)
	}
	if key.IsPrivate() {
		return nil, 0, errors.New("private key provided")
	}

	// Get the desired version bytes for a p2wpkh extended key for this network.
	var wantVer uint32
	switch chainParams.Net {
	case wire.MainNet:
		wantVer = mainnetWitnessHashVer
	case wire.TestNet, wire.TestNet3: // regnet and testnet3 have the same version bytes
		wantVer = testnetWitnessHashVer
	default:
		return nil, 0, fmt.Errorf("unsupported network %v", chainParams.Name)
	}

	// See if we recognize the version bytes for the given network. key.IsForNet
	// will only verify against chainParams.HDPublicKeyID, which btcsuite sets
	// for the legacy (non-segwit) P2PKH derivation path.
	ver := key.Version()
	prefix := pubVerPrefix(ver, chainParams.Net)
	if prefix == "" {
		return nil, 0, fmt.Errorf("key type is not recognized for network %s", chainParams.Name)
	}
	version := binary.BigEndian.Uint32(ver)
	if version != wantVer {
		// Patch the public key version bytes to suggest the p2wpkh equivalent.
		key.SetNet(&chaincfg.Params{HDPublicKeyID: verBytes(wantVer)})
		return nil, 0, fmt.Errorf("provided extended key (prefix %q) not for p2wpkh, equivalent is %q",
			prefix, key.String())
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
		params:   chainParams,
		storeIdx: storKey,
		xpub:     external,
		next:     next,
	}, next, nil
}

func getChild(xkey *hdkeychain.ExtendedKey, i uint32) (*hdkeychain.ExtendedKey, uint32, error) {
	for {
		child, err := xkey.Derive(i) // standard BIP32, not compatible with Child method used in legacy btcwallets
		switch {
		case errors.Is(err, hdkeychain.ErrInvalidChild):
			i++
			continue
		case err == nil:
			return child, i, nil
		default: // Should never happen with a valid xpub.
			return nil, 0, err
		}
	}
}

// NextAddress retrieves the p2wpkh address for the next pubkey. While this
// should always return a valid address, an empty string may be returned in the
// event of an unexpected internal error.
func (ap *AddressDeriver) NextAddress() (string, error) {
	ap.mtx.Lock()
	defer ap.mtx.Unlock()

	child, i, err := getChild(ap.xpub, ap.next)
	if err != nil {
		return "", err // should never happen
	}
	addr, err := child.Address(ap.params)
	if err != nil {
		// Address cannot error presently because NewAddressPubKeyHash only
		// errors if it is given a hash that is not 20 bytes, and Address calls
		// Hash160 first. But be safe in case this changes.
		return "", err
	}
	if err = ap.storeIdx(i); err != nil {
		return "", err
	} // not necessarily ap.next++
	ap.next = i + 1
	hash := addr.Hash160()
	addrP2WPKH, err := btcutil.NewAddressWitnessPubKeyHash(hash[:], ap.params)
	if err != nil {
		// This can only error if hash is not 20 bytes (ripemd160.Size), but we
		// guarantee it. Still, btcutil could change, so return an empty string
		// so caller can treat this as an unsupported asset rather than panic.
		return "", err
	}
	return addrP2WPKH.String(), nil
}

func verBytes(ver uint32) [4]byte {
	var vers [4]byte
	binary.BigEndian.PutUint32(vers[:], ver)
	return vers
}

// HD version bytes and prefixes
// https://github.com/satoshilabs/slips/blob/master/slip-0132.md

const (
	mainnetWitnessHashVer uint32 = 0x04b24746
	testnetWitnessHashVer uint32 = 0x045f1cf6 // also regtest/regnet
)

var pubVersionPrefixesMainnet = map[uint32]string{
	0x0488b21e: "xpub", // P2PKH or P2SH
	0x049d7cb2: "ypub", // P2WPKH *in* P2SH
	0x04b24746: "zpub", // P2WPKH
	0x0295b43f: "Ypub", // Multi-signature P2WSH in P2SH
	0x02aa7ed3: "Zpub", // Multi-signature P2WSH
}
var pubPrefixVersionsMainnet map[string]uint32

var pubVersionPrefixesTestnet = map[uint32]string{
	0x043587cf: "tpub", // P2PKH or P2SH
	0x044a5262: "upub", // P2WPKH *in* P2SH
	0x045f1cf6: "vpub", // P2WPKH
	0x024289ef: "Upub", // Multi-signature P2WSH in P2SH
	0x02575483: "Vpub", // Multi-signature P2WSH
}
var pubPrefixVersionsTestnet map[string]uint32

func init() {
	pubPrefixVersionsMainnet = make(map[string]uint32, len(pubVersionPrefixesMainnet))
	for ver, pref := range pubVersionPrefixesMainnet {
		pubPrefixVersionsMainnet[pref] = ver
	}
	pubPrefixVersionsTestnet = make(map[string]uint32, len(pubVersionPrefixesTestnet))
	for ver, pref := range pubVersionPrefixesTestnet {
		pubPrefixVersionsTestnet[pref] = ver
	}
}

func pubVerPrefix(vers []byte, net wire.BitcoinNet) string {
	if len(vers) != 4 {
		return ""
	}
	ver := binary.BigEndian.Uint32(vers)
	switch net {
	case wire.MainNet:
		return pubVersionPrefixesMainnet[ver]
	case wire.TestNet, wire.TestNet3: // regnet and testnet3 have same version bytes
		return pubVersionPrefixesTestnet[ver]
	default:
		return ""
	}
}

func pubPrefixVer(prefix string, net wire.BitcoinNet) []byte {
	if len(prefix) != 4 {
		return nil
	}
	var ver uint32
	var found bool
	switch net {
	case wire.MainNet:
		ver, found = pubPrefixVersionsMainnet[prefix]
	case wire.TestNet, wire.TestNet3: // regnet and testnet3 have same version bytes
		ver, found = pubPrefixVersionsTestnet[prefix]
	}
	if !found {
		return nil
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, ver)
	return version
}
