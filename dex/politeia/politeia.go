package politeia

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/udb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	gov "github.com/decred/dcrdata/gov/v5/politeia"
	v1 "github.com/decred/politeia/politeiawww/api/records/v1"
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
	*gov.ProposalsDB
	client *piclient.Client // for sending votes
	files  sync.Map         // token -> decoded index.md file contents
}

func New(politeiaURL string, dbPath string, log dex.Logger) (*Politeia, error) {
	gov.UseLogger(log)

	pdb, err := gov.NewProposalsDB(politeiaURL, dbPath)
	if err != nil {
		return nil, err
	}

	pc, err := piclient.New(politeiaURL+"/api", piclient.Opts{})
	if err != nil {
		return nil, err
	}

	return &Politeia{
		ProposalsDB: pdb,
		client:      pc,
		files:       sync.Map{},
	}, nil
}

func (p *Politeia) FetchProposalDescription(token string) (string, error) {
	// Ensure proposal exists in our db.
	proposal, err := p.ProposalByToken(token)
	if err != nil {
		return "", err
	}

	req := v1.Details{
		Token: token,
	}
	resp, err := p.client.RecordDetails(req)
	if err != nil {
		return "", err
	}

	cacheKey := fmt.Sprintf("%v-%v", token, proposal.Version)
	if resp.Version == proposal.Version {
		// Check cache for proposal files.
		if v, ok := p.files.Load(cacheKey); ok {
			return string(v.([]byte)), nil
		}
	}

	for _, file := range resp.Files {
		if file.Name == "index.md" {
			b, err := base64.StdEncoding.DecodeString(file.Payload)
			if err != nil {
				return "", err
			}

			// Update cached proposal file version
			p.files.Delete(cacheKey)
			p.files.Store(fmt.Sprintf("%v-%v", token, resp.Version), b)

			return string(b), nil
		}
	}

	return "", errors.New(ErrNotExist)
}

// WalletProposalVoteDetails retrieves the vote details for a proposal token
// and cross references them with the tickets owned by the provided wallet.
// It returns the eligible tickets that have not yet voted, the tickets that
// have voted along with their votes, and the total yes and no votes cast by
// the wallet.
func (p *Politeia) WalletProposalVoteDetails(ctx context.Context, wallet VotingWallet, token string) (*WalletProposalVoteDetails, error) {
	// Ensure proposal exists in our db.
	_, err := p.ProposalByToken(token)
	if err != nil {
		return nil, err
	}

	req := tkv1.Results{
		Token: token,
	}
	votesResults, err := p.client.TicketVoteResults(req)
	if err != nil {
		return nil, err
	}

	hashes := make([]*chainhash.Hash, len(votesResults.Votes))
	castVotes := make(map[string]string)
	for _, v := range votesResults.Votes {
		hash, err := chainhash.NewHashFromStr(v.Ticket)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
		castVotes[v.Ticket] = v.VoteBit
	}

	walletTicketHashes, addresses, err := wallet.CommittedTickets(ctx, hashes)
	if err != nil {
		return nil, err
	}

	eligibleWalletTickets := make([]*EligibleTicket, 0) // eligibleWalletTickets are wallet tickets that have not yet voted.
	walletVotedTickets := make([]*ProposalVote, 0)      // walletVotedTickets are wallet tickets that have voted.
	var yesVotes, noVotes int32
	for i := 0; i < len(walletTicketHashes); i++ {
		ticket := &EligibleTicket{
			Hash:    walletTicketHashes[i].String(),
			Address: addresses[i].String(),
		}

		isMine, accountNumber, err := walletAddressAccount(ctx, wallet, ticket.Address)
		if err != nil {
			return nil, err
		}

		// filter out tickets controlled by imported accounts or not owned by this wallet
		if !isMine || accountNumber == udb.ImportedAddrAccount {
			continue
		}

		// filter out wallet tickets that have voted.
		if voteBit, ok := castVotes[ticket.Hash]; ok {
			pv := &ProposalVote{
				Ticket: ticket,
			}

			switch voteBit {
			case "1":
				noVotes++
				pv.Bit = VoteBitNo
			case "2":
				yesVotes++
				pv.Bit = VoteBitYes
			}

			walletVotedTickets = append(walletVotedTickets, pv)
			continue
		}

		eligibleWalletTickets = append(eligibleWalletTickets, ticket)
	}

	return &WalletProposalVoteDetails{
		EligibleTickets: eligibleWalletTickets,
		Votes:           walletVotedTickets,
		YesVotes:        yesVotes,
		NoVotes:         noVotes,
	}, nil
}

// CastVotes casts votes for the provided eligible tickets using the provided
// wallet and passphrase for signing. The proposal identified by token must
// exist in the politeia db. wallet must be unlocked prior to calling CastVotes.
func (p *Politeia) CastVotes(ctx context.Context, wallet VotingWallet, eligibleTickets []*ProposalVote, token string) error {
	// Ensure proposal exists in our db.
	_, err := p.ProposalByToken(token)
	if err != nil {
		return err
	}

	vDetails, err := p.client.TicketVoteDetails(tkv1.Details{Token: token})
	if err != nil {
		return err
	}

	votes := make([]tkv1.CastVote, 0)
	for _, eligibleTicket := range eligibleTickets {
		var voteBitHex string
		for _, vv := range vDetails.Vote.Params.Options { // Verify that the vote bit is valid.
			if vv.ID == eligibleTicket.Bit {
				voteBitHex = strconv.FormatUint(vv.Bit, 16)
				break
			}
		}

		if voteBitHex == "" {
			return errors.New("invalid vote bit")
		}

		ticket := eligibleTicket.Ticket

		msg := token + ticket.Hash + voteBitHex

		signature, err := walletSignMessage(ctx, wallet, ticket.Address, msg)
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

func walletAddressAccount(ctx context.Context, wallet VotingWallet, address string) (bool, uint32, error) {
	addr, err := stdaddr.DecodeAddress(address, wallet.ChainParams())
	if err != nil {
		return false, 0, err
	}

	known, _ := wallet.KnownAddress(ctx, addr)
	if known != nil {
		accountNumber, err := wallet.AccountNumber(ctx, "")
		return true, accountNumber, err
	}

	return false, 0, nil
}

func walletSignMessage(ctx context.Context, wallet VotingWallet, address string, message string) ([]byte, error) {
	addr, err := stdaddr.DecodeAddress(address, wallet.ChainParams())
	if err != nil {
		return nil, translateError(err)
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	switch addr.(type) {
	case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
	default:
		return nil, errors.New(ErrInvalidAddress)
	}

	sig, err := wallet.SignMessage(ctx, message, addr)
	if err != nil {
		return nil, translateError(err)
	}

	return sig, nil
}
