// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// submitMarketStarted identifies orders to revoke during startup and submits
// a market_started event with the trading parameters and startup epoch.
func (m *Market) submitMarketStarted(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if m.mesh == nil {
		return 0, fmt.Errorf("market %s startup requires SetMeshService before Run", m.name)
	}

	params := m.startParams(time.Now())
	// Funding checks can take several epochs, so choose the startup epoch after
	// they finish. Repeat the checks if a suspension became due and changed
	// which trading parameters apply.
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		bookedRevokes, err := m.startupBookedRevokes(ctx, params.LotSize)
		if err != nil {
			return 0, err
		}
		epochRevokes, err := m.startupEpochRevokes(ctx)
		if err != nil {
			return 0, err
		}

		// Funding checks may cross the final epoch boundary. If startup now
		// needs the stored parameters, recompute revocations with their lot size.
		now := time.Now()
		if currentParams := m.startParams(now); currentParams != params {
			params = currentParams
			continue
		}
		currentEpochIdx := now.UnixMilli() / params.epochDur
		revocationTime := now.Truncate(time.Millisecond).UTC()
		startedEvent := meshevents.NewMarketStartedEvent(m.name, currentEpochIdx, params.epochDur,
			params.MarketRunParams, revocationTime, encodeStartupOrderRevokes(bookedRevokes), encodeStartupOrderRevokes(epochRevokes))
		event, err := mesh.NewEvent(startedEvent)
		if err != nil {
			return 0, err
		}
		if _, err := m.mesh.ApplyEvent(ctx, event); err != nil {
			return 0, err
		}
		// The epoch driver checks whether startup completed a suspension
		// before accepting orders in this epoch.
		return currentEpochIdx, nil
	}
}

// startParams returns the trading parameters to include in the market_started
// event. It returns the configured parameters unless startup is completing a
// suspension, in which case it returns the current run's parameters. Resume
// will apply the configured parameters and recheck which orders should be retained.
func (m *Market) startParams(now time.Time) marketRun {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	live := *m.liveParams.Load()
	if m.lifecycleState == db.MarketStateDraining ||
		(m.pendingLifecycleAction == db.MarketPendingSuspend && now.UnixMilli()/live.epochDur > m.pendingLifecycleEpochIdx) {
		return live
	}
	return m.configuredParams
}

func currentMarketEpochIdx(epochDur int64) int64 {
	return time.Now().UnixMilli() / epochDur
}

// startupEpochRevokes returns revocations for all stored epoch orders,
// which were left unfinished by the previous market run.
func (m *Market) startupEpochRevokes(ctx context.Context) ([]*db.StartupOrderRevoke, error) {
	ords, err := m.storage.EpochOrders(m.base, m.quote)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sortOrdersByID(ords)
	revokes := make([]*db.StartupOrderRevoke, 0, len(ords))
	for _, ord := range ords {
		revokes = append(revokes, &db.StartupOrderRevoke{
			Order:  ord,
			Reason: meshevents.StartupOrderRevokeReasonEpochAbandoned,
		})
	}
	return revokes, nil
}

// startupBookedRevokes selects booked orders with an incompatible lot size,
// spent funding coins, or insufficient account balances.
func (m *Market) startupBookedRevokes(ctx context.Context, lotSize uint64) ([]*db.StartupOrderRevoke, error) {
	bookOrders, err := m.storage.BookOrders(m.base, m.quote)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var revokes []*db.StartupOrderRevoke
	remaining := make([]*order.LimitOrder, 0, len(bookOrders))
	baseAccounts, quoteAccounts := make(map[string]bool), make(map[string]bool)
	for _, lo := range bookOrders {
		if book.IncompatibleLotSize(lo, lotSize) {
			revokes = append(revokes, &db.StartupOrderRevoke{Order: lo, Reason: meshevents.StartupOrderRevokeReasonLotSizeIncompatible})
			continue
		}
		// Partially filled orders may have spent their original funding coins
		// in swaps. Their account balances still need to be checked below.
		if lo.FillAmt == 0 {
			spent, err := m.bookedOrderFundingSpent(ctx, lo)
			if err != nil {
				return nil, err
			}
			if spent {
				revokes = append(revokes, &db.StartupOrderRevoke{Order: lo, Reason: meshevents.StartupOrderRevokeReasonFundingCoinSpent})
				continue
			}
		}
		remaining = append(remaining, lo)
		if m.coinLockerBase == nil {
			baseAccounts[lo.BaseAccount()] = true
		}
		if m.coinLockerQuote == nil {
			quoteAccounts[lo.QuoteAccount()] = true
		}
	}

	// Funds may have been spent since these orders were accepted.
	// Recheck account balances before starting or resuming trading.
	failedBase, err := m.failedStartupAccounts(ctx, baseAccounts, m.base)
	if err != nil {
		return nil, err
	}
	failedQuote, err := m.failedStartupAccounts(ctx, quoteAccounts, m.quote)
	if err != nil {
		return nil, err
	}
	for _, lo := range remaining {
		if failedBase[lo.BaseAccount()] || failedQuote[lo.QuoteAccount()] {
			revokes = append(revokes, &db.StartupOrderRevoke{Order: lo, Reason: meshevents.StartupOrderRevokeReasonAccountLowBalance})
		}
	}

	sortStartupOrderRevokes(revokes)
	return revokes, nil
}

func (m *Market) bookedOrderFundingSpent(ctx context.Context, lo *order.LimitOrder) (bool, error) {
	assetID := m.quote
	utxoFunded := m.coinLockerQuote != nil
	if lo.Sell {
		assetID = m.base
		utxoFunded = m.coinLockerBase != nil
	}
	if !utxoFunded {
		return false, nil
	}
	for _, coinID := range lo.Coins {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := m.swapper.CheckUnspent(callCtx, assetID, coinID)
		cancel()
		if err == nil {
			continue
		}
		if errors.Is(err, asset.CoinNotFoundError) {
			return true, nil
		}
		// TODO(mesh): on other RPC failures, we skip the check to avoid the entire node
		// going down during failover due a potentially temporary issue. The tradeoff is
		// allowing some orders to remain on the books until they either match or the user
		// tries to submit another order. Consider some alternative options.
		log.Errorf("Unexpected error checking coinID %v for order %v: %v", coinID, lo.ID(), err)
		return false, nil
	}
	return false, nil
}

// failedStartupAccounts returns the accounts whose balances cannot cover
// their existing orders and pending swaps.
func (m *Market) failedStartupAccounts(ctx context.Context, accounts map[string]bool, assetID uint32) (map[string]bool, error) {
	if len(accounts) == 0 {
		return nil, nil
	}
	if m.balancer == nil {
		return nil, fmt.Errorf("market %s startup cleanup requires account balancer", m.name)
	}
	failed := make(map[string]bool)
	for addr := range accounts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !m.balancer.CheckReserved(addr, assetID) {
			failed[addr] = true
		}
	}
	return failed, nil
}

func sortStartupOrderRevokes(revokes []*db.StartupOrderRevoke) {
	sort.Slice(revokes, func(i, j int) bool {
		idi := revokes[i].Order.ID()
		idj := revokes[j].Order.ID()
		return bytes.Compare(idi[:], idj[:]) < 0
	})
}

// encodeStartupOrderRevokes encodes the revoked orders and includes their
// revocation reasons in event records.
func encodeStartupOrderRevokes(revokes []*db.StartupOrderRevoke) []meshevents.StartupOrderRevokeRecord {
	records := make([]meshevents.StartupOrderRevokeRecord, 0, len(revokes))
	for _, revoke := range revokes {
		records = append(records, meshevents.NewStartupOrderRevokeRecord(revoke.Order, revoke.Reason))
	}
	return records
}

// applyMarketStartedMemory removes revoked orders, unlocks their funding
// coins, and resets the market's epoch queues. It returns the orders removed
// from the book.
func (m *Market) applyMarketStartedMemory(update *db.MarketStartedUpdate) (removed []*order.LimitOrder) {
	removed = m.applyMarketStartedBook(update)
	m.applyMarketStartedEpochs(update)
	// Unlock every revoked epoch order, including orders that were not
	// restored into the epoch queues.
	for _, revoke := range update.EpochRevokes {
		m.unlockOrderCoins(revoke.Order)
	}
	return removed
}

// applyMarketStartedBook removes revoked orders from the book, unlocks their
// funding coins, and sets the book epoch. It returns the orders actually
// removed from the book.
func (m *Market) applyMarketStartedBook(update *db.MarketStartedUpdate) (removed []*order.LimitOrder) {
	m.bookMtx.Lock()
	for _, revoke := range update.BookedRevokes {
		if lo, ok := m.book.Remove(revoke.Order.ID()); ok {
			removed = append(removed, lo)
		}
	}
	m.bookEpochIdx = update.CurrentEpochIdx
	m.bookMtx.Unlock()
	for _, revoke := range update.BookedRevokes {
		m.unlockOrderCoins(revoke.Order)
	}
	return removed
}

// applyMarketStartedEpochs clears the epoch orders and commitments, creates
// empty queues for the current and next epochs, and sets the market's start
// and active epoch indexes.
func (m *Market) applyMarketStartedEpochs(update *db.MarketStartedUpdate) {
	m.epochMtx.Lock()
	m.epochOrders = make(map[order.OrderID]order.Order)
	m.epochCommitments = make(map[order.Commitment]order.OrderID)
	m.currentEpoch = NewEpoch(update.CurrentEpochIdx, update.EpochDur)
	m.nextEpoch = NewEpoch(update.CurrentEpochIdx+1, update.EpochDur)
	m.startEpochIdx = update.CurrentEpochIdx
	m.activeEpochIdx = update.CurrentEpochIdx
	m.epochMtx.Unlock()
}
