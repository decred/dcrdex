// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	rv1 "github.com/decred/politeia/politeiawww/api/records/v1"
	tv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
)

type VotingWallet interface {
	AddressAccount(address string) (bool, uint32, error)
	SignMessage(msg string, addr string) ([]byte, error)
	CommittedTickets(tickets []*chainhash.Hash) ([]*chainhash.Hash, []stdaddr.Address, error)
}

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
}

type EligibleTicket struct {
	Hash    string `json:"hash"`
	Address string `json:"address"`
}

type ProposalVote struct {
	Ticket *EligibleTicket `json:"ticket"`
	Bit    string          `json:"bit"`
}

// Proposal is the struct that holds all politeia data that dcrdata needs
// for each proposal. This is the object that is saved to stormdb. It uses data
// from three politeia API's: records, comments and ticketvote.
type Proposal struct {
	ID int `json:"id" storm:"id,increment"`

	// Record API data
	State       rv1.RecordStateT  `json:"state"`
	Status      rv1.RecordStatusT `json:"status"`
	Token       string            `json:"token"`
	Version     uint32            `json:"version"`
	Timestamp   uint64            `json:"timestamp" storm:"index"`
	Username    string            `json:"username"`
	Description string            `json:"description"`

	// Pi metadata
	Name string `json:"name"`

	// User metadata
	UserID string `json:"userid"`

	// Comments API data
	CommentsCount int32 `json:"commentscount"`

	// Ticketvote API data
	VoteStatus       tv1.VoteStatusT    `json:"votestatus"`
	VoteResults      []tv1.VoteResult   `json:"voteresults"`
	StatusChangeMsg  string             `json:"statuschangemsg"`
	EligibleTickets  uint32             `json:"eligibletickets"`
	StartBlockHeight uint32             `json:"startblockheight"`
	EndBlockHeight   uint32             `json:"endblockheight"`
	QuorumPercentage uint32             `json:"quorumpercentage"`
	PassPercentage   uint32             `json:"passpercentage"`
	TotalVotes       uint64             `json:"totalvotes"`
	ChartData        *ProposalChartData `json:"chartdata"`

	// Synced is used to indicate that this proposal is already fully
	// synced with politeia server, and does not need to make any more
	// http requests for this proposal
	Synced bool `json:"synced"`

	// Timestamps
	PublishedAt uint64 `json:"publishedat" storm:"index"`
	CensoredAt  uint64 `json:"censoredat"`
	AbandonedAt uint64 `json:"abandonedat"`

	// If a wallet that satisfies politeia.VotingWallet does not exist.
	// VoteDetails will be nil.
	VoteDetails *WalletProposalVoteDetails `json:"-"`
}

type ProposalMetadata struct {
	IsPassing          bool
	Approval           float32
	Rejection          float32
	Yes                int64
	No                 int64
	VoteCount          int64
	QuorumCount        int64
	QuorumAchieved     bool
	PassPercent        float32
	VoteStatusDesc     string
	ProposalStateDesc  string
	ProposalStatusDesc string
}

// Metadata performs some common manipulations of the ProposalRecord data to
// prepare figures for display.
func (pi *Proposal) Metadata() *ProposalMetadata {
	meta := new(ProposalMetadata)
	switch pi.VoteStatus {
	case tv1.VoteStatusStarted, tv1.VoteStatusFinished,
		tv1.VoteStatusApproved, tv1.VoteStatusRejected:
		for _, count := range pi.VoteResults {
			switch count.ID {
			case "yes":
				meta.Yes = int64(count.Votes)
			case "no":
				meta.No = int64(count.Votes)
			}
		}
		meta.VoteCount = meta.Yes + meta.No
		quorumPct := float32(pi.QuorumPercentage)
		meta.QuorumCount = int64(quorumPct * float32(pi.EligibleTickets))
		meta.PassPercent = float32(pi.PassPercentage)
		pctVoted := float32(meta.VoteCount) / float32(pi.EligibleTickets)
		meta.QuorumAchieved = pctVoted > quorumPct
		if meta.VoteCount > 0 {
			meta.Approval = (float32(meta.Yes) / float32(meta.VoteCount)) * 100
			meta.Rejection = (100 - meta.Approval)
		}
		meta.IsPassing = meta.Approval > meta.PassPercent
	}
	meta.VoteStatusDesc = VotesStatuses[pi.VoteStatus]
	meta.ProposalStateDesc = rv1.RecordStates[pi.State]
	meta.ProposalStatusDesc = rv1.RecordStatuses[pi.Status]
	return meta
}

// ProposalChartData defines the data used to plot proposal ticket votes
// charts.
type ProposalChartData struct {
	Yes  []uint64 `json:"yes"`
	No   []uint64 `json:"no"`
	Time []int64  `json:"time"`
}

// IsEqual compares data between the two ProposalRecord structs passed.
func (pi *Proposal) IsEqual(b Proposal) bool {
	if pi.Token != b.Token || pi.Name != b.Name || pi.State != b.State ||
		pi.Status != b.Status || pi.StatusChangeMsg != b.StatusChangeMsg ||
		pi.CommentsCount != b.CommentsCount || pi.Timestamp != b.Timestamp ||
		pi.VoteStatus != b.VoteStatus || pi.TotalVotes != b.TotalVotes ||
		pi.PublishedAt != b.PublishedAt || pi.CensoredAt != b.CensoredAt ||
		pi.AbandonedAt != b.AbandonedAt || pi.ChartData != b.ChartData {
		return false
	}
	return true
}
