package firo

import (
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

const (
	PKH_LEN                    = 20
	MAINNET_VER_BYTE_PKH       = 0x52
	MAINNET_VER_BYTE_EXX       = 0x01
	TESTNET_VER_BYTE_PKH       = 0x41
	TESTNET_VER_BYTE_EXT       = 0x01
	ALLNETS_EXTRA_BYTE_EX_ONE  = 0xb9 // python: ADDRTYPE_EXP2PKH
	MAINNET_EXTRA_BYTE_EXX_TWO = 0xbb
	TESTNET_EXTRA_BYTE_EXT_TWO = 0xb1

	EXCHANGE_MARKER_BYTE = 0xe0 // python: OP_EXCHANGEADDR
	SCRIPT_LEN           = 26
)

func isExxAddress(address string) bool {
	return strings.HasPrefix(address, "EXX") || strings.HasPrefix(address, "EXT")
}

// decodeExxAddress decodes a Firo exchange address.
func decodeExxAddress(encodedAddr string, net *chaincfg.Params) (btcutil.Address, error) {
	decoded, _, err := base58.CheckDecode(encodedAddr)
	if err != nil {
		return nil, err
	}
	// pull out the pkh - Mainnet: (ver=0x01)   <0xb9> <0xbb> <PKH>
	decLen := len(decoded)
	if decLen < PKH_LEN+1 {
		return nil, fmt.Errorf("bad decoded len %d", decLen)
	}
	decExtra := decLen - PKH_LEN
	pkh := decoded[decExtra:]

	addrPKH, err := btcutil.NewAddressPubKeyHash(pkh, net)
	if err != nil {
		return nil, err
	}
	return btcutil.Address(addrPKH), nil
}

func buildExxPayToScript(addr btcutil.Address, address string) ([]byte, error) {
	if _, isPKH := addr.(*btcutil.AddressPubKeyHash); !isPKH {
		return nil, fmt.Errorf("address %s does not contain a pubkey hash", address)
	}
	baseScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}
	script := make([]byte, 0, len(baseScript)+1)
	script = append(script, EXCHANGE_MARKER_BYTE)
	script = append(script, baseScript...)
	return script, nil
}
