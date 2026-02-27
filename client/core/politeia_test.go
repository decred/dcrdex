//go:build !harness && !botlive

package core

import (
	"fmt"
	"testing"

	"decred.org/dcrdex/client/asset"
	pi "decred.org/dcrdex/dex/politeia"
)

// tPoliteiaVoter is a mock wallet implementing asset.PoliteiaVoter for testing.
type tPoliteiaVoter struct {
	asset.Wallet
	politeiaDetails func() (string, bool, int64)
	proposalsAll    func(int, int, string, ...int) ([]*pi.Proposal, int, error)
	proposal        func(string) (*pi.Proposal, error)
	proposalsInProg func() ([]*pi.MiniProposal, error)
	castVote        func(string, string) error
	enabled         bool
}

func (t *tPoliteiaVoter) PoliteiaDetails() (string, bool, int64) {
	if t.politeiaDetails != nil {
		return t.politeiaDetails()
	}
	return "", false, 0
}
func (t *tPoliteiaVoter) ProposalsAll(offset, rowsCount int, searchPhrase string, filterByVoteStatus ...int) ([]*pi.Proposal, int, error) {
	if t.proposalsAll != nil {
		return t.proposalsAll(offset, rowsCount, searchPhrase, filterByVoteStatus...)
	}
	return nil, 0, nil
}
func (t *tPoliteiaVoter) Proposal(token string) (*pi.Proposal, error) {
	if t.proposal != nil {
		return t.proposal(token)
	}
	return nil, nil
}
func (t *tPoliteiaVoter) ProposalsInProgress() ([]*pi.MiniProposal, error) {
	if t.proposalsInProg != nil {
		return t.proposalsInProg()
	}
	return nil, nil
}
func (t *tPoliteiaVoter) CastVote(token, bit string) error {
	if t.castVote != nil {
		return t.castVote(token, bit)
	}
	return nil
}
func (t *tPoliteiaVoter) PoliteiaEnabled() bool { return t.enabled }

// newTPoliteiaCore creates a Core with a mock PoliteiaVoter wallet wired up
// at asset ID 42 (DCR).
func newTPoliteiaCore(pv *tPoliteiaVoter) *Core {
	c := &Core{}
	c.wallets = make(map[uint32]*xcWallet)
	c.wallets[42] = &xcWallet{
		Wallet:  pv,
		AssetID: 42,
		traits:  asset.WalletTraitPoliteiaVoter,
	}
	return c
}

// TestPoliteiaDetailsNoWallet verifies that PoliteiaDetails returns safe
// defaults when no DCR wallet is loaded.
func TestPoliteiaDetailsNoWallet(t *testing.T) {
	c := &Core{}
	c.wallets = make(map[uint32]*xcWallet)
	url, syncing, lastSync := c.PoliteiaDetails()
	if url != "" {
		t.Errorf("URL: got %q, want empty", url)
	}
	if syncing {
		t.Error("syncing should be false when no wallet")
	}
	if lastSync != 0 {
		t.Error("lastSync should be 0 when no wallet")
	}
}

// TestPoliteiaDetailsDelegates verifies that PoliteiaDetails delegates to the
// wallet's PoliteiaVoter.
func TestPoliteiaDetailsDelegates(t *testing.T) {
	pv := &tPoliteiaVoter{
		politeiaDetails: func() (string, bool, int64) {
			return "https://proposals.decred.org", true, 12345
		},
	}
	c := newTPoliteiaCore(pv)
	url, syncing, lastSync := c.PoliteiaDetails()
	if url != "https://proposals.decred.org" {
		t.Errorf("URL: got %q, want %q", url, "https://proposals.decred.org")
	}
	if !syncing {
		t.Error("syncing should be true")
	}
	if lastSync != 12345 {
		t.Errorf("lastSync: got %d, want 12345", lastSync)
	}
}

// TestProposalsAllNoWallet verifies that ProposalsAll returns an error
// when no DCR wallet is loaded.
func TestProposalsAllNoWallet(t *testing.T) {
	c := &Core{}
	c.wallets = make(map[uint32]*xcWallet)
	_, _, err := c.ProposalsAll(0, 10, "")
	if err == nil {
		t.Fatal("expected error when no wallet")
	}
}

// TestProposalsAllDelegates verifies that ProposalsAll delegates to the
// wallet's PoliteiaVoter.
func TestProposalsAllDelegates(t *testing.T) {
	want := []*pi.Proposal{{Name: "test-proposal"}}
	pv := &tPoliteiaVoter{
		proposalsAll: func(offset, rowsCount int, searchPhrase string, filterByVoteStatus ...int) ([]*pi.Proposal, int, error) {
			if offset != 5 || rowsCount != 20 || searchPhrase != "decred" {
				return nil, 0, fmt.Errorf("unexpected args: %d, %d, %q", offset, rowsCount, searchPhrase)
			}
			return want, 1, nil
		},
	}
	c := newTPoliteiaCore(pv)
	got, count, err := c.ProposalsAll(5, 20, "decred")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("count: got %d, want 1", count)
	}
	if len(got) != 1 || got[0].Name != "test-proposal" {
		t.Errorf("proposals mismatch: got %+v", got)
	}
}

// TestProposalNoWallet verifies that Proposal returns an error
// when no DCR wallet is loaded.
func TestProposalNoWallet(t *testing.T) {
	c := &Core{}
	c.wallets = make(map[uint32]*xcWallet)
	_, err := c.Proposal(42, "sometoken")
	if err == nil {
		t.Fatal("expected error when no wallet")
	}
}

// TestProposalDelegates verifies that Proposal delegates to the wallet's
// PoliteiaVoter.
func TestProposalDelegates(t *testing.T) {
	want := &pi.Proposal{Name: "my-proposal"}
	pv := &tPoliteiaVoter{
		proposal: func(token string) (*pi.Proposal, error) {
			if token != "abc123" {
				return nil, fmt.Errorf("unexpected token: %s", token)
			}
			return want, nil
		},
	}
	c := newTPoliteiaCore(pv)
	got, err := c.Proposal(42, "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "my-proposal" {
		t.Errorf("proposal name: got %q, want %q", got.Name, "my-proposal")
	}
}

// TestProposalsInProgressNoWallet verifies that ProposalsInProgress returns
// nil, nil (non-error) when no DCR wallet is loaded, since this is used on the
// wallet staking status page and should not block the UI.
func TestProposalsInProgressNoWallet(t *testing.T) {
	c := &Core{}
	c.wallets = make(map[uint32]*xcWallet)
	proposals, err := c.ProposalsInProgress()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposals != nil {
		t.Fatal("expected nil proposals when no wallet")
	}
}

// TestProposalsInProgressDelegates verifies that ProposalsInProgress delegates
// to the wallet's PoliteiaVoter.
func TestProposalsInProgressDelegates(t *testing.T) {
	want := []*pi.MiniProposal{{Name: "in-progress"}}
	pv := &tPoliteiaVoter{
		proposalsInProg: func() ([]*pi.MiniProposal, error) {
			return want, nil
		},
	}
	c := newTPoliteiaCore(pv)
	got, err := c.ProposalsInProgress()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "in-progress" {
		t.Errorf("proposals mismatch: got %+v", got)
	}
}

// TestCastVoteNoWallet verifies that CastVote returns an error
// when no DCR wallet is loaded.
func TestCastVoteNoWallet(t *testing.T) {
	c := &Core{}
	c.wallets = make(map[uint32]*xcWallet)
	err := c.CastVote(42, []byte("pw"), "sometoken", "yes")
	if err == nil {
		t.Fatal("expected error when no wallet")
	}
}
