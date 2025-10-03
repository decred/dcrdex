package lbc

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/lbryio/lbcd/btcec/secp256k1/v4"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/chaincfg/v3"
	"github.com/lbryio/lbcd/txscript/v4"
	"github.com/lbryio/lbcd/txscript/v4/stdaddr"
	"github.com/lbryio/lbcd/wire"
)

var (
	tStamp        = int64(1574264305)
	tParams       = chaincfg.MainNetParams()
	invalidScript = []byte{txscript.OP_DATA_75}
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	_, _ = rand.Read(b)
	return b
}

func newPubKey() []byte {
	prk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		fmt.Printf("error creating pubkey: %v\n", err)
	}
	return prk.PubKey().SerializeCompressed()
}

type tAddrs struct {
	pkh       *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0
	sh        *stdaddr.AddressScriptHashV0
	pk1       *stdaddr.AddressPubKeyEcdsaSecp256k1V0
	pk2       *stdaddr.AddressPubKeyEcdsaSecp256k1V0
	edwards   *stdaddr.AddressPubKeyHashEd25519V0
	schnorrPK *stdaddr.AddressPubKeySchnorrSecp256k1V0
	multiSig  []byte
}

func multiSigScript(pubkeys []*stdaddr.AddressPubKeyEcdsaSecp256k1V0, numReq int64) ([]byte, error) {
	builder := txscript.NewScriptBuilder().AddInt64(numReq)
	for _, key := range pubkeys {
		script, err := AddressScript(key)
		if err != nil {
			return nil, err
		}
		builder.AddData(script)
	}
	builder.AddInt64(int64(len(pubkeys))).AddOp(txscript.OP_CHECKMULTISIG)
	return builder.Script()
}

func testAddresses() *tAddrs {
	p2pkh, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
	pk1, _ := stdaddr.NewAddressPubKeyEcdsaSecp256k1V0Raw(newPubKey(), tParams)
	pk2, _ := stdaddr.NewAddressPubKeyEcdsaSecp256k1V0Raw(newPubKey(), tParams)
	edwards, _ := stdaddr.NewAddressPubKeyHashEd25519V0(randBytes(20), tParams)
	schnorrPK, _ := stdaddr.NewAddressPubKeySchnorrSecp256k1V0Raw(newPubKey(), tParams)
	multiSig, _ := multiSigScript([]*stdaddr.AddressPubKeyEcdsaSecp256k1V0{pk1, pk2}, 1)
	p2sh, _ := stdaddr.NewAddressScriptHashV0(multiSig, tParams)
	return &tAddrs{
		pkh:       p2pkh,
		sh:        p2sh,
		pk1:       pk1,
		pk2:       pk2,
		edwards:   edwards,
		schnorrPK: schnorrPK,
		multiSig:  multiSig,
	}
}

func TestExtractBondDetailsV0(t *testing.T) {
	bondScript, _ := hex.DecodeString("041d013663b17576a91465b064a4c6bbb1b57362b5b390c00dbd33bdb2b888ac")
	lockTime, pkh, err := ExtractBondDetailsV0(0, bondScript)
	if err != nil {
		t.Fatal(err)
	}
	wantLock := uint32(1664483613)
	if lockTime != wantLock {
		t.Errorf("wanted locktime %d, got %d", wantLock, lockTime)
	}
	wantPkh, _ := hex.DecodeString("65b064a4c6bbb1b57362b5b390c00dbd33bdb2b8")
	if !bytes.Equal(pkh, wantPkh) {
		t.Errorf("wanted pkh %x, got %x", wantPkh, pkh)
	}
}

// Test ScriptType methods and pkScript identification via ParseScriptType.
// TestInputInfo verifies combined P2SH pkScript + redeemscript identification.
// This test also verifies ExtractScriptHashV0.
func TestScriptType(t *testing.T) {
	addrs := testAddresses()

	var scriptType ScriptType
	parse := func(addr stdaddr.Address, stakeOpcode byte) ([]byte, ScriptType) {
		t.Helper()
		_, pkScript := addr.PaymentScript()
		if stakeOpcode != 0 {
			pkScript = append([]byte{stakeOpcode}, pkScript...)
		}
		scriptType = ParseScriptType(0, pkScript)
		return pkScript, scriptType
	}

	check := func(name string, res bool, exp bool) {
		t.Helper()
		if res != exp {
			t.Fatalf("%s check failed. wanted %t, got %t", name, exp, res)
		}
	}

	parse(addrs.pk1, 0)
	check("p2pk-IsP2PK", scriptType.IsP2PK(), true)
	check("p2pk-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2pk-IsP2SH", scriptType.IsP2SH(), false)
	check("p2pk-IsStake", scriptType.IsStake(), false)
	check("p2pk-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.pkh, 0)
	check("p2pkh-IsP2PK", scriptType.IsP2PK(), false)
	check("p2pkh-IsP2PKH", scriptType.IsP2PKH(), true)
	check("p2pkh-IsP2SH", scriptType.IsP2SH(), false)
	check("p2pkh-IsStake", scriptType.IsStake(), false)
	check("p2pkh-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.pkh, txscript.OP_SSGEN)
	check("stakePKH-IsP2PK", scriptType.IsP2PK(), false)
	check("stakePKH-IsP2PKH", scriptType.IsP2PKH(), true)
	check("stakePKH-IsP2SH", scriptType.IsP2SH(), false)
	check("stakePKH-IsStake", scriptType.IsStake(), true)
	check("stakePKH-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.edwards, 0)
	check("edwards-IsP2PK", scriptType.IsP2PK(), false)
	check("edwards-IsP2PKH", scriptType.IsP2PKH(), true)
	check("edwards-IsP2SH", scriptType.IsP2SH(), false)
	check("edwards-IsStake", scriptType.IsStake(), false)
	check("edwards-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.schnorrPK, 0)
	check("schnorrPK-IsP2PK", scriptType.IsP2PK(), true)
	check("schnorrPK-IsP2PKH", scriptType.IsP2PKH(), false)
	check("schnorrPK-IsP2SH", scriptType.IsP2SH(), false)
	check("schnorrPK-IsStake", scriptType.IsStake(), false)
	check("schnorrPK-IsMultiSig", scriptType.IsMultiSig(), false)

	pkScript, scriptType := parse(addrs.sh, 0)
	check("p2sh-IsP2PK", scriptType.IsP2PK(), false)
	check("p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("p2pkh-IsStake", scriptType.IsStake(), false)

	scriptHash := ExtractScriptHashV0(pkScript)
	if scriptHash == nil {
		t.Fatalf("error extracting non-stake script hash")
	}

	// Identification of a pkScript combined with a multisig redeemscript is
	// verified in TestInputInfo; here we just test the IsMultiSig method.
	scriptType |= ScriptMultiSig
	check("p2sh-IsMultiSig", scriptType.IsMultiSig(), true)
	// retest other Is methods now that we've set the multisig bit
	check("p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("p2pkh-IsStake", scriptType.IsStake(), false)

	pkScript, scriptType = parse(addrs.sh, txscript.OP_SSGEN)
	check("stake-p2sh-IsP2PK", scriptType.IsP2PK(), false)
	check("stake-p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("stake-p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("stake-p2pkh-IsStake", scriptType.IsStake(), true)

	scriptType |= ScriptMultiSig
	check("stake-p2sh-IsMultiSig", scriptType.IsMultiSig(), true)
	check("stake-p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("stake-p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("stake-p2pkh-IsStake", scriptType.IsStake(), true)

	scriptHash = ExtractScriptHashV0(pkScript)
	if scriptHash == nil {
		t.Fatalf("error extracting stake script hash")
	}
}

func TestMakeContract(t *testing.T) {
	ra, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
	recipient := ra.String()
	sa, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
	sender := sa.String()
	badAddr := "notanaddress"

	// Bad recipient
	_, err := MakeContract(badAddr, sender, randBytes(32), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for bad recipient")
	}
	// Wrong recipient address type
	p2sh, _ := stdaddr.NewAddressScriptHashV0(randBytes(50), tParams)
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
	altAddr, _ := stdaddr.NewAddressPubKeyHashEd25519V0(randBytes(20), tParams)
	_, err = MakeContract(recipient, altAddr.String(), randBytes(32), tStamp, tParams)
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

func TestMakePrivateContract(t *testing.T) {
	redeemPKH := randBytes(20)
	refundPKH := randBytes(20)
	locktime := int64(1234567890)

	// Test with valid parameters
	script, err := MakePrivateContract(redeemPKH, refundPKH, locktime)
	if err != nil {
		t.Fatalf("error for valid contract parameters: %v", err)
	}
	if len(script) != PrivateSwapContractSize {
		t.Fatalf("expected script length %d, got %d", PrivateSwapContractSize, len(script))
	}

	// Test with invalid redeem PKH size
	_, err = MakePrivateContract(randBytes(10), refundPKH, locktime)
	if err == nil {
		t.Fatalf("no error for invalid redeem PKH size")
	}

	// Test with invalid refund PKH size
	_, err = MakePrivateContract(redeemPKH, randBytes(15), locktime)
	if err == nil {
		t.Fatalf("no error for invalid refund PKH size")
	}

	// Test with both invalid PKH sizes
	_, err = MakePrivateContract(randBytes(10), randBytes(15), locktime)
	if err == nil {
		t.Fatalf("no error for both invalid PKH sizes")
	}

	// Verify the generated script can be parsed by ExtractPrivateSwapDetails
	refunder, redeemer, extractedLocktime, err := ExtractPrivateSwapDetails(script)
	if err != nil {
		t.Fatalf("error extracting private swap details: %v", err)
	}

	// Verify extracted values match input values
	if !bytes.Equal(refunder[:], refundPKH) {
		t.Fatalf("refunder PKH mismatch: expected %x, got %x", refundPKH, refunder[:])
	}
	if !bytes.Equal(redeemer[:], redeemPKH) {
		t.Fatalf("redeemer PKH mismatch: expected %x, got %x", redeemPKH, redeemer[:])
	}
	if extractedLocktime != locktime {
		t.Fatalf("locktime mismatch: expected %d, got %d", locktime, extractedLocktime)
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
			// Zero-valued output fails txscript.IsUnspendable.
			"zero value with zero relay fee",
			wire.TxOut{Value: 0, PkScript: pkScript},
			0,
			true,
		},
		{
			// Zero value is dust with any relay fee"
			"zero value with very small tx fee",
			wire.TxOut{Value: 0, PkScript: pkScript},
			1,
			true,
		},
		{
			"38 byte public key script with value 6419",
			wire.TxOut{Value: 6419, PkScript: pkScript},
			10,
			true,
		},
		{
			"38 byte public key script with value 6420",
			wire.TxOut{Value: 6420, PkScript: pkScript},
			10,
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
		}
	}
}

// Test InputInfo, ParseScriptType, ExtractScriptData, ExtractScriptAddrs, and
// ExtractScriptHash with a non-standard script.
func Test_nonstandardScript(t *testing.T) {
	// The tx hash of a LBC testnet swap contract.
	contractTx, err := chainhash.NewHashFromStr("4a14a2d79c1374d286ebd68d2c104343bcf8be44ed54045b5963fbf73667cecc")
	if err != nil {
		t.Fatal(err)
	}
	vout := 0 // the contract output index, 1 is change

	// The contract output's P2SH pkScript and the corresponding redeem script
	// (the actual contract).
	scriptVersion := uint16(0)
	pkScript, _ := hex.DecodeString("a9146d4bc656b3287a0e6b0d38802db6400f7053111787") // verboseTx.Vout[vout].ScriptPubKey.Hex from getrawtransaction(verbose)
	contractScript, _ := hex.DecodeString("6382012088c020c6de3217594af525fb" +
		"57eaf1f2aae04c305ddc67d465edd325151685fc5a5e428876a914479eddda81" +
		"b6ed289515f2dbcc95f05ce80dff466704fe03865eb17576a91498a67ed502ad" +
		"b04173d88fb1ef92d0317711c3816888ac")

	scriptType := ParseScriptType(scriptVersion, pkScript)
	if !scriptType.IsP2SH() {
		t.Fatalf("script was not P2SH, got %v (see script.go)", scriptType)
	}

	// Double check that the pkScript's script hash matches the hash of the
	// redeem (contract) script.
	scriptHash := ExtractScriptHash(scriptVersion, pkScript)
	if scriptHash == nil {
		t.Fatalf("ExtractScriptHash failed")
	}
	if !bytes.Equal(btcutil.Hash160(contractScript), scriptHash) {
		t.Fatalf("script hash check failed for output %s,%d", contractTx, vout)
	}

	// ExtractScriptAddrs should detect non-standard scripts.
	chainParams := chaincfg.TestNet3Params()
	scriptType, _ = ExtractScriptAddrs(scriptVersion, contractScript, chainParams)
	if scriptType != ScriptUnsupported {
		t.Errorf("expected non-standard script")
	}
	// ... as should ExtractScriptData
	scriptType, _, _ = ExtractScriptData(scriptVersion, contractScript, chainParams)
	if scriptType != ScriptUnsupported {
		t.Errorf("expected non-standard script")
	}

	// InputInfo currently calls ExtractScriptAddrs at the time of writing, but
	// InputInfo should error regardless.
	spendInfo, err := InputInfo(scriptVersion, pkScript, contractScript, chainParams)
	if err != nil {
		t.Fatalf("InputInfo failed: %v", err)
	}
	if spendInfo.ScriptType != ScriptP2SH {
		t.Errorf("ScriptType should still be P2SH")
	}
	if !spendInfo.NonStandardScript {
		t.Errorf("contract script should be non-standard")
	}
}

func TestExtractScriptAddrs(t *testing.T) {
	addrs := testAddresses()
	type test struct {
		addr   stdaddr.Address
		nonStd bool
		script []byte
		pk     int
		pkh    int
		sigs   int
	}

	tests := []test{
		{addrs.pkh, false, nil, 0, 1, 1},
		{addrs.sh, false, nil, 0, 1, 1},
		{nil, false, addrs.multiSig, 2, 0, 1},
		{nil, true, []byte{1}, 0, 0, 0},
	}

	for _, tt := range tests {
		s := tt.script
		if s == nil {
			_, s = tt.addr.PaymentScript()
		}
		scriptType, scriptAddrs := ExtractScriptAddrs(0, s, tParams)
		if (scriptType == ScriptUnsupported) != tt.nonStd {
			t.Fatalf("expected nonStd=%v, got %v", tt.nonStd, scriptType)
		}
		if len(scriptAddrs.PubKeys) != tt.pk {
			t.Fatalf("wrong number of hash addresses. wanted %d, got %d", tt.pk, len(scriptAddrs.PubKeys))
		}
		if len(scriptAddrs.PkHashes) != tt.pkh {
			t.Fatalf("wrong number of pubkey-hash addresses. wanted %d, got %d", tt.pkh, len(scriptAddrs.PkHashes))
		}
		if scriptAddrs.NRequired != tt.sigs {
			t.Fatalf("wrong number of required signatures. wanted %d, got %d", tt.sigs, scriptAddrs.NRequired)
		}
	}

}

func TestExtractSwapDetails(t *testing.T) {
	rAddr, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
	recipient := rAddr.String()
	sAddr, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
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

	// incorrect length
	_, _, _, _, err = ExtractSwapDetails(contract[:len(contract)-1], tParams)
	if err == nil {
		t.Fatalf("no error for vandalized contract")
	} else if !strings.HasPrefix(err.Error(), "incorrect swap contract length") {
		t.Errorf("incorrect error for incorrect swap contract length: %v", err)
	}

	// bad secret size
	contract[3] = 250
	_, _, _, _, err = ExtractSwapDetails(contract, tParams)
	if err == nil {
		t.Fatalf("no error for contract with invalid secret size")
	} else if !strings.HasPrefix(err.Error(), "invalid secret size") {
		t.Errorf("incorrect error for invalid secret size: %v", err)
	}
}

func TestInputInfo(t *testing.T) {
	addrs := testAddresses()
	var spendInfo *SpendInfo
	var err error

	check := func(name string, sigScriptSize uint32, scriptType ScriptType) {
		if spendInfo.SigScriptSize != sigScriptSize {
			t.Fatalf("%s: wrong SigScriptSize, wanted %d, got %d", name, sigScriptSize, spendInfo.SigScriptSize)
		}
		if spendInfo.ScriptType != scriptType {
			t.Fatalf("%s: wrong ScriptType, wanted %d, got %d", name, scriptType, spendInfo.ScriptType)
		}
	}

	var script []byte
	payToAddr := func(addr stdaddr.Address, redeem []byte) {
		t.Helper()
		_, script = addr.PaymentScript()
		spendInfo, err = InputInfo(0, script, redeem, tParams)
		if err != nil {
			t.Fatalf("InputInfo script: %v", err)
		}
	}

	payToAddr(addrs.pkh, nil)
	check("p2pkh", P2PKHSigScriptSize, ScriptP2PKH)

	payToAddr(addrs.sh, addrs.multiSig)
	check("p2sh", 74+uint32(len(addrs.multiSig))+1, ScriptP2SH|ScriptMultiSig)

	payToAddr(addrs.sh, []byte{1, 2, 3})
	check("p2sh with non-standard redeemscript", 0 /* unknown sigscript size */, ScriptP2SH)
	if !spendInfo.NonStandardScript {
		t.Fatalf("non-standard redeemscript was not reported as such")
	}

	// bad version
	_, script = addrs.pkh.PaymentScript()
	spendInfo, err = InputInfo(1, script, nil, tParams)
	if err != dex.UnsupportedScriptError {
		t.Fatalf("InputInfo should have errored for script version 1")
	}

	// Unknown script type.
	_, err = InputInfo(0, []byte{0x02, 0x03}, nil, tParams)
	if err == nil {
		t.Fatalf("no error for unknown script type")
	}

	// InputInfo P2SH requires a redeem script
	version, script := addrs.sh.PaymentScript()
	_, err = InputInfo(version, script, nil, tParams)
	if err == nil {
		t.Fatalf("no error for missing redeem script")
	}
}

func TestFindKeyPush(t *testing.T) {
	rAddr, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
	recipient := rAddr.String()
	sAddr, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(randBytes(20), tParams)
	sender := sAddr.String()

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	contract, _ := MakeContract(recipient, sender, secretHash[:], tStamp, tParams)
	contractHash := btcutil.Hash160(contract)
	sigScript, err := RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	if err != nil {
		t.Fatalf("error creating redeem script: %v", err)
	}

	key, err := FindKeyPush(0, sigScript, contractHash, tParams)
	if err != nil {
		t.Fatalf("findKeyPush error: %v", err)
	}
	if !bytes.Equal(key, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, key)
	}

	// Empty script is an error.
	_, err = FindKeyPush(0, []byte{}, contractHash, tParams)
	if err == nil {
		t.Fatalf("no error for empty script")
	}

	// Bad script
	_, err = FindKeyPush(0, invalidScript, contractHash, tParams)
	if err == nil {
		t.Fatalf("no error for bad script")
	}

	// Random but valid contract won't work.
	contract, _ = MakeContract(recipient, sender, randBytes(32), tStamp, tParams)
	sigScript, err = RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	if err != nil {
		t.Fatalf("error creating contract: %v", err)
	}
	_, err = FindKeyPush(0, sigScript, contractHash, tParams)
	if err == nil {
		t.Fatalf("no error for bad script")
	}
}

func TestDataPrefixSize(t *testing.T) {
	tests := []struct {
		name    string
		theData []byte
		want    uint8
	}{
		{
			name:    "empty",
			theData: nil,
			want:    0,
		},
		{
			name:    "oneLow",
			theData: []byte{16},
			want:    0,
		},
		{
			name:    "OP_1NEGATE",
			theData: []byte{0x81},
			want:    0,
		},
		{
			name:    "oneHigh",
			theData: []byte{17},
			want:    1,
		},
		{
			name:    "smallMultiByte0",
			theData: make([]byte, 2),
			want:    1,
		},
		{
			name:    "smallMultiByte1",
			theData: make([]byte, 75),
			want:    1,
		},
		{
			name:    "mediumMultiByte0",
			theData: make([]byte, 76),
			want:    2,
		},
		{
			name:    "contract",
			theData: make([]byte, SwapContractSize),
			want:    2,
		},
		{
			name:    "mediumMultiByte1",
			theData: make([]byte, 255),
			want:    2,
		},
		{
			name:    "largeMultiByte0",
			theData: make([]byte, 256),
			want:    3,
		},
		{
			name:    "largeMultiByte1",
			theData: make([]byte, 65535),
			want:    3,
		},
		{
			name:    "megaMultiByte0",
			theData: make([]byte, 65536),
			want:    5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DataPrefixSize(tt.theData); got != tt.want {
				t.Errorf("DataPrefixSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func hexBytes(h string) []byte {
	b, err := hex.DecodeString(h)
	if err != nil {
		panic(err.Error())
	}
	return b
}

func TestIsRefundScript(t *testing.T) {
	tests := []struct {
		name          string
		scriptVersion uint16
		sigScript     []byte
		contract      []byte
		want          bool
	}{
		{
			"redeem",
			0,
			hexBytes("47304402203cf1e969830a6255d02e40a9333b502d802f0c17331e54dc5d8028fb6a3bd54c02202cfc3673c8013416e38dd27d3c96082a4f069cf296ca2bbba7d039308df1bdc401210227b951b91a01e11600cdd1d3e9f2894e2e38567b6883b9b3e7a4f99946a3d445209a5a58c653c2c384825bc4217443a2b3e3dead04db5e0586570c774706d97a0d514c616382012088c020c66b3b91cffadc613689cf9c391c7792307e5d5390407ad3e33b6ff0bf4a95218876a91425e5d9b138e16fa526bc765a721193da79fc5c32670420489d63b17576a9147f1686f0a6f1b0afd5b9efaee37d234b417ca2ee6888ac"),
			hexBytes("6382012088c020c66b3b91cffadc613689cf9c391c7792307e5d5390407ad3e33b6ff0bf4a95218876a91425e5d9b138e16fa526bc765a721193da79fc5c32670420489d63b17576a9147f1686f0a6f1b0afd5b9efaee37d234b417ca2ee6888ac"),
			false,
		},
		{
			"refund",
			0,
			hexBytes("473044022063793a405b3c60a37180d009f191bf68a8ce0560093718612220b019596ebd9602201f9028644d45744279f5676db18ba3621760eb33d18d6ba74909a2e98393e40c012102bc03f292e7ec067c10cdcbc22eb9dc49b2693a1b7dd7c3b4c4d9a0bf054f3f2d004c616382012088c020eec3b3ffb1bf921701653437600eba8a8fc95dd5bbefa65e977b3d91db9ca6248876a9145ca2a115488f1b264591a014734d33443bbfa379670410bf3563b17576a914462748b29df61bd1fedd7dc1a953392aa2c05d996888ac"),
			hexBytes("6382012088c020eec3b3ffb1bf921701653437600eba8a8fc95dd5bbefa65e977b3d91db9ca6248876a9145ca2a115488f1b264591a014734d33443bbfa379670410bf3563b17576a914462748b29df61bd1fedd7dc1a953392aa2c05d996888ac"),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if is := IsRefundScript(tt.scriptVersion, tt.sigScript, tt.contract, false); is != tt.want {
				t.Errorf("want %v, got %v", tt.want, is)
			}
		})
	}
}
