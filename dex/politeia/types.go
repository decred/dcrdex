// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"encoding/json"
	"math"

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
	EligibleTickets []*Ticket `json:"eligibleTickets"`
	Votes           []*Ticket `json:"votes"`
}

type Ticket struct {
	Hash    string `json:"hash"`
	Address string `json:"address"`
}

// Proposal is the struct that holds all politeia data that Bison Wallet needs
// for each proposal. This is the object that is saved to the lexi DB. It uses
// data from three politeia API's: records, comments and ticketvote.
type Proposal struct {
	ID int `json:"id"`

	// Record API data
	State       rv1.RecordStateT  `json:"state"`
	Status      rv1.RecordStatusT `json:"status"`
	Token       string            `json:"token"`
	Version     uint32            `json:"version"`
	Timestamp   uint64            `json:"timestamp"`
	Username    string            `json:"username"`
	Description string            `json:"description"`

	// Pi metadata
	Name string `json:"name"`

	// User metadata
	UserID string `json:"userid"`

	// Comments API data
	CommentsCount int32 `json:"commentscount"`

	// Ticketvote API data
	VoteStatus       tv1.VoteStatusT  `json:"votestatus"`
	VoteResults      []tv1.VoteResult `json:"voteresults"`
	StatusChangeMsg  string           `json:"statuschangemsg"`
	EligibleTickets  uint32           `json:"eligibletickets"`
	StartBlockHeight uint32           `json:"startblockheight"`
	EndBlockHeight   uint32           `json:"endblockheight"`
	QuorumPercentage uint32           `json:"quorumpercentage"`
	PassPercentage   uint32           `json:"passpercentage"`
	TotalVotes       uint64           `json:"totalvotes"`

	// Synced is used to indicate that this proposal is already fully
	// synced with politeia server, and does not need to make any more
	// http requests for this proposal
	Synced bool `json:"synced"`

	// Timestamps
	PublishedAt uint64 `json:"publishedat"`
	CensoredAt  uint64 `json:"censoredat"`
	AbandonedAt uint64 `json:"abandonedat"`

	// If a wallet that satisfies politeia.VotingWallet does not exist.
	// VoteDetails will be nil.
	VoteDetails *WalletProposalVoteDetails `json:"-"`
}

// MarshalBinary satisfies encoding.BinaryMarshaler for Proposal
// so that it can be saved to lexi.
func (p *Proposal) MarshalBinary() ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalBinary satisfies encoding.BinaryUnmarshaler for Proposal.
func (p *Proposal) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, &p)
}

type ProposalMetadata struct {
	IsPassing          bool
	Approval           float64
	Rejection          float64
	Yes                int64
	No                 int64
	VoteCount          int64
	QuorumCount        int64
	QuorumAchieved     bool
	PassPercent        float64
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
		quorumPct := float64(pi.QuorumPercentage) / 100
		meta.QuorumCount = int64(quorumPct * float64(pi.EligibleTickets))
		meta.PassPercent = float64(pi.PassPercentage)
		if pi.EligibleTickets > 0 {
			pctVoted := float64(meta.VoteCount) / float64(pi.EligibleTickets)
			meta.QuorumAchieved = pctVoted >= quorumPct
		}
		denominator := math.Max(float64(meta.VoteCount), float64(meta.QuorumCount))
		if denominator > 0 {
			meta.Approval = (float64(meta.Yes) / denominator) * 100
			meta.Rejection = (float64(meta.No) / denominator) * 100
		}

		meta.IsPassing = meta.Approval > meta.PassPercent
	}
	meta.VoteStatusDesc = VotesStatuses[pi.VoteStatus]
	meta.ProposalStateDesc = rv1.RecordStates[pi.State]
	meta.ProposalStatusDesc = rv1.RecordStatuses[pi.Status]
	return meta
}

// IsEqual compares data between the two ProposalRecord structs passed.
func (pi *Proposal) IsEqual(b Proposal) bool {
	if pi.Token != b.Token || pi.Name != b.Name || pi.State != b.State ||
		pi.Status != b.Status || pi.StatusChangeMsg != b.StatusChangeMsg ||
		pi.CommentsCount != b.CommentsCount || pi.Timestamp != b.Timestamp ||
		pi.VoteStatus != b.VoteStatus || pi.TotalVotes != b.TotalVotes ||
		pi.PublishedAt != b.PublishedAt || pi.CensoredAt != b.CensoredAt ||
		pi.AbandonedAt != b.AbandonedAt || pi.Description != b.Description ||
		pi.Version != b.Version || pi.Username != b.Username ||
		pi.EligibleTickets != b.EligibleTickets ||
		pi.StartBlockHeight != b.StartBlockHeight ||
		pi.EndBlockHeight != b.EndBlockHeight ||
		pi.QuorumPercentage != b.QuorumPercentage ||
		pi.PassPercentage != b.PassPercentage {
		return false
	}
	if len(pi.VoteResults) != len(b.VoteResults) {
		return false
	}
	for i := range pi.VoteResults {
		if pi.VoteResults[i].ID != b.VoteResults[i].ID ||
			pi.VoteResults[i].Votes != b.VoteResults[i].Votes {
			return false
		}
	}
	return true
}

// ToMiniProposal returns a MiniProposal struct with a subset of the data in Proposal.
func (pi *Proposal) ToMiniProposal() *MiniProposal {
	return &MiniProposal{
		Token:      pi.Token,
		Name:       pi.Name,
		Username:   pi.Username,
		Version:    pi.Version,
		VoteStatus: VotesStatuses[pi.VoteStatus],
	}
}

// MiniProposal is a struct that holds a subset of the data in Proposal.
type MiniProposal struct {
	Token      string `json:"token"`
	Name       string `json:"name"`
	Username   string `json:"username"`
	Version    uint32 `json:"version"`
	VoteStatus string `json:"voteStatus"`
}
