//go:build live

package politeia

import (
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrdata/gov/v5/politeia/types"
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

	p, err := New(PoliteiaMainnetHost, dbFile, log)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer p.Close()

	go func() {
		err = p.ProposalsSync()
		if err != nil {
			log.Errorf("ProposalsSync error: %v", err)
		}
	}()

	// No need to wait for sync to complete to test fetching proposals.
	// If we get at least 5 proposals, we can proceed.
	var proposals []*types.ProposalRecord
	var totalCount int
	for {
		proposals, totalCount, err = p.ProposalsDB.ProposalsAll(0, proposalLimit)
		if err != nil {
			t.Fatal(err.Error())
		}
		if len(proposals) > 5 {
			log.Infof("Fetched %d proposals (total count: %d)", len(proposals), totalCount)
			break
		}
		time.Sleep(2 * time.Minute)
		log.Infof("Waiting for proposals to be available...")
	}

	for i, proposal := range proposals {
		if i >= 5 {
			break
		}
		desc, err := p.FetchProposalDescription(proposal.Token)
		if err != nil {
			log.Errorf("FetchProposalDescription error: %v", err)
			continue
		}

		log.Infof("Fetched description for proposal %s - %s...", proposal.Name, desc[:80])
	}

	// TODO: add tests for actual voting functions.
	// p.CastVotes(t.Context(), nil, nil, "", "")
	// p.WalletProposalVoteDetails(t.Context(), &wallet.Wallet{}, "")

	// Close the db here. The sync goroutine may still be running but will exit once db is close.
	p.Close()

	log.Infof("Politeia tests completed")
}
