// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build live

package pi

import (
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
)

const proposalLimit = 400

// TestPoliteia performs basic tests of the Politeia integration.
// Run with a longer timeout to allow for initial sync.
func TestPoliteia(t *testing.T) {
	log := dex.NewLogger("Politeia", dex.LevelDebug, os.Stdout)

	// Use a persistent db file for easier manual testing because the first run takes
	// a long time fetching all the required data for all the proposals.
	// You can manually delete it between test runs to start fresh.
	dbFile := "politeia_test.db"

	p, err := New(t.Context(), PoliteiaMainnetHost, dbFile, log)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer p.Close()

	go func() {
		p.ProposalsSync()
	}()

	// No need to wait for sync to complete to test fetching proposals.
	// If we get at least 5 proposals, we can proceed.
	var proposals []*Proposal
	var totalCount int
	for {
		proposals, totalCount, err = p.ProposalsAll(0, proposalLimit, "")
		if err != nil {
			t.Fatal(err.Error())
		}
		if len(proposals) > 5 {
			log.Infof("Fetched %d proposals (total count: %d)", len(proposals), totalCount)
			break
		}
		time.Sleep(30 * time.Second)
		log.Infof("Waiting for proposals to be available...")
	}

	for i, proposal := range proposals {
		if i >= 5 {
			break
		}

		p, err := p.ProposalByToken(proposal.Token)
		if err != nil {
			t.Errorf("ProposalByToken error: %v", err)
			continue
		}

		log.Infof("Fetched proposal %s - %s", p.Name, p.Token)
	}

	// TODO: add tests for actual voting functions.
	// p.CastVotes(t.Context(), nil, nil, "", "")
	// p.WalletProposalVoteDetails(t.Context(), &wallet.Wallet{}, "")

	// Close the db here. The sync goroutine may still be running but will exit once db is close.

	log.Infof("Politeia tests completed")
}
