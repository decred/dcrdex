package core

import (
	"fmt"

	pi "decred.org/dcrdex/dex/politeia"
	tv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
)

// PoliteiaDetails retrieves the current state for c.politeia, including the politeia URL.
func (c *Core) PoliteiaDetails() (string, bool, int64) {
	if c.politeia == nil {
		return "", false, 0
	}
	return c.politeiaURL, c.politiaSyncing.Load(), c.politeia.ProposalsLastSync()
}

// ProposalsAll fetches the proposals data from local db.
// The argument searchPhrase and filterByVoteStatus are optional.
func (c *Core) ProposalsAll(offset, rowsCount int, searchPhrase string, filterByVoteStatus ...int) ([]*pi.Proposal, int, error) {
	if c.politeia == nil {
		return nil, 0, fmt.Errorf("politeia not configured")
	}
	return c.politeia.ProposalsAll(offset, rowsCount, searchPhrase, filterByVoteStatus...)
}

// Proposal retrieves the proposal for the given token argument. If assetID is
// a configured TicketBuyer and the proposal is currently voting, the
// wallet voting details will be returned as part of the proposal details.
func (c *Core) Proposal(assetID uint32, token string) (*pi.Proposal, error) {
	if c.politeia == nil {
		return nil, fmt.Errorf("politeia not configured")
	}

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

	w.mtx.Lock()
	ss := *w.syncStatus
	tip := ss.Blocks
	w.mtx.Unlock()

	// Only return wallet voting info if this proposal is still voting.
	if uint64(proposal.EndBlockHeight) > tip {
		// Proposal already ended. Cannot vote.
		return proposal, nil
	}

	proposal.VoteDetails, err = c.politeia.WalletProposalVoteDetails(tb, token)
	if err != nil {
		return nil, err
	}

	return proposal, nil
}

// CastVotes casts votes for the provided eligible tickets using the provided
// wallet and passphrase for signing. The proposal identified by token must
// exist in the politeia db. wallet must be unlocked prior to calling CastVotes.
func (c *Core) CastVote(assetID uint32, pw []byte, token, bit string) error {
	if c.politeia == nil {
		return fmt.Errorf("politeia not configured")
	}

	if bit != pi.VoteBitYes && bit != pi.VoteBitNo {
		return fmt.Errorf("invalid vote bit: %s", bit)
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

	votingTickets := make([]*pi.ProposalVote, 0, len(voteDetails.EligibleTickets))
	for _, et := range voteDetails.EligibleTickets {
		pv := &pi.ProposalVote{
			Ticket: et,
			Bit:    bit,
		}
		votingTickets = append(votingTickets, pv)
	}

	return c.politeia.CastVotes(tb, votingTickets, token)
}
