package firo

import (
	"errors"
	"fmt"
	"strings"

	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

// An EXX Address, also called an Exchange Address, is a re-encoding of a
// transparent Firo P2PKH address. It is required in order to send funds to
// Binance and some other centralized exchanges.

const (
	PKH_LEN    = 20
	SCRIPT_LEN = 26
)

const (
	VERSION_01             = 0x01
	MAINNET_VER_BYTE_PKH   = 0x52
	MAINNET_VER_BYTE_EXX   = 0x01
	TESTNET_VER_BYTE_PKH   = 0x41
	TESTNET_VER_BYTE_EXT   = 0x01
	ALLNETS_EXTRA_BYTE_ONE = 0xb9
	MAINNET_EXTRA_BYTE_TWO = 0xbb
	TESTNET_EXTRA_BYTE_TWO = 0xb1
)

// OP_EXCHANGEADDR is an unused bitcoin script opcode used to 'mark' the output
// as an exchange address for the recipient.
const OP_EXCHANGEADDR = 0xe0

var (
	errInvalidVersion       = errors.New("invalid version")
	errInvalidExtra         = errors.New("invalid extra")
	errInvalidDecodedLength = errors.New("invalid decoded length")
)

// isExxAddress determines whether the address encoding is a mainnet EXX address
// or a testnet EXT address.
func isExxAddress(address string) bool {
	return strings.HasPrefix(address, "EXX") || strings.HasPrefix(address, "EXT")
}

// decodeExxAddress decodes a Firo exchange address.
func decodeExxAddress(encodedAddr string, net *chaincfg.Params) (btcutil.Address, error) {
	decoded, ver, err := base58.CheckDecode(encodedAddr)
	if err != nil {
		return nil, err
	}

	if ver != VERSION_01 {
		return nil, errInvalidVersion
	}
	if decoded[0] != ALLNETS_EXTRA_BYTE_ONE {
		return nil, errInvalidExtra
	}
	switch net {
	case dexfiro.MainNetParams:
		if decoded[1] != MAINNET_EXTRA_BYTE_TWO {
			return nil, errInvalidExtra
		}
	case dexfiro.TestNetParams:
		if decoded[1] != TESTNET_EXTRA_BYTE_TWO {
			return nil, errInvalidExtra
		}
	default:
		return nil, errInvalidExtra
	}
	decLen := len(decoded)
	if decLen < PKH_LEN+1 {
		return nil, errInvalidDecodedLength
	}

	decExtra := decLen - PKH_LEN
	pkh := decoded[decExtra:]
	addrPKH, err := btcutil.NewAddressPubKeyHash(pkh, net)
	if err != nil {
		return nil, err
	}
	return btcutil.Address(addrPKH), nil
}

// buildExxPayToScript builds a P2PKH output script for a Firo exchange address.
func buildExxPayToScript(addr btcutil.Address, address string) ([]byte, error) {
	if _, isPKH := addr.(*btcutil.AddressPubKeyHash); !isPKH {
		return nil, fmt.Errorf("address %s does not contain a pubkey hash", address)
	}
	baseScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}
	script := make([]byte, 0, len(baseScript)+1)
	script = append(script, OP_EXCHANGEADDR)
	script = append(script, baseScript...)
	return script, nil
}
