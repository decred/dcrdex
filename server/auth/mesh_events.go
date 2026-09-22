// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"context"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// MeshService provides the mesh operations used by auth.
type MeshService interface {
	ExecuteCommand(context.Context, mesh.CommandRequest) *msgjson.Error
	ProxyClientMessage(context.Context, *mesh.ClientProxyMessage) error
}

// Events returns the mesh event appliers.
func (auth *AuthManager) Events() map[string]mesh.EventApplier {
	return map[string]mesh.EventApplier{
		meshevents.EventKindBondPosted: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			posted, err := meshevents.DecodeBondPostedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return auth.applyBondPostedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), posted)
		},
		meshevents.EventKindPrepaidBondsCreated: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			created, err := meshevents.DecodePrepaidBondsCreatedEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return auth.storage.ApplyPrepaidBondsCreatedEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), created)
		},
		meshevents.EventKindReputationForgiven: func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			forgiven, err := meshevents.DecodeReputationForgivenEvent(event.Payload)
			if err != nil {
				return nil, err
			}
			return auth.applyReputationForgivenEvent(applyCtx, dbEventLogMeta(applyCtx.Position, event), forgiven)
		},
	}
}

// applyBondPostedEvent stores the account and bond and builds the signed postbond result.
func (auth *AuthManager) applyBondPostedEvent(applyCtx *mesh.EventApplyContext, logMeta *db.EventLogMeta, event *meshevents.BondPostedEvent) (*db.EventLogEntry, error) {
	result, err := auth.storage.ApplyBondPostedEvent(applyCtx, logMeta, event, scoringOrderLimit, ScoringMatchLimit, cancelThreshWindow)
	if err != nil {
		return nil, err
	}
	score, _, _ := auth.integrateOutcomes(
		newLatestOutcomes(result.Matches, ScoringMatchLimit),
		newLatestOutcomes(result.Preimages, scoringOrderLimit),
		newLatestOutcomes(result.Orders, cancelThreshWindow),
	)
	data := newRepData(true, score, result.Bonds)
	threshold := time.Now().Add(auth.bondExpiry).Unix()
	rep := auth.reputationFromData(data, threshold)
	bond := event.Bond
	postBondRes := &msgjson.PostBondResult{
		AccountID:  event.Account.AccountID[:],
		AssetID:    bond.AssetID,
		Amount:     uint64(bond.Amount),
		Expiry:     uint64(time.Unix(bond.LockTime, 0).Add(-auth.bondExpiry).Unix()),
		Strength:   bond.Strength,
		BondID:     bond.CoinID,
		Reputation: rep,
	}
	auth.Sign(postBondRes)
	applyCtx.SetResult(postBondRes)
	return result.Log, nil
}

func (auth *AuthManager) applyReputationForgivenEvent(applyCtx *mesh.EventApplyContext, logMeta *db.EventLogMeta, event *meshevents.ReputationForgivenEvent) (*db.EventLogEntry, error) {
	ctx := applyCtx.Context
	stored, err := auth.storage.ApplyReputationForgivenEvent(ctx, logMeta, event)
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.Log == nil {
		return nil, fmt.Errorf("storage returned nil reputation forgiveness result")
	}

	result := &reputationForgivenessResult{
		Forgiven: stored.Forgiven,
	}
	// Forgiveness is already committed. A failed reputation lookup only
	// prevents us from confirming that the user can trade.
	rep, refreshErr := auth.loadUserReputationWithTimeout(ctx, event.AccountID)
	if refreshErr == nil {
		result.Unbanned = rep != nil && rep.EffectiveTier() > 0
	} else {
		log.Errorf("failed to refresh reputation after forgiveness for account %v: %v", event.AccountID, refreshErr)
	}
	applyCtx.SetResult(result)
	return stored.Log, nil
}

// dbEventLogMeta builds the storage event-log metadata for a replicated mesh
// event.
func dbEventLogMeta(meta *db.EventLogPosition, event *mesh.Event) *db.EventLogMeta {
	logMeta := &db.EventLogMeta{
		Event: event.Payload,
	}
	if meta != nil {
		logMeta.Seq = meta.Seq
		logMeta.ExpectedTipHash = meta.TipHash
	}
	return logMeta
}
