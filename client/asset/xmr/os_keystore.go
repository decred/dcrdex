package xmr

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

type keystore struct{}

func (ks *keystore) put(pw string) error {
	svc, user, err := makeCreds()
	if err != nil {
		return err
	}
	err = keyring.Set(svc, user, pw)
	if err != nil {
		return err
	}
	return nil
}

func (ks *keystore) get() (string, error) {
	svc, user, err := makeCreds()
	if err != nil {
		return "", err
	}
	pw, err := keyring.Get(svc, user)
	if err != nil {
		return "", err
	}
	return pw, nil
}

func (ks *keystore) delete() error { // unused but tested
	svc, user, err := makeCreds()
	if err != nil {
		return err
	}
	err = keyring.Delete(svc, user)
	if err != nil {
		return err
	}
	return nil
}

func makeCreds() (string, string, error) {
	user, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current user: %w", err)
	}
	svc := "svc_" + user.Uid
	return svc, user.Username, nil
}
