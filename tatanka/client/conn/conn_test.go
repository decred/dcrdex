package conn

import (
	"bytes"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func TestEncryption(t *testing.T) {
	alicePriv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("error generating private key: %v", err)
	}

	bobPrivKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("error generating private key: %v", err)
	}

	aliceSharedSecret := sharedSecret(alicePriv, bobPrivKey.PubKey())
	bobSharedSecret := sharedSecret(bobPrivKey, alicePriv.PubKey())

	msg := []byte(
		"this is an unencrypted message. " +
			"AES chunk size is 16 bytes. We will create a message that is over 16 bytes, " +
			"So to make it that long, we'll just continue jabbering about nothing. " +
			"Nothing, nothing, nothing, nothing, nothing.",
	)

	enc, err := encryptAES(aliceSharedSecret, msg)
	if err != nil {
		t.Fatalf("encryptAES error: %v", err)
	}

	reMsg, err := decryptAES(bobSharedSecret, enc)
	if err != nil {
		t.Fatalf("decryptAES error: %v", err)
	}

	if !bytes.Equal(reMsg, msg) {
		t.Fatalf("wrong message. %q != %q", string(reMsg), string(msg))
	}
}
