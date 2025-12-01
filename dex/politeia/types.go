package politeia

import (
	"context"

	"decred.org/dcrwallet/v5/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	tv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
)

// VotesStatuses maps politeia vote status codes to human-readable strings.
var VotesStatuses = map[tv1.VoteStatusT]string{
	tv1.VoteStatusUnauthorized: "Unauthorized",
	tv1.VoteStatusAuthorized:   "Authorized",
	tv1.VoteStatusStarted:      "Started",
	tv1.VoteStatusFinished:     "Finished",
	tv1.VoteStatusApproved:     "Approved",
	tv1.VoteStatusRejected:     "Rejected",
	tv1.VoteStatusIneligible:   "Ineligible",
}

// WalletProposalVoteDetails contains the details of votes for a proposal for a wallet.
type WalletProposalVoteDetails struct {
	EligibleTickets []*EligibleTicket `json:"eligibleTickets"`
	Votes           []*ProposalVote   `json:"votes"`
	YesVotes        int32             `json:"yesVotes"`
	NoVotes         int32             `json:"noVotes"`
}

type EligibleTicket struct {
	Hash    string `json:"hash"`
	Address string `json:"address"`
}

type ProposalVote struct {
	Ticket *EligibleTicket `json:"ticket"`
	Bit    string          `json:"bit"`
}

type VotingWallet interface {
	ChainParams() *chaincfg.Params
	AccountNumber(ctx context.Context, accountName string) (uint32, error)
	KnownAddress(ctx context.Context, a stdaddr.Address) (*wallet.KnownAddress, error)
	SignMessage(ctx context.Context, msg string, addr stdaddr.Address) (sig []byte, err error)
	CommittedTickets(ctx context.Context, tickets []*chainhash.Hash) ([]*chainhash.Hash, []stdaddr.Address, error)
}
