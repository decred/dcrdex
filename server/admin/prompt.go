// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

// PasswordPrompt prompts the user to enter a password. Password must not be an
// empty string.
func PasswordPrompt(prompt string) (string, error) {
	fmt.Print(prompt)
	pass, err := terminal.ReadPassword(syscall.Stdin)
	fmt.Println()
	if err != nil {
		return "", err
	}
	if pass == nil {
		return "", errors.New("password must not be empty")
	}
	return string(pass), nil
}

// PasswordHashPrompt prompts the user to enter a password and returns its
// SHA256 hash. Password must not be an empty string.
func PasswordHashPrompt(prompt string) ([32]byte, error) {
	var authSHA [32]byte
	password, err := PasswordPrompt(prompt)
	if err != nil {
		return authSHA, err
	}
	passBytes := []byte(password)
	authSHA = sha256.Sum256(passBytes)
	// Zero password bytes. What about the password string?
	ClearBytes(passBytes)
	return authSHA, nil
}

func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
