// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/lexi"
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

	// dbVersion is the current required version of the proposals db.
	dbVersion = 1
)

type Politeia struct {
	ctx          context.Context
	db           *lexi.DB
	dbConn       *dex.ConnectionMaster
	proposals    *lexi.Table
	proposalMeta *lexi.Table
	log          dex.Logger
	client       *piclient.Client
	lastSync     atomic.Int64
	updating     atomic.Bool
}

// New creates and returns a new instance of *Politeia.
func New(ctx context.Context, politeiaURL string, dbPath string, log dex.Logger) (*Politeia, error) {
	if dbPath == "" {
		return nil, errors.New("missing db path")
	}

	db, err := lexi.New(&lexi.Config{
		Path: dbPath,
		Log:  log.SubLogger("DB"),
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing db: %w", err)
	}

	version, err := db.GetDBVersion()
	if err != nil {
		return nil, err
	}

	// Checks if the correct db version has been set. Must happen before
	// table/index setup since DropAll destroys lexi internal mappings.
	if version == 0 {
		if err = db.SetDBVersion(dbVersion); err != nil {
			return nil, fmt.Errorf("error setting db version: %w", err)
		}

		log.Infof("Proposals DB version %v was set...", dbVersion)
	} else if version > dbVersion {
		return nil, fmt.Errorf("db version is newer than expected: got %v, want %v", version, dbVersion)
	} else if version < dbVersion {
		log.Warnf("Proposals DB version is outdated: got %v, want %v. Attempting to update...", version, dbVersion)

		if err = db.DropAll(); err != nil {
			return nil, fmt.Errorf("error dropping db: %w", err)
		}

		if err = db.SetDBVersion(dbVersion); err != nil {
			return nil, fmt.Errorf("error setting db version: %w", err)
		}

		log.Infof("Proposals DB version %v was set...", dbVersion)
	}

	proposalsTable, err := db.Table(proposalsTable)
	if err != nil {
		return nil, err
	}

	// Add indexes
	proposalsTable.AddUniqueIndex(proposalStatusIndex, func(k, v lexi.KV) ([]byte, error) {
		p, ok := v.(*Proposal)
		if !ok {
			return nil, fmt.Errorf("unexpected type for proposalStatus: %T", v)
		}
		return proposalsStatusIndex(p), nil
	})

	proposalsTable.AddUniqueIndex(proposalTimestampIndex, func(k, v lexi.KV) ([]byte, error) {
		p, ok := v.(*Proposal)
		if !ok {
			return nil, fmt.Errorf("unexpected type for proposalTimestamp: %T", v)
		}
		return proposalsTimestampIndex(p), nil
	})

	proposalsMetaTable, err := db.Table(proposalsMetaTable)
	if err != nil {
		return nil, err
	}

	pc, err := piclient.New(politeiaURL+"/api", piclient.Opts{})
	if err != nil {
		return nil, err
	}

	dbConn := dex.NewConnectionMaster(db)
	if err := dbConn.ConnectOnce(ctx); err != nil {
		return nil, err
	}

	return &Politeia{
		ctx:          ctx,
		log:          log,
		db:           db,
		dbConn:       dbConn,
		proposals:    proposalsTable,
		proposalMeta: proposalsMetaTable,
		client:       pc,
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
			if strings.Contains(err.Error(), "address not found in wallet") {
				continue // address not found in wallet, skip this ticket
			}
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

	if vDetails.Vote == nil {
		return fmt.Errorf("no vote details available for proposal %s", token)
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

	var voteErrors []string
	for _, receipt := range resp.Receipts {
		if receipt.ErrorContext != "" {
			voteErrors = append(voteErrors, fmt.Sprintf("ticket %s: %s", receipt.Ticket, receipt.ErrorContext))
		}
	}

	if len(voteErrors) > 0 {
		return fmt.Errorf("errors casting votes: %v", strings.Join(voteErrors, "; "))
	}

	return nil
}

// Close closes the Politeia db and disconnects from the db connection master.
func (p *Politeia) Close() error {
	p.dbConn.Disconnect()
	return nil
}
