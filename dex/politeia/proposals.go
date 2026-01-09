// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	commentsv1 "github.com/decred/politeia/politeiawww/api/comments/v1"
	recordsv1 "github.com/decred/politeia/politeiawww/api/records/v1"
	ticketvotev1 "github.com/decred/politeia/politeiawww/api/ticketvote/v1"
	piclient "github.com/decred/politeia/politeiawww/client"
)

var (
	// errDef defines the default error returned if the proposals db was not
	// initialized correctly.
	errDef = fmt.Errorf("ProposalDB was not initialized correctly")

	// dbVersion is the current required version of the proposals.db.
	dbVersion = dex.NewSemver(1, 0, 0)
)

// dbinfo defines the property that holds the db version.
const dbinfo = "_proposals.db_"

// proposalsDB defines the object that interacts with the local proposals
// db, and with decred's politeia server.
type proposalsDB struct {
	ctx      context.Context
	lastSync int64  // atomic
	updating uint32 // atomic
	dbP      *storm.DB
	client   *piclient.Client
	log      dex.Logger
}

// newProposalsDB opens an existing database or creates a new a storm DB
// instance with the provided path. It also sets up a new politeia http
// client and returns them on a proposals DB instance.
func newProposalsDB(ctx context.Context, dbPath string, log dex.Logger, client *piclient.Client) (*proposalsDB, error) {
	// Validate arguments
	if dbPath == "" {
		return nil, errors.New("missing db path")
	}
	if client == nil {
		return nil, errors.New("missing Politeia Client")
	}

	// Check path and open storm DB
	_, err := os.Stat(dbPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	db, err := storm.Open(dbPath)
	if err != nil {
		return nil, err
	}

	// Checks if the correct db version has been set.
	var version string
	err = db.Get(dbinfo, "version", &version)
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return nil, err
	}

	if version != dbVersion.String() {
		// Attempt to delete the ProposalRecord bucket.
		if err = db.Drop(&Proposal{}); err != nil {
			// If error due bucket not found was returned, ignore it.
			if !strings.Contains(err.Error(), "not found") {
				return nil, fmt.Errorf("delete bucket struct failed: %w", err)
			}
		}

		// Set the required db version.
		err = db.Set(dbinfo, "version", dbVersion.String())
		if err != nil {
			return nil, err
		}
		log.Infof("proposals.db version %v was set", dbVersion)
	}

	proposalDB := &proposalsDB{
		ctx:    ctx,
		dbP:    db,
		client: client,
		log:    log,
	}

	return proposalDB, nil
}

// Close closes the proposal DB instance.
func (db *proposalsDB) Close() error {
	if db == nil || db.dbP == nil {
		return nil
	}

	return db.dbP.Close()
}

// ProposalsLastSync reads the last sync timestamp from the atomic db.
func (db *proposalsDB) ProposalsLastSync() int64 {
	return atomic.LoadInt64(&db.lastSync)
}

// ProposalsSync is responsible for keeping an up-to-date database synced
// with politeia's latest updates.
func (db *proposalsDB) ProposalsSync() error {
	// Sanity check
	if db == nil || db.dbP == nil {
		return errDef
	}

	if !atomic.CompareAndSwapUint32(&db.updating, 0, 1) {
		db.log.Debug("ProposalsSync: proposals update already in progress.")
		return nil
	}
	defer atomic.StoreUint32(&db.updating, 0)

	// Save the timestamp of the last update check.
	defer atomic.StoreInt64(&db.lastSync, time.Now().UTC().Unix())

	// Update db with any new proposals on politeia server.
	err := db.proposalsNewUpdate()
	if err != nil {
		return err
	}

	// Update all current proposals who might still be suffering changes
	// with edits, and that has undergone some data change.
	err = db.proposalsInProgressUpdate()
	if err != nil {
		return err
	}

	db.log.Info("Politeia records were synced.")

	return nil
}

// ProposalsAll fetches the proposals data from the local db.
// The argument filterByVoteStatus is optional.
func (db *proposalsDB) ProposalsAll(offset, rowsCount int, searchPhrase string,
	filterByVoteStatus ...int) ([]*Proposal, int, error) {
	// Sanity check
	if db == nil || db.dbP == nil {
		return nil, 0, errDef
	}

	searchPhrase = strings.TrimSpace(strings.ToLower(searchPhrase))

	matchers := []q.Matcher{q.True()}

	if len(filterByVoteStatus) > 0 {
		matchers = append(
			matchers,
			q.Eq(
				"VoteStatus",
				ticketvotev1.VoteStatusT(filterByVoteStatus[0]),
			),
		)
	}

	if searchPhrase != "" {
		matchers = append(
			matchers,
			q.Re("Name", "(?i)"+regexp.QuoteMeta(searchPhrase)),
		)
	}

	query := db.dbP.Select(matchers...)

	// Count the proposals based on the query created above.
	totalCount, err := query.Count(&Proposal{})
	if err != nil {
		return nil, 0, err
	}

	// Return the proposals listing starting with the newest.
	var proposals []*Proposal
	err = query.Skip(offset).Limit(rowsCount).Reverse().OrderBy("Timestamp").
		Find(&proposals)
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return nil, 0, err
	}

	return proposals, totalCount, nil
}

// ProposalByToken retrieves the proposal for the given token argument.
func (db *proposalsDB) ProposalByToken(token string) (*Proposal, error) {
	if db == nil || db.dbP == nil {
		return nil, errDef
	}

	var proposal Proposal
	err := db.dbP.Select(q.Eq("Token", token)).Limit(1).First(&proposal)
	if err != nil {
		return nil, err
	}

	return &proposal, nil
}

// fetchProposalsData returns the parsed vetted proposals from politeia
// API's. It cooks up the data needed to save the proposals in stormdb. It
// first fetches the proposal details, then comments and then vote summary.
// This data is needed for the information provided in the Bison Wallet UI. The
// data returned does not include ticket vote data.
func (db *proposalsDB) fetchProposalsData(tokens []string) ([]*Proposal, error) {
	// Fetch record details for each token from the inventory.
	recordDetails, err := db.fetchRecordDetails(tokens)
	if err != nil {
		return nil, err
	}

	// Fetch comments count for each token from the inventory.
	commentsCounts, err := db.fetchCommentsCounts(tokens)
	if err != nil {
		return nil, err
	}

	// Fetch vote summary for each token from the inventory.
	voteSummaries, err := db.fetchTicketVoteSummaries(tokens)
	if err != nil {
		return nil, err
	}

	// Iterate through every record and feed data used by Bison Wallet.
	proposals := make([]*Proposal, 0, len(recordDetails))
	for _, record := range recordDetails {
		if err := db.ctx.Err(); err != nil {
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
			db.log.Errorf("Comments count for proposal %v not returned by API", proposal.Token)
			continue
		}
		proposal.CommentsCount = int32(commentsCount)

		// Vote summary data
		summary, ok := voteSummaries[proposal.Token]
		if !ok {
			db.log.Errorf("Vote summary for proposal %v not returned by API", proposal.Token)
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
func (db *proposalsDB) fetchVettedTokensInventory() ([]string, error) {
	page := uint32(1)
	var vettedTokens []string
	for {
		if err := db.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		inventoryReq := recordsv1.InventoryOrdered{
			State: recordsv1.RecordStateVetted,
			Page:  page,
		}
		reply, err := db.client.RecordInventoryOrdered(inventoryReq)
		if err != nil {
			return nil, fmt.Errorf("pi client RecordInventoryOrdered err: %w", err)
		}

		vettedTokens = append(vettedTokens, reply.Tokens...)

		if len(reply.Tokens) < int(recordsv1.InventoryPageSize) {
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
func (db *proposalsDB) fetchRecordDetails(tokens []string) (map[string]*recordsv1.Record, error) {
	records := make(map[string]*recordsv1.Record, len(tokens))
	for _, token := range tokens {
		if err := db.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		detailsReq := recordsv1.Details{
			Token: token,
		}
		dr, err := db.client.RecordDetails(detailsReq)
		if err != nil {
			return nil, fmt.Errorf("pi client RecordDetails err: %w", err)
		}
		records[token] = dr
	}

	return records, nil
}

// fetchCommentsCounts fetches the comments counts for the given proposal tokens.
func (db *proposalsDB) fetchCommentsCounts(tokens []string) (map[string]uint32, error) {
	commentsCounts := make(map[string]uint32, len(tokens))
	paginatedTokens := paginateTokens(tokens, commentsv1.CountPageSize)
	for i := range paginatedTokens {
		if err := db.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		cr, err := db.client.CommentCount(commentsv1.Count{
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
func (db *proposalsDB) fetchTicketVoteSummaries(tokens []string) (map[string]ticketvotev1.Summary, error) {
	voteSummaries := make(map[string]ticketvotev1.Summary, len(tokens))
	paginatedTokens := paginateTokens(tokens, ticketvotev1.SummariesPageSize)
	for i := range paginatedTokens {
		if err := db.ctx.Err(); err != nil {
			return nil, err // context canceled or timed out
		}

		sr, err := db.client.TicketVoteSummaries(ticketvotev1.Summaries{
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

// proposalsSave saves the batch proposals data to the db. This is ran when the
// proposals sync function finds new proposals that don't exist on our db yet.
// Before saving a proposal to the db, set the synced property to false to
// indicate that the proposal is not fully synced with politeia yet.
func (db *proposalsDB) proposalsSave(proposals []*Proposal) error {
	for _, proposal := range proposals {
		if err := db.ctx.Err(); err != nil {
			return err // context canceled or timed out
		}

		proposal.Synced = false
		err := db.dbP.Save(proposal)
		if err != nil {
			if errors.Is(err, storm.ErrAlreadyExists) {
				// Proposal exists, update instead of inserting new.
				data, err := db.ProposalByToken(proposal.Token)
				if err != nil {
					return fmt.Errorf("ProposalsDB ProposalByToken err: %w", err)
				}
				updateData := *proposal
				updateData.ID = data.ID
				err = db.dbP.Update(&updateData)
				if err != nil {
					return fmt.Errorf("stormdb update err: %w", err)
				}
			} else {
				return fmt.Errorf("stormdb save err: %w", err)
			}
		}
	}

	return nil
}

// proposalsNewUpdate verifies if there is any new proposals on the politeia
// server that are not yet synced with our stormdb.
func (db *proposalsDB) proposalsNewUpdate() error {
	db.log.Infof("Loading all proposal records from DB...")
	var proposals []*Proposal
	err := db.dbP.All(&proposals)
	if err != nil {
		return fmt.Errorf("stormdb All err: %w", err)
	}
	db.log.Infof("Loaded %d proposal records from DB...", len(proposals))

	// Create proposals map from local stormdb proposals.
	proposalsMap := make(map[string]struct{}, len(proposals))
	for _, prop := range proposals {
		proposalsMap[prop.Token] = struct{}{}
	}

	// Empty db so first time fetching proposals, fetch all vetted tokens.
	db.log.Infof("Fetching all proposal tokens...")
	tokens, err := db.fetchVettedTokensInventory()
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
		db.log.Infof("No new proposals found.")
		return nil
	}

	// Fetch data for found tokens.
	db.log.Infof("Fetching data for %d new proposals...", len(newTokens))
	prs, err := db.fetchProposalsData(newTokens)
	if err != nil {
		return err
	}
	db.log.Infof("Obtained data for %d new proposals.", len(prs))

	// Save proposals data in the db.
	return db.proposalsSave(prs)
}

// proposalsInProgressUpdate retrieves proposals with the vote status equal to
// unauthorized, authorized and started. Afterwords, it proceeds to check with
// newly fetched data if any of them need to be updated on stormdb.
func (db *proposalsDB) proposalsInProgressUpdate() error {
	var propsInProgress []*Proposal
	err := db.dbP.Select(
		q.Or(
			q.Eq("VoteStatus", ticketvotev1.VoteStatusUnauthorized),
			q.Eq("VoteStatus", ticketvotev1.VoteStatusAuthorized),
			q.Eq("VoteStatus", ticketvotev1.VoteStatusStarted),
		),
	).Find(&propsInProgress)
	if err != nil && !errors.Is(err, storm.ErrNotFound) {
		return err
	}

	db.log.Infof("Fetching data for %d in-progress proposals...", len(propsInProgress))
	for _, prop := range propsInProgress {
		if err := db.ctx.Err(); err != nil {
			return err // context canceled or timed out
		}

		// Fetch fresh data for the proposal.
		proposals, err := db.fetchProposalsData([]string{prop.Token})
		if err != nil {
			return fmt.Errorf("fetchProposalsData failed with err: %w", err)
		}
		proposal := proposals[0]

		if prop.IsEqual(*proposal) {
			// No changes made to proposal, skip db call.
			continue
		}

		// Insert ID from storm DB to update proposal.
		proposal.ID = prop.ID

		err = db.dbP.Update(proposal)
		if err != nil {
			return fmt.Errorf("storm db Update failed with err: %w", err)
		}
	}

	return nil
}
