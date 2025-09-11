package mm

import (
	"errors"
	"math"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex/msgjson"
)

// steppedRate rounds the rate to the nearest integer multiple of the step.
// The minimum returned value is step.
func steppedRate(r, step uint64) uint64 {
	steps := math.Round(float64(r) / float64(step))
	if steps == 0 {
		return step
	}
	return uint64(math.Round(steps * float64(step)))
}

// updateBotProblemsBasedOnError updates BotProblems based on an error
// encountered during market making.
func updateBotProblemsBasedOnError(problems *BotProblems, err error) {
	if err == nil {
		return
	}

	if noPeersErr, is := err.(*core.WalletNoPeersError); is {
		if problems.NoWalletPeers == nil {
			problems.NoWalletPeers = make(map[uint32]bool)
		}
		problems.NoWalletPeers[noPeersErr.AssetID] = true
		return
	}

	if noSyncErr, is := err.(*core.WalletSyncError); is {
		if problems.WalletNotSynced == nil {
			problems.WalletNotSynced = make(map[uint32]bool)
		}
		problems.WalletNotSynced[noSyncErr.AssetID] = true
		return
	}

	if errors.Is(err, core.ErrAccountSuspended) {
		problems.AccountSuspended = true
		return
	}

	var mErr *msgjson.Error
	if errors.As(err, &mErr) && mErr.Code == msgjson.OrderQuantityTooHigh {
		problems.UserLimitTooLow = true
		return
	}

	if errors.Is(err, errNoBasisPrice) {
		problems.NoPriceSource = true
		return
	}

	if errors.Is(err, libxc.ErrUnsyncedOrderbook) {
		problems.CEXOrderbookUnsynced = true
		return
	}

	if errors.Is(err, errOracleFiatMismatch) {
		problems.OracleFiatMismatch = true
		return
	}

	problems.UnknownError = err.Error()
}

func feeAssetID(assetID uint32) uint32 {
	token := asset.TokenInfo(assetID)
	if token != nil {
		return token.ParentID
	}
	return assetID
}
