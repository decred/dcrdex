//go:build !harness && !botlive

package core

import (
	"testing"
)

// TestPoliteiaDetailsNilPoliteia verifies that PoliteiaDetails returns safe
// defaults when c.politeia is nil (e.g., initialization failure).
func TestPoliteiaDetailsNilPoliteia(t *testing.T) {
	c := &Core{politeiaURL: "https://proposals.decred.org"}
	url, syncing, lastSync := c.PoliteiaDetails()
	if url != "https://proposals.decred.org" {
		t.Errorf("URL: got %q, want %q", url, "https://proposals.decred.org")
	}
	if syncing {
		t.Error("syncing should be false when politeia is nil")
	}
	if lastSync != 0 {
		t.Error("lastSync should be 0 when politeia is nil")
	}
}

// TestProposalsAllNilPoliteia verifies that ProposalsAll returns an error
// when c.politeia is nil.
func TestProposalsAllNilPoliteia(t *testing.T) {
	c := &Core{}
	_, _, err := c.ProposalsAll(0, 10, "")
	if err == nil {
		t.Fatal("expected error when politeia is nil")
	}
}

// TestProposalNilPoliteia verifies that Proposal returns an error
// when c.politeia is nil.
func TestProposalNilPoliteia(t *testing.T) {
	c := &Core{}
	_, err := c.Proposal(42, "sometoken")
	if err == nil {
		t.Fatal("expected error when politeia is nil")
	}
}

// TestProposalsInProgressNilPoliteia verifies that ProposalsInProgress returns
// nil, nil (non-error) when c.politeia is nil, since this is used on the
// wallet staking status page and should not block the UI.
func TestProposalsInProgressNilPoliteia(t *testing.T) {
	c := &Core{}
	proposals, err := c.ProposalsInProgress()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposals != nil {
		t.Fatal("expected nil proposals when politeia is nil")
	}
}

// TestCastVoteNilPoliteia verifies that CastVote returns an error
// when c.politeia is nil.
func TestCastVoteNilPoliteia(t *testing.T) {
	c := &Core{}
	err := c.CastVote(42, []byte("pw"), "sometoken", "yes")
	if err == nil {
		t.Fatal("expected error when politeia is nil")
	}
}
