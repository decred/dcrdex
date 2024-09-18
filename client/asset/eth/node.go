// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func importKeyToKeyStore(ks *keystore.KeyStore, priv *ecdsa.PrivateKey, pw []byte) error {
	accounts := ks.Accounts()
	if len(accounts) == 0 {
		_, err := ks.ImportECDSA(priv, string(pw))
		return err
	} else if len(accounts) == 1 {
		address := crypto.PubkeyToAddress(priv.PublicKey)
		if !bytes.Equal(accounts[0].Address.Bytes(), address.Bytes()) {
			errMsg := "importKeyToKeyStore: attemping to import account to eth wallet: %v, " +
				"but node already contains imported account: %v"
			return fmt.Errorf(errMsg, address, accounts[0].Address)
		}
	} else {
		return fmt.Errorf("importKeyToKeyStore: eth wallet keystore contains %v accounts", accounts)
	}
	return nil
}

// accountCredentials captures the account-specific geth interfaces.
type accountCredentials struct {
	ks      *keystore.KeyStore
	acct    *accounts.Account
	addr    common.Address
	wallet  accounts.Wallet
	chainID *big.Int
}

func (c *accountCredentials) signedTx(txOpts *bind.TransactOpts, to common.Address, data []byte) (*types.Transaction, error) {
	return c.ks.SignTx(*c.acct, types.NewTx(&types.DynamicFeeTx{
		To:        &to,
		ChainID:   c.chainID,
		Nonce:     txOpts.Nonce.Uint64(),
		Gas:       txOpts.GasLimit,
		GasFeeCap: txOpts.GasFeeCap,
		GasTipCap: txOpts.GasTipCap,
		Value:     txOpts.Value,
		Data:      data,
	}), c.chainID)
}

func pathCredentials(chainID *big.Int, dir string) (*accountCredentials, error) {
	// TODO: Use StandardScryptN and StandardScryptP?
	return credentialsFromKeyStore(keystore.NewKeyStore(dir, keystore.LightScryptN, keystore.LightScryptP), chainID)

}

func credentialsFromKeyStore(ks *keystore.KeyStore, chainID *big.Int) (*accountCredentials, error) {
	accts := ks.Accounts()
	if len(accts) != 1 {
		return nil, fmt.Errorf("unexpected number of accounts, %d", len(accts))
	}
	acct := accts[0]
	wallets := ks.Wallets()
	if len(wallets) != 1 {
		return nil, fmt.Errorf("unexpected number of wallets, %d", len(wallets))
	}
	return &accountCredentials{
		ks:      ks,
		acct:    &acct,
		addr:    acct.Address,
		wallet:  wallets[0],
		chainID: chainID,
	}, nil
}

func signData(creds *accountCredentials, data []byte) (sig, pubKey []byte, err error) {
	h := crypto.Keccak256(data)
	sig, err = creds.ks.SignHash(*creds.acct, h)
	if err != nil {
		return nil, nil, err
	}
	if len(sig) != 65 {
		return nil, nil, fmt.Errorf("unexpected signature length %d", len(sig))
	}

	pubKey, err = recoverPubkey(h, sig)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error recovering pubkey %w", err)
	}

	// Lop off the "recovery id", since we already recovered the pub key and
	// it's not used for validation.
	sig = sig[:64]

	return
}

func walletCredentials(chainID *big.Int, dir string, net dex.Network) (*accountCredentials, error) {
	walletDir := getWalletDir(dir, net)
	return pathCredentials(chainID, filepath.Join(walletDir, "keystore"))
}
