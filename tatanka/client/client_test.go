package client

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestEncryption(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, rsaPrivateKeyLength)
	if err != nil {
		t.Fatalf("error generating rsa key: %v", err)
	}
	p := &peer{
		encryptionKey: &priv.PublicKey,
		decryptionKey: priv,
	}

	msg := []byte(
		"this is an unencrypted message. " +
			"For a size 2048 private key -> 256 byte public key, it needs to be longer than 190 bytes, " +
			"so that we can test out our chunking loop." +
			"So to make it that long, we'll just continue jabbering about nothing. " +
			"Nothing, nothing, nothing, nothing, nothing.",
	)

	enc, err := p.encryptRSA(msg)
	if err != nil {
		t.Fatalf("encryptRSA error: %v", err)
	}

	reMsg, err := p.decryptRSA(enc)
	if err != nil {
		t.Fatalf("decryptRSA error: %v", err)
	}

	if !bytes.Equal(reMsg, msg) {
		t.Fatalf("wrong message. %q != %q", string(reMsg), string(msg))
	}
}
