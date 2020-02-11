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

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/coinwaiter"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
)

var (
	// The coin waiter will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the Swapper will wait for a coin waiter.
	// This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = time.Minute

	requestExpiration = 30 * time.Second

	// nop is a dummy expire callback function.
	nop = func() {}
)

func unixMsNow() time.Time {
	return time.Now().Truncate(time.Millisecond).UTC()
}

// The AuthManager handles client-related actions, including authorization and
// communications.
type AuthManager interface {
	Route(string, func(account.AccountID, *msgjson.Message) *msgjson.Error)
	Auth(user account.AccountID, msg, sig []byte) error
	Sign(...msgjson.Signable) error
	Send(account.AccountID, *msgjson.Message)
	Request(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message)) error
	RequestWithTimeout(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message), time.Duration, func()) error
	Penalize(account.AccountID, account.Rule)
}

// Storage updates match data in what is presumably a database.
type Storage interface {
	LastErr() error
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

// A blockNotification is used internally when an asset.Backend reports a new
// block.
type blockNotification struct {
	time    time.Time
	height  uint32
	assetID uint32
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
	asset *asset.BackedAsset
	// isBaseAsset will be true if the current step involves a transaction on the
	// match market's base asset blockchain, false if on quote asset's blockchain.
	isBaseAsset bool
	step        order.MatchStatus
	nextStep    order.MatchStatus
	// checkVal holds the trade amount in units of the currently acting asset,
	// and is used to validate the swap transaction details.
	checkVal uint64
}

// LockableAsset pairs an Asset with a CoinLocker.
type LockableAsset struct {
	*asset.BackedAsset
	coinlock.CoinLocker // should be *coinlock.AssetCoinLocker
}

// Config is the swapper configuration settings. A Config instance is the only
// argument to the Swapper constructor.
type Config struct {
	// Assets is a map to all the asset information, including the asset backends,
	// used by this Swapper.
	Assets map[uint32]*LockableAsset
	// Mgr is the AuthManager for client messaging and authentication.
	AuthManager AuthManager
	// A database backend.
	Storage Storage
	// BroadcastTimeout is how long the Swapper will wait for expected swap
	// transactions following new blocks.
	BroadcastTimeout time.Duration
}

// Swapper handles order matches by handling authentication and inter-party
// communications between clients, or 'users'. The Swapper authenticates users
// (vua AuthManager) and validates transactions as they are reported.
type Swapper struct {
	// coins is a map to all the Asset information, including the asset backends,
	// used by this Swapper.
	coins map[uint32]*LockableAsset
	// storage is a Database backend.
	storage Storage
	// authMgr is an AuthManager for client messaging and authentication.
	authMgr AuthManager
	// The matches map and the contained matches are protected by the matchMtx.
	matchMtx sync.RWMutex
	matches  map[string]*matchTracker
	// Each asset backend gets a goroutine to monitor for new blocks. When a new
	// block is received, it is passed to the start loop through the block
	// channel.
	block chan *blockNotification
	// The broadcast timeout.
	bTimeout time.Duration
	// coinWaiter is a coinwaiter.Waiter to deal with latency.
	coinWaiter *coinwaiter.Waiter
}

// NewSwapper is a constructor for a Swapper.
func NewSwapper(cfg *Config) *Swapper {
	authMgr := cfg.AuthManager
	swapper := &Swapper{
		coins:      cfg.Assets,
		storage:    cfg.Storage,
		authMgr:    authMgr,
		coinWaiter: coinwaiter.New(recheckInterval, authMgr.Send),
		matches:    make(map[string]*matchTracker),
		block:      make(chan *blockNotification, 8),
		bTimeout:   cfg.BroadcastTimeout,
	}

	// The swapper is only concerned with two types of client-originating
	// method requests.
	authMgr.Route(msgjson.InitRoute, swapper.handleInit)
	authMgr.Route(msgjson.RedeemRoute, swapper.handleRedeem)

	return swapper
}

// Run is the main Swapper loop.
func (s *Swapper) Run(ctx context.Context) {
	ctxSwap, cancel := context.WithCancel(ctx)
	defer cancel()

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

	// Start a listen loop for each asset's block channel.
	var wg sync.WaitGroup
	for assetID, asset := range s.coins {
		wg.Add(1)
		go func(assetID uint32, blockSource <-chan uint32) {
			defer wg.Done()
		out:
			for {
				select {
				case h := <-blockSource:
					s.block <- &blockNotification{
						time:    time.Now().UTC(),
						height:  h,
						assetID: assetID,
					}
				case <-ctxSwap.Done():
					break out
				}
			}
		}(assetID, asset.Backend.BlockChannel(5))
	}

	wg.Add(1)
	go func() {
		s.coinWaiter.Run(ctxSwap)
		wg.Done()
	}()

out:
	for {
		select {
		case block := <-s.block:
			// Schedule a check of matches with one side equal to this block's asset
			// by appending the block to the bcastTriggers.
			bcastTriggers = append(bcastTriggers, block)
			// processBlock will update confirmation times in the swapStatus structs.
			s.processBlock(block)
		case <-bcastTicker.C:
			for {
				// checkInaction will fail if the DB is failing. TODO: Consider
				// a mechanism to complete existing swaps and then quit.
				// Presently Negotiate denies new swaps and processInit responds
				// with an error to clients attempting to start a swap.
				if err := s.storage.LastErr(); err != nil {
					cancel()
					break out
				}

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
					break
				} else {
					setTimeout(bcastTriggers[0])
				}
			}
		case <-ctxSwap.Done():
			break out
		}
	}

	wg.Wait()
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
		status.swapConfirmed = time.Now().UTC()
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
	s.matchMtx.Lock()
	defer s.matchMtx.Unlock()
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
			makerRedeemed, takerRedeemed := s.redeemStatus(match)
			// TODO: Can coins be unlocked now regardless of redemption?
			if makerRedeemed && takerRedeemed {
				completions = append(completions, match)
			}
		}
	}

	for _, match := range completions {
		s.unlockOrderCoins(match.Taker)
		s.unlockOrderCoins(match.Maker)
		// Remove the completed match. Note that checkInaction may also remove
		// matches, so this entire function must lock even if there are no
		// completions.
		delete(s.matches, match.ID().String())
	}
}

func (s *Swapper) redeemStatus(match *matchTracker) (makerRedeemComplete, takerRedeemComplete bool) {
	makerRedeemComplete = s.makerRedeemStatus(match)
	tStatus := match.takerStatus
	// Taker is only complete if the maker is complete because
	// order.MatchComplete follows order.MakerRedeemed.
	if makerRedeemComplete && !tStatus.redeemTime.IsZero() {
		confs, err := tStatus.redemption.Confirmations()
		if err != nil {
			log.Errorf("Confirmations failed for taker redemption %v: err",
				tStatus.redemption.TxID(), err)
			return
		}
		takerRedeemComplete = confs >= int64(s.coins[match.makerStatus.swapAsset].SwapConf)
	}
	return
}

func (s *Swapper) makerRedeemStatus(match *matchTracker) (makerRedeemComplete bool) {
	mStatus, tStatus := match.makerStatus, match.takerStatus
	if !mStatus.redeemTime.IsZero() {
		confs, err := mStatus.redemption.Confirmations()
		if err != nil {
			log.Errorf("Confirmations failed for maker redemption %v: err",
				mStatus.redemption.TxID(), err) // Severity?
			return
		}
		makerRedeemComplete = confs >= int64(s.coins[tStatus.swapAsset].SwapConf)
	}
	return
}

// TxMonitored determines whether the transaction for the given user is involved
// in a DEX-monitored trade. Note that the swap contract tx is considered
// monitored until the swap is complete, regardless of confirms. This allows
// change outputs from a dex-monitored swap contract to be used to fund
// additional swaps prior to FundConf. e.g. OrderRouter may allow coins to fund
// orders where: (coins.confs >= FundConf) OR TxMonitored(coins.tx).
func (s *Swapper) TxMonitored(user account.AccountID, asset uint32, txid string) bool {
	s.matchMtx.RLock()
	defer s.matchMtx.RUnlock()

	for _, match := range s.matches {
		// The swap contract of either the maker or taker must correspond to
		// specified asset to be of interest.
		switch asset {
		case match.makerStatus.swapAsset:
			// Maker's swap transaction is the asset of interest.
			if user == match.Maker.User() && match.makerStatus.swap.TxID() == txid {
				// The swap contract tx is considered monitored until the swap
				// is complete, regardless of confirms.
				return true
			}

			// Taker's redemption transaction is the asset of interest.
			_, takerRedeemDone := s.redeemStatus(match)
			if !takerRedeemDone && user == match.Taker.User() &&
				match.takerStatus.redemption.TxID() == txid {
				return true
			}
		case match.takerStatus.swapAsset:
			// Taker's swap transaction is the asset of interest.
			if user == match.Taker.User() && match.takerStatus.swap.TxID() == txid {
				// The swap contract tx is considered monitored until the swap
				// is complete, regardless of confirms.
				return true
			}

			// Maker's redemption transaction is the asset of interest.
			makerRedeemDone := s.makerRedeemStatus(match)
			if !makerRedeemDone && user == match.Maker.User() &&
				match.makerStatus.redemption.TxID() == txid {
				return true
			}
		default:
			continue
		}
	}

	return false
}

// checkInaction scans the swapStatus structures relevant to the specified
// asset. If a client is found to have not acted when required, a match may be
// revoked and a penalty assigned to the user.
func (s *Swapper) checkInaction(assetID uint32) {
	// If the DB is failing, do not penalize or attempt to start revocations.
	if err := s.storage.LastErr(); err != nil {
		log.Errorf("DB in failing state.")
		return
	}

	oldestAllowed := time.Now().Add(-s.bTimeout).UTC()
	var deletions []string
	s.matchMtx.Lock()
	defer s.matchMtx.Unlock()
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
				s.authMgr.Penalize(match.Maker.User(), account.FailureToAct)
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
				s.authMgr.Penalize(match.Taker.User(), account.FailureToAct)
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
				s.authMgr.Penalize(match.Maker.User(), account.FailureToAct)
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
				s.authMgr.Penalize(match.Taker.User(), account.FailureToAct)
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
		asset:        s.coins[actor.swapAsset].BackedAsset,
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
	ack := new(msgjson.Acknowledgement)
	err := msg.UnmarshalResult(ack)
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

// processInit processes the `init` RPC request, which is used to inform the
// DEX of a newly broadcast swap transaction. Once the transaction is seen and
// and audited by the Swapper, the counter-party is informed with an 'audit'
// request. This method is run as a coin waiter.
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
		return coinwaiter.TryAgain
	}
	// Decode the contract and audit the contract.
	recipient, val, err := coin.AuditContract()
	if err != nil {
		s.respondError(msg.ID, actor.user, msgjson.ContractError, fmt.Sprintf("error auditing contract: %v", err))
		return coinwaiter.DontTryAgain
	}
	if recipient != counterParty.order.Trade().SwapAddress() {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("incorrect recipient. expected %s. got %s", recipient, counterParty.order.Trade().SwapAddress()))
		return coinwaiter.DontTryAgain
	}
	if val != stepInfo.checkVal {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("contract error. expected contract value to be %d, got %d", stepInfo.checkVal, val))
		return coinwaiter.DontTryAgain
	}

	// Update the match.
	swapTime := unixMsNow()
	s.matchMtx.Lock()
	matchID := stepInfo.match.ID()
	actor.status.swap = coin
	actor.status.swapTime = swapTime
	stepInfo.match.Status = stepInfo.nextStep
	match := stepInfo.match.Match
	s.matchMtx.Unlock()

	// Store or update the match in the Swapper's storage backend. Failure to
	// update match status is a fatal DB error. If we continue, the swap may
	// succeed, but there will be no way to recover or retrospectively determine
	// the swap outcome. Abort.
	if err := s.storage.UpdateMatch(match); err != nil {
		log.Errorf("UpdateMatch (id=%v) failed: %v", match.ID(), err)
		s.respondError(msg.ID, actor.user, msgjson.UnknownMarketError,
			fmt.Sprintf("internal server error"))
		return coinwaiter.DontTryAgain // fatal
	}

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
		Time:     uint64(encode.UnixMilli(swapTime)),
		Contract: params.Contract,
	}
	notification, err := msgjson.NewRequest(comms.NextID(), msgjson.AuditRoute, auditParams)
	if err != nil {
		// This is likely an impossibly condition.
		log.Errorf("error creating audit request: %v", err)
		return coinwaiter.DontTryAgain
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
	return coinwaiter.DontTryAgain
}

// processRedeem processes a 'redeem' request from a client. processRedeem does
// not perform user authentication, which is handled in handleRedeem before
// processRedeem is invoked. This method is run as a coin waiter.
func (s *Swapper) processRedeem(msg *msgjson.Message, params *msgjson.Redeem, stepInfo *stepInformation) bool {
	// Get the transaction
	actor, counterParty := stepInfo.actor, stepInfo.counterParty
	chain := stepInfo.asset.Backend
	coin, err := chain.Coin(params.CoinID, nil)
	// If there is an error, don't return an error yet, since it could be due to
	// network latency. Instead, queue it up for another check.
	if err != nil {
		return coinwaiter.TryAgain
	}
	// Make sure that the expected output is being spent.
	status := counterParty.status
	spends, err := coin.SpendsCoin(status.swap.ID())
	if err != nil {
		s.respondError(msg.ID, actor.user, msgjson.RedemptionError,
			fmt.Sprintf("error checking redemption %x: %v", status.swap.ID(), err))
		return coinwaiter.DontTryAgain
	}
	if !spends {
		s.respondError(msg.ID, actor.user, msgjson.RedemptionError,
			fmt.Sprintf("redemption does not spend %x", status.swap.ID()))
		return coinwaiter.DontTryAgain
	}

	// Modify the match's swapStatuses.
	swapTime := unixMsNow()
	s.matchMtx.Lock()
	matchID := stepInfo.match.ID()
	// Update the match.
	actor.status.redemption = coin
	actor.status.redeemTime = swapTime
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
		Time:    uint64(encode.UnixMilli(swapTime)),
	}

	notification, err := msgjson.NewRequest(comms.NextID(), msgjson.RedemptionRoute, rParams)
	if err != nil {
		log.Errorf("error creating redemption request: %v", err)
		return coinwaiter.DontTryAgain
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
	return coinwaiter.DontTryAgain

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

	// Since we have to consider latency, run this as a coin waiter.
	s.coinWaiter.Wait(coinwaiter.NewSettings(user, msg, params.CoinID, txWaitExpiration), func() bool {
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

	// Since we have to consider latency, run this as a coin waiter.
	s.coinWaiter.Wait(coinwaiter.NewSettings(user, msg, params.CoinID, txWaitExpiration), func() bool {
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
	// Unlock the maker and taker order coins.
	s.unlockOrderCoins(match.Taker)
	s.unlockOrderCoins(match.Maker)

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
	nowMs := unixMsNow()
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

			matches = append(matches, &matchTracker{
				Match: match,
				time:  nowMs,
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

// extractAddress extracts the address from the order. If the order is a cancel
// order, an empty string is returned.
func extractAddress(ord order.Order) string {
	trade := ord.Trade()
	if trade == nil {
		return ""
	}
	return trade.Address
}

// matchNotifications creates a pair of msgjson.MatchNotification from a
// matchTracker.
func matchNotifications(match *matchTracker) (makerMsg *msgjson.Match, takerMsg *msgjson.Match) {
	return &msgjson.Match{
			OrderID:  idToBytes(match.Maker.ID()),
			MatchID:  idToBytes(match.ID()),
			Quantity: match.Quantity,
			Rate:     match.Rate,
			Address:  extractAddress(match.Taker),
		}, &msgjson.Match{
			OrderID:  idToBytes(match.Taker.ID()),
			MatchID:  idToBytes(match.ID()),
			Quantity: match.Quantity,
			Rate:     match.Rate,
			Address:  extractAddress(match.Maker),
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
	err := msg.UnmarshalResult(&acks)
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

// LockOrdersCoins locks the backing coins for the provided orders.
func (s *Swapper) LockOrdersCoins(orders []order.Order) {
	// Separate orders according to the asset of their locked coins.
	assetCoinOrders := make(map[uint32][]order.Order)
	for _, ord := range orders {
		// Identify the asset of the locked coins.
		asset := ord.Quote()
		if ord.Trade().Sell {
			asset = ord.Base()
		}
		assetCoinOrders[asset] = append(assetCoinOrders[asset], ord)
	}

	for asset, orders := range assetCoinOrders {
		s.lockOrdersCoins(asset, orders)
	}
}

func (s *Swapper) lockOrdersCoins(asset uint32, orders []order.Order) {
	assetLock := s.coins[asset]
	if assetLock == nil {
		panic(fmt.Sprintf("Unable to lock coins for asset %d", asset))
	}

	assetLock.LockOrdersCoins(orders)
}

// LockCoins locks coins of a given asset. The OrderID is used for tracking.
func (s *Swapper) LockCoins(asset uint32, coins map[order.OrderID][]order.CoinID) {
	assetLock := s.coins[asset]
	if assetLock == nil {
		panic(fmt.Sprintf("Unable to lock coins for asset %d", asset))
	}

	assetLock.LockCoins(coins)
}

// unlockOrderCoins is not exported since only the Swapper knows when to unlock
// coins (when swaps are completed).
func (s *Swapper) unlockOrderCoins(ord order.Order) {
	asset := ord.Quote()
	if ord.Trade().Sell {
		asset = ord.Base()
	}

	s.unlockOrderIDCoins(asset, ord.ID())
}

// unlockOrderIDCoins is not exported since only the Swapper knows when to unlock
// coins (when swaps are completed).
func (s *Swapper) unlockOrderIDCoins(asset uint32, oid order.OrderID) {
	assetLock := s.coins[asset]
	if assetLock == nil {
		panic(fmt.Sprintf("Unable to lock coins for asset %d", asset))
	}

	assetLock.UnlockOrderCoins(oid)
}

// Negotiate takes ownership of the matches and begins swap negotiation.
func (s *Swapper) Negotiate(matchSets []*order.MatchSet) {
	// Set up the matchTrackers, which includes a slice of Matches.
	matches := s.readMatches(matchSets)

	// Record the matches. If any DB updates fail, no swaps proceed. We could
	// let the others proceed, but that could seem selective trickery to the
	// clients.
	for _, match := range matches {
		if err := s.storage.UpdateMatch(match.Match); err != nil {
			log.Errorf("UpdateMatch (match id=%v) failed: %v", match.ID(), err)
			// TODO: notify clients (notification or response to what?)
			// abortAll()
			return
		}
	}

	userMatches := make(map[account.AccountID][]*messageAcker)
	// addUserMatch signs a match notification message, and add the data
	// required to process the acknowledgment to the userMatches map.
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
			// If this is a cancellation, there is nothing to track. Just cancel
			// the target order by removing it from the DB. It is already
			// removed from book by the Market.
			err := s.storage.CancelOrder(match.Maker)
			if err != nil {
				log.Errorf("Failed to cancel order %v", match.Maker)
				// If the DB update failed, the target order status was not
				// updated, but removed from the in-memory book. This is
				// potentially a critical failure since the dex will restore the
				// book from the DB. TODO: Notify clients.
				return
			}
		} else {
			toMonitor = append(toMonitor, match)
		}

		makerAck, takerAck := newMatchAckers(match)
		addUserMatch(makerAck)
		addUserMatch(takerAck)
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
