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
	pw := "4kliaOCha2"
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

	// bad additional data (key hash)
	c, _ := crypter.(*argonPolyCrypter)
	ogKey := c.key
	var k Key
	k[0] = 1
	c.key = k
	_, err = crypter.Decrypt(encThing)
	if err == nil {
		t.Fatalf("no error for changed key")
	}
	c.key = ogKey
	reCheck()
}

func TestSerialize(t *testing.T) {
	pw := "20O6KcujCU"
	crypter := NewCrypter(pw)
	thing := randB(50)
	encThing, err := crypter.Encrypt(thing)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	check := func(tag string, c Crypter) {
		reThing, err := c.Decrypt(encThing)
		if err != nil {
			t.Fatalf("%s: Decrypt error: %v", tag, err)
		}
		if !bytes.Equal(thing, reThing) {
			t.Fatalf("%s: %x != %x", tag, thing, reThing)
		}
	}
	check("begin", crypter)
	serializedCrypter, err := crypter.Serialize()
	if err != nil {
		t.Fatalf("crypter.Serialize error: %v", err)
	}
	reCrypter, err := Deserialize(pw, serializedCrypter)
	if err != nil {
		t.Fatalf("Deserialize error: %v", err)
	}
	check("end", reCrypter)

	// Won't serialize zeroed key.
	crypter.Close()
	_, err = crypter.Serialize()
	if err == nil {
		t.Fatalf("no error when serializing closed Crypter")
	}
}

func TestRandomness(t *testing.T) {
	pw := string(randB(15))
	crypter := NewCrypter(pw)
	numToDo := 10_000
	if testing.Short() {
		numToDo /= 10
	}
	for i := 0; i < numToDo; i++ {
		thing := randB(rand.Intn(100))
		encThing, err := crypter.Encrypt(thing)
		if err != nil {
			t.Fatalf("error encrypting %x with password %x", thing, []byte(pw))
		}
		reThing, err := crypter.Decrypt(encThing)
		if err != nil {
			t.Fatalf("error decrypting %x, which is encrypted %x with password %x", encThing, thing, []byte(pw))
		}
		if !bytes.Equal(thing, reThing) {
			t.Fatalf("decrypted %x is different that encrypted %x with password %x", reThing, thing, []byte(pw))
		}
	}
}
