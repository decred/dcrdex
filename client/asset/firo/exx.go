package firo

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"

	"decred.org/dcrdex/client/asset/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

// An EXX Address, also called an Exchange Address, is a re-encoding of a
// transparent Firo P2PKH address. It is required in order to send funds to
// Binance and some other centralized exchanges.

// OP_EXCHANGEADDR is an unused bitcoin script opcode used to 'mark' the output
// as an exchange address for the recipient.
const (
	ExxMainnet      byte = 0xbb
	ExxTestnet      byte = 0xb1
	ExxSimnet       byte = 0xac
	OP_EXCHANGEADDR byte = 0xe0
)

var (
	ExxVersionedPrefix = [2]byte{0x01, 0xb9}
)

// isExxAddress determines whether the address encoding is an EXX, EXT address
// for mainnet, testnet or regtest networks.
func isExxAddress(addr string) bool {
	b, ver, err := base58.CheckDecode(addr)
	switch {
	case err != nil:
		return false
	case ver != ExxVersionedPrefix[0]:
		return false
	case len(b) != ripemd160HashSize+2:
		return false
	case b[0] != ExxVersionedPrefix[1]:
		return false
	}
	return true
}

func checksum(input []byte) (csum [4]byte) {
	h0 := sha256.Sum256(input)
	h1 := sha256.Sum256(h0[:])
	copy(csum[:], h1[:])
	return
}

// decodeExxAddress decodes a Firo exchange address.
func decodeExxAddress(encodedAddr string, net *chaincfg.Params) (btcutil.Address, error) {
	const (
		checksumLength = 4
		prefixLength   = 3
		decodedLen     = prefixLength + ripemd160HashSize + checksumLength // exx prefix + hash + checksum
	)

	decoded := base58.Decode(encodedAddr)

	if len(decoded) != decodedLen {
		return nil, fmt.Errorf("base 58 decoded to incorrect length. %d != %d", len(decoded), decodedLen)
	}
	netID := decoded[2]
	var expNet string
	switch netID {
	case ExxMainnet:
		expNet = dexfiro.MainNetParams.Name
	case ExxTestnet:
		expNet = dexfiro.TestNetParams.Name
	case ExxSimnet:
		expNet = dexfiro.RegressionNetParams.Name
	default:
		return nil, fmt.Errorf("unrecognized network name %s", expNet)
	}
	if net.Name != expNet {
		return nil, fmt.Errorf("wrong network. expected %s, got %s", net.Name, expNet)
	}
	csum := decoded[decodedLen-checksumLength:]
	expectedCsum := checksum(decoded[:decodedLen-checksumLength])
	if !bytes.Equal(csum, expectedCsum[:]) {
		return nil, errors.New("checksum mismatch")
	}
	var h [ripemd160HashSize]byte
	copy(h[:], decoded[prefixLength:decodedLen-checksumLength])
	return &addressEXX{
		hash:  h,
		netID: netID,
	}, nil
}

const ripemd160HashSize = 20

// addressEXX implements btcutil.Address and btc.PaymentScripter
type addressEXX struct {
	hash  [ripemd160HashSize]byte
	netID byte
}

var _ btcutil.Address = (*addressEXX)(nil)
var _ btc.PaymentScripter = (*addressEXX)(nil)

func (a *addressEXX) String() string {
	return a.EncodeAddress()
}

func (a *addressEXX) EncodeAddress() string {
	return base58.CheckEncode(append([]byte{ExxVersionedPrefix[1], a.netID}, a.hash[:]...), ExxVersionedPrefix[0])
}

func (a *addressEXX) ScriptAddress() []byte {
	return a.hash[:]
}

func (a *addressEXX) IsForNet(chainParams *chaincfg.Params) bool {
	switch a.netID {
	case ExxMainnet:
		return chainParams.Name == dexfiro.MainNetParams.Name
	case ExxTestnet:
		return chainParams.Name == dexfiro.TestNetParams.Name
	case ExxSimnet:
		return chainParams.Name == dexfiro.RegressionNetParams.Name
	}
	return false
}

func (a *addressEXX) PaymentScript() ([]byte, error) {
	// OP_EXCHANGEADDR << OP_DUP << OP_HASH160 << ToByteVector(keyID) << OP_EQUALVERIFY << OP_CHECKSIG;
	return txscript.NewScriptBuilder().
		AddOp(OP_EXCHANGEADDR).
		AddOp(txscript.OP_DUP).
		AddOp(txscript.OP_HASH160).
		AddData(a.hash[:]).
		AddOp(txscript.OP_EQUALVERIFY).
		AddOp(txscript.OP_CHECKSIG).
		Script()
}
