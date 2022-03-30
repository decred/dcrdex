// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zec

import (
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
	"github.com/decred/base58"
)

type AddressParams struct {
	PubKeyHashAddrID [2]byte
	ScriptHashAddrID [2]byte
}

// DecodeAddress decodes an address string into an internal btc address.
// ZCash uses a double SHA-256 checksum but with a 2-byte address ID, so
// a little customization is needed.
// TODO: There also appears to be a bech32 encoding and something called a
// "unified payment address", but for our use of this function client-side,
// we will never generate those addresses.
// Do we need to revisit before NU5?
func DecodeAddress(a string, addrParams *AddressParams, btcParams *chaincfg.Params) (btcutil.Address, error) {
	b := base58.Decode(a)
	if len(b) < 7 {
		return nil, fmt.Errorf("invalid address")
	}

	var addrID [2]byte
	copy(addrID[:], b[:2])

	var checkSum [4]byte
	copy(checkSum[:], b[len(b)-4:])

	data := b[2 : len(b)-4]
	hashDigest := b[:len(b)-4]

	if checksum(hashDigest) != checkSum {
		return nil, fmt.Errorf("invalid checksum")
	}

	switch addrID {
	case addrParams.PubKeyHashAddrID:
		return btcutil.NewAddressPubKeyHash(data, btcParams)
	case addrParams.ScriptHashAddrID:
		return btcutil.NewAddressScriptHashFromHash(data, btcParams)
	}

	return nil, fmt.Errorf("unknown address type %v", addrID)
}

// RecodeAddress converts an internal btc address to a ZCash address string.
func RecodeAddress(addr string, addrParams *AddressParams, btcParams *chaincfg.Params) (string, error) {
	btcAddr, err := btcutil.DecodeAddress(addr, btcParams)
	if err != nil {
		return "", err
	}

	return EncodeAddress(btcAddr, addrParams)
}

// EncodeAddress converts a btcutil.Address from the BTC backend into a ZCash
// address string.
func EncodeAddress(btcAddr btcutil.Address, addrParams *AddressParams) (string, error) {
	switch btcAddr.(type) {
	case *btcutil.AddressPubKeyHash:
		return b58Encode(btcAddr.ScriptAddress(), addrParams.PubKeyHashAddrID), nil
	case *btcutil.AddressScriptHash:
		return b58Encode(btcAddr.ScriptAddress(), addrParams.ScriptHashAddrID), nil
	}

	return "", fmt.Errorf("unsupported address type %T", btcAddr)
}

// b58Encode base-58 encodes the address with the serialization
//   addrID | input | 4-bytes of double-sha256 checksum
func b58Encode(input []byte, addrID [2]byte) string {
	b := make([]byte, 0, 2+len(input)+4)
	b = append(b, addrID[:]...)
	b = append(b, input[:]...)
	cksum := checksum(b)
	b = append(b, cksum[:]...)
	return base58.Encode(b)
}

// checksum computes a checksum, which is the first 4 bytes of a double-sha256
// hash of the input message.
func checksum(input []byte) (cksum [4]byte) {
	h := sha256.Sum256(input)
	h2 := sha256.Sum256(h[:])
	copy(cksum[:], h2[:4])
	return
}
