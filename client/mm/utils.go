package mm

import (
	"errors"
	"math"

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

// clearBotProblemErrors clears all the problems that may be set in
// updateBotProblemsBasedOnError.
func clearBotProblemErrors(problems *BotProblems) {
	problems.UserLimitTooLow = false
	problems.WalletNotSynced = map[uint32]bool{}
	problems.NoWalletPeers = map[uint32]bool{}
	problems.AccountSuspended = false
	problems.NoOracleAvailable = false
	problems.EmptyMarket = false
}

// updateBotProblemsBasedOnError updates BotProblems based on an error
// encountered during market making. True is returned if the error maps
// to a known problem.
func updateBotProblemsBasedOnError(problems *BotProblems, err error) bool {
	if err == nil {
		return false
	}

	if noPeersErr, is := err.(*core.WalletNoPeersError); is {
		if problems.NoWalletPeers == nil {
			problems.NoWalletPeers = make(map[uint32]bool)
		}
		problems.NoWalletPeers[noPeersErr.AssetID] = true
		return true
	}

	if noSyncErr, is := err.(*core.WalletSyncError); is {
		if problems.WalletNotSynced == nil {
			problems.WalletNotSynced = make(map[uint32]bool)
		}
		problems.WalletNotSynced[noSyncErr.AssetID] = true
		return true
	}

	if errors.Is(err, core.ErrAccountSuspended) {
		problems.AccountSuspended = true
		return true
	}

	var mErr *msgjson.Error
	if errors.As(err, &mErr) && mErr.Code == msgjson.OrderQuantityTooHigh {
		problems.UserLimitTooLow = true
		return true
	}

	if errors.Is(err, errNoOracleAvailable) {
		problems.NoOracleAvailable = true
		return true
	}

	if errors.Is(err, errNoBasisPrice) {
		problems.EmptyMarket = true
		return true
	}

	if errors.Is(err, libxc.ErrUnsyncedOrderbook) {
		problems.CEXOrderbookUnsynced = true
		return true
	}

	return false
}
