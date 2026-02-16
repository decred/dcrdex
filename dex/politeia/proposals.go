// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/lexi"
	cv1 "github.com/decred/politeia/politeiawww/api/comments/v1"
	rv1 "github.com/decred/politeia/politeiawww/api/records/v1"
	tv1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
	"github.com/dgraph-io/badger/v4"
)

var (
	proposalPrefix               = "proposal"
	proposalStatusIndexPrefix    = "proposal:status"
	proposalTimestampIndexPrefix = "proposal:ts"
	proposalsCountKey            = []byte("proposalsCount")
	inProgressStatuses           = []string{
		VotesStatuses[tv1.VoteStatusUnauthorized],
		VotesStatuses[tv1.VoteStatusAuthorized],
		VotesStatuses[tv1.VoteStatusStarted],
	}

	// Tables
	proposalsTable     = "proposals"
	proposalsMetaTable = "proposals_meta"

	// Indexes
	proposalStatusIndex    = "proposalStatus"
	proposalTimestampIndex = "proposalTimestamp"
)

// ProposalsLastSync reads the last sync timestamp from the atomic p.lastSync.
func (p *Politeia) ProposalsLastSync() int64 {
	return p.lastSync.Load()
}

// ProposalsSync is responsible for keeping an up-to-date database synced
// with politeia's latest updates.
func (p *Politeia) ProposalsSync() error {
	if !p.updating.CompareAndSwap(false, true) {
		p.log.Debug("ProposalsSync: proposals update already in progress.")
		return nil
	}
	defer p.updating.Store(false)

	// Save the timestamp of the last update check.
	defer p.lastSync.Store(time.Now().UTC().Unix())

	// Update db with any new proposals on politeia server.
	err := p.proposalsNewUpdate()
	if err != nil {
		return err
	}

	// Update all current proposals that might still be suffering changes
	// with edits, and that has undergone some data change.
	err = p.proposalsInProgressUpdate()
	if err != nil {
		return err
	}

	p.log.Info("Politeia records were synced.")

	return nil
}

// ProposalsAll fetches the proposals data from the local db.
// The argument filterByVoteStatus is optional.
func (p *Politeia) ProposalsAll(offset, rowsCount int, searchPhrase string,
	filterByVoteStatus ...int) ([]*Proposal, int, error) {
	searchPhrase = strings.TrimSpace(strings.ToLower(searchPhrase))

	var proposals []*Proposal

	var voteStatus string
	if len(filterByVoteStatus) > 0 {
		voteStatus = VotesStatuses[tv1.VoteStatusT(filterByVoteStatus[0])]
	}

	var count int
	var collected int
	err := iterateIndex(p.db, -1, voteStatus, func(_, val []byte) error {
		proposal := new(Proposal)
		err := proposal.UnmarshalBinary(val)
		if err != nil {
			return fmt.Errorf("error unmarshaling proposal: %w", err)
		}

		if searchPhrase != "" && !strings.Contains(strings.ToLower(proposal.Name), searchPhrase) {
			return nil // Skip this proposal, it doesn't match the search phrase.
		}

		count++

		if count <= offset {
			return nil // Skip this proposal, it doesn't match the search phrase.
		}

		if collected < rowsCount {
			proposals = append(proposals, proposal)
			collected++
		}

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("error fetching proposals: %w", err)
	}

	if searchPhrase != "" || voteStatus != "" {
		// If there is a search phrase or vote status filter, we need to return the count of the filtered proposals, which is the count variable we have been incrementing on the loop above.
		return proposals, count, nil
	}

	var totalCount int
	err = p.proposalMeta.View(func(txn *badger.Txn) error {
		countB, err := p.proposalMeta.GetRaw(proposalsCountKey, lexi.WithGetTxn(txn))
		if err != nil {
			// Fallback to the count of fetched proposals if the total count is not found on the db.
			totalCount = len(proposals)

			if !errors.Is(err, lexi.ErrKeyNotFound) {
				// Log the error if it's different from key not found, but don't return it because we can still return the fetched proposals and their count as a fallback.
				p.log.Errorf("error fetching proposals count from db: %v", err)
			}
		} else {
			totalCount = int(encode.BytesToUint64(countB))
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	return proposals, totalCount, nil
}

// ProposalByToken retrieves the proposal for the given token argument.
func (p *Politeia) ProposalByToken(token string) (*Proposal, error) {
	proposal := new(Proposal)
	err := p.proposals.View(func(txn *badger.Txn) error {
		key := proposalKey(token)
		return p.proposals.Get(key, proposal, lexi.WithGetTxn(txn))
	})
	if err != nil {
		if errors.Is(err, lexi.ErrKeyNotFound) {
			return nil, fmt.Errorf("proposal with token %v not found", token)
		}
		return nil, fmt.Errorf("error fetching proposal with token %v: %w", token, err)
	}

	return proposal, nil
}

// fetchProposalsData returns the parsed vetted proposals from politeia's
// API. It cooks up the data needed to save the proposals in the db. It
// first fetches the proposal details, then comments and then vote summary.
// This data is needed for the information provided in the Bison Wallet UI. The
// data returned does not include ticket vote data.
func (p *Politeia) fetchProposalsData(tokens []string) ([]*Proposal, error) {
	// Fetch record details for each token from the inventory.
	recordDetails, err := p.fetchRecordDetails(tokens)
	if err != nil {
		return nil, err
	}

	// Fetch comments count for each token from the inventory.
	commentsCounts, err := p.fetchCommentsCounts(tokens)
	if err != nil {
		return nil, err
	}

	// Fetch vote summary for each token from the inventory.
	voteSummaries, err := p.fetchTicketVoteSummaries(tokens)
	if err != nil {
		return nil, err
	}

	// Iterate through every record and feed data used by Bison Wallet.
	proposals := make([]*Proposal, 0, len(recordDetails))
	for _, record := range recordDetails {
		if err := p.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		// Record data
		proposal := &Proposal{
			State:     record.State,
			Status:    record.Status,
			Version:   record.Version,
			Timestamp: uint64(record.Timestamp),
			Username:  record.Username,
			Token:     record.CensorshipRecord.Token,
		}

		// Proposal metadata
		pm, proposalDesc, err := proposalMetadataDecode(record.Files)
		if err != nil {
			return nil, fmt.Errorf("proposalMetadataDecode err: %w", err)
		}
		proposal.Name = pm.Name
		proposal.Description = proposalDesc

		// User metadata
		um, err := userMetadataDecode(record.Metadata)
		if err != nil {
			return nil, fmt.Errorf("userMetadataDecode err: %w", err)
		}
		proposal.UserID = um.UserID

		// Comments count
		commentsCount, ok := commentsCounts[proposal.Token]
		if !ok {
			p.log.Errorf("Comments count for proposal %v not returned by API", proposal.Token)
			continue
		}
		proposal.CommentsCount = int32(commentsCount)

		// Vote summary data
		summary, ok := voteSummaries[proposal.Token]
		if !ok {
			p.log.Errorf("Vote summary for proposal %v not returned by API", proposal.Token)
			continue
		}
		proposal.VoteStatus = summary.Status
		proposal.VoteResults = summary.Results
		proposal.EligibleTickets = summary.EligibleTickets
		proposal.StartBlockHeight = summary.StartBlockHeight
		proposal.EndBlockHeight = summary.EndBlockHeight
		proposal.QuorumPercentage = summary.QuorumPercentage
		proposal.PassPercentage = summary.PassPercentage

		var totalVotes uint64
		for _, v := range summary.Results {
			totalVotes += v.Votes
		}
		proposal.TotalVotes = totalVotes

		// Status change metadata
		statusTimestamps, changeMsg, err := statusChangeMetadataDecode(record.Metadata)
		if err != nil {
			return nil, fmt.Errorf("statusChangeMetadataDecode err: %w", err)
		}
		proposal.PublishedAt = uint64(statusTimestamps.published)
		proposal.CensoredAt = uint64(statusTimestamps.censored)
		proposal.AbandonedAt = uint64(statusTimestamps.abandoned)
		proposal.StatusChangeMsg = changeMsg

		// Append proposal after inserting the relevant data
		proposals = append(proposals, proposal)
	}

	return proposals, nil
}

// fetchVettedTokens fetches all vetted tokens ordered by the timestamp of
// their last status change.
func (p *Politeia) fetchVettedTokensInventory() ([]string, error) {
	page := uint32(1)
	var vettedTokens []string
	for {
		if err := p.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		inventoryReq := rv1.InventoryOrdered{
			State: rv1.RecordStateVetted,
			Page:  page,
		}
		reply, err := p.client.RecordInventoryOrdered(inventoryReq)
		if err != nil {
			return nil, fmt.Errorf("pi client RecordInventoryOrdered err: %w", err)
		}

		vettedTokens = append(vettedTokens, reply.Tokens...)

		if len(reply.Tokens) < int(rv1.InventoryPageSize) {
			// Break loop if we fetch last page. An empty token slice is
			// returned if we request an non-existent/empty page, so in the
			// case of the last page size being equal to the limit page size,
			// we'll fetch an empty page afterwords and know the last page was
			// fetched.
			break
		}

		page++
	}

	return vettedTokens, nil
}

// fetchRecordDetails fetches the record details of the given proposal tokens.
func (p *Politeia) fetchRecordDetails(tokens []string) (map[string]*rv1.Record, error) {
	records := make(map[string]*rv1.Record, len(tokens))
	for _, token := range tokens {
		if err := p.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		detailsReq := rv1.Details{
			Token: token,
		}
		dr, err := p.client.RecordDetails(detailsReq)
		if err != nil {
			return nil, fmt.Errorf("pi client RecordDetails err: %w", err)
		}
		records[token] = dr
	}

	return records, nil
}

// fetchCommentsCounts fetches the comments counts for the given proposal tokens.
func (p *Politeia) fetchCommentsCounts(tokens []string) (map[string]uint32, error) {
	commentsCounts := make(map[string]uint32, len(tokens))
	paginatedTokens := paginateTokens(tokens, cv1.CountPageSize)
	for i := range paginatedTokens {
		if err := p.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		cr, err := p.client.CommentCount(cv1.Count{
			Tokens: paginatedTokens[i],
		})
		if err != nil {
			return nil, fmt.Errorf("pi client CommentCount err: %w", err)
		}
		for token, count := range cr.Counts {
			commentsCounts[token] = count
		}
	}
	return commentsCounts, nil
}

// fetchTicketVoteSummaries fetches the vote summaries for the given proposal tokens.
func (p *Politeia) fetchTicketVoteSummaries(tokens []string) (map[string]tv1.Summary, error) {
	voteSummaries := make(map[string]tv1.Summary, len(tokens))
	paginatedTokens := paginateTokens(tokens, tv1.SummariesPageSize)
	for i := range paginatedTokens {
		if err := p.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		sr, err := p.client.TicketVoteSummaries(tv1.Summaries{
			Tokens: paginatedTokens[i],
		})
		if err != nil {
			return nil, fmt.Errorf("pi client TicketVoteSummaries err: %w", err)
		}
		for token := range sr.Summaries {
			voteSummaries[token] = sr.Summaries[token]
		}
	}
	return voteSummaries, nil
}

// paginateTokens paginates tokens in a matrix according to the provided
// page size.
func paginateTokens(tokens []string, pageSize uint32) [][]string {
	n := len(tokens) / int(pageSize) // number of pages needed
	if len(tokens)%int(pageSize) != 0 {
		n++
	}
	ts := make([][]string, n)
	page := 0
	for i := 0; i < len(tokens); i++ {
		if len(ts[page]) >= int(pageSize) {
			page++
		}
		ts[page] = append(ts[page], tokens[i])
	}
	return ts
}

// saveProposals saves the batch proposals data to the db. This is ran when the
// proposals sync function finds new proposals that don't exist on our db yet.
func (p *Politeia) saveProposals(proposals []*Proposal) error {
	return p.proposals.Update(func(txn *badger.Txn) error {
		for _, proposal := range proposals {
			proposal.Synced = false

			key := proposalKey(proposal.Token)
			err := p.proposals.Set(key, proposal, lexi.WithReplace(), lexi.WithTxn(txn))
			if err != nil {
				return fmt.Errorf("error saving proposal with token %v: %w", proposal.Token, err)
			}
		}

		return nil
	})
}

// proposalsNewUpdate verifies if there is any new proposals on the politeia
// server that are not yet synced with our db.
func (p *Politeia) proposalsNewUpdate() error {
	p.log.Debug("Loading all proposal records from db...")
	proposals, err := p.fetchAllProposals()
	if err != nil {
		return fmt.Errorf("p.fetchAllProposals err: %w", err)
	}

	nProposals := len(proposals)
	p.log.Infof("Loaded %d proposal records from db...", nProposals)

	// Create proposals map from local db proposals.
	proposalsMap := make(map[string]struct{})
	for _, prop := range proposals {
		proposalsMap[prop.Token] = struct{}{}
	}

	// Empty db so first time fetching proposals, fetch all vetted tokens.
	p.log.Debug("Fetching all proposal tokens...")
	tokens, err := p.fetchVettedTokensInventory()
	if err != nil {
		return err
	}

	// Filter new proposals to be fetched.
	var newTokens []string
	for _, token := range tokens {
		if _, ok := proposalsMap[token]; ok {
			continue
		}
		// New proposal found.
		newTokens = append(newTokens, token)
	}

	if len(newTokens) == 0 {
		p.log.Debug("No new proposals found.")
		return nil
	}

	// Fetch data for found tokens.
	p.log.Infof("Fetching data for %d new proposals...", len(newTokens))
	prs, err := p.fetchProposalsData(newTokens)
	if err != nil {
		return err
	}
	p.log.Debugf("Obtained data for %d new proposals.", len(prs))

	// Save proposals data in the db.
	err = p.saveProposals(prs)
	if err != nil {
		return fmt.Errorf("p.saveProposals err: %w", err)
	}

	// Update db with current total proposals count.
	count := uint64(nProposals)
	if count == 0 {
		count = uint64(len(prs))
	} else if count > 0 && len(newTokens) > 0 {
		count += uint64(len(prs))
	}

	err = p.proposalMeta.Set(proposalsCountKey, encode.Uint64Bytes(count), lexi.WithReplace())
	if err != nil {
		return fmt.Errorf("error updating proposals count: %w", err)
	}

	return nil
}

// ProposalsInProgress returns the mini proposals for the proposals that are currently in
// progress, meaning their vote status is unauthorized, authorized or started.
// This is used to display the in progress proposals on the Bison Wallet UI.
func (p *Politeia) ProposalsInProgress() ([]*MiniProposal, error) {
	var propsInProgress []*MiniProposal
	for _, status := range inProgressStatuses {
		err := iterateIndex(p.db, -1, status, func(_, val []byte) error {
			proposal := new(Proposal)
			err := proposal.UnmarshalBinary(val)
			if err != nil {
				return fmt.Errorf("error unmarshaling proposal: %w", err)
			}

			propsInProgress = append(propsInProgress, proposal.ToMiniProposal())

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("error fetching in-progress proposals: %w", err)
		}
	}
	return propsInProgress, nil
}

// proposalsInProgressUpdate retrieves proposals with the vote status equal to
// unauthorized, authorized and started. Afterwords, it proceeds to check with
// newly fetched data if any of them need to be updated in the db.
func (p *Politeia) proposalsInProgressUpdate() error {
	var propsInProgress []*Proposal
	for _, status := range inProgressStatuses {
		err := iterateIndex(p.db, -1, status, func(_, val []byte) error {
			proposal := new(Proposal)
			err := proposal.UnmarshalBinary(val)
			if err != nil {
				return fmt.Errorf("error unmarshaling proposal: %w", err)
			}

			propsInProgress = append(propsInProgress, proposal)

			return nil
		})
		if err != nil {
			return fmt.Errorf("error fetching in-progress proposals: %w", err)
		}
	}

	p.log.Infof("Fetching data for %d in-progress proposals...", len(propsInProgress))

	for _, prop := range propsInProgress {
		if err := p.ctx.Err(); err != nil {
			return err // context canceled or timed out
		}

		// Fetch fresh data for the proposal.
		proposals, err := p.fetchProposalsData([]string{prop.Token})
		if err != nil {
			return fmt.Errorf("fetchProposalsData failed with err: %w", err)
		}
		proposal := proposals[0]

		if prop.IsEqual(*proposal) {
			continue // No changes made to proposal, skip db call.
		}

		// Insert ID from DB to update proposal.
		proposal.ID = prop.ID

		key := proposalKey(proposal.Token)
		err = p.proposals.Set(key, proposal, lexi.WithReplace())
		if err != nil {
			return fmt.Errorf("error updating proposal with token %v: %w", proposal.Token, err)
		}
	}

	return nil
}

func (p *Politeia) fetchAllProposals() ([]*Proposal, error) {
	var proposals []*Proposal
	err := iterateTable(p.db, proposalsTable, -1, func(val []byte) error {
		proposal := new(Proposal)
		err := proposal.UnmarshalBinary(val)
		if err != nil {
			return fmt.Errorf("error unmarshaling proposal: %w", err)
		}

		proposals = append(proposals, proposal)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error fetching proposals: %w", err)
	}

	return proposals, err
}

// proposalKey maps a proposal token to a key used to save the proposal in the db.
// The key is the proposal token prefixed with proposalPrefix to avoid key
// collisions with other data saved in the db.
func proposalKey(proposalToken string) []byte {
	return []byte(fmt.Sprintf("%s:%s", proposalPrefix, proposalToken))
}

// proposalsStatusIndex creates the key for the proposal status index. The key is
// the proposal token prefixed with proposalStatusIndexPrefix and the proposal
// vote status to allow querying proposals by their vote status.
func proposalsStatusIndex(p *Proposal) []byte {
	reversed := math.MaxInt64 - p.Timestamp
	return []byte(fmt.Sprintf(
		"%s:%s:%020d:%s",
		proposalStatusIndexPrefix,
		VotesStatuses[p.VoteStatus],
		reversed,
		p.Token,
	))
}

// proposalsTimestampIndex creates the key for the proposal timestamp index. The key is
// the proposal token prefixed with proposalTimestampIndexPrefix and the proposal
// timestamp to allow querying proposals by their timestamp.
func proposalsTimestampIndex(p *Proposal) []byte {
	reversed := math.MaxInt64 - p.Timestamp
	return []byte(fmt.Sprintf(
		"%s:%020d:%s",
		proposalTimestampIndexPrefix,
		reversed,
		p.Token,
	))
}

// iterateTable iterates all entries in a table and returns the entries.
func iterateTable(db *lexi.DB, tableName string, limit int, f func(val []byte) error) error {
	table, err := db.Table(tableName)
	if err != nil {
		return err
	}

	count := 0
	err = table.Iterate(nil, func(it *lexi.Iter) error {
		var v []byte
		if err := it.V(func(vB []byte) error {
			v = lexi.CloneBytes(vB)
			return nil
		}); err != nil {
			return err
		}

		err = f(v)
		if err != nil {
			return err
		}

		count++

		if limit > 0 && count >= limit {
			return lexi.ErrEndIteration
		}
		return nil
	})

	return nil
}

// iterateIndex iterates all entries in an index and returns the entries
// in index order.
func iterateIndex(db *lexi.DB, limit int, voteStatus string, f func(k, val []byte) error) error {
	table, err := db.Table(proposalsTable)
	if err != nil {
		return err
	}

	indexName := proposalTimestampIndex
	if voteStatus != "" {
		indexName = proposalStatusIndex
	}

	indexPrefix, err := lexi.PrefixForName(db.DB, proposalsTable+lexi.IndexNameSeparator+indexName)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return fmt.Errorf("index %q not found on table %q", indexName, proposalsTable)
	}
	if err != nil {
		return err
	}

	var count int
	return db.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = indexPrefix[:]
		it := txn.NewIterator(opts)
		defer it.Close()

		var voteSearchKey []byte
		if voteStatus != "" {
			voteSearchKey = lexi.PrefixedKey(indexPrefix, []byte(fmt.Sprintf("%s:%s", proposalStatusIndexPrefix, voteStatus)))
			it.Seek(voteSearchKey)
		} else {
			it.Rewind()
		}

		for ; it.Valid(); it.Next() {
			item := it.Item()
			idxEntryKey := item.KeyCopy(nil)
			if len(idxEntryKey) < lexi.PrefixSize+8 {
				// indexPrefix + (idxKey + dbid) must be at least 8 bytes beyond prefix
				continue
			}

			rest := idxEntryKey[lexi.PrefixSize:]
			if len(rest) < 8 {
				continue
			}

			dbIDB := rest[len(rest)-8:]
			idxKey := lexi.CloneBytes(rest[:len(rest)-8])
			if len(voteSearchKey) > 0 && !bytes.HasPrefix(idxEntryKey, voteSearchKey) {
				// We need to return early if we've exhausted all entries for the
				// given vote status. We can check this by verifying that the key
				// prefix is still the same as the one for the given vote status.
				break
			}

			// Resolve the original table key from DBID.
			keyItem, err := txn.Get(lexi.PrefixedKey(lexi.IdToKeyPrefix, dbIDB))
			if err != nil {
				return err
			}
			keyB, err := keyItem.ValueCopy(nil)
			if err != nil {
				return err
			}

			// Resolve the value via the table API (decodes the internal datum).
			vB, err := table.GetRaw(keyB, lexi.WithGetTxn(txn))
			if err != nil {
				return err
			}

			err = f(idxKey, lexi.CloneBytes(vB))
			if err != nil {
				return err
			}

			count++

			if limit > 0 && count >= limit {
				break
			}
		}
		return nil
	})
}
