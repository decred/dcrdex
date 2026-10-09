// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// MeshService is the part of mesh.Service this package calls:
// ExecuteCommand for client requests, ApplyEvent for replicated state.
type MeshService interface {
	ExecuteCommand(context.Context, mesh.CommandRequest) *msgjson.Error
	ApplyEvent(context.Context, *mesh.Event) (any, error)
}

// LifecycleTransition identifies a change to a market's trading lifecycle.
type LifecycleTransition uint8

const (
	LifecycleTransitionStarted LifecycleTransition = iota + 1
	LifecycleTransitionScheduleSuspend
	LifecycleTransitionSuspend
	LifecycleTransitionScheduleResume
	LifecycleTransitionResume
)

// LifecycleUpdated is a callback that receives the applied lifecycle
// transition and updated market lifecycle.
type LifecycleUpdated func(transition LifecycleTransition, lc *db.MarketLifecycle)

// Events returns the market event appliers keyed by event kind.
func Events(markets map[string]*Market, bookRouter *BookRouter, sendIfLocal func(account.AccountID, *msgjson.Message) error, lifecycleUpdated LifecycleUpdated) map[string]mesh.EventApplier {
	return map[string]mesh.EventApplier{
		meshevents.EventKindOrderAccepted: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyOrderAcceptedEvent(applyCtx, markets, bookRouter, event)
		},
		meshevents.EventKindMarketStarted: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyMarketStartedEvent(applyCtx, markets, bookRouter, lifecycleUpdated, event)
		},
		meshevents.EventKindMarketSuspendScheduled: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyMarketSuspendScheduledEvent(applyCtx, markets, bookRouter, lifecycleUpdated, event)
		},
		meshevents.EventKindMarketSuspended: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyMarketSuspendedEvent(applyCtx, markets, bookRouter, lifecycleUpdated, event)
		},
		meshevents.EventKindMarketResumeScheduled: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyMarketResumeScheduledEvent(applyCtx, markets, bookRouter, lifecycleUpdated, event)
		},
		meshevents.EventKindMarketResumed: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyMarketResumedEvent(applyCtx, markets, bookRouter, lifecycleUpdated, event)
		},
		meshevents.EventKindSuspendedCancel: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applySuspendedCancelEvent(applyCtx, markets, bookRouter, event)
		},
		meshevents.EventKindAdvanceEpoch: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyAdvanceEpochEvent(applyCtx, markets, bookRouter, event)
		},
		meshevents.EventKindEpochProcessed: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyEpochProcessedEvent(applyCtx, markets, bookRouter, sendIfLocal, event)
		},
		meshevents.EventKindOrdersRevoked: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			return applyOrdersRevokedEvent(applyCtx, markets, bookRouter, event)
		},
	}
}

type validatedMarketStartedEvent struct {
	mkt    *Market
	book   *msgBook
	update *db.MarketStartedUpdate
}

// applyMarketStartedEvent records startup revocations and lifecycle changes,
// updates the market and book router, and notifies affected order owners.
func applyMarketStartedEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, lifecycleUpdated LifecycleUpdated, event *mesh.Event) (*db.EventLogEntry, error) {
	validated, err := validateMarketStartedEvent(markets, bookRouter, event)
	if err != nil {
		return nil, err
	}

	mkt, update := validated.mkt, validated.update
	result, err := mkt.storage.ApplyMarketStartedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), update)
	if err != nil {
		return nil, err
	}

	removedOrders := mkt.applyMarketStartedMemory(update)
	mkt.applyMarketLifecycleRow(result.Lifecycle)
	bookRouter.applyMarketStartedEvent(validated.book, update.CurrentEpochIdx, removedOrders)

	for _, revoke := range update.BookedRevokes {
		ord := revoke.Order
		mkt.sendRevokeOrderNote(ord.ID(), ord.User())
	}
	for _, revoke := range update.EpochRevokes {
		ord := revoke.Order
		if ord.Type() == order.CancelOrderType {
			// Abandoned cancel orders receive nomatch notifications, just like
			// cancels that did not match during epoch processing.
			mkt.sendNoMatchNote(ord.ID(), ord.User())
			continue
		}
		mkt.sendRevokeOrderNote(ord.ID(), ord.User())
	}

	if lifecycleUpdated != nil && result.Lifecycle != nil {
		lifecycleUpdated(LifecycleTransitionStarted, result.Lifecycle)
	}
	return result.Log, nil
}

// validateMarketStartedEvent decodes the event, checks its revocations and
// book lot-size compatibility, and builds the database update.
func validateMarketStartedEvent(markets map[string]*Market, bookRouter *BookRouter, event *mesh.Event) (*validatedMarketStartedEvent, error) {
	started, err := meshevents.DecodeMarketStartedEvent(event.Payload)
	if err != nil {
		return nil, err
	}

	mkt, book, err := marketAndBook(markets, bookRouter, started.Market)
	if err != nil {
		return nil, err
	}

	bookedRevokes, err := decodeStartupOrderRevokeRecords(started.BookedRevokes)
	if err != nil {
		return nil, err
	}
	seen := make(map[order.OrderID]bool, len(bookedRevokes))
	if err := mkt.validateStartupOrderRevokes(bookedRevokes, seen, true, "booked"); err != nil {
		return nil, err
	}
	// Every booked order incompatible with the new lot size must be revoked.
	// Check before adding epoch revocations to seen so only booked revocations
	// are excluded from the lot-size check.
	if err := mkt.book.CheckLotSize(started.RunParams.LotSize, seen); err != nil {
		return nil, fmt.Errorf("market_started for %s: %w", started.Market, err)
	}

	epochRevokes, err := decodeStartupOrderRevokeRecords(started.EpochRevokes)
	if err != nil {
		return nil, err
	}
	if err := mkt.validateStartupOrderRevokes(epochRevokes, seen, false, "epoch"); err != nil {
		return nil, err
	}

	return &validatedMarketStartedEvent{
		mkt:  mkt,
		book: book,
		update: &db.MarketStartedUpdate{
			Market:          started.Market,
			Base:            mkt.base,
			Quote:           mkt.quote,
			CurrentEpochIdx: started.CurrentEpochIdx,
			EpochDur:        started.EpochDur,
			RunParams:       started.RunParams,
			RevocationTime:  time.UnixMilli(started.RevocationTime).UTC(),
			BookedRevokes:   bookedRevokes,
			EpochRevokes:    epochRevokes,
		},
	}, nil
}

// decodeStartupOrderRevokeRecords decodes revoked orders and validates
// their revocation reasons.
func decodeStartupOrderRevokeRecords(records []meshevents.StartupOrderRevokeRecord) ([]*db.StartupOrderRevoke, error) {
	revokes := make([]*db.StartupOrderRevoke, 0, len(records))
	for _, record := range records {
		if !meshevents.ValidStartupOrderRevokeReason(record.Reason) {
			return nil, fmt.Errorf("invalid order revoke reason %d", record.Reason)
		}
		if len(record.EncodedOrder) == 0 {
			return nil, fmt.Errorf("startup revocation has no encoded order")
		}
		ord, err := order.DecodeOrder(record.EncodedOrder)
		if err != nil {
			return nil, fmt.Errorf("decode startup revoked order: %w", err)
		}
		revokes = append(revokes, &db.StartupOrderRevoke{
			Order:  ord,
			Reason: record.Reason,
		})
	}
	return revokes, nil
}

// validateStartupOrderRevokes checks for duplicate IDs, wrong markets, and,
// when requireLimit is true, non-limit orders. It adds IDs to seen so duplicates
// can be detected across revocation lists.
func (m *Market) validateStartupOrderRevokes(revokes []*db.StartupOrderRevoke, seen map[order.OrderID]bool,
	requireLimit bool, label string) error {

	for _, revoke := range revokes {
		ord := revoke.Order
		oid := ord.ID()
		if seen[oid] {
			return fmt.Errorf("duplicate startup order revoke %v", oid)
		}
		seen[oid] = true

		if ord.Base() != m.base || ord.Quote() != m.quote {
			return fmt.Errorf("startup %s revoke %v market mismatch", label, oid)
		}

		if requireLimit && ord.Type() != order.LimitOrderType {
			return fmt.Errorf("startup %s revoke %v type %v, want limit", label, oid, ord.Type())
		}
	}
	return nil
}

// dbEventLogMeta builds event-log metadata from the event payload and
// optional log position.
func dbEventLogMeta(position *db.EventLogPosition, event *mesh.Event) *db.EventLogMeta {
	if event == nil {
		return nil
	}
	logMeta := &db.EventLogMeta{
		Event: append([]byte(nil), event.Payload...),
	}
	if position != nil {
		logMeta.Seq = position.Seq
		logMeta.ExpectedTipHash = append([]byte(nil), position.TipHash...)
	}
	return logMeta
}

// marketAndBook finds the market and its corresponding book in the book router.
func marketAndBook(markets map[string]*Market, bookRouter *BookRouter, marketName string) (*Market, *msgBook, error) {
	mkt := markets[marketName]
	if mkt == nil {
		return nil, nil, fmt.Errorf("unknown event market %q", marketName)
	}
	book := bookRouter.books[marketName]
	if book == nil {
		return nil, nil, fmt.Errorf("unknown event book market %q", marketName)
	}
	return mkt, book, nil
}

type validatedOrderAcceptedEvent struct {
	mkt            *Market
	book           *msgBook
	update         *db.OrderAcceptedUpdate
	alreadyApplied bool
}

type validatedSuspendedCancelEvent struct {
	mkt    *Market
	book   *msgBook
	update *db.SuspendedCancelUpdate
}

type revokeTarget struct {
	mkt  *Market
	book *msgBook
	lo   *order.LimitOrder
}

type resolvedOrdersRevokedEvent struct {
	reason     meshevents.OrderRevokeReason
	revokeTime time.Time
	targets    []revokeTarget
}

func applyOrdersRevokedEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter,
	event *mesh.Event) (*db.EventLogEntry, error) {

	resolved, err := resolveOrdersRevokedEvent(markets, bookRouter, event)
	if err != nil {
		return nil, err
	}
	update := &db.OrdersRevokedUpdate{
		Reason:     resolved.reason,
		RevokeTime: resolved.revokeTime,
		Orders:     make([]*order.LimitOrder, 0, len(resolved.targets)),
	}
	for _, target := range resolved.targets {
		update.Orders = append(update.Orders, target.lo)
	}
	mkt := resolved.targets[0].mkt
	logEntry, err := mkt.storage.ApplyOrdersRevokedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event),
		mkt.auth.ReputationOutcomePolicy(), update)
	if err != nil {
		return nil, err
	}
	applyOrdersRevokedMemory(bookRouter, resolved)
	applyCtx.SetResult(update.Orders)
	return logEntry, nil
}

// resolveOrdersRevokedEvent finds the booked orders identified by the event's
// account or order IDs. It skips orders no longer booked. For spent-funding
// revocations with explicit order IDs, it also skips orders that have since
// partially filled.
func resolveOrdersRevokedEvent(markets map[string]*Market, bookRouter *BookRouter, evt *mesh.Event) (*resolvedOrdersRevokedEvent, error) {
	event, err := meshevents.DecodeOrdersRevokedEvent(evt.Payload)
	if err != nil {
		return nil, err
	}

	var targets []revokeTarget
	if len(event.User) > 0 {
		// Find the account's booked orders across all markets.
		user := account.AccountID(event.User)
		mktNames := make([]string, 0, len(markets))
		for name := range markets {
			mktNames = append(mktNames, name)
		}
		sort.Strings(mktNames)
		for _, name := range mktNames {
			mkt, book, err := marketAndBook(markets, bookRouter, name)
			if err != nil {
				return nil, err
			}
			buys, sells := mkt.book.UserOrders(user)
			for _, lo := range append(buys, sells...) {
				targets = append(targets, revokeTarget{mkt: mkt, book: book, lo: lo})
			}
		}
	} else {
		// Look up the explicitly listed orders.
		mkt, book, err := marketAndBook(markets, bookRouter, event.Market)
		if err != nil {
			return nil, err
		}
		for _, rawID := range event.OrderIDs {
			oid := order.OrderID(rawID)
			lo := mkt.book.Order(oid)
			if lo == nil {
				log.Debugf("Skipping orders_revoked target %v: no longer booked on market %s",
					oid, event.Market)
				continue
			}
			if event.Reason == meshevents.OrderRevokeReasonFundingSpent && lo.Filled() != 0 {
				log.Debugf("Skipping funding-spent orders_revoked target %v: order has partially filled", oid)
				continue
			}
			targets = append(targets, revokeTarget{mkt: mkt, book: book, lo: lo})
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("orders_revoked event has no booked targets")
	}
	// ID order keeps database updates and notifications deterministic.
	slices.SortFunc(targets, func(a, b revokeTarget) int {
		aID, bID := a.lo.ID(), b.lo.ID()
		return bytes.Compare(aID[:], bID[:])
	})
	// An explicit list may name the same order more than once.
	targets = slices.CompactFunc(targets, func(a, b revokeTarget) bool {
		return a.lo.ID() == b.lo.ID()
	})

	return &resolvedOrdersRevokedEvent{
		reason:     event.Reason,
		revokeTime: time.UnixMilli(event.RevokeTime).UTC(),
		targets:    targets,
	}, nil
}

func applyOrdersRevokedMemory(bookRouter *BookRouter, resolved *resolvedOrdersRevokedEvent) {
	for _, target := range resolved.targets {
		if target.mkt.applyOrderRevokedMemory(target.lo) {
			bookRouter.unbookOrder(target.book, target.lo)
		}
	}
	if resolved.reason != meshevents.OrderRevokeReasonPenalty {
		return
	}
	notified := make(map[account.AccountID]bool, 1)
	for _, target := range resolved.targets {
		user := target.lo.User()
		if notified[user] {
			continue
		}
		notified[user] = true
		target.mkt.sendPenaltyNote(user, resolved.revokeTime)
	}
}

func applyOrderAcceptedEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, event *mesh.Event) (*db.EventLogEntry, error) {
	validated, err := validateOrderAcceptedEvent(markets, bookRouter, event)
	if err != nil {
		return nil, err
	}
	mkt, update := validated.mkt, validated.update
	logEntry, err := mkt.storage.ApplyOrderAcceptedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), update)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to store accepted order %v: %w", errEpochOrderStorage, update.Order.ID(), err)
	}

	// The event is logged even if the order is already in epoch memory.
	if !validated.alreadyApplied {
		mkt.applyOrderAcceptedMemory(update)
		note := epochOrderNote(update.Order, mkt.name, update.EpochIdx)
		bookRouter.applyOrderAcceptedEvent(validated.book, note, update.EpochIdx)
	}
	return logEntry, nil
}

func validateOrderAcceptedEvent(markets map[string]*Market, bookRouter *BookRouter, event *mesh.Event) (*validatedOrderAcceptedEvent, error) {
	accepted, err := meshevents.DecodeOrderAcceptedEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	ord, err := accepted.Order()
	if err != nil {
		return nil, err
	}
	mktName, err := dex.MarketName(ord.Base(), ord.Quote())
	if err != nil {
		return nil, err
	}
	mkt, book, err := marketAndBook(markets, bookRouter, mktName)
	if err != nil {
		return nil, err
	}
	return mkt.validateOrderAcceptedEvent(ord, book)
}

// applyMarketSuspendScheduledEvent stores the suspension schedule and updates market state.
func applyMarketSuspendScheduledEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, lifecycleUpdated LifecycleUpdated, event *mesh.Event) (*db.EventLogEntry, error) {
	payload, err := meshevents.DecodeMarketSuspendScheduledEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	mkt, _, err := marketAndBook(markets, bookRouter, payload.Market)
	if err != nil {
		return nil, err
	}
	if err := mkt.validateScheduleSuspendEvent(payload.FinalEpochIdx, payload.EpochDur); err != nil {
		return nil, err
	}
	update := &db.MarketSuspendScheduledUpdate{
		Market:        payload.Market,
		Base:          mkt.base,
		Quote:         mkt.quote,
		FinalEpochIdx: payload.FinalEpochIdx,
		EpochDur:      payload.EpochDur,
		PersistBook:   payload.PersistBook,
	}
	result, err := mkt.storage.ApplyMarketSuspendScheduledEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), update)
	if err != nil {
		return nil, err
	}
	mkt.applyMarketLifecycleRow(result.Lifecycle)
	if lifecycleUpdated != nil {
		lifecycleUpdated(LifecycleTransitionScheduleSuspend, result.Lifecycle)
	}
	return result.Log, nil
}

// applyMarketSuspendedEvent completes suspension and updates the book and subscribers.
func applyMarketSuspendedEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, lifecycleUpdated LifecycleUpdated, event *mesh.Event) (*db.EventLogEntry, error) {
	payload, err := meshevents.DecodeMarketSuspendedEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	mkt, book, err := marketAndBook(markets, bookRouter, payload.Market)
	if err != nil {
		return nil, err
	}
	update := &db.MarketSuspendedUpdate{
		Market:        payload.Market,
		Base:          mkt.base,
		Quote:         mkt.quote,
		FinalEpochIdx: payload.FinalEpochIdx,
		EpochDur:      payload.EpochDur,
		Timestamp:     time.UnixMilli(payload.Timestamp).UTC(),
	}
	result, err := mkt.storage.ApplyMarketSuspendedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), update)
	if err != nil {
		return nil, err
	}
	mkt.applyMarketLifecycleRow(result.Lifecycle)
	persist := result.Lifecycle.PersistBook != nil && *result.Lifecycle.PersistBook
	if !persist {
		mkt.applyMarketSuspendPurge(result.PurgeOrders)
	}
	bookRouter.applyMarketSuspendedEvent(book, result.Lifecycle.FinalEpochIdx, persist, result.PurgeOrders)
	if lifecycleUpdated != nil {
		lifecycleUpdated(LifecycleTransitionSuspend, result.Lifecycle)
	}
	return result.Log, nil
}

// applyMarketResumeScheduledEvent stores the resumption schedule and updates market state.
func applyMarketResumeScheduledEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, lifecycleUpdated LifecycleUpdated, event *mesh.Event) (*db.EventLogEntry, error) {
	payload, err := meshevents.DecodeMarketResumeScheduledEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	mkt, _, err := marketAndBook(markets, bookRouter, payload.Market)
	if err != nil {
		return nil, err
	}
	update := &db.MarketResumeScheduledUpdate{
		Market:        payload.Market,
		Base:          mkt.base,
		Quote:         mkt.quote,
		StartEpochIdx: payload.StartEpochIdx,
		EpochDur:      payload.EpochDur,
	}
	result, err := mkt.storage.ApplyMarketResumeScheduledEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), update)
	if err != nil {
		return nil, err
	}
	mkt.applyMarketLifecycleRow(result.Lifecycle)
	if lifecycleUpdated != nil {
		lifecycleUpdated(LifecycleTransitionScheduleResume, result.Lifecycle)
	}
	return result.Log, nil
}

// applyMarketResumedEvent adopts the new trading parameters and removes revoked orders.
func applyMarketResumedEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, lifecycleUpdated LifecycleUpdated, event *mesh.Event) (*db.EventLogEntry, error) {
	payload, err := meshevents.DecodeMarketResumedEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	mkt, book, err := marketAndBook(markets, bookRouter, payload.Market)
	if err != nil {
		return nil, err
	}
	resumeRevokes, err := decodeStartupOrderRevokeRecords(payload.ResumeRevokes)
	if err != nil {
		return nil, err
	}
	// The revoke set must cover every order the new lot size strands.
	revoked := make(map[order.OrderID]bool, len(resumeRevokes))
	for _, revoke := range resumeRevokes {
		revoked[revoke.Order.ID()] = true
	}
	if err := mkt.book.CheckLotSize(payload.RunParams.LotSize, revoked); err != nil {
		return nil, fmt.Errorf("resume for %s: %w", payload.Market, err)
	}
	update := &db.MarketResumedUpdate{
		Market:        payload.Market,
		Base:          mkt.base,
		Quote:         mkt.quote,
		StartEpochIdx: payload.StartEpochIdx,
		EpochDur:      payload.EpochDur,
		Timestamp:     time.UnixMilli(payload.Timestamp).UTC(),
		ResumeRevokes: resumeRevokes,
		RunParams:     payload.RunParams,
	}
	result, err := mkt.storage.ApplyMarketResumedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), update)
	if err != nil {
		return nil, err
	}
	removed := mkt.applyMarketResumeCleanup(result.ResumeRevokes)
	mkt.applyMarketLifecycleRow(result.Lifecycle)
	if lifecycleUpdated != nil {
		lifecycleUpdated(LifecycleTransitionResume, result.Lifecycle)
	}
	bookRouter.applyMarketResumedEvent(book, result.Lifecycle.StartEpochIdx, removed)
	return result.Log, nil
}

func applySuspendedCancelEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter,
	event *mesh.Event) (*db.EventLogEntry, error) {

	validated, err := validateSuspendedCancelEvent(markets, bookRouter, event)
	if err != nil {
		return nil, err
	}
	result, err := validated.mkt.storage.ApplySuspendedCancelEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), validated.update)
	if err != nil {
		return nil, err
	}
	mkt := validated.mkt
	target := result.TargetOrder
	mkt.bookMtx.Lock()
	delete(mkt.settling, target.ID())
	mkt.book.Remove(target.ID())
	mkt.bookMtx.Unlock()
	mkt.unlockOrderCoins(target)

	// Attempt the acceptance response before sending the cancellation match.
	notifySuspendedCancel := func(context.Context) {
		bookRouter.unbookOrder(validated.book, target)
		mkt.sendSuspendedCancelMatchRequest(result.Cancel.User(), result.Match, validated.update.MatchServerTime)
	}
	if !applyCtx.AfterCommandResult(notifySuspendedCancel) {
		bookRouter.unbookOrder(validated.book, target)
	}
	return result.Log, nil
}

func validateSuspendedCancelEvent(markets map[string]*Market, bookRouter *BookRouter, event *mesh.Event) (*validatedSuspendedCancelEvent, error) {
	cancelEvent, err := meshevents.DecodeSuspendedCancelEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	cancel, err := cancelEvent.CancelOrder()
	if err != nil {
		return nil, err
	}
	target, err := cancelEvent.TargetOrder()
	if err != nil {
		return nil, err
	}
	mkt, book, err := marketAndBook(markets, bookRouter, cancelEvent.Market)
	if err != nil {
		return nil, err
	}
	if cancel.Base() != mkt.base || cancel.Quote() != mkt.quote {
		return nil, fmt.Errorf("suspended_cancel market mismatch")
	}
	if target.Base() != mkt.base || target.Quote() != mkt.quote {
		return nil, fmt.Errorf("suspended_cancel target market mismatch")
	}
	match := &order.Match{
		Taker:        cancel,
		Maker:        target,
		Quantity:     target.Remaining(),
		Rate:         target.Rate,
		Epoch:        order.EpochID{Idx: uint64(cancelEvent.EpochIdx), Dur: uint64(cancelEvent.EpochDur)},
		FeeRateBase:  cancelEvent.FeeRateBase,
		FeeRateQuote: cancelEvent.FeeRateQuote,
		Status:       order.MatchComplete,
	}
	return &validatedSuspendedCancelEvent{
		mkt:  mkt,
		book: book,
		update: &db.SuspendedCancelUpdate{
			Market:          cancelEvent.Market,
			Base:            cancelEvent.Base,
			Quote:           cancelEvent.Quote,
			Cancel:          cancel,
			TargetOrderID:   target.ID(),
			TargetAccount:   target.AccountID,
			TargetSell:      target.Sell,
			EpochIdx:        cancelEvent.EpochIdx,
			EpochDur:        cancelEvent.EpochDur,
			FeeRateBase:     cancelEvent.FeeRateBase,
			FeeRateQuote:    cancelEvent.FeeRateQuote,
			MatchServerTime: cancelEvent.MatchTime(),
			Match:           match,
		},
	}, nil
}

func applyAdvanceEpochEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter, event *mesh.Event) (*db.EventLogEntry, error) {
	advance, err := meshevents.DecodeAdvanceEpochEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	mkt, book, err := marketAndBook(markets, bookRouter, advance.Market)
	if err != nil {
		return nil, err
	}
	closedOrders, err := mkt.validateAdvanceEpochState(advance)
	if err != nil {
		return nil, err
	}
	logEntry, err := mkt.storage.ApplyAdvanceEpochEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), advance)
	if err != nil {
		return nil, err
	}
	mkt.applyAdvanceEpochEvent(advance, closedOrders)
	if advance.OpenedEpochIdx > 0 {
		book.setEpoch(advance.OpenedEpochIdx)
	}
	return logEntry, nil
}

func applyEpochProcessedEvent(applyCtx *mesh.EventApplyContext, markets map[string]*Market, bookRouter *BookRouter,
	sendIfLocal func(account.AccountID, *msgjson.Message) error, event *mesh.Event) (*db.EventLogEntry, error) {

	processed, err := meshevents.DecodeEpochProcessedEvent(event.Payload)
	if err != nil {
		return nil, err
	}
	mkt, book, err := marketAndBook(markets, bookRouter, processed.Market)
	if err != nil {
		return nil, err
	}
	result, err := mkt.applyEpochProcessedEvent(applyCtx, processed, event)
	if err != nil {
		return nil, err
	}
	bookRouter.applyEpochProcessedEvent(book, processed, result)
	for _, ord := range result.nomatched {
		oid := ord.Order.ID()
		msg, err := noMatchMessage(oid)
		if err != nil {
			log.Errorf("Failed to encode 'nomatch' notification.")
			continue
		}
		if err := sendIfLocal(ord.Order.User(), msg); err != nil {
			log.Infof("Failed to send nomatch to user %s: %v", ord.Order.User(), err)
		}
	}
	bookRouter.publishEpochReport(book, &epochReport{
		epochIdx:     processed.EpochIdx,
		epochDur:     processed.EpochDur,
		spot:         result.spot,
		stats:        result.stats,
		baseFeeRate:  processed.FeeRateBase,
		quoteFeeRate: processed.FeeRateQuote,
		matches:      result.matchReport,
	})
	mkt.sendMMSnapshots(processed.EpochIdx, processed.EpochDur)
	applyCtx.SetResult(result)
	return result.dbLog, nil
}
