package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

func actualMain() error {
	secPrivKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return err
	}
	// privkeyFile, err := os.OpenFile("dexprivkey.pem", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	// if err != nil {
	// 	return err
	// }
	// defer privkeyFile.Close()

	// err = pem.Encode(privkeyFile, &pem.Block{
	// 	Type:  "EC PRIVATE KEY",
	// 	Bytes: secPrivKey.Serialize(),
	// })
	// if err != nil {
	// 	return err
	// }

	err = ioutil.WriteFile("dexprivkey", secPrivKey.Serialize(), 0600)
	if err != nil {
		return err
	}

	pubKey := secPrivKey.PubKey()
	// pubkeyFile, err := os.OpenFile("dexpubkey.pem", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660)
	// if err != nil {
	// 	return err
	// }
	// defer pubkeyFile.Close()

	// err = pem.Encode(pubkeyFile, &pem.Block{
	// 	Type:  "PUBLIC KEY",
	// 	Bytes: pubKey.SerializeCompressed(),
	// })
	// if err != nil {
	// 	return err
	// }

	err = ioutil.WriteFile("dexpubkey", pubKey.SerializeCompressed(), 0600)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	if err := actualMain(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}
