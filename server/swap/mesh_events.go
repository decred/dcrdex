// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// MeshService executes swap commands and applies swap events.
type MeshService interface {
	ExecuteCommand(context.Context, mesh.CommandRequest) *msgjson.Error
	ApplyEvent(context.Context, *mesh.Event) (any, error)
}

// SetMeshService sets the mesh service used by the swapper.
func (s *Swapper) SetMeshService(mesh MeshService) {
	s.mesh = mesh
}

// Events returns the mesh event appliers.
func (s *Swapper) Events() map[string]mesh.EventApplier {
	return map[string]mesh.EventApplier{
		meshevents.EventKindMatchAcksRecorded: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			acks, err := meshevents.DecodeMatchAcksRecordedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return s.applyMatchAcksRecordedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), acks)
		},
		meshevents.EventKindMatchFailed: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			failed, err := meshevents.DecodeMatchFailedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return s.applyMatchFailedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), failed)
		},
		meshevents.EventKindSwapContractRecorded: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			recorded, err := meshevents.DecodeSwapContractRecordedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return s.applySwapContractRecordedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), recorded)
		},
		meshevents.EventKindSwapRedemptionRecorded: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			recorded, err := meshevents.DecodeSwapRedemptionRecordedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return s.applySwapRedemptionRecordedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), recorded)
		},
		meshevents.EventKindAuditAckRecorded: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			recorded, err := meshevents.DecodeAuditAckRecordedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return s.applyAuditAckRecordedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), recorded)
		},
		// TODO(mesh): consider if we actually need the redemption acknowledgement events.
		meshevents.EventKindRedemptionAckRecorded: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			recorded, err := meshevents.DecodeRedemptionAckRecordedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return s.applyRedemptionAckRecordedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), recorded)
		},
	}
}

func dbEventLogMeta(position *db.EventLogPosition, event *mesh.Event) *db.EventLogMeta {
	if event == nil {
		return nil
	}
	logMeta := &db.EventLogMeta{
		Event: event.Payload,
	}
	if position != nil {
		logMeta.Seq = position.Seq
		logMeta.ExpectedTipHash = position.TipHash
	}
	return logMeta
}

func newSwapContractRecordedEvent(step *stepInformation, params *msgjson.Init, contract *asset.Contract, swapTime time.Time) (*mesh.Event, error) {
	event := &meshevents.SwapContractRecordedEvent{
		MatchID:     step.match.ID(),
		Base:        step.match.Maker.BaseAsset,
		Quote:       step.match.Maker.QuoteAsset,
		Maker:       step.actor.isMaker,
		Status:      step.nextStep,
		CoinID:      params.CoinID,
		CoinTxID:    contract.TxID(),
		CoinString:  contract.String(),
		Value:       contract.Value(),
		FeeRate:     contract.FeeRate(),
		Contract:    params.Contract,
		SwapAddress: contract.SwapAddress,
		SecretHash:  contract.SecretHash,
		LockTime:    contract.LockTime.UnixMilli(),
		TxData:      contract.TxData,
		SwapTime:    swapTime.UnixMilli(),
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return mesh.NewEvent(event)
}

// eventCoin implements asset.Coin using the details recorded in an event,
// allowing event application to reconstruct a contract without querying the
// blockchain. Confirmation counts are fetched from the backend when requested.
type eventCoin struct {
	id            dex.Bytes
	txID          string
	coinString    string
	value         uint64
	feeRate       uint64
	confirmations func(context.Context) (int64, error)
}

func (c *eventCoin) Confirmations(ctx context.Context) (int64, error) {
	if c.confirmations != nil {
		return c.confirmations(ctx)
	}
	return 0, nil
}

func (c *eventCoin) ID() []byte {
	return c.id
}

func (c *eventCoin) TxID() string {
	if c.txID != "" {
		return c.txID
	}
	return fmt.Sprintf("%x", []byte(c.id))
}

func (c *eventCoin) String() string {
	if c.coinString != "" {
		return c.coinString
	}
	return c.TxID()
}

func (c *eventCoin) Value() uint64 {
	return c.value
}

func (c *eventCoin) FeeRate() uint64 {
	return c.feeRate
}

func (s *Swapper) applySwapContractRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.SwapContractRecordedEvent) (*db.EventLogEntry, error) {
	match, status, contract, err := s.prepareSwapContractUpdate(event)
	if err != nil {
		return nil, err
	}
	logEntry, err := s.storage.ApplySwapContractRecordedEvent(ctx, meta, event)
	if err != nil {
		return nil, fmt.Errorf("saving swap contract for match %v: %w", event.MatchID, err)
	}

	s.registerSwapContractDedup(event.MatchID, event.CoinID, event.Contract, event.SecretHash, event.Maker)

	status.mtx.Lock()
	status.swap = contract
	status.swapTime = time.UnixMilli(event.SwapTime).UTC()
	status.mtx.Unlock()

	match.mtx.Lock()
	match.Status = event.Status
	match.mtx.Unlock()
	return logEntry, nil
}

// prepareSwapContractUpdate checks the match and contract reuse, then reconstructs
// the contract from the event's recorded details.
func (s *Swapper) prepareSwapContractUpdate(event *meshevents.SwapContractRecordedEvent) (*matchTracker, *swapStatus, *asset.Contract, error) {
	s.matchMtx.RLock()
	match := s.matches[event.MatchID]
	s.matchMtx.RUnlock()
	if match == nil {
		return nil, nil, nil, fmt.Errorf("swap contract recorded event for unknown match %v", event.MatchID)
	}
	if event.Base != match.Maker.BaseAsset || event.Quote != match.Maker.QuoteAsset {
		return nil, nil, nil, fmt.Errorf("swap contract recorded market mismatch for match %v", event.MatchID)
	}

	status := match.takerStatus
	if event.Maker {
		status = match.makerStatus
	}
	swapAsset := s.coins[status.swapAsset]
	if swapAsset == nil {
		return nil, nil, nil, fmt.Errorf("no swap asset %d for match %v", status.swapAsset, event.MatchID)
	}

	if err := s.checkSwapContractDedup(event.MatchID, event.CoinID, event.Contract, event.SecretHash, event.Maker); err != nil {
		return nil, nil, nil, err
	}

	coin := &eventCoin{
		id:         append(dex.Bytes(nil), event.CoinID...),
		txID:       event.CoinTxID,
		coinString: event.CoinString,
		value:      event.Value,
		feeRate:    event.FeeRate,
		confirmations: func(ctx context.Context) (int64, error) {
			contract, err := swapAsset.Backend.Contract(event.CoinID, event.Contract)
			if err != nil {
				return 0, err
			}
			return contract.Confirmations(ctx)
		},
	}
	contract := &asset.Contract{
		Coin:         coin,
		SwapAddress:  event.SwapAddress,
		ContractData: append([]byte(nil), event.Contract...),
		SecretHash:   append([]byte(nil), event.SecretHash...),
		LockTime:     time.UnixMilli(event.LockTime).UTC(),
		TxData:       append([]byte(nil), event.TxData...),
	}

	return match, status, contract, nil
}

func newSwapRedemptionRecordedEvent(step *stepInformation, params *msgjson.Redeem, redemption asset.Coin, redeemTime time.Time) (*mesh.Event, error) {
	var secret dex.Bytes
	if step.actor.isMaker {
		secret = params.Secret
	}
	event := &meshevents.SwapRedemptionRecordedEvent{
		MatchID:    step.match.ID(),
		Base:       step.match.Maker.BaseAsset,
		Quote:      step.match.Maker.QuoteAsset,
		Maker:      step.actor.isMaker,
		Status:     step.nextStep,
		CoinID:     params.CoinID,
		CoinTxID:   redemption.TxID(),
		CoinString: redemption.String(),
		Value:      redemption.Value(),
		FeeRate:    redemption.FeeRate(),
		Secret:     secret,
		RedeemTime: redeemTime.UnixMilli(),
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return mesh.NewEvent(event)
}

// applySwapRedemptionRecordedEvent records a redemption, updates the match and
// order state, and removes the match once the taker has redeemed.
func (s *Swapper) applySwapRedemptionRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.SwapRedemptionRecordedEvent) (*db.EventLogEntry, error) {
	match, status, redemption, err := s.prepareSwapRedemptionUpdate(event)
	if err != nil {
		return nil, err
	}
	logEntry, err := s.storage.ApplySwapRedemptionRecordedEvent(ctx, meta, s.authMgr.ReputationOutcomePolicy(), event)
	if err != nil {
		return nil, fmt.Errorf("saving redeem transaction (match id=%v, maker=%v): %w",
			event.MatchID, event.Maker, err)
	}

	status.mtx.Lock()
	status.redemption = redemption
	status.redeemTime = time.UnixMilli(event.RedeemTime).UTC()
	if event.Maker {
		status.secret = append([]byte(nil), event.Secret...)
	}
	status.mtx.Unlock()

	match.mtx.Lock()
	match.Status = event.Status
	match.mtx.Unlock()

	actorOrder := match.Taker
	if event.Maker {
		actorOrder = match.Maker
	}
	s.swapDone(actorOrder, match.Match, false)

	if !event.Maker {
		log.Debugf("Deleting completed match %v", event.MatchID)
		s.matchMtx.Lock()
		if s.matches[event.MatchID] == match {
			s.deleteMatch(match)
		}
		s.matchMtx.Unlock()
	}
	return logEntry, nil
}

// prepareSwapRedemptionUpdate checks the match and counterparty contract, then
// reconstructs the redemption from the event's recorded details.
func (s *Swapper) prepareSwapRedemptionUpdate(event *meshevents.SwapRedemptionRecordedEvent) (*matchTracker, *swapStatus, asset.Coin, error) {
	s.matchMtx.RLock()
	match := s.matches[event.MatchID]
	s.matchMtx.RUnlock()
	if match == nil {
		return nil, nil, nil, fmt.Errorf("swap redemption recorded event for unknown match %v", event.MatchID)
	}
	if event.Base != match.Maker.BaseAsset || event.Quote != match.Maker.QuoteAsset {
		return nil, nil, nil, fmt.Errorf("swap redemption recorded market mismatch for match %v", event.MatchID)
	}

	actorStatus, counterpartyStatus := match.takerStatus, match.makerStatus
	if event.Maker {
		actorStatus, counterpartyStatus = match.makerStatus, match.takerStatus
	}
	counterpartyStatus.mtx.RLock()
	hasContract := counterpartyStatus.swap != nil && len(counterpartyStatus.swap.ContractData) != 0
	counterpartyStatus.mtx.RUnlock()
	if !hasContract {
		return nil, nil, nil, fmt.Errorf("counterparty swap contract missing for redemption event match %v", event.MatchID)
	}

	s.matchMtx.RLock()
	if s.matches[event.MatchID] != match {
		s.matchMtx.RUnlock()
		return nil, nil, nil, fmt.Errorf("redeem txn found after match was revoked (match id=%v, maker=%v)",
			event.MatchID, event.Maker)
	}
	requiredStatus := order.MakerRedeemed
	if event.Maker {
		requiredStatus = order.TakerSwapCast
	}
	match.mtx.RLock()
	localStatus := match.Status
	match.mtx.RUnlock()
	if localStatus != requiredStatus {
		s.matchMtx.RUnlock()
		return nil, nil, nil, fmt.Errorf("swap redemption recorded event requires status %v, local status is %v for match %v",
			requiredStatus, localStatus, event.MatchID)
	}
	s.matchMtx.RUnlock()

	redemption := &eventCoin{
		id:         append(dex.Bytes(nil), event.CoinID...),
		txID:       event.CoinTxID,
		coinString: event.CoinString,
		value:      event.Value,
		feeRate:    event.FeeRate,
	}
	return match, actorStatus, redemption, nil
}

func newAuditAckRecordedEvent(match *matchTracker, maker bool, sig []byte) (*mesh.Event, error) {
	event := &meshevents.AuditAckRecordedEvent{
		MatchID: match.ID(),
		Base:    match.Maker.BaseAsset,
		Quote:   match.Maker.QuoteAsset,
		Maker:   maker,
		Sig:     sig,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return mesh.NewEvent(event)
}

// applyAuditAckRecordedEvent stores an audit signature before updating the
// tracked match's acknowledgements.
func (s *Swapper) applyAuditAckRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.AuditAckRecordedEvent) (*db.EventLogEntry, error) {
	s.matchMtx.RLock()
	match := s.matches[event.MatchID]
	s.matchMtx.RUnlock()
	if match == nil {
		return nil, fmt.Errorf("audit ack recorded event for unknown match %v", event.MatchID)
	}
	if event.Base != match.Maker.BaseAsset || event.Quote != match.Maker.QuoteAsset {
		return nil, fmt.Errorf("audit ack recorded market mismatch for match %v", event.MatchID)
	}
	logEntry, err := s.storage.ApplyAuditAckRecordedEvent(ctx, meta, event)
	if err != nil {
		return nil, fmt.Errorf("saving audit ack signature for match %v: %w", event.MatchID, err)
	}
	match.mtx.Lock()
	if event.Maker {
		match.Sigs.MakerAudit = event.Sig
	} else {
		match.Sigs.TakerAudit = event.Sig
	}
	match.mtx.Unlock()
	return logEntry, nil
}

func newRedemptionAckRecordedEvent(match *matchTracker, maker bool, sig []byte) (*mesh.Event, error) {
	event := &meshevents.RedemptionAckRecordedEvent{
		MatchID: match.ID(),
		Base:    match.Maker.BaseAsset,
		Quote:   match.Maker.QuoteAsset,
		Maker:   maker,
		Sig:     sig,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return mesh.NewEvent(event)
}

// applyRedemptionAckRecordedEvent records an acknowledgement and updates the
// tracked match's signature, removing the match for a maker acknowledgement.
func (s *Swapper) applyRedemptionAckRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.RedemptionAckRecordedEvent) (*db.EventLogEntry, error) {
	s.matchMtx.RLock()
	match := s.matches[event.MatchID]
	s.matchMtx.RUnlock()
	if match == nil {
		log.Debugf("Recording %s redemption ack for already removed match %v",
			makerTaker(event.Maker), event.MatchID)
	} else if event.Base != match.Maker.BaseAsset || event.Quote != match.Maker.QuoteAsset {
		return nil, fmt.Errorf("redemption ack recorded market mismatch for match %v", event.MatchID)
	}

	entry, err := s.storage.ApplyRedemptionAckRecordedEvent(ctx, meta, event)
	if err != nil {
		return nil, fmt.Errorf("saving redemption ack signature for match %v: %w", event.MatchID, err)
	}
	if match == nil {
		return entry, nil
	}

	if event.Maker {
		// Taker redemption normally removes the match before this acknowledgement.
		log.Errorf("Maker redemption ack found live match %v; removing it", event.MatchID)
		s.matchMtx.Lock()
		if s.matches[event.MatchID] == match {
			s.deleteMatch(match)
		}
		s.matchMtx.Unlock()
	}

	match.mtx.Lock()
	if event.Maker {
		match.Sigs.MakerRedeem = append([]byte(nil), event.Sig...)
	} else {
		match.Sigs.TakerRedeem = append([]byte(nil), event.Sig...)
	}
	match.mtx.Unlock()
	return entry, nil
}

// newMatchFailedEvent encodes a failure using the state captured at detection.
func newMatchFailedEvent(failure matchFailure) (*mesh.Event, error) {
	match := failure.match
	event := &meshevents.MatchFailedEvent{
		MatchID:           match.ID(),
		Base:              match.Maker.BaseAsset,
		Quote:             match.Maker.QuoteAsset,
		FailTime:          time.Now().UnixMilli(),
		Status:            failure.status,
		Fault:             failure.fault,
		MakerAddressKnown: failure.makerAddressKnown,
		TakerAddressKnown: failure.takerAddressKnown,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return mesh.NewEvent(event)
}

// errMatchFailureSuperseded indicates that a match changed after a failure
// was detected, so the old decision no longer applies.
var errMatchFailureSuperseded = errors.New("match failure superseded")

// applyMatchFailedEvent records a failure, removes the tracked match, updates
// the affected orders in memory, and notifies the clients.
func (s *Swapper) applyMatchFailedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.MatchFailedEvent) (*db.EventLogEntry, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	s.matchMtx.RLock()
	match := s.matches[event.MatchID]
	s.matchMtx.RUnlock()
	if match == nil {
		return nil, fmt.Errorf("%w: unknown match %v", errMatchFailureSuperseded, event.MatchID)
	}
	if match.Taker.Type() == order.CancelOrderType {
		return nil, fmt.Errorf("match_failed event for cancel match %v", event.MatchID)
	}
	if event.Base != match.Maker.BaseAsset || event.Quote != match.Maker.QuoteAsset {
		return nil, fmt.Errorf("match_failed market mismatch for match %v", event.MatchID)
	}

	match.mtx.RLock()
	status := match.Status
	makerAddressKnown := match.makerSwapAddr != ""
	takerAddressKnown := match.takerSwapAddr != ""
	match.mtx.RUnlock()
	if status != event.Status {
		return nil, fmt.Errorf("%w: failure requires status %v, found %v for match %v",
			errMatchFailureSuperseded, event.Status, status, event.MatchID)
	}
	// An acknowledgement can supply an address and restart the maker's timeout
	// without changing NewlyMatched status. The failure must still refer to
	// the address state captured when the timeout was detected.
	if event.Status == order.NewlyMatched && event.Fault != meshevents.MatchFailureNoUserFault {
		if makerAddressKnown != event.MakerAddressKnown || takerAddressKnown != event.TakerAddressKnown {
			return nil, fmt.Errorf("%w: swap addresses changed for match %v", errMatchFailureSuperseded, event.MatchID)
		}
	}

	entry, err := s.storage.ApplyMatchFailedEvent(ctx, meta, s.authMgr.ReputationOutcomePolicy(), event)
	if err != nil {
		return nil, fmt.Errorf("applying match_failed event for match %v: %w", event.MatchID, err)
	}
	s.matchMtx.Lock()
	if s.matches[event.MatchID] == match {
		s.deleteMatch(match)
	}
	s.matchMtx.Unlock()

	// The maker's redemption already handled its order completion.
	if event.Status != order.MakerRedeemed {
		s.swapDone(match.Maker, match.Match, event.Fault == meshevents.MatchFailureMakerFault)
	}
	s.swapDone(match.Taker, match.Match, event.Fault == meshevents.MatchFailureTakerFault)
	s.revoke(match)
	return entry, nil
}

func newMatchAcksRecordedEvent(ackTime time.Time, records []meshevents.MatchAckRecord) (*mesh.Event, error) {
	event := &meshevents.MatchAcksRecordedEvent{
		AckTime: ackTime.UnixMilli(),
		Records: records,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return mesh.NewEvent(event)
}

// applyMatchAcksRecordedEvent stores acknowledgements before updating tracked
// swaps and sending counterparty addresses.
func (s *Swapper) applyMatchAcksRecordedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.MatchAcksRecordedEvent) (*db.EventLogEntry, error) {
	acks, err := s.resolveMatchAckRecords(event.Records)
	if err != nil {
		return nil, err
	}
	logEntry, err := s.storage.ApplyMatchAcksRecordedEvent(ctx, meta, event)
	if err != nil {
		return nil, fmt.Errorf("saving match acks: %w", err)
	}
	s.applyMatchAckUpdates(time.UnixMilli(event.AckTime).UTC(), acks)
	return logEntry, nil
}

type resolvedMatchAck struct {
	record meshevents.MatchAckRecord
	match  *matchTracker
}

// resolveMatchAckRecords finds the tracked swaps and checks that their market
// and match type agree with the records. Cancel acknowledgements are omitted
// because cancel matches are not tracked in memory.
func (s *Swapper) resolveMatchAckRecords(records []meshevents.MatchAckRecord) ([]resolvedMatchAck, error) {
	acks := make([]resolvedMatchAck, 0, len(records))
	for _, record := range records {
		s.matchMtx.RLock()
		match := s.matches[record.MatchID]
		s.matchMtx.RUnlock()
		if match == nil {
			if !record.Cancel {
				return nil, fmt.Errorf("match %v was not registered", record.MatchID)
			}
			continue
		}
		if (match.Taker.Type() == order.CancelOrderType) != record.Cancel {
			return nil, fmt.Errorf("match %v cancel flag mismatch", record.MatchID)
		}
		if record.Base != match.Maker.BaseAsset || record.Quote != match.Maker.QuoteAsset {
			return nil, fmt.Errorf("match %v market mismatch", record.MatchID)
		}
		if record.Cancel {
			continue
		}
		acks = append(acks, resolvedMatchAck{record: record, match: match})
	}
	return acks, nil
}

// applyMatchAckUpdates updates acknowledgement signatures and swap addresses.
// When both addresses become available, it starts the maker's deadline and
// notifies each counterparty.
func (s *Swapper) applyMatchAckUpdates(ackTime time.Time, acks []resolvedMatchAck) {
	var addrNotifications []*matchTracker
	for _, ack := range acks {
		match, record := ack.match, ack.record
		match.mtx.Lock()
		sig, addr := &match.Sigs.TakerMatch, &match.takerSwapAddr
		if record.Maker {
			sig, addr = &match.Sigs.MakerMatch, &match.makerSwapAddr
		}

		*sig = record.Sig
		if *addr == "" {
			*addr = record.Address
		} else if *addr != record.Address {
			log.Warnf("applyMatchAckUpdates: match %v (maker=%v) re-ack address %q, "+
				"keeping recorded %q",
				record.MatchID, record.Maker, record.Address, *addr)
		}
		if match.makerSwapAddr != "" && match.takerSwapAddr != "" && !match.counterPartyAddrsSent {
			match.counterPartyAddrsSent = true
			// Give the maker the full broadcast timeout once both addresses are available.
			match.time = ackTime
			addrNotifications = append(addrNotifications, match)
		}
		match.mtx.Unlock()
	}

	for _, match := range addrNotifications {
		s.sendCounterPartyAddresses(match)
	}
}

var _ MeshService = (*mesh.Service)(nil)
