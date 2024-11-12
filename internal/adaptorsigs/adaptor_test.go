package adaptorsigs

import (
	"math/rand"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
)

func TestAdaptorSignatureRandom(t *testing.T) {
	seed := time.Now().Unix()
	rng := rand.New(rand.NewSource(seed))
	defer func(t *testing.T, seed int64) {
		if t.Failed() {
			t.Logf("random seed: %d", seed)
		}
	}(t, seed)

	for i := 0; i < 100; i++ {
		// Generate two private keys.
		privKey1, err := secp256k1.GeneratePrivateKeyFromRand(rng)
		if err != nil {
			t.Fatalf("failed to read random private key: %v", err)
		}
		privKey2, err := secp256k1.GeneratePrivateKeyFromRand(rng)
		if err != nil {
			t.Fatalf("failed to read random private key: %v", err)
		}

		// Generate random hashes to sign.
		var hash1, hash2 [32]byte
		if _, err := rng.Read(hash1[:]); err != nil {
			t.Fatalf("failed to read random hash: %v", err)
		}
		if _, err := rng.Read(hash2[:]); err != nil {
			t.Fatalf("failed to read random hash: %v", err)
		}

		// Generate random signature tweak
		var tBuf [32]byte
		if _, err := rng.Read(tBuf[:]); err != nil {
			t.Fatalf("failed to read random private key: %v", err)
		}
		var tweak secp256k1.ModNScalar
		tweak.SetBytes(&tBuf)

		// Sign hash1 with private key 1
		sig, err := schnorr.Sign(privKey1, hash1[:])
		if err != nil {
			t.Fatalf("Sign error: %v", err)
		}

		// The owner of priv key 1 knows the tweak. Sends a priv key tweaked adaptor sig
		// to the owner of priv key 2.
		adaptorSigPrivKeyTweak := PrivateKeyTweakedAdaptorSig(sig, privKey1.PubKey(), &tweak)
		err = adaptorSigPrivKeyTweak.Verify(hash1[:], privKey1.PubKey())
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}

		// The owner of privKey2 creates a public key tweaked adaptor sig using
		// tweak * G, and sends it to the owner of privKey1.
		adaptorSigPubKeyTweak, err := PublicKeyTweakedAdaptorSig(privKey2, hash2[:], adaptorSigPrivKeyTweak.PublicTweak())
		if err != nil {
			t.Fatalf("PublicKeyTweakedAdaptorSig error: %v", err)
		}

		// The owner of privKey1 knows the tweak, so they can decrypt the
		// public key tweaked adaptor sig.
		decryptedSig, err := adaptorSigPubKeyTweak.Decrypt(&tweak)
		if err != nil {
			t.Fatal(err)
		}
		if !decryptedSig.Verify(hash2[:], privKey2.PubKey()) {
			t.Fatal("failed to verify decrypted signature")
		}

		// Using the decrypted version of their sig, which has been made public,
		// the owner of privKey2 can recover the tweak.
		recoveredTweak, err := adaptorSigPubKeyTweak.RecoverTweak(decryptedSig)
		if err != nil {
			t.Fatal(err)
		}
		if !recoveredTweak.Equals(&tweak) {
			t.Fatalf("original tweak %v != recovered %v", tweak, recoveredTweak)
		}

		// Using the recovered tweak, the original priv key tweaked adaptor sig
		// can be decrypted.
		decryptedOriginalSig, err := adaptorSigPrivKeyTweak.Decrypt(&tweak)
		if err != nil {
			t.Fatal(err)
		}
		if valid := decryptedOriginalSig.Verify(hash1[:], privKey1.PubKey()); !valid {
			t.Fatal("decrypted original sig is invalid")
		}
	}
}

func RandomBytes(len int) []byte {
	bytes := make([]byte, len)
	_, err := rand.Read(bytes)
	if err != nil {
		panic("error reading random bytes: " + err.Error())
	}
	return bytes
}

func TestPublicKeyTweakParsing(t *testing.T) {
	for i := 0; i < 100; i++ {
		privKey, err := secp256k1.GeneratePrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		hash := encode.RandomBytes(32)
		tweak, err := secp256k1.GeneratePrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		var T secp256k1.JacobianPoint
		tweak.PubKey().AsJacobian(&T)

		adaptorSig, err := PublicKeyTweakedAdaptorSig(privKey, hash, &T)
		if err != nil {
			t.Fatal(err)
		}

		serialized := adaptorSig.Serialize()
		parsed, err := ParseAdaptorSignature(serialized)
		if err != nil {
			t.Fatal(err)
		}

		if !adaptorSig.IsEqual(parsed) {
			t.Fatalf("parsed sig does not equal original")
		}
	}
}

func TestPrivateKeyTweakParsing(t *testing.T) {
	for i := 0; i < 100; i++ {
		privKey, err := secp256k1.GeneratePrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		hash := encode.RandomBytes(32)
		tweak, err := secp256k1.GeneratePrivateKey()
		if err != nil {
			t.Fatal(err)
		}

		sig, err := schnorr.Sign(privKey, hash)
		if err != nil {
			t.Fatal(err)
		}

		adaptorSig := PrivateKeyTweakedAdaptorSig(sig, privKey.PubKey(), &tweak.Key)
		serialized := adaptorSig.Serialize()
		parsed, err := ParseAdaptorSignature(serialized)
		if err != nil {
			t.Fatal(err)
		}

		if !adaptorSig.IsEqual(parsed) {
			t.Fatalf("parsed sig does not equal original")
		}
	}
}
