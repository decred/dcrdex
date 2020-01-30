// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encrypt

import (
	"crypto/rand"
	"fmt"
	"runtime"

	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/crypto/blake256"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Crypter is an encryption algorithm. Both key deriviation and encryption are
// handled by the Crypter.
type Crypter interface {
	Encrypt(b []byte) ([]byte, error)
	Decrypt(b []byte) ([]byte, error)
	Serialize() ([]byte, error)
	Close()
}

const (
	// defaultTime is the default time parameter for argon2id key derivation.
	defaultTime = 1
	// defaultMem is the default memory parameter for argon2id key derivation.
	defaultMem = 64 * 1024
	// KeySize is the size of the encryption key.
	KeySize = 32
	// checkPhrase is used to ensure the integrity of the deserialized Crypter.
	checkPhrase = "dcrdex checkphrase"
)

var (
	// Big-endian encoder.
	intCoder = encode.IntCoder
	// A zeroed Key to check for un-initialized or zeroed keys.
	zeroKey Key
)

// Key is 32 bytes.
type Key [KeySize]byte

// Salt is randomness used as part of key deriviation. This is different from
// the salt generated during xchacha20poly1305 encryption, which is shorter.
type Salt [KeySize]byte

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

// Deserialize deserializes the encryption key for the password.
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
	salt   Salt
	params *argonParams
}

// newArgonPolyCrypter is the constructor for an argonPolyCrypter.
func newArgonPolyCrypter(pw string) *argonPolyCrypter {
	pwB := []byte(pw)
	salt := newSalt()
	threads := uint8(runtime.NumCPU())
	keyB := argon2.IDKey(pwB, salt[:], defaultTime, defaultMem, threads, KeySize)
	var key Key
	copy(key[:], keyB)
	return &argonPolyCrypter{
		key:  key,
		salt: salt,
		params: &argonParams{
			time:    defaultTime,
			memory:  defaultMem,
			threads: threads,
		},
	}
}

// Encrypt encrypts the plaintext.
func (c *argonPolyCrypter) Encrypt(plainText []byte) ([]byte, error) {
	keyHash := blake256.Sum256(c.key[:])
	boxer, err := chacha20poly1305.NewX(c.key[:])
	if err != nil {
		return nil, fmt.Errorf("aead error: %v", err)
	}
	nonceSize := boxer.NonceSize()
	nonce := make([]byte, nonceSize)
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce generation error: %v", err)
	}
	cipherText := boxer.Seal(nil, nonce, plainText, keyHash[:])
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
	keyHash := blake256.Sum256(c.key[:])
	plainText, err := boxer.Open(nil, nonce, cipherText, keyHash[:])
	if err != nil {
		return nil, fmt.Errorf("aead.Open: %v", err)
	}
	return plainText, nil
}

// Serialize serializes the argonPolyCrypter. It is an error to call Serialize
// on an un-initialized or a closed argonPolyCrypter.
func (c *argonPolyCrypter) Serialize() ([]byte, error) {
	if c.key == zeroKey {
		return nil, fmt.Errorf("cannot Serialize a closed argonPolyCrypter")
	}
	ckSum, err := c.Encrypt([]byte(checkPhrase))
	if err != nil {
		return nil, fmt.Errorf("checksum encryption: %v", err)
	}
	return c.serializeParams().AddData(ckSum), nil
}

// Close zeros the key. The argonPolyCrypter is useless after calling close.
func (c *argonPolyCrypter) Close() {
	for i := range c.key {
		c.key[i] = 0
	}
}

// serializeParams serializes the KDF parameters and salt.
func (c *argonPolyCrypter) serializeParams() encode.BuildyBytes {
	return encode.BuildyBytes{0}.
		AddData(c.salt[:]).
		AddData(encode.Uint32Bytes(c.params.time)).
		AddData(encode.Uint32Bytes(c.params.memory)).
		AddData([]byte{c.params.threads})
}

// decodeArgonPoly_v0 decodes an argonPolyCrypter from the pushes extracted from
// a version 0 blob.
func decodeArgonPoly_v0(pw string, pushes [][]byte) (*argonPolyCrypter, error) {
	if len(pushes) != 5 {
		return nil, fmt.Errorf("decodeArgonPoly_v0 expected 5 pushes, but got %d", len(pushes))
	}
	saltB, timeB, memB, threadsB := pushes[0], pushes[1], pushes[2], pushes[3]
	ckSumB := pushes[4]

	if len(saltB) != KeySize {
		return nil, fmt.Errorf("expected salt of length %d, got %d", KeySize, len(saltB))
	}
	var salt Salt
	copy(salt[:], saltB)

	if len(threadsB) != 1 {
		return nil, fmt.Errorf("threads parameter encoding length is incorrect. expected 1, got %d", len(threadsB))
	}

	// Create the argonPolyCrypter.
	params := &argonParams{
		time:    intCoder.Uint32(timeB),
		memory:  intCoder.Uint32(memB),
		threads: threadsB[0],
	}
	c := &argonPolyCrypter{
		salt:   salt,
		params: params,
	}

	// Prepare the key.
	pwB := []byte(pw)
	keyB := argon2.IDKey(pwB, c.salt[:], params.time, params.memory, params.threads, KeySize)
	copy(c.key[:], keyB)

	// Check the checkphrase.
	ckPhrase, err := c.Decrypt(ckSumB)
	if err != nil {
		return nil, fmt.Errorf("checkphrase decryption error: %v", err)
	}
	if string(ckPhrase) != checkPhrase {
		return nil, fmt.Errorf("checkphrase mismatch")
	}
	return c, nil
}
