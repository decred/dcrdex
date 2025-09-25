package xmr

import (
	"fmt"
	"os/user"

	"decred.org/dcrdex/dex"
	"github.com/zalando/go-keyring"
)

type keystore struct{}

func (ks *keystore) put(pw string, net dex.Network) error {
	svc, user, err := makeCreds(net)
	if err != nil {
		return err
	}
	err = keyring.Set(svc, user, pw)
	if err != nil {
		return err
	}
	return nil
}

func (ks *keystore) get(net dex.Network) (string, error) {
	svc, user, err := makeCreds(net)
	if err != nil {
		return "", err
	}
	pw, err := keyring.Get(svc, user)
	if err != nil {
		return "", err
	}
	return pw, nil
}

func (ks *keystore) delete(net dex.Network) error { // unused but tested
	svc, user, err := makeCreds(net)
	if err != nil {
		return err
	}
	err = keyring.Delete(svc, user)
	if err != nil {
		return err
	}
	return nil
}

func makeCreds(net dex.Network) (string, string, error) {
	user, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current user: %w", err)
	}
	var svc string
	switch net {
	case dex.Mainnet:
		svc = fmt.Sprintf("svc_pw_%d_%s", dex.Mainnet, user.Uid)
	case dex.Testnet:
		svc = fmt.Sprintf("svc_pw_%d_%s", dex.Testnet, user.Uid)
	case dex.Simnet:
		svc = fmt.Sprintf("svc_pw_%d_%s", dex.Simnet, user.Uid)
	default:
		return "", "", fmt.Errorf("unknown network: %d", net)
	}
	return svc, user.Username, nil
}
