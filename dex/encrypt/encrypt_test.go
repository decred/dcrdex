// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encrypt

import (
	"bytes"
	"math/rand"
	"testing"

	"decred.org/dcrdex/dex/encode"
)

var (
	copyB = encode.CopySlice
	randB = encode.RandomBytes
)

func TestDecrypt(t *testing.T) {
	pw := []byte("4kliaOCha2")
	crypter := NewCrypter(pw)
	thing := randB(50)
	encThing, err := crypter.Encrypt(thing)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	reCheck := func() {
		reThing, err := crypter.Decrypt(encThing)
		if err != nil {
			t.Fatalf("Decrypt error: %v", err)
		}
		if !bytes.Equal(thing, reThing) {
			t.Fatalf("%x != %x", thing, reThing)
		}
	}
	reCheck()

	// Change the version.
	var badThing encode.BuildyBytes = copyB(encThing)
	badThing[0] = 1
	_, err = crypter.Decrypt(badThing)
	if err == nil {
		t.Fatalf("no error for wrong version")
	}
	reCheck()

	// Drop the second push.
	ver, pushes, _ := encode.DecodeBlob(encThing)
	badThing = encode.BuildyBytes{ver}.AddData(pushes[0])
	_, err = crypter.Decrypt(badThing)
	if err == nil {
		t.Fatalf("no error for trunacted pushes")
	}
	reCheck()

	// Wrong size nonce.
	badThing = encode.BuildyBytes{ver}.AddData(append(copyB(pushes[0]), 5)).AddData(pushes[1])
	_, err = crypter.Decrypt(badThing)
	if err == nil {
		t.Fatalf("no error for extended nonce")
	}
	reCheck()

	// Corrupted blob
	badThing = append(copyB(encThing), 123)
	_, err = crypter.Decrypt(badThing)
	if err == nil {
		t.Fatalf("no error for corrupted blob")
	}
	reCheck()

}

func TestSerialize(t *testing.T) {
	pw := []byte("20O6KcujCU")
	crypter := NewCrypter(pw)
	thing := randB(50)
	encThing, err := crypter.Encrypt(thing)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	serializedCrypter := crypter.Serialize()
	reCheck := func(tag string) {
		reCrypter, err := Deserialize(pw, serializedCrypter)
		if err != nil {
			t.Fatalf("%s: reCheck deserialization error: %v", tag, err)
		}
		reThing, err := reCrypter.Decrypt(encThing)
		if err != nil {
			t.Fatalf("%s: reCrypter failed to decrypt thing: %v", tag, err)
		}
		if !bytes.Equal(reThing, thing) {
			t.Fatalf("%s: reCrypter's decoded thing is messed up: %x != %x", tag, reThing, thing)
		}
	}
	reCheck("first")

	// Can't deserialize with wrong password.
	_, err = Deserialize([]byte("wrong password"), serializedCrypter)
	if err == nil {
		t.Fatalf("no Deserialize error for wrong password")
	}
	reCheck("after wrong password")

	// Adding a random bytes should fail at DecodeBlob
	badCrypter := append(copyB(serializedCrypter), 5)
	_, err = Deserialize(pw, badCrypter)
	if err == nil {
		t.Fatalf("no Deserialize error for corrupted blob")
	}
	reCheck("after corrupted blob")

	// Version 1 not known.
	badCrypter = copyB(serializedCrypter)
	badCrypter[0] = 1
	_, err = Deserialize(pw, badCrypter)
	if err == nil {
		t.Fatalf("no Deserialize error for blob from the future")
	}
	reCheck("after wrong password")

	// Trim the salt, which is push 0 and normally 16 bytes.
	badCrypter = trimPush(copyB(serializedCrypter), 0, 15)
	_, err = Deserialize(pw, badCrypter)
	if err == nil {
		t.Fatalf("no Deserialize error trimmed salt")
	}
	reCheck("after trimmed salt")

	// Trim the threads parameter, which is push 3 and normally 1 byte.
	badCrypter = trimPush(copyB(serializedCrypter), 3, 0)
	_, err = Deserialize(pw, badCrypter)
	if err == nil {
		t.Fatalf("no Deserialize error zero-length threads")
	}
	reCheck("after zero-length threads")

	// Trim the tag parameter, which is push 4 and normally 16 bytes.
	badCrypter = trimPush(copyB(serializedCrypter), 4, 15)
	_, err = Deserialize(pw, badCrypter)
	if err == nil {
		t.Fatalf("no Deserialize error trimmed tag")
	}
	reCheck("after trimmed tag")
}

func TestRandomness(t *testing.T) {
	pw := randB(15)
	crypter := NewCrypter(pw)
	numToDo := 10_000
	if testing.Short() {
		numToDo /= 10
	}
	for i := 0; i < numToDo; i++ {
		thing := randB(rand.Intn(100))
		encThing, err := crypter.Encrypt(thing)
		if err != nil {
			t.Fatalf("error encrypting %x with password %x", thing, pw)
		}
		reThing, err := crypter.Decrypt(encThing)
		if err != nil {
			t.Fatalf("error decrypting %x, which is encrypted %x with password %x", encThing, thing, pw)
		}
		if !bytes.Equal(thing, reThing) {
			t.Fatalf("decrypted %x is different that encrypted %x with password %x", reThing, thing, pw)
		}
	}
}

func trimPush(b []byte, idx, newLen int) []byte {
	ver, pushes, _ := encode.DecodeBlob(b)
	newB := encode.BuildyBytes{ver}
	for i := range pushes {
		if i == idx {
			newB = newB.AddData(pushes[i][:newLen])
		} else {
			newB = newB.AddData(pushes[i])
		}
	}
	return newB
}
