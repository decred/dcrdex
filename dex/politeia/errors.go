package politeia

import (
	"decred.org/dcrwallet/v4/errors"
)

const (
	ErrInsufficientBalance = "insufficient_balance"
	ErrNotExist            = "not_exists"
	ErrInvalidAddress      = "invalid_address"
	ErrNoPeers             = "no_peers"
)

func translateError(err error) error {
	if err, ok := err.(*errors.Error); ok {
		switch err.Kind {
		case errors.InsufficientBalance:
			return errors.New(ErrInsufficientBalance)
		case errors.NotExist:
			return errors.New(ErrNotExist)
		case errors.NoPeers:
			return errors.New(ErrNoPeers)
		}
	}
	return err
}
