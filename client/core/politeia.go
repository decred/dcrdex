package core

import (
	"errors"
	"fmt"

	pi "decred.org/dcrdex/dex/politeia"
	tv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
)

// PoliteiaDetails retrieves the current state for c.politeia, including the politeia URL.
func (c *Core) PoliteiaDetails() (string, bool, int64) {
	return c.politeiaURL, c.politiaSyncing.Load(), c.politeia.ProposalsLastSync()
}

// ProposalsAll fetches the proposals data from local db.
// The argument searchPhrase and filterByVoteStatus are optional.
func (c *Core) ProposalsAll(offset, rowsCount int, searchPhrase string, filterByVoteStatus ...int) ([]*pi.Proposal, int, error) {
	return c.politeia.ProposalsAll(offset, rowsCount, searchPhrase, filterByVoteStatus...)
}

// Proposal retrieves the proposal for the given token argument. If assetID is
// a configured TicketBuyer and the proposal is currently voting, the
// wallet voting details will be returned as part of the proposal details.
func (c *Core) Proposal(assetID uint32, token string) (*pi.Proposal, error) {
	proposal, err := c.politeia.ProposalByToken(token)
	if err != nil {
		return nil, err
	}

	if proposal.VoteStatus != tv1.VoteStatusStarted { // not voting.
		return proposal, nil
	}

	w, tb, err := c.stakingWallet(assetID)
	if err != nil {
		return proposal, nil // voting wallet not configured
	}

	w.mtx.RLock()
	ss := *w.syncStatus
	tip := ss.Blocks
	w.mtx.RUnlock()

	if tip > uint64(proposal.EndBlockHeight) { // Proposal voting already ended. Cannot vote.
		return proposal, nil
	}

	// Return wallet voting info since this proposal is still voting.
	proposal.VoteDetails, err = c.politeia.WalletProposalVoteDetails(tb, token)
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// ProposalsInProgress returns the mini proposals for the proposals that are currently in
// progress, meaning their vote status is unauthorized, authorized or started.
// This is used to display the in progress proposals on the Bison Wallet UI.
func (c *Core) ProposalsInProgress() ([]*pi.MiniProposal, error) {
	return c.politeia.ProposalsInProgress()
}

// CastVotes casts votes for the provided eligible tickets using the provided
// wallet and passphrase for signing.
// The proposal identified by token must exist in the Politeia DB.
// The wallet must be unlocked prior to calling c.politeia.CastVotes.
func (c *Core) CastVote(assetID uint32, pw []byte, token, bit string) error {
	if c.politeia == nil {
		return fmt.Errorf("politeia not configured")
	}

	if bit != pi.VoteBitYes && bit != pi.VoteBitNo {
		return errors.New("invalid vote bit")
	}

	wallet, tb, err := c.stakingWallet(assetID)
	if err != nil {
		return err
	}

	voteDetails, err := c.politeia.WalletProposalVoteDetails(tb, token)
	if err != nil {
		return err
	}

	if len(voteDetails.EligibleTickets) == 0 {
		if len(voteDetails.Votes) > 0 {
			return fmt.Errorf("all eligible tickets have already voted on proposal (%s)", token)
		}
		return fmt.Errorf("no eligible tickets found for voting wallet")
	}

	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return fmt.Errorf("password error: %w", err)
	}
	defer crypter.Close()

	if err = c.connectAndUnlock(crypter, wallet); err != nil {
		return err
	}

	return c.politeia.CastVotes(tb, voteDetails.EligibleTickets, bit, token)
}
