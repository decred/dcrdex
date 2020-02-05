// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encrypt

import (
	"crypto/rand"
	"fmt"
	"runtime"

	"decred.org/dcrdex/dex/encode"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/poly1305"
)

// Crypter is an interface for an encryption key and encryption/decryption
// algorithms. Create a Crypter with the NewCrypter function.
type Crypter interface {
	// Encrypt encrypts the plaintext.
	Encrypt(b []byte) ([]byte, error)
	// Decrypt decrypts the ciphertext created by Encrypt.
	Decrypt(b []byte) ([]byte, error)
	// Serialize serializes the Crypter. Use the Deserialize function to create
	// a Crypter from the resulting bytes. Deserializing requires the password
	// used to create the Crypter.
	Serialize() []byte
	// Close zeros the encryption key. The Crypter is useless after closing.
	Close()
}

const (
	// defaultTime is the default time parameter for argon2id key derivation.
	defaultTime = 1
	// defaultMem is the default memory parameter for argon2id key derivation.
	defaultMem = 64 * 1024
	// KeySize is the size of the encryption key.
	KeySize = 32
	// SaltSize is the size of the argon2id salt.
	SaltSize = 16
)

// Big-endian encoder.
var intCoder = encode.IntCoder

// Key is 32 bytes.
type Key [KeySize]byte

// Salt is randomness used as part of key deriviation. This is different from
// the salt generated during xchacha20poly1305 encryption, which is shorter.
type Salt [SaltSize]byte

// newSalt is the constructor for a salt based on randomness from crypto/rand.
func newSalt() Salt {
	var s Salt
	_, err := rand.Read(s[:])
	if err != nil {
		panic("newSalt: " + err.Error())
	}
	return s
}

// argonParams is a set of parameters for key derivation.
type argonParams struct {
	time    uint32
	memory  uint32
	threads uint8
}

// NewCrypter derives an encryption key from a password string.
func NewCrypter(pw string) Crypter {
	return newArgonPolyCrypter(pw)
}

// Deserialize deserializes the Crypter for the password.
func Deserialize(pw string, encCrypter []byte) (Crypter, error) {
	ver, pushes, err := encode.DecodeBlob(encCrypter)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeArgonPoly_v0(pw, pushes)
	default:
		return nil, fmt.Errorf("unknown Crypter version %d", ver)
	}
}

// argonPolyCryper is an encryption algorithm based on argon2id for key
// derivation and xchacha20poly1305 for symmetric encryption.
type argonPolyCrypter struct {
	key    Key
	tag    [poly1305.TagSize]byte
	salt   Salt
	params *argonParams
}

// newArgonPolyCrypter is the constructor for an argonPolyCrypter.
func newArgonPolyCrypter(pw string) *argonPolyCrypter {
	pwB := []byte(pw)
	salt := newSalt()
	threads := uint8(runtime.NumCPU())

	keyB := argon2.IDKey(pwB, salt[:], defaultTime, defaultMem, threads, KeySize*2)
	// The argon2id key is split into two keys, The encryption key is the first 32
	// bytes.
	encKeyB := keyB[:KeySize]
	var encKey Key
	copy(encKey[:], encKeyB)
	// MAC key is the second 32 bytes.
	polyKeyB := keyB[KeySize:]
	var polyKey [KeySize]byte
	copy(polyKey[:], polyKeyB)

	c := &argonPolyCrypter{
		key:  encKey,
		salt: salt,
		params: &argonParams{
			time:    defaultTime,
			memory:  defaultMem,
			threads: threads,
		},
	}
	// Use the mac key and the serialized parameters to generate the
	// authenticator.
	poly1305.Sum(&c.tag, c.serializeParams(), &polyKey)
	return c
}

// Encrypt encrypts the plaintext.
func (c *argonPolyCrypter) Encrypt(plainText []byte) ([]byte, error) {
	boxer, err := chacha20poly1305.NewX(c.key[:])
	if err != nil {
		return nil, fmt.Errorf("aead error: %v", err)
	}
	nonce := make([]byte, boxer.NonceSize())
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce generation error: %v", err)
	}
	cipherText := boxer.Seal(nil, nonce, plainText, nil)
	return encode.BuildyBytes{0}.AddData(nonce).AddData(cipherText), nil
}

// Decrypt decrypts the ciphertext created by Encrypt.
func (c *argonPolyCrypter) Decrypt(encrypted []byte) ([]byte, error) {
	ver, pushes, err := encode.DecodeBlob(encrypted)
	if err != nil {
		return nil, fmt.Errorf("DecodeBlob: %v", err)
	}
	if ver != 0 {
		return nil, fmt.Errorf("only version 0 encryptions are known. got version %d", ver)
	}
	if len(pushes) != 2 {
		return nil, fmt.Errorf("expected 2 pushes. got %d", len(pushes))
	}
	boxer, err := chacha20poly1305.NewX(c.key[:])
	if err != nil {
		return nil, fmt.Errorf("aead error: %v", err)
	}
	nonce, cipherText := pushes[0], pushes[1]
	if len(nonce) != boxer.NonceSize() {
		return nil, fmt.Errorf("incompatible nonce length. expected %d, got %d", boxer.NonceSize(), len(nonce))
	}
	plainText, err := boxer.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, fmt.Errorf("aead.Open: %v", err)
	}
	return plainText, nil
}

// Serialize serializes the argonPolyCrypter.
func (c *argonPolyCrypter) Serialize() []byte {
	return c.serializeParams().AddData(c.tag[:])
}

// serializeParams serializes the argonPolyCrypter parameters, without the
// poly1305 auth tag.
func (c *argonPolyCrypter) serializeParams() encode.BuildyBytes {
	return encode.BuildyBytes{0}.
		AddData(c.salt[:]).
		AddData(encode.Uint32Bytes(c.params.time)).
		AddData(encode.Uint32Bytes(c.params.memory)).
		AddData([]byte{c.params.threads})
}

// Close zeros the key. The argonPolyCrypter is useless after closing.
func (c *argonPolyCrypter) Close() {
	for i := range c.key {
		c.key[i] = 0
	}
}

// decodeArgonPoly_v0 decodes an argonPolyCrypter from the pushes extracted from
// a version 0 blob.
func decodeArgonPoly_v0(pw string, pushes [][]byte) (*argonPolyCrypter, error) {
	if len(pushes) != 5 {
		return nil, fmt.Errorf("decodeArgonPoly_v0 expected 5 pushes, but got %d", len(pushes))
	}
	saltB, tagB := pushes[0], pushes[4]
	timeB, memB, threadsB := pushes[1], pushes[2], pushes[3]

	if len(saltB) != SaltSize {
		return nil, fmt.Errorf("expected salt of length %d, got %d", SaltSize, len(saltB))
	}
	var salt Salt
	copy(salt[:], saltB)

	if len(threadsB) != 1 {
		return nil, fmt.Errorf("threads parameter encoding length is incorrect. expected 1, got %d", len(threadsB))
	}

	if len(tagB) != poly1305.TagSize {
		return nil, fmt.Errorf("mac authenticator of incorrect length. wanted %d, got %d", poly1305.TagSize, len(tagB))
	}
	var polyTag [poly1305.TagSize]byte
	copy(polyTag[:], tagB)

	// Create the argonPolyCrypter.
	params := &argonParams{
		time:    intCoder.Uint32(timeB),
		memory:  intCoder.Uint32(memB),
		threads: threadsB[0],
	}
	c := &argonPolyCrypter{
		salt:   salt,
		tag:    polyTag,
		params: params,
	}

	// Prepare the key.
	pwB := []byte(pw)
	keyB := argon2.IDKey(pwB, c.salt[:], params.time, params.memory, params.threads, KeySize*2)

	// Check the poly1305 auth to verify the password.
	polyKeyB := keyB[KeySize:]
	var polyKey [KeySize]byte
	copy(polyKey[:], polyKeyB)
	if !poly1305.Verify(&c.tag, c.serializeParams(), &polyKey) {
		return nil, fmt.Errorf("incorrect password")
	}

	// Copy the key.
	copy(c.key[:], keyB[:KeySize])

	return c, nil
}
