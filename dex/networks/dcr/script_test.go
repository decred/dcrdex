package dcr

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
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
	pkh       *dcrutil.AddressPubKeyHash
	sh        *dcrutil.AddressScriptHash
	pk1       *dcrutil.AddressSecpPubKey
	pk2       *dcrutil.AddressSecpPubKey
	edwards   *dcrutil.AddressPubKeyHash
	schnorrPK *dcrutil.AddressSecSchnorrPubKey
	multiSig  []byte
}

func multiSigScript(pubkeys []*dcrutil.AddressSecpPubKey, numReq int64) ([]byte, error) {
	builder := txscript.NewScriptBuilder().AddInt64(numReq)
	for _, key := range pubkeys {
		builder.AddData(key.ScriptAddress())
	}
	builder.AddInt64(int64(len(pubkeys))).AddOp(txscript.OP_CHECKMULTISIG)
	return builder.Script()
}

func testAddresses() *tAddrs {
	p2pkh, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
	pk1, _ := dcrutil.NewAddressSecpPubKey(newPubKey(), tParams)
	pk2, _ := dcrutil.NewAddressSecpPubKey(newPubKey(), tParams)
	edwards, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEd25519)
	schnorrPK, _ := dcrutil.NewAddressSecSchnorrPubKey(newPubKey(), tParams)
	multiSig, _ := multiSigScript([]*dcrutil.AddressSecpPubKey{pk1, pk2}, 1)
	p2sh, _ := dcrutil.NewAddressScriptHash(multiSig, tParams)
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

func TestParseScriptType(t *testing.T) {
	addrs := testAddresses()

	var scriptType DCRScriptType
	parse := func(addr dcrutil.Address, redeem []byte, stakeOpcode byte) ([]byte, DCRScriptType) {
		t.Helper()
		pkScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			t.Fatalf("error creating script for address %s: %v", addr, err)
		}
		if stakeOpcode != 0 {
			pkScript = append([]byte{stakeOpcode}, pkScript...)
		}
		scriptType = ParseScriptType(CurrentScriptVersion, pkScript, redeem)
		return pkScript, scriptType
	}

	check := func(name string, res bool, exp bool) {
		if res != exp {
			t.Fatalf("%s check failed. wanted %t, got %t", name, exp, res)
		}
	}

	parse(addrs.pkh, nil, 0)
	check("p2pkh-IsP2PKH", scriptType.IsP2PKH(), true)
	check("p2pkh-IsP2SH", scriptType.IsP2SH(), false)
	check("p2pkh-IsStake", scriptType.IsStake(), false)
	check("p2pkh-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.pkh, nil, txscript.OP_SSGEN)
	check("stakePKH-IsP2PKH", scriptType.IsP2PKH(), true)
	check("stakePKH-IsP2SH", scriptType.IsP2SH(), false)
	check("stakePKH-IsStake", scriptType.IsStake(), true)
	check("stakePKH-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.edwards, nil, 0)
	check("edwards-IsP2PKH", scriptType.IsP2PKH(), true)
	check("edwards-IsP2SH", scriptType.IsP2SH(), false)
	check("edwards-IsStake", scriptType.IsStake(), false)
	check("edwards-IsMultiSig", scriptType.IsMultiSig(), false)

	parse(addrs.schnorrPK, nil, 0)
	check("schnorrPK-IsP2PKH", scriptType.IsP2PKH(), false)
	check("schnorrPK-IsP2SH", scriptType.IsP2SH(), false)
	check("schnorrPK-IsStake", scriptType.IsStake(), false)
	check("schnorrPK-IsMultiSig", scriptType.IsMultiSig(), false)

	pkScript, scriptType := parse(addrs.sh, addrs.multiSig, 0)
	check("p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("p2pkh-IsStake", scriptType.IsStake(), false)
	check("p2sh-IsMultiSig", scriptType.IsMultiSig(), true)

	_, err := ExtractScriptHashByType(scriptType, pkScript)
	if err != nil {
		t.Fatalf("error extracting non-stake script hash: %v", err)
	}

	pkScript, scriptType = parse(addrs.sh, addrs.multiSig, txscript.OP_SSGEN)
	check("stake-p2sh-IsP2PKH", scriptType.IsP2PKH(), false)
	check("stake-p2sh-IsP2SH", scriptType.IsP2SH(), true)
	check("stake-p2pkh-IsStake", scriptType.IsStake(), true)
	check("stake-p2sh-IsMultiSig", scriptType.IsMultiSig(), true)

	_, err = ExtractScriptHashByType(scriptType, pkScript)
	if err != nil {
		t.Fatalf("error extracting stake script hash: %v", err)
	}
}

func TestMakeContract(t *testing.T) {
	ra, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
	recipient := ra.String()
	sa, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
	sender := sa.String()
	badAddr := "notanaddress"

	// Bad recipient
	_, err := MakeContract(badAddr, sender, randBytes(32), tStamp, tParams)
	if err == nil {
		t.Fatalf("no error for bad recipient")
	}
	// Wrong recipient address type
	p2sh, _ := dcrutil.NewAddressScriptHash(randBytes(50), tParams)
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
	altAddr, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEd25519)
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
			continue
		}
	}
}

// Test both InputInfo and ExtractScriptHashByType with a non-standard script.
func Test_nonstandardScript(t *testing.T) {
	// The tx hash of a DCR testnet swap contract.
	contractTx, err := chainhash.NewHashFromStr("4a14a2d79c1374d286ebd68d2c104343bcf8be44ed54045b5963fbf73667cecc")
	if err != nil {
		t.Fatal(err)
	}
	vout := 0 // the contract output index, 1 is change

	// The contract output's P2SH pkScript and the corresponding redeem script
	// (the actual contract).
	pkScript, _ := hex.DecodeString("a9146d4bc656b3287a0e6b0d38802db6400f7053111787") // verboseTx.Vout[vout].ScriptPubKey.Hex from getrawtransaction(verbose)
	contractScript, _ := hex.DecodeString("6382012088c020c6de3217594af525fb" +
		"57eaf1f2aae04c305ddc67d465edd325151685fc5a5e428876a914479eddda81" +
		"b6ed289515f2dbcc95f05ce80dff466704fe03865eb17576a91498a67ed502ad" +
		"b04173d88fb1ef92d0317711c3816888ac")

	scriptType := ParseScriptType(CurrentScriptVersion, pkScript, contractScript)
	if !scriptType.IsP2SH() {
		t.Fatalf("script was not P2SH, got %v (see script.go)", scriptType)
	}

	// Double check that the pkScript's script hash matches the hash of the
	// redeem (contract) script.
	scriptHash, err := ExtractScriptHashByType(scriptType, pkScript)
	if err != nil {
		t.Fatalf(fmt.Sprintf("ExtractScriptHashByType error: %v", err))
	}
	if !bytes.Equal(dcrutil.Hash160(contractScript), scriptHash) {
		t.Fatalf(fmt.Sprintf("script hash check failed for output %s,%d", contractTx, vout))
	}

	// ExtractScriptAddrs should not error for non-standard scripts, but should
	// detect them as such.
	chainParams := chaincfg.TestNet3Params()
	_, nonStd, err := ExtractScriptAddrs(contractScript, chainParams)
	if err != nil {
		t.Fatalf("ExtractScriptAddrs failed: %v", err)
	}
	if !nonStd {
		t.Errorf("expected non-standard script")
	}

	// InputInfo currently calls ExtractScriptAddrs at the time of writing, but
	// InputInfo should error regardless.
	spendInfo, err := InputInfo(pkScript, contractScript, chainParams)
	if err != nil {
		t.Fatalf("InputInfo failed: %v", err)
	}
	if !spendInfo.NonStandardScript {
		t.Errorf("contract script should be non-standard")
	}
}

func TestExtractScriptAddrs(t *testing.T) {
	addrs := testAddresses()
	type test struct {
		addr   dcrutil.Address
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
	rAddr, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
	recipient := rAddr.String()
	sAddr, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
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

	check := func(name string, sigScriptSize uint32, scriptType DCRScriptType) {
		if spendInfo.SigScriptSize != sigScriptSize {
			t.Fatalf("%s: wrong SigScriptSize, wanted %d, got %d", name, sigScriptSize, spendInfo.SigScriptSize)
		}
		if spendInfo.ScriptType != scriptType {
			t.Fatalf("%s: wrong ScriptType, wanted %d, got %d", name, scriptType, spendInfo.ScriptType)
		}
	}

	var script []byte
	payToAddr := func(addr dcrutil.Address, redeem []byte) {
		script, _ = txscript.PayToAddrScript(addr)
		spendInfo, err = InputInfo(script, redeem, tParams)
		if err != nil {
			t.Fatalf("InputInfo script: %v", err)
		}
	}

	payToAddr(addrs.pkh, nil)
	check("p2pkh", P2PKHSigScriptSize, ScriptP2PKH)

	payToAddr(addrs.sh, addrs.multiSig)
	check("p2sh", 74+uint32(len(addrs.multiSig))+1, ScriptP2SH|ScriptMultiSig)

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
}

func TestFindKeyPush(t *testing.T) {
	rAddr, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
	recipient := rAddr.String()
	sAddr, _ := dcrutil.NewAddressPubKeyHash(randBytes(20), tParams, dcrec.STEcdsaSecp256k1)
	sender := sAddr.String()

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	contract, _ := MakeContract(recipient, sender, secretHash[:], tStamp, tParams)
	contractHash := dcrutil.Hash160(contract)
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
	_, err := ExtractContractHash("zz")
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// invalid script
	_, err = ExtractContractHash(hex.EncodeToString(invalidScript))
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// multi-sig
	_, err = ExtractContractHash(hex.EncodeToString(addrs.multiSig))
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// wrong script types
	p2pkh, _ := txscript.PayToAddrScript(addrs.pkh)
	_, err = ExtractContractHash(hex.EncodeToString(p2pkh))
	if err == nil {
		t.Fatalf("no error for non-hex contract")
	}
	// ok
	p2sh, _ := txscript.PayToAddrScript(addrs.sh)
	checkHash, err := ExtractContractHash(hex.EncodeToString(p2sh))
	if err != nil {
		t.Fatalf("error extracting contract hash: %v", err)
	}
	if !bytes.Equal(checkHash, addrs.sh.ScriptAddress()) {
		t.Fatalf("hash mismatch. wanted %x, got %x", addrs.sh.ScriptAddress(), checkHash)
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
