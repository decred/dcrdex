// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
)

func dexKey(path string, pass []byte) (*secp256k1.PrivateKey, error) {
	var privKey *secp256k1.PrivateKey
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Infof("Creating new DEX signing key file at %s...", path)
		privKey, err = createAndStoreKey(path, pass)
		if err != nil {
			return nil, fmt.Errorf("failed to load DEX private key from file %s: %v",
				path, err)
		}
	} else {
		log.Infof("Loading DEX signing key from %s...", path)
		privKey, err = loadKeyFile(path, pass)
		if err != nil {
			return nil, fmt.Errorf("failed to load DEX private key from file %s: %v",
				path, err)
		}
	}
	return privKey, nil
}

func loadKeyFile(path string, pass []byte) (*secp256k1.PrivateKey, error) {
	// Load and decrypt it.
	pkFileBuffer, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ReadFile: %v", err)
	}

	ver, pushes, err := encode.DecodeBlob(pkFileBuffer)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal DEX signing key data: %v", err)
	}
	if ver != 0 {
		return nil, fmt.Errorf("unrecognized key file version %d: %v", ver, err)
	}
	if len(pushes) != 2 {
		return nil, fmt.Errorf("invalid signing key file, "+
			"containing %d data pushes instead of 2", len(pushes))
	}
	keyParams := pushes[0]
	encKey := pushes[1]

	crypter, err := encrypt.Deserialize(pass, keyParams)
	if err != nil {
		return nil, err
	}

	keyB, err := crypter.Decrypt(encKey)
	if err != nil {
		return nil, err
	}
	return secp256k1.PrivKeyFromBytes(keyB), nil
}

func createAndStoreKey(path string, pass []byte) (*secp256k1.PrivateKey, error) {
	// Disallow an empty password.
	if len(pass) == 0 {
		return nil, fmt.Errorf("empty password")
	}
	// Do not overwrite existing key files.
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("key file exists")
	}

	// Create and store a new key.
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate DEX signing key: %v", err)
	}

	// Encrypt the private key.
	crypter := encrypt.NewCrypter(pass)
	keyParams := crypter.Serialize()
	encKey, err := crypter.Encrypt(privKey.Serialize())
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt DEX signing key: %v", err)
	}
	// Check a round trip with this key data.
	_, err = crypter.Decrypt(encKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt DEX signing key: %v", err)
	}

	// Store it.
	data := encode.BuildyBytes{0}.AddData(keyParams).AddData(encKey)
	err = ioutil.WriteFile(path, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write DEX signing key: %v", err)
	}

	return privKey, nil
}
