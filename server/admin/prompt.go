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

// PasswordPrompt prompts the user to enter a password and returns its sha256
// hash. Password must not be an empty string.
func PasswordPrompt(prompt string) ([32]byte, error) {
	var authSHA [32]byte
	fmt.Println(prompt)
	password, err := terminal.ReadPassword(syscall.Stdin)
	if err != nil {
		return authSHA, err
	}
	if password == nil {
		return authSHA, errors.New("password must not be empty")
	}
	authSHA = sha256.Sum256(password)
	// Zero password bytes.
	for i := range password {
		password[i] = 0x00
	}
	return authSHA, nil
}
