// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"syscall"

	"decred.org/dcrdex/dex/encode"
	"golang.org/x/crypto/ssh/terminal"
)

type passwordReadResponse struct {
	password []byte
	err      error
}

// PasswordPrompt prompts the user to enter a password. Password must not be an
// empty string.
func PasswordPrompt(ctx context.Context, prompt string) ([]byte, error) {
	// Get the initial state of the terminal.
	initialTermState, err := terminal.GetState(syscall.Stdin)
	if err != nil {
		return nil, err
	}

	passwordReadChan := make(chan passwordReadResponse, 1)

	go func() {
		fmt.Print(prompt)
		pass, err := terminal.ReadPassword(syscall.Stdin)
		fmt.Println()
		passwordReadChan <- passwordReadResponse{
			password: pass,
			err:      err,
		}
	}()

	select {
	case <-ctx.Done():
		_ = terminal.Restore(syscall.Stdin, initialTermState)
		return nil, ctx.Err()

	case res := <-passwordReadChan:
		if res.err != nil {
			return nil, res.err
		}
		if res.password == nil {
			return nil, errors.New("password must not be empty")
		}
		return res.password, nil
	}
}

// PasswordHashPrompt prompts the user to enter a password and returns its
// SHA256 hash. Password must not be an empty string.
func PasswordHashPrompt(ctx context.Context, prompt string) ([sha256.Size]byte, error) {
	var authSHA [sha256.Size]byte
	passBytes, err := PasswordPrompt(ctx, prompt)
	if err != nil {
		return authSHA, err
	}
	authSHA = sha256.Sum256(passBytes)
	// Zero password bytes.
	encode.ClearBytes(passBytes)
	return authSHA, nil
}
