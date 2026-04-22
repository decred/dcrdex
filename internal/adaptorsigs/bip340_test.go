package adaptorsigs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
)

// bip340OfficialVectors contains the sign-verify test vectors from the
// canonical BIP-340 test-vectors.csv (rows 0-3, which have secret keys).
// These are used to cross-check our shared primitives - nonce derivation,
// challenge hash, and the signing equation - against the reference output.
// If signBIP340Standard matches these bit-exactly, the same primitives
// reused inside PublicKeyTweakedAdaptorSigBIP340 are vector-validated.
var bip340OfficialVectors = []struct {
	name      string
	secretKey string
	pubKey    string
	auxRand   string
	message   string
	signature string
}{
	{
		name:      "vector 0",
		secretKey: "0000000000000000000000000000000000000000000000000000000000000003",
		pubKey:    "F9308A019258C31049344F85F89D5229B531C845836F99B08601F113BCE036F9",
		auxRand:   "0000000000000000000000000000000000000000000000000000000000000000",
		message:   "0000000000000000000000000000000000000000000000000000000000000000",
		signature: "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
	},
	{
		name:      "vector 1",
		secretKey: "B7E151628AED2A6ABF7158809CF4F3C762E7160F38B4DA56A784D9045190CFEF",
		pubKey:    "DFF1D77F2A671C5F36183726DB2341BE58FEAE1DA2DECED843240F7B502BA659",
		auxRand:   "0000000000000000000000000000000000000000000000000000000000000001",
		message:   "243F6A8885A308D313198A2E03707344A4093822299F31D0082EFA98EC4E6C89",
		signature: "6896BD60EEAE296DB48A229FF71DFE071BDE413E6D43F917DC8DCF8C78DE33418906D11AC976ABCCB20B091292BFF4EA897EFCB639EA871CFA95F6DE339E4B0A",
	},
	{
		name:      "vector 2",
		secretKey: "C90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B14E5C9",
		pubKey:    "DD308AFEC5777E13121FA72B9CC1B7CC0139715309B086C960E18FD969774EB8",
		auxRand:   "C87AA53824B4D7AE2EB035A2B5BBBCCC080E76CDC6D1692C4B0B62D798E6D906",
		message:   "7E2D58D8B3BCDF1ABADEC7829054F90DDA9805AAB56C77333024B9D0A508B75C",
		signature: "5831AAEED7B44BB74E5EAB94BA9D4294C49BCF2A60728D8B4C200F50DD313C1BAB745879A5AD954A72C45A91C3A51D3C7ADEA98D82F8481E0E1E03674A6F3FB7",
	},
	{
		name:      "vector 3",
		secretKey: "0B432B2677937381AEF05BB02A66ECD012773062CF3FA2549E44F58ED2401710",
		pubKey:    "25D1DFF95105F5253C4022F628A996AD3A0D95FBF21D468A1B33F8C160D8F517",
		auxRand:   "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		message:   "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		signature: "7EB0509757E246F19449885651611CB965ECC1A187DD51B64FDA1EDC9637D5EC97582B9CB13DB3933705B32BA982AF5AF25FD78881EBB32771FC5922EFC66EA3",
	},
}

// TestBIP340StandardSignVectors checks that signBIP340Standard (which
// shares its nonce, challenge, and signing code with the adaptor path)
// reproduces the canonical BIP-340 test vectors bit-exactly.
func TestBIP340StandardSignVectors(t *testing.T) {
	for _, tv := range bip340OfficialVectors {
		t.Run(tv.name, func(t *testing.T) {
			dBytes, err := hex.DecodeString(tv.secretKey)
			if err != nil {
				t.Fatalf("decode secret key: %v", err)
			}
			msg, err := hex.DecodeString(tv.message)
			if err != nil {
				t.Fatalf("decode message: %v", err)
			}
			auxRand, err := hex.DecodeString(tv.auxRand)
			if err != nil {
				t.Fatalf("decode auxRand: %v", err)
			}
			wantSig, err := hex.DecodeString(tv.signature)
			if err != nil {
				t.Fatalf("decode signature: %v", err)
			}

			privKey, _ := btcec.PrivKeyFromBytes(dBytes)
			sig, err := signBIP340Standard(privKey, msg, auxRand)
			if err != nil {
				t.Fatalf("signBIP340Standard: %v", err)
			}
			gotSig := sig.Serialize()
			if !bytes.Equal(gotSig, wantSig) {
				t.Fatalf("signature mismatch\ngot  %x\nwant %x", gotSig, wantSig)
			}
		})
	}
}

// TestBIP340AdaptorRoundtrip exercises the full adaptor flow:
//
//  1. Signer creates a pub-key-tweaked adaptor under BIP-340.
//  2. Verifier (no tweak) checks it with VerifyBIP340.
//  3. Recipient (knows tweak) decrypts to a standard BIP-340 signature.
//  4. Standard signature validates under btcec's canonical BIP-340 Verify.
//  5. Given the decrypted signature, the signer can recover the tweak.
func TestBIP340AdaptorRoundtrip(t *testing.T) {
	for i := 0; i < 50; i++ {
		// Signer's key.
		privKey, err := btcec.NewPrivateKey()
		if err != nil {
			t.Fatalf("privkey: %v", err)
		}

		// Hidden scalar t and its point T = t*G.
		tweakKey, err := btcec.NewPrivateKey()
		if err != nil {
			t.Fatalf("tweak: %v", err)
		}
		tweak := &tweakKey.Key
		var T btcec.JacobianPoint
		btcec.ScalarBaseMultNonConst(tweak, &T)

		var hash [32]byte
		if _, err := rand.Read(hash[:]); err != nil {
			t.Fatalf("read hash: %v", err)
		}

		sig, err := PublicKeyTweakedAdaptorSigBIP340(privKey, hash[:], &T)
		if err != nil {
			t.Fatalf("iter %d: adaptor sign: %v", i, err)
		}

		if err := sig.VerifyBIP340(hash[:], privKey.PubKey()); err != nil {
			t.Fatalf("iter %d: adaptor verify: %v", i, err)
		}

		decrypted, err := sig.DecryptBIP340(tweak)
		if err != nil {
			t.Fatalf("iter %d: decrypt: %v", i, err)
		}
		if !decrypted.Verify(hash[:], privKey.PubKey()) {
			t.Fatalf("iter %d: decrypted signature failed BIP-340 Verify", i)
		}

		recovered, err := sig.RecoverTweakBIP340(decrypted)
		if err != nil {
			t.Fatalf("iter %d: recover: %v", i, err)
		}
		if !recovered.Equals(tweak) {
			t.Fatalf("iter %d: recovered tweak mismatch", i)
		}
	}
}

// TestBIP340AdaptorWrongTweak ensures Decrypt rejects a tweak that does not
// correspond to the stored T.
func TestBIP340AdaptorWrongTweak(t *testing.T) {
	privKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("privkey: %v", err)
	}
	tweakKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("tweak: %v", err)
	}
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&tweakKey.Key, &T)

	var hash [32]byte
	if _, err := rand.Read(hash[:]); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	sig, err := PublicKeyTweakedAdaptorSigBIP340(privKey, hash[:], &T)
	if err != nil {
		t.Fatalf("adaptor sign: %v", err)
	}

	wrongKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("wrong tweak: %v", err)
	}
	if _, err := sig.DecryptBIP340(&wrongKey.Key); err == nil {
		t.Fatal("expected error from DecryptBIP340 with wrong tweak")
	}
}

// TestBIP340AdaptorTamper confirms that mutating any field of an adaptor
// signature makes VerifyBIP340 fail.
func TestBIP340AdaptorTamper(t *testing.T) {
	privKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("privkey: %v", err)
	}
	tweakKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("tweak: %v", err)
	}
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&tweakKey.Key, &T)

	var hash [32]byte
	if _, err := rand.Read(hash[:]); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	sig, err := PublicKeyTweakedAdaptorSigBIP340(privKey, hash[:], &T)
	if err != nil {
		t.Fatalf("adaptor sign: %v", err)
	}
	if err := sig.VerifyBIP340(hash[:], privKey.PubKey()); err != nil {
		t.Fatalf("baseline verify: %v", err)
	}

	// Tamper with s.
	tampered := *sig
	var one btcec.ModNScalar
	one.SetInt(1)
	tampered.s.Add(&one)
	if err := tampered.VerifyBIP340(hash[:], privKey.PubKey()); err == nil {
		t.Fatal("expected verify to fail after s tamper")
	}

	// Tamper with the message.
	badHash := hash
	badHash[0] ^= 0x01
	if err := sig.VerifyBIP340(badHash[:], privKey.PubKey()); err == nil {
		t.Fatal("expected verify to fail on wrong hash")
	}

	// Wrong pubkey.
	otherKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("other privkey: %v", err)
	}
	if err := sig.VerifyBIP340(hash[:], otherKey.PubKey()); err == nil {
		t.Fatal("expected verify to fail on wrong pubkey")
	}
}
