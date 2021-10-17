package keygen

import (
	"bytes"
	"testing"

	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/hdkeychain/v3"
)

func TestGenDeepChild(t *testing.T) {
	seed := encode.RandomBytes(64)

	root, err := hdkeychain.NewMaster(seed, &RootKeyParams{})
	if err != nil {
		t.Fatalf("error getting HD keychain root: %v", err)
	}

	expectedChild, err := root.Child(1)
	if err != nil {
		t.Fatalf("error deriving child: %v", err)
	}

	expectedChild, err = expectedChild.Child(2)
	if err != nil {
		t.Fatalf("error deriving child: %v", err)
	}

	expectedChild, err = expectedChild.Child(3)
	if err != nil {
		t.Fatalf("error deriving child: %v", err)
	}

	child, err := GenDeepChild(seed, []uint32{1, 2, 3})
	if err != nil {
		t.Fatalf("error in GenDeepChild: %v", err)
	}

	expectedSerializedPrivKey, err := expectedChild.SerializedPrivKey()
	if err != nil {
		t.Fatalf("error serializing priv key: %v", err)
	}

	serializedPrivKey, err := child.SerializedPrivKey()
	if err != nil {
		t.Fatalf("error serializing priv key: %v", err)
	}

	if !bytes.Equal(expectedSerializedPrivKey, serializedPrivKey) {
		t.Fatalf("private keys not equal:\n%x\n%x", expectedSerializedPrivKey, serializedPrivKey)
	}
}
