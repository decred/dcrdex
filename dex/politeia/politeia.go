// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/udb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	tkv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
	piclient "github.com/decred/politeia/politeiawww/client"
)

const (
	// VoteBitYes is the string value for identifying "yes" vote bits
	VoteBitYes = tkv1.VoteOptionIDApprove
	// VoteBitNo is the string value for identifying "no" vote bits
	VoteBitNo = tkv1.VoteOptionIDReject

	PoliteiaMainnetHost = "https://proposals.decred.org"
)

type Politeia struct {
	*proposalsDB
	log    dex.Logger
	client *piclient.Client
}

// New creates and returns a new instance of *Politeia.
func New(ctx context.Context, politeiaURL string, dbPath string, log dex.Logger) (*Politeia, error) {
	pc, err := piclient.New(politeiaURL+"/api", piclient.Opts{})
	if err != nil {
		return nil, err
	}

	pdb, err := newProposalsDB(ctx, dbPath, log, pc)
	if err != nil {
		return nil, err
	}

	return &Politeia{
		log:         log,
		proposalsDB: pdb,
		client:      pc,
	}, nil
}

// WalletProposalVoteDetails retrieves the vote details for a proposal token
// and cross references them with the tickets owned by the provided wallet.
// It returns the eligible tickets that have not yet voted, the tickets that
// have voted along with their votes, and the total yes and no votes cast by
// the wallet.
func (p *Politeia) WalletProposalVoteDetails(wallet VotingWallet, token string) (*WalletProposalVoteDetails, error) {
	req := tkv1.Results{
		Token: token,
	}
	votesResults, err := p.client.TicketVoteResults(req)
	if err != nil {
		return nil, err
	}

	hashes := make([]*chainhash.Hash, 0, len(votesResults.Votes))
	castVotes := make(map[string]string)
	for _, v := range votesResults.Votes {
		hash, err := chainhash.NewHashFromStr(v.Ticket)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
		castVotes[v.Ticket] = v.VoteBit
	}

	walletTicketHashes, addresses, err := wallet.CommittedTickets(hashes)
	if err != nil {
		return nil, err
	}

	eligibleWalletTickets := make([]*Ticket, 0) // eligibleWalletTickets are wallet tickets that have not yet voted.
	walletVotedTickets := make([]*Ticket, 0)    // walletVotedTickets are wallet tickets that have voted.
	for i := 0; i < len(walletTicketHashes); i++ {
		ticket := &Ticket{
			Hash:    walletTicketHashes[i].String(),
			Address: addresses[i].String(),
		}

		isMine, accountNumber, err := wallet.AddressAccount(ticket.Address)
		if err != nil {
			return nil, err
		}

		// filter out tickets controlled by imported accounts or not owned by this wallet
		if !isMine || accountNumber == udb.ImportedAddrAccount {
			continue
		}

		// filter out wallet tickets that have voted.
		if _, ok := castVotes[ticket.Hash]; ok {
			walletVotedTickets = append(walletVotedTickets, ticket)
			continue
		}

		eligibleWalletTickets = append(eligibleWalletTickets, ticket)
	}

	return &WalletProposalVoteDetails{
		EligibleTickets: eligibleWalletTickets,
		Votes:           walletVotedTickets,
	}, nil
}

// CastVotes casts votes for the provided eligible tickets using the provided
// wallet and passphrase for signing. The proposal identified by token must
// exist in the politeia db. wallet must be unlocked prior to calling CastVotes.
func (p *Politeia) CastVotes(wallet VotingWallet, eligibleTickets []*Ticket, bit, token string) error {
	vDetails, err := p.client.TicketVoteDetails(tkv1.Details{Token: token})
	if err != nil {
		return err
	}

	var voteBitHex string
	for _, vv := range vDetails.Vote.Params.Options { // Verify that the vote bit is valid.
		if vv.ID == bit {
			voteBitHex = strconv.FormatUint(vv.Bit, 16)
			break
		}
	}

	if voteBitHex == "" {
		return errors.New("invalid vote bit")
	}

	votes := make([]tkv1.CastVote, 0)
	for i := range eligibleTickets {
		ticket := eligibleTickets[i]

		msg := token + ticket.Hash + voteBitHex

		signature, err := wallet.SignMessage(msg, ticket.Address)
		if err != nil {
			return err
		}

		vote := tkv1.CastVote{
			Token:     token,
			Ticket:    ticket.Hash,
			VoteBit:   voteBitHex,
			Signature: hex.EncodeToString(signature),
		}
		votes = append(votes, vote)
	}

	resp, err := p.client.TicketVoteCastBallot(tkv1.CastBallot{Votes: votes})
	if err != nil {
		return err
	}

	var errors []string
	for _, receipt := range resp.Receipts {
		if receipt.ErrorContext != "" {
			errors = append(errors, fmt.Sprintf("ticket %s: %s", receipt.Ticket, receipt.ErrorContext))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors casting votes: %v", strings.Join(errors, "; "))
	}

	return nil
}
