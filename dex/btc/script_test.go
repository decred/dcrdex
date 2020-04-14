package btc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

var (
	tStamp        = int64(1574264305)
	tParams       = &chaincfg.MainNetParams
	invalidScript = []byte{txscript.OP_DATA_75}
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func newPubKey() []byte {
	priv, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		fmt.Printf("error creating pubkey: %v\n", err)
	}
	return priv.PubKey().SerializeCompressed()
}

type tAddrs struct {
	pkh      *btcutil.AddressPubKeyHash
	wpkh     *btcutil.AddressWitnessPubKeyHash
	sh       *btcutil.AddressScriptHash
	wsh      *btcutil.AddressWitnessScriptHash
	pk1      *btcutil.AddressPubKey
	pk2      *btcutil.AddressPubKey
	multiSig []byte
}

func testAddresses() *tAddrs {
	p2pkh, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	p2wpkh, _ := btcutil.NewAddressWitnessPubKeyHash(randBytes(20), tParams)
	p2wsh, _ := btcutil.NewAddressWitnessScriptHash(randBytes(32), tParams)
	pk1, _ := btcutil.NewAddressPubKey(newPubKey(), tParams)
	pk2, _ := btcutil.NewAddressPubKey(newPubKey(), tParams)
	multiSig, _ := txscript.MultiSigScript([]*btcutil.AddressPubKey{pk1, pk2}, 1)
	p2sh, _ := btcutil.NewAddressScriptHash(multiSig, tParams)
	return &tAddrs{
		pkh:      p2pkh,
		wpkh:     p2wpkh,
		sh:       p2sh,
		wsh:      p2wsh,
		pk1:      pk1,
		pk2:      pk2,
		multiSig: multiSig,
	}
}

func TestParseScriptType(t *testing.T) {
	addrs := testAddresses()

	var scriptType BTCScriptType
	parse := func(addr btcutil.Address, redeem []byte) {
		pkScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			t.Fatalf("error creating script for address %s", addr)
		}
		scriptType = ParseScriptType(pkScript, redeem)
	}

	check := func(name string, res bool, exp bool) {
		if res != exp {
			t.Fatalf("%s check failed. wanted %t, got %t", name, exp, res)
		}
	}

	parse(addrs.pkh, nil)
	check("p2pkh-IsP2PKH", scriptType.IsP2PKH(), true)
	check("p2pkh-IsP2SH", scriptType.IsP2SH(), false)
	check("p2pkh-IsP2WPKH", scriptType.IsP2WPKH(), false)
	check("p2pkh-IsP2WSH", scriptType.IsP2WSH(), false)
	check("p2pkh-IsMultiSig", scriptType.IsMultiSig(), false)
	check("p2pkh-IsSegwit", scriptType.IsSegwit(), false)

	parse(addrs.wpkh, nil)
	check("p2wpkh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2wpkh-IsP2SH", scriptType.IsP2SH(), false)
	check("p2wpkh-IsP2WPKH", scriptType.IsP2WPKH(), true)
	check("p2wpkh-IsP2WSH", scriptType.IsP2WSH(), false)
	check("p2wpkh-IsMultiSig", scriptType.IsMultiSig(), false)
	check("p2wpkh-IsSegwit", scriptType.IsSegwit(), true)

	parse(addrs.sh, addrs.multiSig)
	check("p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("p2sh-IsP2WPKH", scriptType.IsP2WPKH(), false)
	check("p2sh-IsP2WSH", scriptType.IsP2WSH(), false)
	check("p2sh-IsMultiSig", scriptType.IsMultiSig(), true)
	check("p2sh-IsSegwit", scriptType.IsSegwit(), false)

	parse(addrs.wsh, nil)
	check("p2wsh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2wsh-IsP2SH", scriptType.IsP2SH(), false)
	check("p2wsh-IsP2WPKH", scriptType.IsP2WPKH(), false)
	check("p2wsh-IsP2WSH", scriptType.IsP2WSH(), true)
	check("p2wsh-IsMultiSig", scriptType.IsMultiSig(), false)
	check("p2wsh-IsSegwit", scriptType.IsSegwit(), true)
}

func TestMakeContract(t *testing.T) {
	ra, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	recipient := ra.String()
	sa, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	sender := sa.String()
	badAddr := "notanaddress"

	// Bad recipient
	_, err := MakeContract(badAddr, sender, randBytes(32), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for bad recipient")
	}
	// Wrong recipient address type
	p2sh, _ := btcutil.NewAddressScriptHash(randBytes(50), tParams)
	_, err = MakeContract(p2sh.String(), sender, randBytes(32), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for wrong recipient address type")
	}

	// Bad sender
	_, err = MakeContract(recipient, badAddr, randBytes(32), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for bad sender")
	}
	// Wrong sender address type.
	p2wpkh, _ := btcutil.NewAddressWitnessPubKeyHash(randBytes(20), tParams)
	_, err = MakeContract(recipient, p2wpkh.String(), randBytes(32), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for wrong sender address type")
	}

	// Bad secret hash
	_, err = MakeContract(recipient, sender, randBytes(10), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for bad secret hash")
	}

	// Good to go
	_, err = MakeContract(recipient, sender, randBytes(32), tStamp, tParams)
	if err != nil {
		t.Fatalf("error for valid contract parameters: %v", err)
	}
}

func TestIsDust(t *testing.T) {
	pkScript := []byte{0x76, 0xa9, 0x21, 0x03, 0x2f, 0x7e, 0x43,
		0x0a, 0xa4, 0xc9, 0xd1, 0x59, 0x43, 0x7e, 0x84, 0xb9,
		0x75, 0xdc, 0x76, 0xd9, 0x00, 0x3b, 0xf0, 0x92, 0x2c,
		0xf3, 0xaa, 0x45, 0x28, 0x46, 0x4b, 0xab, 0x78, 0x0d,
		0xba, 0x5e, 0x88, 0xac}

	tests := []struct {
		name     string // test description
		txOut    wire.TxOut
		relayFee uint64 // minimum relay transaction fee.
		isDust   bool
	}{
		{
			// Any value is allowed with a zero relay fee.
			"zero value with zero relay fee",
			wire.TxOut{Value: 0, PkScript: pkScript},
			0,
			false,
		},
		{
			// Zero value is dust with any relay fee"
			"zero value with very small tx fee",
			wire.TxOut{Value: 0, PkScript: pkScript},
			1,
			true,
		},
		{
			"38 byte public key script with value 584",
			wire.TxOut{Value: 584, PkScript: pkScript},
			1000,
			true,
		},
		{
			"38 byte public key script with value 585",
			wire.TxOut{Value: 585, PkScript: pkScript},
			1000,
			false,
		},
		{
			// Maximum allowed value is never dust.
			"max satoshi amount is never dust",
			wire.TxOut{Value: btcutil.MaxSatoshi, PkScript: pkScript},
			btcutil.MaxSatoshi,
			false,
		},
		{
			// Maximum int64 value causes overflow.
			"maximum int64 value",
			wire.TxOut{Value: 1<<63 - 1, PkScript: pkScript},
			1<<63 - 1,
			true,
		},
		{
			// Unspendable pkScript due to an invalid public key
			// script.
			"unspendable pkScript",
			wire.TxOut{Value: 5000, PkScript: []byte{0x01}},
			0, // no relay fee
			true,
		},
	}
	for _, test := range tests {
		res := IsDust(&test.txOut, test.relayFee)
		if res != test.isDust {
			t.Fatalf("Dust test '%s' failed: want %v got %v",
				test.name, test.isDust, res)
			continue
		}
	}
}

func TestExtractScriptAddrs(t *testing.T) {
	// Invalid script
	_, nonStd, err := ExtractScriptAddrs(invalidScript, tParams)
	if err == nil {
		t.Fatalf("no error for bad script")
	}
	if !nonStd {
		t.Errorf("expected non-standard script")
	}

	addrs := testAddresses()
	type test struct {
		addr   btcutil.Address
		nonStd bool
		script []byte
		pk     int
		pkh    int
		sigs   int
	}

	tests := []test{
		{addrs.pkh, false, nil, 0, 1, 1},
		{addrs.sh, false, nil, 0, 1, 1},
		{addrs.wpkh, false, nil, 0, 1, 1},
		{addrs.wsh, false, nil, 0, 1, 1},
		{nil, false, addrs.multiSig, 2, 0, 1},
	}

	for _, tt := range tests {
		s := tt.script
		if s == nil {
			s, _ = txscript.PayToAddrScript(tt.addr)
		}
		scriptAddrs, nonStd, err := ExtractScriptAddrs(s, tParams)
		if err != nil {
			t.Fatalf("error extracting script addresses: %v", err)
		}
		if nonStd != tt.nonStd {
			t.Fatalf("expected nonStd=%v, got %v", tt.nonStd, nonStd)
		}
		if scriptAddrs.NumPK != tt.pk {
			t.Fatalf("wrong number of hash addresses. wanted %d, got %d", tt.pk, scriptAddrs.NumPK)
		}
		if scriptAddrs.NumPKH != tt.pkh {
			t.Fatalf("wrong number of pubkey-hash addresses. wanted %d, got %d", tt.pkh, scriptAddrs.NumPKH)
		}
		if scriptAddrs.NRequired != tt.sigs {
			t.Fatalf("wrong number of required signatures. wanted %d, got %d", tt.sigs, scriptAddrs.NRequired)
		}
	}

}

func TestExtractSwapDetails(t *testing.T) {
	rAddr, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	recipient := rAddr.String()
	sAddr, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	sender := sAddr.String()
	keyHash := randBytes(32)
	contract, err := MakeContract(recipient, sender, keyHash, tStamp, tParams)
	if err != nil {
		t.Fatalf("error creating contract: %v", err)
	}

	sa, ra, lockTime, secretHash, err := ExtractSwapDetails(contract, tParams)
	if err != nil {
		t.Fatalf("error for valid contract: %v", err)
	}
	if sa.String() != sender {
		t.Fatalf("sender address mismatch. wanted %s, got %s", sender, sa.String())
	}
	if ra.String() != recipient {
		t.Fatalf("recipient address mismatch. wanted %s, got %s", recipient, ra.String())
	}
	if lockTime != uint64(tStamp) {
		t.Fatalf("incorrect lock time. wanted 5, got %d", lockTime)
	}
	if !bytes.Equal(secretHash, keyHash) {
		t.Fatalf("wrong secret hash. wanted %x, got %x", keyHash, secretHash)
	}

	_, _, _, _, err = ExtractSwapDetails(contract[:len(contract)-1], tParams)
	if err == nil {
		t.Fatalf("no error for vandalized contract")
	}
}

func TestInputInfo(t *testing.T) {
	addrs := testAddresses()
	var spendInfo *SpendInfo
	var err error

	check := func(name string, sigScriptSize, witnessSize uint32, scriptType BTCScriptType) {
		if spendInfo.SigScriptSize != sigScriptSize {
			t.Fatalf("%s: wrong SigScriptSize, wanted %d, got %d", name, sigScriptSize, spendInfo.SigScriptSize)
		}
		if spendInfo.WitnessSize != witnessSize {
			t.Fatalf("%s: wrong WitnessSize, wanted %d, got %d", name, witnessSize, spendInfo.WitnessSize)
		}
		if spendInfo.ScriptType != scriptType {
			t.Fatalf("%s: wrong ScriptType, wanted %d, got %d", name, scriptType, spendInfo.ScriptType)
		}
	}

	var script []byte
	payToAddr := func(addr btcutil.Address, redeem []byte) {
		script, _ = txscript.PayToAddrScript(addr)
		spendInfo, err = InputInfo(script, redeem, tParams)
		if err != nil {
			t.Fatalf("InputInfo script: %v", err)
		}
	}

	payToAddr(addrs.pkh, nil)
	check("p2pkh", RedeemP2PKHSigScriptSize, 0, ScriptP2PKH)

	payToAddr(addrs.sh, addrs.multiSig)
	check("p2sh", 74+uint32(len(addrs.multiSig))+2, 0, ScriptP2SH|ScriptMultiSig)

	payToAddr(addrs.wpkh, nil)
	check("p2wpkh", 0, RedeemP2WPKHInputWitnessWeight, ScriptP2PKH|ScriptTypeSegwit)

	payToAddr(addrs.wsh, addrs.multiSig)
	check("p2wsh", 0, 74+uint32(len(addrs.multiSig))+1, ScriptP2SH|ScriptTypeSegwit|ScriptMultiSig)

	// Unknown script type.
	_, err = InputInfo([]byte{0x02, 0x03}, nil, tParams)
	if err == nil {
		t.Fatalf("no error for unknown script type")
	}

	// InputInfo P2SH requires a redeem script
	script, _ = txscript.PayToAddrScript(addrs.sh)
	_, err = InputInfo(script, nil, tParams)
	if err == nil {
		t.Fatalf("no error for missing redeem script")
	}
	// Redeem script must be parseable.
	_, err = InputInfo(script, invalidScript, tParams)
	if err == nil {
		t.Fatalf("no error for unparseable redeem script")
	}
}

func TestFindKeyPush(t *testing.T) {
	rAddr, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	recipient := rAddr.String()
	sAddr, _ := btcutil.NewAddressPubKeyHash(randBytes(20), tParams)
	sender := sAddr.String()

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	contract, _ := MakeContract(recipient, sender, secretHash[:], tStamp, tParams)
	contractHash := btcutil.Hash160(contract)
	sigScript, err := RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	if err != nil {
		t.Fatalf("error creating redeem script: %v", err)
	}

	key, err := FindKeyPush(sigScript, contractHash, tParams)
	if err != nil {
		t.Fatalf("findKeyPush error: %v", err)
	}
	if !bytes.Equal(key, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, key)
	}

	// Empty script is an error.
	_, err = FindKeyPush([]byte{}, contractHash, tParams)
	if err == nil {
		t.Fatalf("no error for empty script")
	}

	// Bad script
	_, err = FindKeyPush(invalidScript, contractHash, tParams)
	if err == nil {
		t.Fatalf("no error for bad script")
	}

	// Random but valid contract won't work.
	contract, _ = MakeContract(recipient, sender, randBytes(32), tStamp, tParams)
	sigScript, err = RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	if err != nil {
		t.Fatalf("error creating contract: %v", err)
	}
	_, err = FindKeyPush(sigScript, contractHash, tParams)
	if err == nil {
		t.Fatalf("no error for bad script")
	}
}

func TestExtractContractHash(t *testing.T) {
	addrs := testAddresses()
	// non-hex
	_, err := ExtractContractHash("zz", tParams)
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// invalid script
	_, err = ExtractContractHash(hex.EncodeToString(invalidScript), tParams)
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// multi-sig
	_, err = ExtractContractHash(hex.EncodeToString(addrs.multiSig), tParams)
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// wrong script types
	p2pkh, _ := txscript.PayToAddrScript(addrs.pkh)
	_, err = ExtractContractHash(hex.EncodeToString(p2pkh), tParams)
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// ok
	p2sh, _ := txscript.PayToAddrScript(addrs.sh)
	checkHash, err := ExtractContractHash(hex.EncodeToString(p2sh), tParams)
	if err != nil {
		t.Fatalf("error extracting contract hash: %v", err)
	}
	if !bytes.Equal(checkHash, addrs.sh.ScriptAddress()) {
		t.Fatalf("hash mismatch. wanted %x, got %x", addrs.sh.ScriptAddress(), checkHash)
	}
}
