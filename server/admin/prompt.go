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
func PasswordPrompt(prompt string) ([]byte, error) {
	fmt.Print(prompt)
	pass, err := terminal.ReadPassword(syscall.Stdin)
	fmt.Println()
	if err != nil {
		return nil, err
	}
	if pass == nil {
		return nil, errors.New("password must not be empty")
	}
	return pass, nil
}

// PasswordHashPrompt prompts the user to enter a password and returns its
// SHA256 hash. Password must not be an empty string.
func PasswordHashPrompt(prompt string) ([sha256.Size]byte, error) {
	var authSHA [sha256.Size]byte
	passBytes, err := PasswordPrompt(prompt)
	if err != nil {
		return authSHA, err
	}
	authSHA = sha256.Sum256(passBytes)
	// Zero password bytes.
	ClearBytes(passBytes)
	return authSHA, nil
}

func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
