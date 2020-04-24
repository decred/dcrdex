// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

type passwordReadResult struct {
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

	done := make(chan passwordReadResult, 1)

	fmt.Print(prompt)
	go func() {
		pass, err := terminal.ReadPassword(syscall.Stdin)
		done <- passwordReadResult{
			password: pass,
			err:      err,
		}
	}()

	select {
	case <-ctx.Done():
		_ = terminal.Restore(syscall.Stdin, initialTermState)
		return nil, ctx.Err()

	case passwordReadResult := <-done:
		fmt.Println()
		if passwordReadResult.err != nil {
			return nil, passwordReadResult.err
		}
		if passwordReadResult.password == nil {
			return nil, errors.New("password must not be empty")
		}
		return passwordReadResult.password, nil
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
	ClearBytes(passBytes)
	return authSHA, nil
}

func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
