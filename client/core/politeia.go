package core

import (
	"fmt"

	pi "decred.org/dcrdex/dex/politeia"
)

// PoliteiaDetails retrieves the current Politeia state from the DCR wallet.
func (c *Core) PoliteiaDetails() (string, bool, int64) {
	pv, err := c.politeiaVoter()
	if err != nil {
		return "", false, 0
	}
	return pv.PoliteiaDetails()
}

// ProposalsAll fetches the proposals data from the wallet's local db.
// The argument searchPhrase and filterByVoteStatus are optional.
func (c *Core) ProposalsAll(offset, rowsCount int, searchPhrase string, filterByVoteStatus ...int) ([]*pi.Proposal, int, error) {
	pv, err := c.politeiaVoter()
	if err != nil {
		return nil, 0, fmt.Errorf("politeia not configured: %w", err)
	}
	return pv.ProposalsAll(offset, rowsCount, searchPhrase, filterByVoteStatus...)
}

// Proposal retrieves the proposal for the given token argument, including
// wallet voting details if applicable.
func (c *Core) Proposal(assetID uint32, token string) (*pi.Proposal, error) {
	pv, err := c.politeiaVoter(assetID)
	if err != nil {
		return nil, fmt.Errorf("politeia not configured: %w", err)
	}
	return pv.Proposal(token)
}

// ProposalsInProgress returns the mini proposals for the proposals that are
// currently in progress, meaning their vote status is unauthorized, authorized
// or started. This is used to display the in progress proposals on the Bison
// Wallet UI.
func (c *Core) ProposalsInProgress() ([]*pi.MiniProposal, error) {
	pv, err := c.politeiaVoter()
	if err != nil {
		return nil, nil
	}
	return pv.ProposalsInProgress()
}

// CastVote casts votes for the provided eligible tickets using the provided
// wallet and passphrase for signing. The wallet is unlocked before delegating
// to the wallet's CastVote method.
func (c *Core) CastVote(assetID uint32, pw []byte, token, bit string) error {
	pv, err := c.politeiaVoter(assetID)
	if err != nil {
		return fmt.Errorf("politeia not configured: %w", err)
	}

	wallet, _, err := c.stakingWallet(assetID)
	if err != nil {
		return fmt.Errorf("staking wallet error: %w", err)
	}

	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return fmt.Errorf("password error: %w", err)
	}
	defer crypter.Close()

	if err = c.connectAndUnlock(crypter, wallet); err != nil {
		return err
	}

	return pv.CastVote(token, bit)
}
