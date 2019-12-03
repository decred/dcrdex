// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
)

const (
	// These constants are used for a chainWaiter to inform the caller whether
	// the waiter should be run again.
	tryAgain     = false
	dontTryAgain = true
)

var (
	// The chainWaiters will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the Swapper will wait for a chainWaiter.
	// This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = time.Minute
)

// The AuthManager handles client-related actions, including authorization and
// communications.
type AuthManager interface {
	Route(string, func(account.AccountID, *msgjson.Message) *msgjson.Error)
	Auth(user account.AccountID, msg, sig []byte) error
	Sign(...msgjson.Signable)
	Send(account.AccountID, *msgjson.Message)
	Request(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message))
	Penalize(account.AccountID, *order.Match, order.MatchStatus)
}

// Storage updates match data in what is presumably a database.
type Storage interface {
	UpdateMatch(match *order.Match) error
	CancelOrder(*order.LimitOrder) error
}

// swapStatus is information related to the completion or incompletion of each
// sequential step of the atomic swap negotiation process. Each user has their
// own swapStatus.
type swapStatus struct {
	// The asset to which the user broadcasts their swap transaction.
	swapAsset uint32
	// The time that the swap coordinator sees the transaction.
	swapTime time.Time
	swap     asset.Coin
	// The time that the transaction receives its SwapConf'th confirmation.
	swapConfirmed time.Time
	// The time that the swap coordinator sees the user's redemption transaction.
	redeemTime time.Time
	redemption asset.Coin
}

// matchTracker embeds an order.Match and adds some data necessary for tracking
// the match negotiation.
type matchTracker struct {
	*order.Match
	time        time.Time
	makerStatus *swapStatus
	takerStatus *swapStatus
}

// A blockNotification is used internally when an asset.DEXAsset reports a new
// block.
type blockNotification struct {
	time    time.Time
	height  uint32
	assetID uint32
}

// waitSettings is used to define the lifecycle of a chainWaiter.
type waitSettings struct {
	expiration time.Time
	accountID  account.AccountID
	request    *msgjson.Message
	timeoutErr *msgjson.Error
}

// A chainWaiter is a function that is repeated periodically until a boolean
// true is returned, or the expiration time is surpassed. In the latter case a
// timeout error is sent to the client.
type chainWaiter struct {
	params *waitSettings
	f      func() bool
}

// A stepActor is a structure holding information about one party of a match.
// stepActor is used with the stepInformation structure, which is used for
// sequencing swap negotiation.
type stepActor struct {
	user account.AccountID
	// swapAsset is the asset to which this actor broadcasts their swap tx.
	swapAsset uint32
	order     order.Order
	// The swapStatus from the Match. Could be either the
	// (matchTracker).makerStatus or (matchTracker).takerStatus, depending on who
	// this actor is.
	status  *swapStatus
	isMaker bool
}

// stepInformation holds information about the current state of the swap
// negotiation. A new stepInformation should be generated with (Swapper).step at
// every step of the negotiation process.
type stepInformation struct {
	match *matchTracker
	// The actor is the user info for the user who is expected to be broadcasting
	// a swap or redemption transaction next.
	actor stepActor
	// counterParty is the user that is not expected to be acting next.
	counterParty stepActor
	// asset is the asset backend for swapAsset.
	asset *asset.Asset
	// isBaseAsset will be true if the current step involves a transaction on the
	// match market's base asset blockchain, false if on quote asset's blockchain.
	isBaseAsset bool
	step        order.MatchStatus
	nextStep    order.MatchStatus
	// checkVal holds the trade amount in units of the currently acting asset,
	// and is used to validate the swap transaction details.
	checkVal uint64
}

// Config is the swapper configuration settings. A Config instance is the only
// argument to the Swapper constructor.
type Config struct {
	// Ctx is the application context. Swapper will attempt to shutdown cleanly if
	// the application context is cancelled.
	Ctx context.Context
	// Assets is a map to all the asset information, including the asset backends,
	// used by this Swapper.
	Assets map[uint32]*asset.Asset
	// Mgr is the AuthManager for client messaging and authentication.
	AuthManager AuthManager
	// A database backend.
	Storage          Storage
	BroadcastTimeout time.Duration
}

// Swapper handles order matches by handling authentication and inter-party
// communications between clients, or 'users'. The Swapper authenticates users
// (vua AuthManager) and validates transactions as they are reported.
type Swapper struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// coins is a map to all the Asset information, including the asset backends,
	// used by this Swapper.
	coins map[uint32]*asset.Asset
	// storage is a Database backend.
	storage Storage
	// authMgr is an AuthManager for client messaging and authentication.
	authMgr AuthManager
	// The matches map and the contained matches are protected by the matchMtx.
	matchMtx sync.RWMutex
	matches  map[string]*matchTracker
	// To accommodate network latency, when transaction data is being drawn from
	// the backend, the function may run repeatedly on some interval until either
	// the transaction data is successfully retrieved, or a timeout is surpassed.
	// These chainWaiters are added to the monitor loop via the waiters channel.
	waiterMtx sync.RWMutex
	waiters   []*chainWaiter
	// Each asset backend gets a goroutine to monitor for new blocks. When a new
	// block is received, it is passed to the start loop through the block
	// channel.
	block chan *blockNotification
	// The broadcast timeout.
	bTimeout time.Duration
}

// NewSwapper is a constructor for a Swapper.
func NewSwapper(cfg *Config) *Swapper {
	authMgr := cfg.AuthManager
	ctx, cancel := context.WithCancel(cfg.Ctx)
	swapper := &Swapper{
		ctx:      ctx,
		cancel:   cancel,
		coins:    cfg.Assets,
		storage:  cfg.Storage,
		authMgr:  authMgr,
		matches:  make(map[string]*matchTracker),
		waiters:  make([]*chainWaiter, 0, 256),
		block:    make(chan *blockNotification, 8),
		bTimeout: cfg.BroadcastTimeout,
	}
	// The swapper is only concerned with two types of client-originating
	// method requests.
	authMgr.Route(msgjson.InitRoute, swapper.handleInit)
	authMgr.Route(msgjson.RedeemRoute, swapper.handleRedeem)

	swapper.wg.Add(1)
	go swapper.start()
	return swapper
}

// Stop begins the Swapper's shutdown. Use WaitForShutdown after Stop to wait
// for shutdown to complete.
func (s *Swapper) Stop() {
	log.Infof("Swapper shutting down...")
	s.cancel()
}

// WaitForShutdown waits until the main swapper loop is finished.
func (s *Swapper) WaitForShutdown() {
	s.wg.Wait()
}

// waitMempool attempts to run the passed function. If the function returns
// the value dontTryAgain, nothing else is done. If the function returns the
// value tryAgain, the function is queued to run on an interval until it returns
// dontTryAgain, or until an expiration time is exceeded, as specified in the
// waitSettings.
func (s *Swapper) waitMempool(params *waitSettings, f func() bool) {
	if time.Now().After(params.expiration) {
		log.Error("Swapper.waitMempool: waitSettings given expiration before present")
		return
	}
	// Check to see if it passes right away.
	if f() {
		return
	}
	s.waiterMtx.Lock()
	s.waiters = append(s.waiters, &chainWaiter{params: params, f: f})
	s.waiterMtx.Unlock()
}

// start is the main Swapper loop, and must be run as a goroutine.
func (s *Swapper) start() {
	defer s.wg.Done()

	// The latencyTicker triggers a check of all chainWaiter functions.
	latencyTicker := time.NewTicker(recheckInterval)
	defer latencyTicker.Stop()
	// bcastTriggers is used to sequence an examination of an asset's related
	// matches some time (bTimeout) after a block notification is received.
	bcastTriggers := make([]*blockNotification, 0, 16)
	bcastTicker := time.NewTimer(s.bTimeout)
	minTimeout := s.bTimeout / 10
	setTimeout := func(block *blockNotification) {
		timeTil := time.Until(block.time.Add(s.bTimeout))
		if timeTil < minTimeout {
			timeTil = minTimeout
		}
		bcastTicker = time.NewTimer(timeTil)
	}

	runWaiters := func() {
		s.waiterMtx.Lock()
		defer s.waiterMtx.Unlock()
		agains := make([]*chainWaiter, 0)
		// Grab new waiters
		tNow := time.Now()
		for _, mFunc := range s.waiters {
			if !mFunc.f() {
				// If this waiter has expired, issue the timeout error to the client
				// and do not append to the agains slice.
				if mFunc.params.expiration.Before(tNow) {
					p := mFunc.params
					resp, err := msgjson.NewResponse(p.request.ID, nil, p.timeoutErr)
					if err != nil {
						log.Error("NewResponse error in (Swapper).loop: %v", err)
						continue
					}
					s.authMgr.Send(p.accountID, resp)
					continue
				}
				agains = append(agains, mFunc)
			} // End if !mFunc.f(). nothing to do if mFunc returned dontTryAgain=true
		}
		s.waiters = agains
	}

	// Start a listen loop for each asset's block channel.
	for assetID, asset := range s.coins {
		go func(assetID uint32, blockSource <-chan uint32) {
		out:
			for {
				select {
				case h := <-blockSource:
					s.block <- &blockNotification{
						time:    time.Now(),
						height:  h,
						assetID: assetID,
					}
				case <-s.ctx.Done():
					break out
				}
			}
		}(assetID, asset.Backend.BlockChannel(5))
	}

out:
	for {
		select {
		case <-latencyTicker.C:
			runWaiters()
		case block := <-s.block:
			// Schedule a check of matches with one side equal to this block's asset
			// by appending the block to the bcastTriggers.
			bcastTriggers = append(bcastTriggers, block)
			// processBlock will update confirmation times in the swapStatus structs.
			s.processBlock(block)
		case <-bcastTicker.C:
			for {
				if len(bcastTriggers) == 0 {
					bcastTicker = time.NewTimer(s.bTimeout)
					break
				}
				if time.Now().Before(bcastTriggers[0].time.Add(s.bTimeout)) {
					setTimeout(bcastTriggers[0])
					break
				}
				block := bcastTriggers[0]
				bcastTriggers = bcastTriggers[1:]
				s.checkInaction(block.assetID)
				if len(bcastTriggers) == 0 {
					bcastTicker = time.NewTimer(s.bTimeout)
				} else {
					setTimeout(bcastTriggers[0])
				}
			}
		case <-s.ctx.Done():
			break out
		}
	}
}

func (s *Swapper) tryConfirmSwap(status *swapStatus) {
	if status.swapTime.IsZero() || !status.swapConfirmed.IsZero() {
		return
	}
	confs, err := status.swap.Confirmations()
	if err != nil {
		// The transaction has become invalid. No reason to do anything.
		return
	}
	// If a swapStatus was created, the asset.Asset is already known to be in
	// the map.
	if confs >= int64(s.coins[status.swapAsset].SwapConf) {
		status.swapConfirmed = time.Now()
	}

}

// processBlock scans the matches and updates match status based on number of
// confirmations. Once a relevant transaction has the requisite number of
// confirmations, the next-to-act has only duration (Swapper).bTimeout to
// broadcast the next transaction in the settlement sequence. The timeout is
// not evaluated here, but in (Swapper).checkInaction. This method simply sets
// the appropriate flags in the swapStatus structures.
func (s *Swapper) processBlock(block *blockNotification) {
	completions := make([]*matchTracker, 0)
	s.matchMtx.RLock()
	defer s.matchMtx.RUnlock()
	for _, match := range s.matches {
		// If it's neither of the match assets, nothing to do.
		if match.makerStatus.swapAsset != block.assetID && match.takerStatus.swapAsset != block.assetID {
			continue
		}
		switch match.Status {
		case order.NewlyMatched:
		case order.MakerSwapCast:
			if match.makerStatus.swapAsset != block.assetID {
				continue
			}
			// If the maker has broadcast their transaction, the taker's broadcast
			// timeout starts once the maker's swap has SwapConf confs.
			s.tryConfirmSwap(match.makerStatus)
		case order.TakerSwapCast:
			if match.takerStatus.swapAsset != block.assetID {
				continue
			}
			// If the taker has broadcast their transaction, the maker's broadcast
			// timeout (for redemption) starts once the maker's swap has SwapConf
			// confs.
			s.tryConfirmSwap(match.takerStatus)
		case order.MakerRedeemed:
			// It's the taker's turn to redeem. Nothing to do here.
			continue
		case order.MatchComplete:
			// Once both redemption transactions have SwapConf confirmations, the
			// order is complete.
			var makerRedeemed, takerRedeemed bool
			mStatus, tStatus := match.makerStatus, match.takerStatus
			if !mStatus.redeemTime.IsZero() {
				confs, err := mStatus.redemption.Confirmations()
				makerRedeemed = err == nil && confs >= int64(s.coins[tStatus.swapAsset].SwapConf)
			}
			if makerRedeemed && !tStatus.redeemTime.IsZero() {
				confs, err := tStatus.redemption.Confirmations()
				takerRedeemed = err == nil && confs >= int64(s.coins[mStatus.swapAsset].SwapConf)
			}
			if makerRedeemed && takerRedeemed {
				completions = append(completions, match)
			}
		}
	}
	for _, match := range completions {
		delete(s.matches, match.ID().String())
	}
}

// checkInaction scans the swapStatus structures relevant to the specified
// asset. If a client is found to have not acted when required, a match may be
// revoked and a penalty assigned to the user.
func (s *Swapper) checkInaction(assetID uint32) {
	oldestAllowed := time.Now().Add(-s.bTimeout)
	deletions := make([]string, 0)
	s.matchMtx.RLock()
	defer s.matchMtx.RUnlock()
	for _, match := range s.matches {
		if match.makerStatus.swapAsset != assetID && match.takerStatus.swapAsset != assetID {
			continue
		}
		switch match.Status {
		case order.NewlyMatched:
			if match.makerStatus.swapAsset != assetID {
				continue
			}
			// If the maker is not acting, the swapTime won't be set. Check against
			// the time the match notification was sent (match.time) for the broadcast
			// timeout.
			if match.makerStatus.swapTime.IsZero() && match.time.Before(oldestAllowed) {
				deletions = append(deletions, match.ID().String())
				s.authMgr.Penalize(match.Maker.User(), match.Match, match.Status)
				s.revoke(match)
			}
		case order.MakerSwapCast:
			if match.takerStatus.swapAsset != assetID {
				continue
			}
			// If the maker has sent their swap tx, check the taker's broadcast
			// timeout against the time of the swap's SwapConf'th confirmation.
			if match.takerStatus.swapTime.IsZero() &&
				!match.makerStatus.swapConfirmed.IsZero() &&
				match.makerStatus.swapConfirmed.Before(oldestAllowed) {

				deletions = append(deletions, match.ID().String())
				s.authMgr.Penalize(match.Taker.User(), match.Match, match.Status)
				s.revoke(match)
			}
		case order.TakerSwapCast:
			if match.takerStatus.swapAsset != assetID {
				continue
			}
			// If the taker has sent their swap tx, check the maker's broadcast
			// timeout (for redemption) against the time of the swap's SwapConf'th
			// confirmation.
			if match.makerStatus.redeemTime.IsZero() &&
				!match.takerStatus.swapConfirmed.IsZero() &&
				match.takerStatus.swapConfirmed.Before(oldestAllowed) {

				deletions = append(deletions, match.ID().String())
				s.authMgr.Penalize(match.Maker.User(), match.Match, match.Status)
				s.revoke(match)
			}
		case order.MakerRedeemed:
			if match.takerStatus.swapAsset != assetID {
				continue
			}
			// If the maker has redeemed, the taker can redeem immediately, so
			// check the timeout against the time the Swapper received the
			// maker's `redeem` request (and sent the taker's 'redemption').
			if match.takerStatus.redeemTime.IsZero() &&
				!match.makerStatus.redeemTime.IsZero() &&
				match.makerStatus.redeemTime.Before(oldestAllowed) {

				deletions = append(deletions, match.ID().String())
				s.authMgr.Penalize(match.Taker.User(), match.Match, match.Status)
				s.revoke(match)
			}
		case order.MatchComplete:
			// Nothing to do here right now.
		}
	}
	for _, matchID := range deletions {
		delete(s.matches, matchID)
	}
}

// respondError sends an rpcError to a user.
func (s *Swapper) respondError(id uint64, user account.AccountID, code int, errMsg string) {
	log.Debugf("error going to user %x, code: %d, msg: %s", user, code, errMsg)
	msg, err := msgjson.NewResponse(id, nil, &msgjson.Error{
		Code:    code,
		Message: errMsg,
	})
	if err != nil {
		log.Errorf("error creating error response with message '%s': %v", msg, err)
	}
	s.authMgr.Send(user, msg)
}

// respondSuccess sends a successful response to a user.
func (s *Swapper) respondSuccess(id uint64, user account.AccountID, result interface{}) {
	msg, err := msgjson.NewResponse(id, result, nil)
	if err != nil {
		log.Errorf("failed to send success: %v", err)
	}
	s.authMgr.Send(user, msg)
}

// step creates a stepInformation structure for the specified match. A new
// stepInformation should be created for every client communication. The user
// is also validated as the actor. An error is returned if the user has not
// acknowledged their previous DEX requests.
func (s *Swapper) step(user account.AccountID, matchID string) (*stepInformation, *msgjson.Error) {
	s.matchMtx.RLock()
	defer s.matchMtx.RUnlock()
	match, found := s.matches[matchID]
	if !found {
		return nil, &msgjson.Error{
			Code:    msgjson.RPCUnknownMatch,
			Message: "unknown match ID",
		}
	}

	// Get the step-related information for both parties.
	var isBaseAsset bool
	var actor, counterParty stepActor
	var nextStep order.MatchStatus
	maker, taker := match.Maker, match.Taker
	var reqSigs [][]byte
	ackType := msgjson.MatchRoute
	switch match.Status {
	case order.NewlyMatched, order.TakerSwapCast:
		counterParty.order, actor.order = taker, maker
		actor.status = match.makerStatus
		counterParty.status = match.takerStatus
		actor.user = maker.User()
		counterParty.user = taker.User()
		actor.isMaker = true
		if match.Status == order.NewlyMatched {
			nextStep = order.MakerSwapCast
			isBaseAsset = maker.Sell
			reqSigs = [][]byte{match.Sigs.MakerMatch}
		} else {
			nextStep = order.MakerRedeemed
			isBaseAsset = !maker.Sell
			reqSigs = [][]byte{match.Sigs.MakerAudit}
			ackType = msgjson.AuditRoute
		}
	case order.MakerSwapCast, order.MakerRedeemed:
		counterParty.order, actor.order = maker, taker
		actor.status = match.takerStatus
		counterParty.status = match.makerStatus
		actor.user = taker.User()
		counterParty.user = maker.User()
		counterParty.isMaker = true
		if match.Status == order.MakerSwapCast {
			nextStep = order.TakerSwapCast
			isBaseAsset = !maker.Sell
			reqSigs = [][]byte{match.Sigs.TakerMatch, match.Sigs.TakerAudit}
			ackType = msgjson.AuditRoute
		} else {
			nextStep = order.MatchComplete
			isBaseAsset = maker.Sell
			reqSigs = [][]byte{match.Sigs.TakerRedeem}
			ackType = msgjson.RedemptionRoute
		}
	default:
		return nil, &msgjson.Error{
			Code:    msgjson.SettlementSequenceError,
			Message: "unknown settlement sequence identifier",
		}
	}

	// Verify that the user specified is the actor for this step.
	if actor.user != user {
		return nil, &msgjson.Error{
			Code:    msgjson.SettlementSequenceError,
			Message: "expected other party to act",
		}
	}

	// Verify that they have acknowledged their previous notification(s).
	for _, s := range reqSigs {
		if len(s) == 0 {
			return nil, &msgjson.Error{
				Code:    msgjson.SettlementSequenceError,
				Message: "missing acknowledgment for " + ackType + " request",
			}
		}
	}

	// Set the actors' swapAsset and the swap contract checkVal.
	var checkVal uint64
	if isBaseAsset {
		actor.swapAsset = maker.BaseAsset
		counterParty.swapAsset = maker.QuoteAsset
		checkVal = match.Quantity
	} else {
		actor.swapAsset = maker.QuoteAsset
		counterParty.swapAsset = maker.BaseAsset
		checkVal = matcher.BaseToQuote(maker.Rate, match.Quantity)
	}

	return &stepInformation{
		match: match,
		actor: actor,
		// By the time a match is created, the presence of the asset in the map
		// has already been verified.
		asset:        s.coins[actor.swapAsset],
		counterParty: counterParty,
		isBaseAsset:  isBaseAsset,
		step:         match.Status,
		nextStep:     nextStep,
		checkVal:     checkVal,
	}, nil
}

// authUser verifies that the msgjson.Signable is signed by the user. This method
// relies on the AuthManager to validate the signature.
func (s *Swapper) authUser(user account.AccountID, params msgjson.Signable) *msgjson.Error {
	// Authorize the user.
	msg, err := params.Serialize()
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SerializationError,
			Message: fmt.Sprintf("unable to serialize init params: %v", err),
		}
	}
	err = s.authMgr.Auth(user, msg, params.SigBytes())
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "error authenticating init params",
		}
	}
	return nil
}

// coinWaitRules is a constructor for a waitSettings with a 1-minute timeout and
// a standardized error message.
// NOTE: 1 minute is pretty arbitrary. Consider making this a DEX variable, or
// smarter in some way.
func coinWaitRules(user account.AccountID, msg *msgjson.Message, coinID []byte) *waitSettings {
	return &waitSettings{
		// must smarten up this expiration value before merge. Where should this
		// come from?
		expiration: time.Now().Add(txWaitExpiration),
		accountID:  user,
		request:    msg,
		timeoutErr: &msgjson.Error{
			Code:    msgjson.TransactionUndiscovered,
			Message: fmt.Sprintf("failed to find transaction %x", coinID),
		},
	}
}

// messageAcker is information needed to process the user's
// msgjson.AcknowledgementResult.
type messageAcker struct {
	user    account.AccountID
	match   *matchTracker
	params  msgjson.Signable
	isMaker bool
	isAudit bool
}

// processAck processes a single msgjson.AcknowledgementResult, validating the
// signature and updating the (order.Match).Sigs record.
func (s *Swapper) processAck(msg *msgjson.Message, acker *messageAcker) {
	var ack msgjson.Acknowledgement
	resp, err := msg.Response()
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.RPCParseError,
			fmt.Sprintf("error parsing audit notification result: %v", err))
		return
	}
	err = json.Unmarshal(resp.Result, &ack)
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.RPCParseError,
			fmt.Sprintf("error parsing audit notification acknowledgment: %v", err))
		return
	}
	// Check the signature.
	sigBytes, err := hex.DecodeString(ack.Sig)
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.SignatureError,
			fmt.Sprintf("error decoding signature %s: %v", ack.Sig, err))
		return
	}
	sigMsg, err := acker.params.Serialize()
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.SerializationError,
			fmt.Sprintf("unable to serialize match params: %v", err))
		return
	}
	err = s.authMgr.Auth(acker.user, sigMsg, sigBytes)
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.SignatureError,
			fmt.Sprintf("signature validation error: %v", err))
		return
	}
	// Set the appropriate signature, based on the current step and actor.
	// NOTE: Using the matchMtx like this is not ideal. It might be better to have
	// individual locks on the matchTrackers.
	s.matchMtx.Lock()
	defer s.matchMtx.Unlock()
	if acker.isAudit {
		if acker.isMaker {
			acker.match.Sigs.MakerAudit = sigBytes
		} else {
			acker.match.Sigs.TakerAudit = sigBytes
		}
	} else {
		if acker.isMaker {
			acker.match.Sigs.MakerRedeem = sigBytes
		} else {
			acker.match.Sigs.TakerRedeem = sigBytes
		}
	}
}

// saveMatch stores or updates the match in the Swapper's storage backend. The
// update is asynchronous, and any errors are logged but otherwise ignored.
func (s *Swapper) saveMatch(match *order.Match) {
	go func() {
		if err := s.storage.UpdateMatch(match); err != nil {
			log.Errorf("UpdateMatch (id=%v) failed: %v", match.ID(), err)
		}
	}()
}

// processInit processes the `init` RPC request, which is used to inform the
// DEX of a newly broadcast swap transaction. Once the transaction is seen and
// and audited by the Swapper, the counter-party is informed with an 'audit'
// request. This method is run as a chainWaiter.
func (s *Swapper) processInit(msg *msgjson.Message, params *msgjson.Init, stepInfo *stepInformation) bool {
	// Validate the swap contract
	chain := stepInfo.asset.Backend
	actor, counterParty := stepInfo.actor, stepInfo.counterParty
	coin, err := chain.Coin(params.CoinID, params.Contract)
	if err != nil {
		// If there is an error, don't give up yet, since it could be due to network
		// latency. Check again on the next tick.
		// NOTE: Could get a little smarter here by using go 1.13 (Error).Is to
		// check that the transaction was not found, and not some other less
		// recoverable state. Should require minimal modification of backends.
		return tryAgain
	}
	// Decode the contract and audit the contract.
	recipient, val, err := coin.AuditContract()
	if err != nil {
		s.respondError(msg.ID, actor.user, msgjson.ContractError, fmt.Sprintf("error auditing contract: %v", err))
		return dontTryAgain
	}
	if recipient != counterParty.order.SwapAddress() {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("incorrect recipient. expected %s. got %s", recipient, counterParty.order.SwapAddress()))
		return dontTryAgain
	}
	if val != stepInfo.checkVal {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("contract error. expected contract value to be %d, got %d", stepInfo.checkVal, val))
		return dontTryAgain
	}

	// Update the match.
	s.matchMtx.Lock()
	matchID := stepInfo.match.ID()
	actor.status.swap = coin
	actor.status.swapTime = time.Now()
	stepInfo.match.Status = stepInfo.nextStep
	match := stepInfo.match.Match
	s.matchMtx.Unlock()

	// TODO: decide if we can reasonably continue if the storage call fails.
	s.saveMatch(match)

	// Issue a positive response to the actor.
	s.authMgr.Sign(params)
	s.respondSuccess(msg.ID, actor.user, &msgjson.Acknowledgement{
		MatchID: matchID.String(),
		Sig:     params.Sig.String(),
	})

	// Prepare an 'audit' request for the counter-party.
	auditParams := &msgjson.Audit{
		OrderID:  idToBytes(counterParty.order.ID()),
		MatchID:  matchID[:],
		Time:     uint64(time.Now().Unix()),
		Contract: params.Contract,
	}
	notification, err := msgjson.NewRequest(comms.NextID(), msgjson.AuditRoute, auditParams)
	if err != nil {
		// This is likely an impossibly condition.
		log.Errorf("error creating audit request: %v", err)
		return dontTryAgain
	}
	// Set up the acknowledgement for the callback.
	ack := &messageAcker{
		user:    counterParty.user,
		match:   stepInfo.match,
		params:  auditParams,
		isMaker: counterParty.isMaker,
		isAudit: true,
	}
	// Send the 'audit' request to the counter-party.
	s.authMgr.Request(counterParty.order.User(), notification, func(_ comms.Link, msg *msgjson.Message) {
		s.processAck(msg, ack)
	})
	return dontTryAgain
}

// processRedeem processes a 'redeem' request from a client. processRedeem does
// not perform user authentication, which is handled in handleRedeem before
// processRedeem is invoked. This method is run as a chainWaiter.
func (s *Swapper) processRedeem(msg *msgjson.Message, params *msgjson.Redeem, stepInfo *stepInformation) bool {
	// Get the transaction
	actor, counterParty := stepInfo.actor, stepInfo.counterParty
	chain := stepInfo.asset.Backend
	coin, err := chain.Coin(params.CoinID, nil)
	// If there is an error, don't return an error yet, since it could be due to
	// network latency. Instead, queue it up for another check.
	if err != nil {
		return tryAgain
	}
	// Make sure that the expected output is being spent.
	status := counterParty.status
	spends, err := coin.SpendsCoin(status.swap.ID())
	if err != nil {
		s.respondError(msg.ID, actor.user, msgjson.RedemptionError,
			fmt.Sprintf("error checking redemption %x: %v", status.swap.ID(), err))
		return dontTryAgain
	}
	if !spends {
		s.respondError(msg.ID, actor.user, msgjson.RedemptionError,
			fmt.Sprintf("redemption does not spend %x", status.swap.ID()))
		return dontTryAgain
	}

	// Modify the match's swapStatuses.
	s.matchMtx.Lock()
	matchID := stepInfo.match.ID()
	// Update the match.
	actor.status.redemption = coin
	actor.status.redeemTime = time.Now()
	stepInfo.match.Status = stepInfo.nextStep
	s.matchMtx.Unlock()

	// Issue a positive response to the actor.
	s.authMgr.Sign(params)
	s.respondSuccess(msg.ID, actor.user, &msgjson.Acknowledgement{
		MatchID: matchID.String(),
		Sig:     params.Sig.String(),
	})

	// Inform the counterparty.
	rParams := &msgjson.Redemption{
		OrderID: idToBytes(counterParty.order.ID()),
		MatchID: matchID[:],
		CoinID:  params.CoinID,
		Time:    uint64(time.Now().Unix()),
	}

	notification, err := msgjson.NewRequest(comms.NextID(), msgjson.RedemptionRoute, rParams)
	if err != nil {
		log.Errorf("error creating redemption request: %v", err)
		return dontTryAgain
	}
	// Set up the acknowledgement callback.
	ack := &messageAcker{
		user:    counterParty.user,
		match:   stepInfo.match,
		params:  rParams,
		isMaker: counterParty.isMaker,
	}
	s.authMgr.Request(counterParty.order.User(), notification, func(_ comms.Link, msg *msgjson.Message) {
		s.processAck(msg, ack)
	})
	return dontTryAgain

}

// handleInit handles the 'init' request from a user, which is used to inform
// the DEX of a newly broadcast swap transaction. Most of the work is performed
// by processInit, but the request is parsed and user is authenticated first.
func (s *Swapper) handleInit(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	params := new(msgjson.Init)
	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "Error decoding 'init' method params",
		}
	}

	rpcErr := s.authUser(user, params)
	if rpcErr != nil {
		return rpcErr
	}

	stepInfo, rpcErr := s.step(user, params.MatchID.String())
	if rpcErr != nil {
		return rpcErr
	}

	// Since we have to consider latency, run this as a chainWaiter.
	s.waitMempool(coinWaitRules(user, msg, params.CoinID), func() bool {
		return s.processInit(msg, params, stepInfo)
	})
	return nil
}

// handleRedeem handles the 'redeem' request from a user. Most of the work is
// performed by processRedeem, but the request is parsed and user is
// authenticated first.
func (s *Swapper) handleRedeem(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	params := new(msgjson.Redeem)

	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "Error decoding 'init' method params",
		}
	}

	rpcErr := s.authUser(user, params)
	if rpcErr != nil {
		return rpcErr
	}

	stepInfo, rpcErr := s.step(user, params.MatchID.String())
	if rpcErr != nil {
		return rpcErr
	}

	// Since we have to consider latency, run this as a chainWaiter.
	s.waitMempool(coinWaitRules(user, msg, params.CoinID), func() bool {
		return s.processRedeem(msg, params, stepInfo)
	})
	return nil
}

// revocationRequests prepares a match revocation RPC request for each client.
// Both the request and the *msgjson.RevokeMatchParams are returned, since they
// cannot be accessed directly from the request (json.RawMessage).
func revocationRequests(match *matchTracker) (*msgjson.RevokeMatch, *msgjson.Message, *msgjson.RevokeMatch, *msgjson.Message, error) {
	takerParams := &msgjson.RevokeMatch{
		OrderID: idToBytes(match.Taker.ID()),
		MatchID: idToBytes(match.ID()),
	}
	takerReq, err := msgjson.NewRequest(comms.NextID(), msgjson.RevokeMatchRoute, takerParams)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	makerParams := &msgjson.RevokeMatch{
		OrderID: idToBytes(match.Maker.ID()),
		MatchID: idToBytes(match.ID()),
	}
	makerReq, err := msgjson.NewRequest(comms.NextID(), msgjson.RevokeMatchRoute, makerParams)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return takerParams, takerReq, makerParams, makerReq, nil
}

// revoke revokes the match, sending the 'revoke_match' request to each client
// and processing the acknowledgement. This method is not thread-safe.
func (s *Swapper) revoke(match *matchTracker) {
	takerParams, takerReq, makerParams, makerReq, err := revocationRequests(match)
	if err != nil {
		log.Errorf("error creating revocation requests: %v", err)
		return
	}
	takerAck := &messageAcker{
		user:    match.Taker.User(),
		match:   match,
		params:  takerParams,
		isMaker: false,
	}
	s.authMgr.Request(match.Taker.User(), takerReq, func(_ comms.Link, msg *msgjson.Message) {
		s.processAck(msg, takerAck)
	})
	makerAck := &messageAcker{
		user:    match.Maker.User(),
		match:   match,
		params:  makerParams,
		isMaker: true,
	}
	s.authMgr.Request(match.Maker.User(), makerReq, func(_ comms.Link, msg *msgjson.Message) {
		s.processAck(msg, makerAck)
	})
}

// readMatches translates a slice of raw matches from the market manager into
// a slice of matchTrackers.
func (s *Swapper) readMatches(matchSets []*order.MatchSet) []*matchTracker {
	// The initial capacity guess here is a minimum, but will avoid a few
	// reallocs.
	matches := make([]*matchTracker, 0, len(matchSets))
	for _, matchSet := range matchSets {
		for _, match := range matchSet.Matches() {
			maker := match.Maker
			var makerSwapAsset, takerSwapAsset uint32
			if maker.Sell {
				makerSwapAsset = maker.BaseAsset
				takerSwapAsset = maker.QuoteAsset
			} else {
				makerSwapAsset = maker.QuoteAsset
				takerSwapAsset = maker.BaseAsset
			}
			tNow := time.Now()
			matches = append(matches, &matchTracker{
				Match: match,
				time:  tNow,
				makerStatus: &swapStatus{
					swapAsset: makerSwapAsset,
				},
				takerStatus: &swapStatus{
					swapAsset: takerSwapAsset,
				},
			})
		}
	}
	return matches
}

// matchNotifications creates a pair of msgjson.MatchNotification from a
// matchTracker.
func matchNotifications(match *matchTracker) (makerMsg *msgjson.Match, takerMsg *msgjson.Match) {
	return &msgjson.Match{
			OrderID:  idToBytes(match.Maker.ID()),
			MatchID:  idToBytes(match.ID()),
			Quantity: match.Quantity,
			Rate:     match.Rate,
			Address:  match.Taker.SwapAddress(),
			Time:     uint64(match.time.Unix()),
		}, &msgjson.Match{
			OrderID:  idToBytes(match.Taker.ID()),
			MatchID:  idToBytes(match.ID()),
			Quantity: match.Quantity,
			Rate:     match.Rate,
			Address:  match.Maker.SwapAddress(),
			Time:     uint64(match.time.Unix()),
		}
}

// newMatchAckers creates a pair of matchAckers, which are used to validate each
// user's msgjson.AcknowledgementResult response.
func newMatchAckers(match *matchTracker) (*messageAcker, *messageAcker) {
	makerMsg, takerMsg := matchNotifications(match)
	return &messageAcker{
			user:    match.Maker.User(),
			isMaker: true,
			match:   match,
			params:  makerMsg,
		}, &messageAcker{
			user:    match.Taker.User(),
			isMaker: false,
			match:   match,
			params:  takerMsg,
		}
}

// For the 'match' request, the user returns msgjson.AcknowledgementResult with
// an array of signatures, one for each match sent.
func (s *Swapper) processMatchAcks(user account.AccountID, msg *msgjson.Message, matches []*messageAcker) {
	var acks []msgjson.Acknowledgement
	resp, err := msg.Response()
	if err != nil {
		s.respondError(msg.ID, user, msgjson.RPCParseError,
			fmt.Sprintf("error parsing match acknowledgment response: %v", err))
		return
	}
	err = json.Unmarshal(resp.Result, &acks)
	if err != nil {
		s.respondError(msg.ID, user, msgjson.RPCParseError,
			fmt.Sprintf("error parsing match notification acknowledgment: %v", err))
		return
	}
	if len(matches) != len(acks) {
		s.respondError(msg.ID, user, msgjson.AckCountError,
			fmt.Sprintf("expected %d acknowledgements, got %d", len(acks), len(matches)))
		return
	}
	s.matchMtx.Lock()
	defer s.matchMtx.Unlock()
	for i, matchInfo := range matches {
		ack := acks[i]
		if ack.MatchID != matchInfo.match.ID().String() {
			s.respondError(msg.ID, user, msgjson.IDMismatchError,
				fmt.Sprintf("unexpected match ID at acknowledgment index %d", i))
			return
		}
		sigBytes, err := hex.DecodeString(ack.Sig)
		if err != nil {
			s.respondError(msg.ID, user, msgjson.SignatureError,
				fmt.Sprintf("error decoding signature %s: %v", ack.Sig, err))
			return
		}
		sigMsg, err := matchInfo.params.Serialize()
		if err != nil {
			s.respondError(msg.ID, user, msgjson.SerializationError,
				fmt.Sprintf("unable to serialize match params: %v", err))
			return
		}
		err = s.authMgr.Auth(user, sigMsg, sigBytes)
		if err != nil {
			s.respondError(msg.ID, user, msgjson.SignatureError,
				fmt.Sprintf("signature validation error: %v", err))
			return
		}
		if matchInfo.isMaker {
			matchInfo.match.Sigs.MakerMatch = sigBytes
		} else {
			matchInfo.match.Sigs.TakerMatch = sigBytes
		}
	}
}

// Negotiate takes ownership of the matches and begins swap negotiation.
func (s *Swapper) Negotiate(matchSets []*order.MatchSet) {
	matches := s.readMatches(matchSets)
	userMatches := make(map[account.AccountID][]*messageAcker)
	// A helper method for adding to the userMatches map.
	addUserMatch := func(acker *messageAcker) {
		s.authMgr.Sign(acker.params)
		l := userMatches[acker.user]
		userMatches[acker.user] = append(l, acker)
	}
	// Setting length to max possible, which is over-allocating by the number of
	// cancels.
	toMonitor := make([]*matchTracker, 0, len(matches))
	for _, match := range matches {
		if match.Taker.Type() == order.CancelOrderType {
			// If this is a cancellation, there is nothing to track.
			err := s.storage.CancelOrder(match.Maker)
			if err != nil {
				log.Errorf("Failed to cancel order %v", match.Maker)
				// If the maker failed to cancel in storage, we should NOT tell
				// the users that the cancel order was executed.
				// TODO: send a error message to the clients.
				continue
			}
		} else {
			toMonitor = append(toMonitor, match)
		}

		makerAck, takerAck := newMatchAckers(match)
		addUserMatch(makerAck)
		addUserMatch(takerAck)

		// Record the match.
		// TODO: decide if we can reasonably continue if the storage call fails.
		s.saveMatch(match.Match)
	}

	// Add the matches to the map.
	s.matchMtx.Lock()
	for _, match := range toMonitor {
		s.matches[match.ID().String()] = match
	}
	s.matchMtx.Unlock()

	// Send the user notifications.
	for user, matches := range userMatches {
		msgs := make([]msgjson.Signable, 0, len(matches))
		for _, m := range matches {
			msgs = append(msgs, m.params)
		}
		req, err := msgjson.NewRequest(comms.NextID(), msgjson.MatchRoute, msgs)
		if err != nil {
			log.Errorf("error creating match notification request: %v", err)
			continue
		}
		{
			m := matches
			u := user
			s.authMgr.Request(user, req, func(_ comms.Link, msg *msgjson.Message) {
				s.processMatchAcks(u, msg, m)
			})
		}
	}
}

func idToBytes(id [order.OrderIDSize]byte) []byte {
	return id[:]
}
