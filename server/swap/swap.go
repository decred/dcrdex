// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/auth"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
)

var (
	// The coin waiter will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the Swapper will wait for a coin waiter.
	// This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = time.Minute
)

func unixMsNow() time.Time {
	return time.Now().Truncate(time.Millisecond).UTC()
}

// AuthManager handles client-related actions, including authorization and
// communications.
type AuthManager interface {
	Route(string, func(account.AccountID, *msgjson.Message) *msgjson.Error)
	Auth(user account.AccountID, msg, sig []byte) error
	Sign(...msgjson.Signable) error
	Send(account.AccountID, *msgjson.Message) error
	SendWhenConnected(account.AccountID, *msgjson.Message, time.Duration, func())
	Request(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message)) error
	RequestWithTimeout(user account.AccountID, req *msgjson.Message, handlerFunc func(comms.Link, *msgjson.Message),
		expireTimeout time.Duration, expireFunc func()) error
	RequestWhenConnected(user account.AccountID, req *msgjson.Message, handlerFunc func(comms.Link, *msgjson.Message),
		expireTimeout, connectTimeout time.Duration, expireFunc func())
	Penalize(account.AccountID, account.Rule)
	RecordCancel(user account.AccountID, oid, target order.OrderID, t time.Time)
	RecordCompletedOrder(user account.AccountID, oid order.OrderID, t time.Time)
}

// Storage updates match data in what is presumably a database.
type Storage interface {
	db.SwapArchiver
	LastErr() error
	Fatal() <-chan struct{}
	InsertMatch(match *order.Match) error
	CancelOrder(*order.LimitOrder) error
	RevokeOrder(order.Order) (cancelID order.OrderID, t time.Time, err error)
	SetOrderCompleteTime(ord order.Order, compTimeMs int64) error
}

// swapStatus is information related to the completion or incompletion of each
// sequential step of the atomic swap negotiation process. Each user has their
// own swapStatus.
type swapStatus struct {
	// The asset to which the user broadcasts their swap transaction.
	swapAsset   uint32
	redeemAsset uint32

	mtx sync.RWMutex
	// The time that the swap coordinator sees the transaction.
	swapTime time.Time
	swap     asset.Contract
	// The time that the transaction receives its SwapConf'th confirmation.
	swapConfirmed time.Time
	// The time that the swap coordinator sees the user's redemption
	// transaction.
	redeemTime time.Time
	redemption asset.Coin
}

// matchTracker embeds an order.Match and adds some data necessary for tracking
// the match negotiation.
type matchTracker struct {
	mtx sync.RWMutex // Match.Sigs and Match.Status
	*order.Match
	time        time.Time
	matchTime   time.Time
	makerStatus *swapStatus
	takerStatus *swapStatus
}

// A blockNotification is used internally when an asset.Backend reports a new
// block.
type blockNotification struct {
	time    time.Time
	assetID uint32
	err     error
}

// A stepActor is a structure holding information about one party of a match.
// stepActor is used with the stepInformation structure, which is used for
// sequencing swap negotiation.
type stepActor struct {
	user account.AccountID
	// swapAsset is the asset to which this actor broadcasts their swap tx.
	swapAsset uint32
	isMaker   bool
	order     order.Order
	// The swapStatus from the Match. Could be either the
	// (matchTracker).makerStatus or (matchTracker).takerStatus, depending on who
	// this actor is.
	status *swapStatus
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

type orderSwapStat struct {
	SwapCount int
	OffBook   bool
	HasFailed bool
}

// orderSwapTracker facilitates cancellation ratio computation without complex,
// costly, and frequent DB queries.
type orderSwapTracker struct {
	mtx          sync.Mutex
	orderMatches map[order.OrderID]*orderSwapStat
}

func newOrderSwapTracker() *orderSwapTracker {
	return &orderSwapTracker{
		orderMatches: make(map[order.OrderID]*orderSwapStat),
	}
}

// decrementActiveSwapCount decrements the number of active swaps for an order,
// returning a boolean indicating if the order is now considered complete, where
// complete means there are no more active swaps, the order is off-book, and the
// user is not responsible for any swap failures or premature unbooking of the
// order. If the number of active swaps is reduced to zero, the order is removed
// from the tracker, and repeated calls to decrementActiveSwapCount for the
// order will return true.
func (s *orderSwapTracker) decrementActiveSwapCount(ord order.Order, failed bool) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	oid := ord.ID()
	stat := s.orderMatches[oid]
	if stat == nil {
		log.Warnf("isOrderComplete: untracked order %v", oid)
		return true
	}

	stat.HasFailed = stat.HasFailed || failed

	stat.SwapCount--
	if stat.SwapCount != 0 {
		return false
	}

	// Remove the order's entry only if:
	// - it has never failed OR
	// - it is off-book, meaning it will never be seen again
	if !stat.HasFailed || stat.OffBook {
		delete(s.orderMatches, oid)
	}

	return stat.OffBook && !stat.HasFailed
}

// swapSuccess decrements the active swap counter for an order. The return value
// indicates if the order is considered successfully complete, which is a status
// that precludes cancellation of the order, or failure of any swaps involving
// the order on account of the user's (in)action. The order's failure and
// off-book flags are unchanged.
func (s *orderSwapTracker) swapSuccess(ord order.Order) bool {
	return s.decrementActiveSwapCount(ord, false)
}

// swapFailure decrements the active swap counter for an order, and flags the
// order as having failed. The off-book flag is unchanged.
func (s *orderSwapTracker) swapFailure(ord order.Order) {
	s.decrementActiveSwapCount(ord, true)
}

// incActiveSwapCount registers a new swap for the given order, flagging the
// order according to offBook.
func (s *orderSwapTracker) incActiveSwapCount(ord order.Order, offBook bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	oid := ord.ID()
	stat := s.orderMatches[oid]
	if stat == nil {
		s.orderMatches[oid] = &orderSwapStat{
			SwapCount: 1,
			OffBook:   offBook,
		}
		return
	}

	stat.SwapCount++
	stat.OffBook = offBook // should never change true to false
}

// canceled records an order as off-book and failed if it exists in the active
// order swaps map. No new entry is made because there cannot be future swaps
// for a canceled order. For this reason, be sure to use incActiveSwapCount
// before canceled when processing new matches in Negotiate.
func (s *orderSwapTracker) canceled(ord order.Order) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	oid := ord.ID()
	stat := s.orderMatches[oid]
	if stat == nil {
		// No active swaps for canceled order, OK.
		log.Debugf("orderOffBook: untracked order %v", oid)
		return
	}

	// Prevent completion of active swaps from counting the order as
	// successfully completed.
	stat.OffBook = true
	stat.HasFailed = true
}

// Swapper handles order matches by handling authentication and inter-party
// communications between clients, or 'users'. The Swapper authenticates users
// (vua AuthManager) and validates transactions as they are reported.
type Swapper struct {
	// Where state is dumped on shutdown.
	dataDir string
	// coins is a map to all the Asset information, including the asset backends,
	// used by this Swapper.
	coins map[uint32]*LockableAsset
	// storage is a Database backend.
	storage Storage
	// authMgr is an AuthManager for client messaging and authentication.
	authMgr AuthManager
	// unbookHook is a callback the the Swapper's controller (e.g. DEX manager)
	// for removing an order from the book. This should be called when a swap is
	// revoked due to failure of the order's owner to complete the necessary
	// actions.
	unbookHook func(lo *order.LimitOrder) bool
	// The matches map and the contained matches are protected by the matchMtx.
	matchMtx sync.RWMutex
	matches  map[order.MatchID]*matchTracker
	// orders tracks order status and active swaps.
	orders *orderSwapTracker
	// The broadcast timeout.
	bTimeout time.Duration
	// Expected locktimes for maker and taker swaps.
	lockTimeTaker time.Duration
	lockTimeMaker time.Duration
	// latencyQ is a queue for coin waiters to deal with network latency.
	latencyQ *wait.TickerQueue
	// gracePeriod is a flag that indicates how long clients have to respond to
	// requests before being penalized. A value of 1 indicates that the connect
	// timeout should be used, while 0 indicates the shorter response timeout.
	gracePeriod int32

	// handlerMtx should be read-locked for the duration of the comms route
	// handlers (handleInit and handleRedeem) and Negotiate. This blocks
	// shutdown until any coin waiters are registered with latencyQ. It should
	// be write-locked before setting the stop flag.
	handlerMtx sync.RWMutex
	// stop is used to prevent new handlers from starting coin waiters. It is
	// set to true during shutdown of Run.
	stop bool

	// liveWaiters is used to track active coin waiters running in latencyQ.
	liveWaitersMtx sync.Mutex
	liveWaiters    map[waiterKey]*handlerArgs

	// liveAckers is used to track acknowledgement requests that are waiting to
	// be met with client responses. This applies to counterparty contract audit
	// and redeem acks, and match acks.
	liveAckersMtx sync.Mutex
	liveAckers    map[uint64]*msgAckers // keyed by server generated message ID
}

// Config is the swapper configuration settings. A Config instance is the only
// argument to the Swapper constructor.
type Config struct {
	// DataDir is the folder where swap state is stored.
	DataDir string
	// State is the state to restore in construction.
	State *State
	// AllowPartialRestore indicates if it is acceptable to load only part of
	// the State if the Swapper's asset configuration lacks assets required to
	// load all elements of State.
	AllowPartialRestore bool
	// Assets is a map to all the asset information, including the asset backends,
	// used by this Swapper.
	Assets map[uint32]*LockableAsset
	// AuthManager is the auth manager for client messaging and authentication.
	AuthManager AuthManager
	// A database backend.
	Storage Storage
	// BroadcastTimeout is how long the Swapper will wait for expected swap
	// transactions following new blocks.
	BroadcastTimeout time.Duration
	// LockTimeTaker is the locktime Swapper will use for auditing taker swaps.
	LockTimeTaker time.Duration
	// LockTimeTaker is the locktime Swapper will use for auditing maker swaps.
	LockTimeMaker time.Duration
	UnbookHook    func(lo *order.LimitOrder) bool
}

// NewSwapper is a constructor for a Swapper.
func NewSwapper(cfg *Config) (*Swapper, error) {
	// Verify the directory where swap state will be saved.
	inf, err := os.Stat(cfg.DataDir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("data folder %q does not exist", cfg.DataDir)
	}
	if !inf.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", cfg.DataDir)
	}

	for _, asset := range cfg.Assets {
		if asset.MaxFeeRate == 0 {
			return nil, fmt.Errorf("max fee rate of 0 is invalid for asset %q", asset.Symbol)
		}
	}

	authMgr := cfg.AuthManager
	swapper := &Swapper{
		dataDir:       cfg.DataDir,
		coins:         cfg.Assets,
		storage:       cfg.Storage,
		authMgr:       authMgr,
		unbookHook:    cfg.UnbookHook,
		latencyQ:      wait.NewTickerQueue(recheckInterval),
		matches:       make(map[order.MatchID]*matchTracker),
		orders:        newOrderSwapTracker(),
		bTimeout:      cfg.BroadcastTimeout,
		lockTimeTaker: cfg.LockTimeTaker,
		lockTimeMaker: cfg.LockTimeMaker,
		liveWaiters:   make(map[waiterKey]*handlerArgs),
		liveAckers:    make(map[uint64]*msgAckers),
	}

	if cfg.State != nil {
		err = swapper.restoreState(cfg.State, cfg.AllowPartialRestore)
		if err != nil {
			return nil, err
		}
	}

	// The swapper is only concerned with two types of client-originating
	// method requests.
	authMgr.Route(msgjson.InitRoute, swapper.handleInit)
	authMgr.Route(msgjson.RedeemRoute, swapper.handleRedeem)

	return swapper, nil
}

func (s *Swapper) restoreState(state *State, allowPartial bool) error {
	// State binary version check should be done when State is loaded.

	// Check that the assets required by State are included
	missingAssets := make(map[uint32]bool)
	for _, id := range state.Assets {
		if s.coins[id] == nil {
			if !allowPartial {
				return fmt.Errorf("Unable to find backend for asset %d in restore state.", id)
			}
			log.Warnf("Unable to find backend for asset %d in restore state.", id)
			missingAssets[id] = true
		}
	}

	// Load the matchTrackers, calling the Contract and Redemption asset.Backend
	// methods as needed.

	translateSwapStatus := func(ss *swapStatus, ssd *swapStatusData) error {
		swapCoin := ssd.ContractCoinOut
		if len(swapCoin) > 0 {
			assetID := ssd.SwapAsset
			swap, err := s.coins[assetID].Backend.Contract(swapCoin, ssd.ContractScript)
			if err != nil {
				return fmt.Errorf("unable to find swap out coin %x for asset %d: %v", swapCoin, assetID, err)
			}
			ss.swap = swap
			ss.swapTime = encode.UnixTimeMilli(ssd.SwapTime)
		}

		if ssd.SwapConfirmTime != 0 {
			ss.swapConfirmed = encode.UnixTimeMilli(ssd.SwapConfirmTime)
		}

		if redeemCoin := ssd.RedeemCoinIn; len(redeemCoin) > 0 {
			assetID := ssd.RedeemAsset
			redeem, err := s.coins[assetID].Backend.Redemption(redeemCoin, swapCoin)
			if err != nil {
				return fmt.Errorf("unable to find redeem in coin %x for asset %d: %v", redeemCoin, assetID, err)
			}
			ss.redemption = redeem
			ss.redeemTime = encode.UnixTimeMilli(ssd.RedeemTime)
		}

		return nil
	}

	s.matches = make(map[order.MatchID]*matchTracker, len(state.MatchTrackers))
	for mid, mtd := range state.MatchTrackers {
		// Check and skip matches for missing assets.
		makerSwapAsset := mtd.MakerStatus.SwapAsset
		makerRedeemAsset := mtd.MakerStatus.RedeemAsset
		if missingAssets[makerSwapAsset] {
			log.Infof("Skipping match %v with missing asset %d", mid, makerSwapAsset)
			continue
		}
		if missingAssets[makerRedeemAsset] {
			log.Infof("Skipping match %v with missing asset %d", mid, makerRedeemAsset)
			continue
		}

		mt := &matchTracker{
			Match:     mtd.Match,
			time:      encode.UnixTimeMilli(mtd.Time),
			matchTime: mtd.Match.Epoch.End(),
			makerStatus: &swapStatus{
				swapAsset:   makerSwapAsset,
				redeemAsset: makerRedeemAsset,
			},
			takerStatus: &swapStatus{
				swapAsset:   makerRedeemAsset, // mtd.TakerStatus.SwapAsset
				redeemAsset: makerSwapAsset,   // mtd.TakerStatus.RedeemAsset
			},
		}

		if err := translateSwapStatus(mt.makerStatus, mtd.MakerStatus); err != nil {
			log.Errorf("Loading match %v failed: %v", mtd.Match.ID(), err)
			continue
		}
		if err := translateSwapStatus(mt.takerStatus, mtd.TakerStatus); err != nil {
			log.Errorf("Loading match %v failed: %v", mtd.Match.ID(), err)
			continue
		}

		s.matches[mid] = mt
	}

	// Order completion/failure tracking data
	s.orders.orderMatches = state.OrderMatches

	// Live ackers (active acknowledgement requests)
	//
	// Resend the RequestWithTimeout(..., processAck) for each acker as done in
	// processRedeem (RedemptionRoute request), processInit (AuditRoute
	// request), and revoke (RevokeMatchRoute). For match acks (MatchRoute
	// request), resend the RequestWithTimeout(..., processMatchAcks) as done in
	// Negotiate.
	for _, ackData := range state.LiveAckers {
		// Get a new request ID.
		msgID := comms.NextID()
		req := ackData.Request
		req.ID = msgID // reset the ID
		user := ackData.AckData[0].User

		var handlerFun func(link comms.Link, resp *msgjson.Message)

		if req.Route == msgjson.MatchRoute {
			// Request with a processMatchAcks response handler and a
			// []*messageAcker for a batch of matches.
			acks := make([]*messageAcker, 0, len(ackData.AckData))
			for _, dat := range ackData.AckData {
				match := s.matches[dat.MatchID]
				if match == nil {
					log.Errorf("Unable to find match %v", dat.MatchID)
					continue
				}
				acks = append(acks, &messageAcker{
					user:    user,
					match:   match,
					params:  dat.Params,
					isMaker: dat.IsMaker,
					isAudit: req.Route == msgjson.AuditRoute,
				})
			}

			s.setLiveAckers(req, acks)
			handlerFun = func(_ comms.Link, resp *msgjson.Message) {
				s.processMatchAcks(user, resp, acks) // resp.ID == req.ID == msgID
			}
		} else {
			// Request with a processAck response handler and a single
			// *messageAcker for the audit/redeem/revoke.
			if len(ackData.AckData) != 1 {
				log.Errorf("%v route request has %d data elements, need 1",
					req.Route, len(ackData.AckData))
				continue
			}

			dat := ackData.AckData[0]
			match := s.matches[dat.MatchID]
			if match == nil {
				log.Errorf("Unable to find match %v", dat.MatchID)
				continue
			}
			ack := &messageAcker{
				user:    user,
				match:   match,
				params:  dat.Params,
				isMaker: dat.IsMaker,
				isAudit: req.Route == msgjson.AuditRoute,
			}

			handlerFun = func(_ comms.Link, resp *msgjson.Message) {
				s.processAck(resp, ack) // resp.ID == req.ID == msgID
			}
		}

		expireFunc := func() {
			s.rmLiveAckers(msgID)
			log.Errorf("Failed to send request to %v for %v ack", user, req.Route)
		}
		log.Infof("Sending %v request to user %v", req.Route, user)
		log.Debug(req)
		s.authMgr.RequestWhenConnected(user, req, handlerFun, auth.DefaultRequestTimeout,
			auth.DefaultConnectTimeout, expireFunc)
		// NOTE: The user must reconnect to send the ack request. processAck and
		// processMatchAcks themselves won't require connectivity since there
		// should be no error response (already validated by virtue of being in
		// the state's live ackers) and they just save the ack signatures.
	}

	// Live coin waiters started by the comms handlers for client init and
	// redeem messages
	//
	// Rerun handleInit or handleRedeem to revalidate the contract/redeem and
	// start the coin waiters that trigger processInit and processRedeem.
	// Alternatively, we could manually start a waiter. Manually creating a
	// waiter requires (1) s.step to get stepInfo and (2) msg.Payload unmarshal
	// into params, a msgjson.Init or msgjson.Redeem. Manually doing this would
	// skip the msg and contract/redeem validation.
	//
	// The counter-party must be connected so the success function (processInit
	// or processRedeem) called when the coin is found can send an ack request.
	// Hence, processInit and processRedeem now use RequestWhenConnected in
	// conjunction with a grace period where penalty timeout is increased to the
	// reconnect timeout.

	s.gracePeriod = 1
	time.AfterFunc(2*auth.DefaultConnectTimeout, func() {
		atomic.StoreInt32(&s.gracePeriod, 0)
	})

	for _, waitDat := range state.LiveWaiters {
		var msgErr *msgjson.Error
		rt := waitDat.Msg.Route
		switch rt {
		case msgjson.InitRoute:
			msgErr = s.handleInit(waitDat.User, waitDat.Msg)
		case msgjson.RedeemRoute:
			msgErr = s.handleRedeem(waitDat.User, waitDat.Msg)
		default:
			log.Errorf("%s is not a route that starts coinwaiters!", rt)
			continue
		}

		if msgErr != nil {
			log.Errorf("Failed to reprocess %v message: %v", rt, msgErr)
		}
	}

	return nil
}

func (s *Swapper) saveState() {
	// Store state.
	fName := fmt.Sprintf("swapState-%d.gob", encode.UnixMilli(time.Now()))
	f, err := os.Create(filepath.Join(s.dataDir, fName))
	if err != nil {
		log.Errorf("Failed to create swap state file %v: %v", fName, err)
		return
	}
	defer f.Close()

	mtd := make(map[order.MatchID]*matchTrackerData, len(s.matches))
	neededAssets := make(map[uint32]struct{}, len(s.coins))
	for matchID, mt := range s.matches {
		neededAssets[mt.Match.Maker.BaseAsset] = struct{}{}
		neededAssets[mt.Match.Maker.QuoteAsset] = struct{}{}
		mtd[matchID] = &matchTrackerData{
			Match:       mt.Match,
			Time:        encode.UnixMilli(mt.time),
			MakerStatus: mt.makerStatus.Data(),
			TakerStatus: mt.takerStatus.Data(),
		}
	}
	assetIDs := make([]uint32, 0, len(neededAssets))
	for id := range neededAssets {
		assetIDs = append(assetIDs, id)
	}

	st := &State{
		Version:       stateBinaryVersion,
		Assets:        assetIDs,
		MatchTrackers: mtd,
		OrderMatches:  s.orders.orderMatches,
		LiveAckers:    s.liveAckers,
		LiveWaiters:   s.liveWaiters,
	}

	enc := gob.NewEncoder(f)
	if err = enc.Encode(st); err != nil {
		log.Errorf("Failed to save swap state to file %v: %v", fName, err)
		return
	}

	if err = f.Sync(); err != nil {
		log.Errorf("Failed to write swap state data to disk: %v", err)
	} else {
		log.Infof("Saved swap state to file %q, containing %d live matches with "+
			"%d pending client acks and %d live coin waiters", fName,
			len(st.MatchTrackers), len(st.LiveAckers), len(st.LiveWaiters))
	}
}

type msgAckers struct {
	Request *msgjson.Message // request to user, may not be nil
	AckData []*ackerData
}

type ackerData struct {
	User    account.AccountID
	MatchID order.MatchID    // key for a *matchTrackers map
	Params  msgjson.Signable // msgjson.{Match,Audit,Redemption,RevokeMatch}
	IsMaker bool
	//IsAudit bool // inferred if Params.(*msgjson.Audit)
}

func (s *Swapper) setLiveAckers(req *msgjson.Message, ackers []*messageAcker) {
	ackDats := make([]*ackerData, len(ackers))
	for i, ack := range ackers {
		ackDats[i] = &ackerData{
			User:    ack.user,
			MatchID: ack.match.ID(),
			Params:  ack.params,
			IsMaker: ack.isMaker,
		}
	}
	s.liveAckersMtx.Lock()
	s.liveAckers[req.ID] = &msgAckers{
		Request: req,
		AckData: ackDats,
	}
	s.liveAckersMtx.Unlock()
}

func (s *Swapper) setLiveAcker(req *msgjson.Message, ackDat *messageAcker) {
	s.setLiveAckers(req, []*messageAcker{ackDat})
}

func (s *Swapper) rmLiveAckers(msgID uint64) {
	s.liveAckersMtx.Lock()
	delete(s.liveAckers, msgID)
	s.liveAckersMtx.Unlock()
}

// Data input to handleInit and handleRedeem, which both start coin waiters with
// latencyQ. Store handlerArgs before starting the waiter, and remove them when
// the waiter completes or expires. On Swapper shutdown, the stored handlerArgs
// should be saved in the state data after stopping the latencyQ.
type handlerArgs struct {
	User account.AccountID
	Msg  *msgjson.Message // init or redeem inferred from Msg.
}

// client generated message IDs are not globally unique.
type waiterKey struct {
	MsgID uint64
	User  account.AccountID
}

func (s *Swapper) setLiveWaiter(user account.AccountID, msg *msgjson.Message) {
	s.liveWaitersMtx.Lock()
	key := waiterKey{msg.ID, user}
	s.liveWaiters[key] = &handlerArgs{
		User: user,
		Msg:  msg,
	}
	s.liveWaitersMtx.Unlock()
}

func (s *Swapper) rmLiveWaiter(user account.AccountID, msgID uint64) {
	s.liveWaitersMtx.Lock()
	delete(s.liveWaiters, waiterKey{msgID, user})
	s.liveWaitersMtx.Unlock()
}

// Run is the main Swapper loop.
func (s *Swapper) Run(ctx context.Context) {
	// Permit internal cancel on anomaly such as storage failure.
	ctxMaster, cancel := context.WithCancel(ctx)

	// Graceful shutdown first allows active incoming messages to be handled,
	// blocks more incoming messages in the handler functions, stops the helper
	// goroutines (latency queue used by the handlers, and the block ntfn
	// receiver), and finally the main loop via the mainLoop channel.
	var wgHelpers, wgMain sync.WaitGroup
	ctxHelpers, cancelHelpers := context.WithCancel(context.Background())
	mainLoop := make(chan struct{}) // close after helpers stop for graceful shutdown
	defer func() {
		// Stop handlers receiving messages and queueing latency Waiters.
		s.handlerMtx.Lock() // block until active handlers return
		s.stop = true       // prevent new handlers from starting waiters
		// NOTE: could also do authMgr.Route(msgjson.{InitRoute,RedeemRoute}, shuttingDownHandler)
		s.handlerMtx.Unlock()

		// Stop the latencyQ of Waiters and the block update goroutines that
		// send to the main loop.
		cancelHelpers()
		wgHelpers.Wait()

		// Now that handlers AND the coin waiter queue are stopped, the
		// liveWaiters can be accessed without locking.

		// Stop the main loop if if there was no internal error.
		close(mainLoop)
		wgMain.Wait()

		s.saveState()

	}()

	// Start a listen loop for each asset's block channel. Normal shutdown stops
	// this before the main loop since this sends to the main loop.
	blockNotes := make(chan *blockNotification, 128)
	for assetID, lockable := range s.coins {
		wgHelpers.Add(1)
		go func(assetID uint32, blockSource <-chan *asset.BlockUpdate) {
			defer wgHelpers.Done()
			for {
				select {
				case blk, ok := <-blockSource:
					if !ok {
						log.Errorf("Asset %d has closed the block channel.", assetID)
						// Should not happen. Keep running until cancel.
						continue
					}
					// Do not block on anomalous return of main loop, which is
					// the s.block receiver.
					select {
					case <-mainLoop:
						return // ctxHelpers is being canceled anyway
					case blockNotes <- &blockNotification{
						time:    time.Now().UTC(),
						assetID: assetID,
						err:     blk.Err,
					}:
					}
				case <-ctxHelpers.Done():
					return
				}
			}
		}(assetID, lockable.Backend.BlockChannel(32))
	}

	// Start the queue of coinwaiters for the init and redeem handlers. The
	// handlers must be stopped/blocked before stopping this.
	wgHelpers.Add(1)
	go func() {
		s.latencyQ.Run(ctxHelpers)
		wgHelpers.Done()
	}()

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

	// Main loop can stop on internal error via cancel(), or when the caller
	// cancels the parent context triggering graceful shutdown.
	wgMain.Add(1)
	go func() {
		defer wgMain.Done()
		defer cancel() // ctxMaster for anomalous return
		for {
			select {
			case <-s.storage.Fatal():
				return
			case block := <-blockNotes:
				// Schedule a check of matches with one side equal to this
				// block's asset by appending the block to the bcastTriggers.
				if block.err != nil {
					var connectionErr asset.ConnectionError
					if errors.As(block.err, &connectionErr) {
						// Connection issues handling can be triggered here.
						log.Errorf("connection error detected for %d: %v", block.assetID, block.err)
					} else {
						log.Errorf("asset %d is reporting a block notification error: %v", block.assetID, block.err)
					}
					break
				}
				bcastTriggers = append(bcastTriggers, block)
				// processBlock will update confirmation times in the swapStatus
				// structs.
				//
				// WARNING! Confirmation checks in processBlock must be quick
				// because either of the blockNotification or asset.BlockUpdate
				// channels could fill up and we will miss blocks! The asset
				// backends should allow a blocking channel, just retrying on
				// the next block, but that is not a solution.
				//
				// TODO: Consider a timeout for the entire processBlock call:
				//
				//   ctxTime := context.WithTimeout(ctx, 2*time.Second)
				//   s.processBlock(ctxTime, block)
				//
				// Presently, one stuck backend that hangs on Confirmations
				// halts the whole DEX! So timeouts on Confirmations too.
				s.processBlock(block)
			case <-bcastTicker.C:
				for {
					// checkInaction will fail if the DB is failing.
					if err := s.storage.LastErr(); err != nil {
						return
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
			case <-mainLoop:
				return
			}
		}
	}()

	// Wait for caller cancel or anomalous return from main loop.
	<-ctxMaster.Done()
}

func (s *Swapper) tryConfirmSwap(status *swapStatus) {
	status.mtx.Lock()
	defer status.mtx.Unlock()
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
	var completions []*matchTracker
	s.matchMtx.Lock()
	defer s.matchMtx.Unlock()
	for _, match := range s.matches {
		// If it's neither of the match assets, nothing to do.
		if match.makerStatus.swapAsset != block.assetID && match.takerStatus.swapAsset != block.assetID {
			continue
		}

		// Lock the matchTracker so the following checks and updates are atomic
		// with respect to Status.
		match.mtx.RLock()

	statusSwitch:
		switch match.Status {
		case order.NewlyMatched:
		case order.MakerSwapCast:
			if match.makerStatus.swapAsset != block.assetID {
				break statusSwitch
			}
			// If the maker has broadcast their transaction, the taker's broadcast
			// timeout starts once the maker's swap has SwapConf confs.
			s.tryConfirmSwap(match.makerStatus)
		case order.TakerSwapCast:
			if match.takerStatus.swapAsset != block.assetID {
				break statusSwitch
			}
			// If the taker has broadcast their transaction, the maker's broadcast
			// timeout (for redemption) starts once the maker's swap has SwapConf
			// confs.
			s.tryConfirmSwap(match.takerStatus)
		case order.MakerRedeemed:
			// It's the taker's turn to redeem. Nothing to do here.
			break statusSwitch
		case order.MatchComplete:
			// Once both redemption transactions have SwapConf confirmations, the
			// order is complete.
			makerRedeemed, takerRedeemed := s.RedeemStatus(match.makerStatus, match.takerStatus)
			// TODO: Can coins be unlocked now regardless of redemption?
			if makerRedeemed && takerRedeemed {
				completions = append(completions, match)
				// Note that although match status is now "complete", the swap
				// may still be active until both counterparties acknowledge the
				// redemptions. Conversely, the match may already be inactive
				// before the status is complete if the parties have
				// acknowledged the redeem txns before they hit their
				// confirmation requirements, which triggers this status change.
			}
		}

		match.mtx.RUnlock()
	}

	for _, match := range completions {
		// Note that orders are not considered completed for the purposes of
		// cancellation ratio until the user sends their redeem ack.

		s.unlockOrderCoins(match.Taker)
		s.unlockOrderCoins(match.Maker)
		// Remove the completed match. Note that checkInaction may also remove
		// matches, so this entire function must lock even if there are no
		// completions.
		log.Debugf("Deleting completed match %v", match.ID())
		delete(s.matches, match.ID())
	}
}

// RedeemStatus checks maker and taker redemption completion status.
func (s *Swapper) RedeemStatus(mStatus, tStatus *swapStatus) (makerRedeemComplete, takerRedeemComplete bool) {
	mStatus.mtx.RLock()
	defer mStatus.mtx.RUnlock()
	tStatus.mtx.RLock()
	defer tStatus.mtx.RUnlock()

	return s.redeemStatus(mStatus, tStatus)
}

// redeemStatus is not thread-safe with respect to the swapStatuses. The caller
// must lock each swapStatus to guard concurrent status access.
func (s *Swapper) redeemStatus(mStatus, tStatus *swapStatus) (makerRedeemComplete, takerRedeemComplete bool) {
	makerRedeemComplete = s.makerRedeemStatus(mStatus, tStatus.swapAsset)
	// Taker is only complete if the maker is complete because
	// order.MatchComplete follows order.MakerRedeemed.
	if makerRedeemComplete && !tStatus.redeemTime.IsZero() {
		confs, err := tStatus.redemption.Confirmations()
		if err != nil {
			log.Errorf("Confirmations failed for taker redemption %v: err",
				tStatus.redemption.TxID(), err)
			return
		}
		takerRedeemComplete = confs >= int64(s.coins[mStatus.swapAsset].SwapConf)
	}
	return
}

// MakerRedeemStatus checks maker redemption completion status The taker asset
// is required to check the confirmation requirement.
func (s *Swapper) MakerRedeemStatus(mStatus *swapStatus, tAsset uint32) (makerRedeemComplete bool) {
	mStatus.mtx.RLock()
	defer mStatus.mtx.RUnlock()

	return s.makerRedeemStatus(mStatus, tAsset)
}

// makerRedeemStatus is not thread-safe with respect to the swapStatus. The
// caller must lock the swapStatus to guard concurrent status access.
func (s *Swapper) makerRedeemStatus(mStatus *swapStatus, tAsset uint32) (makerRedeemComplete bool) {
	if !mStatus.redeemTime.IsZero() {
		confs, err := mStatus.redemption.Confirmations()
		if err != nil {
			log.Errorf("Confirmations failed for maker redemption %v: err",
				mStatus.redemption.TxID(), err) // Severity?
			return
		}
		makerRedeemComplete = confs >= int64(s.coins[tAsset].SwapConf)
	}
	return
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

	var deletions []*matchTracker
	oldestAllowed := time.Now().Add(-s.bTimeout).UTC()

	checkMatch := func(match *matchTracker) {
		// Lock entire matchTracker so the following is atomic with respect to
		// Status.
		match.mtx.RLock()
		defer match.mtx.RUnlock()

		if match.makerStatus.swapAsset != assetID && match.takerStatus.swapAsset != assetID {
			return
		}

		failMatch := func(makerFault bool) {
			orderAtFault, otherOrder := match.Taker, order.Order(match.Maker) // an order.Order
			if makerFault {
				orderAtFault, otherOrder = match.Maker, match.Taker
			}
			log.Debugf("checkInaction(failMatch): swap %v failing (maker fault = %v) at %v",
				match.ID(), makerFault, match.Status)

			deletions = append(deletions, match)

			// Record the end of this match's processing.
			s.storage.SetMatchInactive(db.MatchID(match.Match))

			// If the at-fault order is a limit order, signal that if it is
			// still on the book is should be unbooked, changed to revoked
			// status, counted against the user's cancellation ratio, and a
			// server-generated cancel order recorded.
			if lo, isLimit := orderAtFault.(*order.LimitOrder); isLimit {
				if s.unbookHook(lo) {
					s.orders.canceled(orderAtFault) // set as off-book and failed
				}
			}

			// That's one less active swap for this order, and a failure. If
			// this order has no other active swaps, it will be removed from the
			// order swap tracker by decrementActiveSwapCount since it is
			// off-book and no new swap negotiations can begin for this order.
			s.orders.swapFailure(orderAtFault)

			// The other order now has one less active swap too.
			if s.orders.swapSuccess(otherOrder) {
				// TODO: We should count this as a successful swap, but should
				// it only be a completed order with the extra stipulation that
				// it had already completed another swap?
				compTime := time.Now().UTC()
				s.authMgr.RecordCompletedOrder(otherOrder.User(), otherOrder.ID(), compTime)
				if err := s.storage.SetOrderCompleteTime(otherOrder, encode.UnixMilli(compTime)); err != nil {
					if db.IsErrGeneralFailure(err) {
						log.Errorf("fatal error with SetOrderCompleteTime for order %v: %v", otherOrder.UID(), err)
					} else {
						log.Warnf("SetOrderCompleteTime for %v: %v", otherOrder.UID(), err)
					}
				}
			}

			// Penalize for failure to act.
			//
			// TODO: Arguably, this obviates the RecordCancel above since this
			// closes the account before the possibility of a cancellation ratio
			// penalty. I'm keeping it this way for now however since penalties
			// may become less severe than account closure (e.g. temporary
			// suspension, cool down, or order throttling), and restored
			// accounts will still require a record of the revoked order.
			s.authMgr.Penalize(orderAtFault.User(), account.FailureToAct)

			// Send the revoke_match messages, and solicit acks.
			s.revoke(match)
		}

		match.makerStatus.mtx.RLock()
		defer match.makerStatus.mtx.RUnlock()
		match.takerStatus.mtx.RLock()
		defer match.takerStatus.mtx.RUnlock()

		switch match.Status {
		case order.NewlyMatched:
			if match.makerStatus.swapAsset != assetID {
				return
			}
			// If the maker is not acting, the swapTime won't be set. Check against
			// the time the match notification was sent (match.time) for the broadcast
			// timeout.
			if match.makerStatus.swapTime.IsZero() && match.time.Before(oldestAllowed) {
				failMatch(true)
			}
		case order.MakerSwapCast:
			if match.takerStatus.swapAsset != assetID {
				return
			}
			// If the maker has sent their swap tx, check the taker's broadcast
			// timeout against the time of the swap's SwapConf'th confirmation.
			if match.takerStatus.swapTime.IsZero() &&
				!match.makerStatus.swapConfirmed.IsZero() &&
				match.makerStatus.swapConfirmed.Before(oldestAllowed) {
				failMatch(false)
			}
		case order.TakerSwapCast:
			if match.takerStatus.swapAsset != assetID {
				return
			}
			// If the taker has sent their swap tx, check the maker's broadcast
			// timeout (for redemption) against the time of the swap's SwapConf'th
			// confirmation.
			if match.makerStatus.redeemTime.IsZero() &&
				!match.takerStatus.swapConfirmed.IsZero() &&
				match.takerStatus.swapConfirmed.Before(oldestAllowed) {
				failMatch(true)
			}
		case order.MakerRedeemed:
			if match.takerStatus.swapAsset != assetID {
				return
			}
			// If the maker has redeemed, the taker can redeem immediately, so
			// check the timeout against the time the Swapper received the
			// maker's `redeem` request (and sent the taker's 'redemption').
			if match.takerStatus.redeemTime.IsZero() &&
				!match.makerStatus.redeemTime.IsZero() &&
				match.makerStatus.redeemTime.Before(oldestAllowed) {
				failMatch(false)
			}
		case order.MatchComplete:
			// Nothing to do here right now.

			// Note: clients still must ack the counterparty's redeem for the
			// swap to be flagged as done/inactive in the DB, but the match may
			// be deleted from s.matches if the redeem txns fully confirm first.
		}
	}

	s.matchMtx.Lock()
	defer s.matchMtx.Unlock()

	for _, match := range s.matches {
		checkMatch(match)
	}

	for _, match := range deletions {
		delete(s.matches, match.ID())
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
	s.authMgr.SendWhenConnected(user, msg, auth.DefaultConnectTimeout, func() {
		log.Infof("Unable to send error response (code %d) to disconnected user %v: %q",
			code, user, errMsg)
	})
}

// respondSuccess sends a successful response to a user.
func (s *Swapper) respondSuccess(id uint64, user account.AccountID, result interface{}) {
	msg, err := msgjson.NewResponse(id, result, nil)
	if err != nil {
		log.Errorf("failed to send success: %v", err)
	}
	s.authMgr.SendWhenConnected(user, msg, auth.DefaultConnectTimeout, func() {
		log.Infof("Unable to send success response to disconnected user %v", user)
	})
}

// step creates a stepInformation structure for the specified match. A new
// stepInformation should be created for every client communication. The user
// is also validated as the actor. An error is returned if the user has not
// acknowledged their previous DEX requests.
func (s *Swapper) step(user account.AccountID, matchID order.MatchID) (*stepInformation, *msgjson.Error) {
	s.matchMtx.RLock()
	match, found := s.matches[matchID]
	s.matchMtx.RUnlock()
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

	// Lock for Status and Sigs.
	match.mtx.RLock()
	defer match.mtx.RUnlock()

	// maker sell: base swap, quote redeem
	// taker buy: quote swap, base redeem

	// maker buy: quote swap, base redeem
	// taker sell: base swap, quote redeem

	// Maker broadcasts the swap contract. Sequence: NewlyMatched ->
	// MakerSwapCast -> TakerSwapCast -> MakerRedeemed -> MatchComplete
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
			isBaseAsset = maker.Sell // maker swap: base asset if sell
			if len(match.Sigs.MakerMatch) == 0 {
				log.Debugf("swap %v at status %v missing MakerMatch signature(s) needed for NewlyMatched->MakerSwapCast",
					match.ID(), match.Status)
			}
			reqSigs = [][]byte{match.Sigs.MakerMatch}
		} else /* TakerSwapCast */ {
			nextStep = order.MakerRedeemed
			isBaseAsset = !maker.Sell // maker redeem: base asset if buy
			if len(match.Sigs.MakerAudit) == 0 {
				log.Debugf("swap %v at status %v missing MakerAudit signature(s) needed for TakerSwapCast->MakerRedeemed",
					match.ID(), match.Status)
			}
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
			isBaseAsset = !maker.Sell // taker swap: base asset if sell (maker buy)
			if len(match.Sigs.TakerMatch) == 0 {
				log.Debugf("swap %v at status %v missing TakerMatch signature(s) needed for MakerSwapCast->TakerSwapCast",
					match.ID(), match.Status)
			}
			if len(match.Sigs.TakerAudit) == 0 {
				log.Debugf("swap %v at status %v missing TakerAudit signature(s) needed for MakerSwapCast->TakerSwapCast",
					match.ID(), match.Status)
			}
			reqSigs = [][]byte{match.Sigs.TakerMatch, match.Sigs.TakerAudit}
			ackType = msgjson.AuditRoute
		} else /* MakerRedeemed */ {
			nextStep = order.MatchComplete
			// Note that the swap is still considered "active" until both
			// counterparties acknowledge the redemptions.
			isBaseAsset = maker.Sell // taker redeem: base asset if buy (maker sell)
			if len(match.Sigs.TakerRedeem) == 0 {
				log.Debugf("swap %v at status %v missing TakerRedeem signature(s) needed for MakerRedeemed->MatchComplete",
					match.ID(), match.Status)
			}
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

	log.Debugf("step(user=%v, match=%v): ready for user %v action, status %v, next %v",
		actor.user, matchID, counterParty.user, match.Status, nextStep)

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

// authUser verifies that the msgjson.Signable is signed by the user. This
// method relies on the AuthManager to validate the signature of the serialized
// data. nil is returned for successful signature verification.
func (s *Swapper) authUser(user account.AccountID, params msgjson.Signable) *msgjson.Error {
	// Authorize the user.
	msg := params.Serialize()
	err := s.authMgr.Auth(user, msg, params.SigBytes())
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "error authenticating init params",
		}
	}
	return nil
}

// messageAcker is information needed to process the user's
// msgjson.Acknowledgement.
type messageAcker struct {
	user    account.AccountID
	match   *matchTracker
	params  msgjson.Signable
	isMaker bool
	isAudit bool
}

// processAck processes a match msgjson.Acknowledgement, validating the
// signature and updating the (order.Match).Sigs record. This is required by
// processInit, processRedeem, and revoke.
func (s *Swapper) processAck(msg *msgjson.Message, acker *messageAcker) {
	// Remove the live acker from Swapper's tracking.
	defer s.rmLiveAckers(msg.ID)

	// The time that the ack is received is stored for redeem acks to facilitate
	// cancellation ratio enforcement.
	tAck := time.Now()

	ack := new(msgjson.Acknowledgement)
	err := msg.UnmarshalResult(ack)
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.RPCParseError,
			fmt.Sprintf("error parsing audit notification acknowledgment: %v", err))
		return
	}
	// Note: ack.MatchID unused, but could be checked against acker.match.ID().

	// Check the signature.
	sigMsg := acker.params.Serialize()
	err = s.authMgr.Auth(acker.user, sigMsg, ack.Sig)
	if err != nil {
		s.respondError(msg.ID, acker.user, msgjson.SignatureError,
			fmt.Sprintf("signature validation error: %v", err))
		return
	}

	// Set and store the appropriate signature, based on the current step and
	// actor.
	mktMatch := db.MatchID(acker.match.Match)

	acker.match.mtx.Lock()
	defer acker.match.mtx.Unlock()

	if rev, ok := acker.params.(*msgjson.RevokeMatch); ok {
		log.Infof("Received revoke ack for match %v, order %v", rev.MatchID, rev.OrderID)
		// TODO: do something in the DB for this??
		return // drop the revoke ack sig for now
	}

	// This is an ack of either contract audit or redeem receipt.
	if acker.isAudit {
		// It's a contract audit ack.
		if acker.isMaker {
			acker.match.Sigs.MakerAudit = ack.Sig         // i.e. audited taker's contract
			s.storage.SaveAuditAckSigA(mktMatch, ack.Sig) // sql error makes backend go fatal
		} else {
			acker.match.Sigs.TakerAudit = ack.Sig
			s.storage.SaveAuditAckSigB(mktMatch, ack.Sig)
		}
		return
	}

	// It's a redeem script ack.

	// This is a redemption acknowledgement. Store the ack signature, and
	// potentially record the order as complete with the auth manager and in
	// persistent storage.
	tAckMS := encode.UnixMilli(tAck)
	if acker.isMaker {
		// A maker order is complete if it has force immediate or no
		// remaining amount, indicating it is off the books.
		lo := acker.match.Maker
		if s.orders.swapSuccess(lo) {
			s.authMgr.RecordCompletedOrder(acker.user, lo.ID(), tAck)
			if err = s.storage.SetOrderCompleteTime(lo, tAckMS); err != nil {
				if db.IsErrGeneralFailure(err) {
					log.Errorf("fatal error with SetOrderCompleteTime for order %v: %v", lo, err)
					s.respondError(msg.ID, acker.user, msgjson.UnknownMarketError, "internal server error")
					return
				}
				log.Warnf("SetOrderCompleteTime for %v: %v", lo, err)
			}
		}

		acker.match.Sigs.MakerRedeem = ack.Sig
		if err = s.storage.SaveRedeemAckSigA(mktMatch, ack.Sig); err != nil {
			s.respondError(msg.ID, acker.user, msgjson.UnknownMarketError, "internal server error")
			log.Errorf("SaveRedeemAckSigA failed for match %v: %v", mktMatch.String(), err)
			return
		}

	} else {
		// A taker order is complete if it is one of: (1) a market order,
		// which does not go on the books, (2) limit with force immediate,
		// or (3) limit with no remaining amount.
		ord := acker.match.Taker
		if s.orders.swapSuccess(ord) {
			s.authMgr.RecordCompletedOrder(acker.user, ord.ID(), tAck)
			if err = s.storage.SetOrderCompleteTime(ord, tAckMS); err != nil {
				if db.IsErrGeneralFailure(err) {
					log.Errorf("fatal error with SetOrderCompleteTime for order %v: %v", ord.UID(), err)
					s.respondError(msg.ID, acker.user, msgjson.UnknownMarketError, "internal server error")
					return
				}
				log.Warnf("SetOrderCompleteTime for %v: %v", ord.UID(), err)
			}
		}

		acker.match.Sigs.TakerRedeem = ack.Sig
		if err = s.storage.SaveRedeemAckSigB(mktMatch, ack.Sig); err != nil {
			s.respondError(msg.ID, acker.user, msgjson.UnknownMarketError, "internal server error")
			log.Errorf("SaveRedeemAckSigB failed for match %v: %v", mktMatch.String(), err)
			return
		}
	}

}

// processInit processes the `init` RPC request, which is used to inform the DEX
// of a newly broadcast swap transaction. Once the transaction is seen and and
// audited by the Swapper, the counter-party is informed with an 'audit'
// request. This method is run as a coin waiter, hence the return value
// indicates if future attempts should be made to check coin status.
func (s *Swapper) processInit(msg *msgjson.Message, params *msgjson.Init, stepInfo *stepInformation) bool {
	// Validate the swap contract
	chain := stepInfo.asset.Backend
	actor, counterParty := stepInfo.actor, stepInfo.counterParty
	contract, err := chain.Contract(params.CoinID, params.Contract)
	if err != nil {
		if err == asset.CoinNotFoundError {
			return wait.TryAgain
		}
		log.Warnf("Contract error encountered for match %s, actor %s using coin ID %x and contract %x: %v",
			stepInfo.match.ID(), actor.user, params.CoinID, params.Contract, err)
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			"redemption error")
		return wait.DontTryAgain
	}
	//actor.swapAsset
	reqFeeRate := stepInfo.match.FeeRateQuote
	if stepInfo.isBaseAsset {
		reqFeeRate = stepInfo.match.FeeRateBase
	}
	if contract.FeeRate() < reqFeeRate {
		// TODO: test this case
		s.respondError(msg.ID, actor.user, msgjson.ContractError, "low tx fee")
		return wait.DontTryAgain
	}
	if contract.SwapAddress() != counterParty.order.Trade().SwapAddress() {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("incorrect recipient. expected %s. got %s",
				contract.SwapAddress(), counterParty.order.Trade().SwapAddress()))
		return wait.DontTryAgain
	}
	if contract.Value() != stepInfo.checkVal {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("contract error. expected contract value to be %d, got %d", stepInfo.checkVal, contract.Value()))
		return wait.DontTryAgain
	}

	reqLockTime := encode.DropMilliseconds(stepInfo.match.matchTime.Add(s.lockTimeTaker))
	if stepInfo.actor.isMaker {
		reqLockTime = encode.DropMilliseconds(stepInfo.match.matchTime.Add(s.lockTimeMaker))
	}
	if contract.LockTime().Before(reqLockTime) {
		s.respondError(msg.ID, actor.user, msgjson.ContractError,
			fmt.Sprintf("contract error. expected lock time >= %s, got %s", reqLockTime, contract.LockTime()))
		return wait.DontTryAgain
	}

	// Update the match.
	swapTime := unixMsNow()
	matchID := stepInfo.match.Match.ID()

	// Store the swap contract and the coinID (e.g. txid:vout) containing the
	// contract script hash. Maker is party A, the initiator. Taker is party B,
	// the participant.
	//
	// Failure to update match status is a fatal DB error. If we continue, the
	// swap may succeed, but there will be no way to recover or retrospectively
	// determine the swap outcome. Abort.
	storFn := s.storage.SaveContractB
	if stepInfo.actor.isMaker {
		storFn = s.storage.SaveContractA
	}
	mktMatch := db.MatchID(stepInfo.match.Match)
	swapTimeMs := encode.UnixMilli(swapTime)
	err = storFn(mktMatch, params.Contract, params.CoinID, swapTimeMs)
	if err != nil {
		log.Errorf("saving swap contract (match id=%v, maker=%v) failed: %v",
			matchID, actor.isMaker, err)
		s.respondError(msg.ID, actor.user, msgjson.UnknownMarketError,
			"internal server error")
		// TODO: revoke the match without penalties instead of retrying forever?
		return wait.TryAgain
	}

	actor.status.mtx.Lock()
	actor.status.swap = contract
	actor.status.swapTime = swapTime
	actor.status.mtx.Unlock()

	stepInfo.match.mtx.Lock()
	stepInfo.match.Status = stepInfo.nextStep
	stepInfo.match.mtx.Unlock()

	log.Debugf("processInit: valid contract received at %v from %v for match %v, "+
		"swapStatus %v => %v", swapTime, actor.user, matchID, stepInfo.step, stepInfo.nextStep)

	// Issue a positive response to the actor.
	s.authMgr.Sign(params)
	// respondSuccess works with auth to send on connect like
	// RequestWhenConnected. If the send fails due to the connect timeout, the
	// user's client may not be satisfied. Deal with this client-side.
	s.respondSuccess(msg.ID, actor.user, &msgjson.Acknowledgement{
		MatchID: matchID[:],
		Sig:     params.Sig,
	})

	// Prepare an 'audit' request for the counter-party.
	auditParams := &msgjson.Audit{
		OrderID:  idToBytes(counterParty.order.ID()),
		MatchID:  matchID[:],
		Time:     uint64(swapTimeMs),
		CoinID:   params.CoinID,
		Contract: params.Contract,
	}
	s.authMgr.Sign(auditParams)
	notification, err := msgjson.NewRequest(comms.NextID(), msgjson.AuditRoute, auditParams)
	if err != nil {
		// This is likely an impossibly condition.
		log.Errorf("error creating audit request: %v", err)
		return wait.DontTryAgain
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
	log.Debugf("processInit: sending contract 'audit' ack request to counterparty %v "+
		"for match %v", ack.user, matchID)

	// Register that there is an outstanding ack request. This is unregistered
	// on completion of processAck or request expiry.
	s.setLiveAcker(notification, ack)

	// Expire function to unregister the outstanding request.
	expireFunc := func() {
		s.rmLiveAckers(notification.ID)
		log.Infof("Failed to send request to %v (maker=%v) for init audit ack.",
			ack.user, ack.isMaker)
	}

	// Send the ack request.

	// A user gets responseTimeout to respond to a successfully sent request,
	// and reconnectTimeout to connect and and have the request sent.
	s.authMgr.RequestWhenConnected(ack.user, notification, func(_ comms.Link, resp *msgjson.Message) {
		s.processAck(resp, ack) // resp.ID == notification.ID
	}, auth.DefaultRequestTimeout, auth.DefaultConnectTimeout, expireFunc)

	return wait.DontTryAgain
}

// processRedeem processes a 'redeem' request from a client. processRedeem does
// not perform user authentication, which is handled in handleRedeem before
// processRedeem is invoked. This method is run as a coin waiter.
func (s *Swapper) processRedeem(msg *msgjson.Message, params *msgjson.Redeem, stepInfo *stepInformation) bool {
	// TODO(consider): Extract secret from initiator's (maker's) redemption
	// transaction. The Backend would need a method identify the component of
	// the redemption transaction that contains the secret and extract it. In a
	// UTXO-based asset, this means finding the input that spends the output of
	// the counterparty's contract, and process that input's signature script
	// with FindKeyPush. Presently this is up to the clients and not stored with
	// the server.

	// Make sure that the expected output is being spent.
	actor, counterParty := stepInfo.actor, stepInfo.counterParty
	counterParty.status.mtx.RLock()
	cpContract := counterParty.status.swap.RedeemScript()
	cpSwapCoin := counterParty.status.swap.ID()
	counterParty.status.mtx.RUnlock()

	// Get the transaction
	match := stepInfo.match
	matchID := match.ID()
	chain := stepInfo.asset.Backend
	if !chain.ValidateSecret(params.Secret, cpContract) {
		log.Errorf("secret validation failed (match id=%v, maker=%v, secret=%x)",
			matchID, actor.isMaker, params.Secret)
		s.respondError(msg.ID, actor.user, msgjson.UnknownMarketError, "secret validation failed")
		return wait.DontTryAgain
	}
	redemption, err := chain.Redemption(params.CoinID, cpSwapCoin)
	// If there is an error, don't return an error yet, since it could be due to
	// network latency. Instead, queue it up for another check.
	if err != nil {
		if err == asset.CoinNotFoundError {
			return wait.TryAgain
		}
		log.Warnf("Redemption error encountered for match %s, actor %s using coin ID %x to satisfy contract at %x: %v",
			stepInfo.match.ID(), actor, params.CoinID, cpSwapCoin, err)
		s.respondError(msg.ID, actor.user, msgjson.RedemptionError, "redemption error")
		return wait.DontTryAgain
	}

	// NOTE: redemption.FeeRate is not checked since the counter party is not
	// inconvenienced by slow confirmation of the redemption.

	// Store the swap contract and the coinID (e.g. txid:vout) containing the
	// contract script hash. Maker is party A, the initiator, who first reveals
	// the secret. Taker is party B, the participant.
	storFn := s.storage.SaveRedeemB // also sets match status to MatchComplete
	if stepInfo.actor.isMaker {
		// Maker redeem stores the secret too.
		storFn = func(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
			return s.storage.SaveRedeemA(mid, coinID, params.Secret, timestamp) // also sets match status to MakerRedeemed
		}
	}
	redeemTime := unixMsNow()
	redeemTimeMs := encode.UnixMilli(redeemTime)
	err = storFn(db.MatchID(match.Match), params.CoinID, redeemTimeMs)
	if err != nil {
		log.Errorf("saving redeem transaction (match id=%v, maker=%v) failed: %v",
			matchID, actor.isMaker, err)
		s.respondError(msg.ID, actor.user, msgjson.UnknownMarketError, "internal server error")
		return wait.TryAgain // why not, the DB might come alive
	}

	// Modify the match's swapStatuses.
	actor.status.mtx.Lock()
	actor.status.redemption = redemption
	actor.status.redeemTime = redeemTime
	actor.status.mtx.Unlock()

	match.mtx.Lock()
	match.Status = stepInfo.nextStep
	match.mtx.Unlock()

	log.Debugf("processRedeem: valid redemption received at %v from %v for match %v, "+
		"swapStatus %v => %v", redeemTime, actor.user, matchID, stepInfo.step, stepInfo.nextStep)

	// Issue a positive response to the actor.
	s.authMgr.Sign(params)
	// respondSuccess works with auth to send on connect like
	// RequestWhenConnected. If the send fails due to the connect timeout, the
	// user's client may not be satisfied. Deal with this client-side.
	s.respondSuccess(msg.ID, actor.user, &msgjson.Acknowledgement{
		MatchID: matchID[:],
		Sig:     params.Sig,
	})

	// Inform the counterparty.
	rParams := &msgjson.Redemption{
		Redeem: msgjson.Redeem{
			OrderID: idToBytes(counterParty.order.ID()),
			MatchID: matchID[:],
			CoinID:  params.CoinID,
			Secret:  params.Secret,
		},
		Time: uint64(redeemTimeMs),
	}
	s.authMgr.Sign(rParams)
	notification, err := msgjson.NewRequest(comms.NextID(), msgjson.RedemptionRoute, rParams)
	if err != nil {
		log.Errorf("error creating redemption request: %v", err)
		return wait.DontTryAgain
	}

	// Set up the acknowledgement callback.
	ack := &messageAcker{
		user:    counterParty.user,
		match:   match,
		params:  rParams,
		isMaker: counterParty.isMaker,
		// isAudit: false,
	}
	log.Debugf("processRedeem: sending 'redeem' ack request to counterparty %v "+
		"for match %v", ack.user, matchID)

	// Register that there is an outstanding ack request. This is unregistered
	// on completion of processAck or request expiry.
	s.setLiveAcker(notification, ack)

	// Expire function to unregister the outstanding request.
	expireFunc := func() {
		s.rmLiveAckers(notification.ID)
		log.Infof("redeem ack request failed for user %v (maker=%v).",
			ack.user, ack.isMaker)
	}

	// Send the ack request.

	// A user gets responseTimeout to respond to a successfully sent request,
	// and reconnectTimeout to connect and and have the request sent.
	s.authMgr.RequestWhenConnected(ack.user, notification, func(_ comms.Link, resp *msgjson.Message) {
		s.processAck(resp, ack) // resp.ID == notification.ID
	}, auth.DefaultRequestTimeout, auth.DefaultConnectTimeout, expireFunc)

	return wait.DontTryAgain
}

// handleInit handles the 'init' request from a user, which is used to inform
// the DEX of a newly broadcast swap transaction. The Init message includes the
// swap contract script and the CoinID of contract. Most of the work is
// performed by processInit, but the request is parsed and user is authenticated
// first.
func (s *Swapper) handleInit(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	s.handlerMtx.RLock()
	defer s.handlerMtx.RUnlock() // block shutdown until registered with latencyQ
	if s.stop {
		return &msgjson.Error{
			Code:    msgjson.TryAgainLaterError,
			Message: "The swapper is stopping. Try again later.",
		}
	}

	params := new(msgjson.Init)
	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "Error decoding 'init' method params",
		}
	}

	// Verify the user's signature of params.
	rpcErr := s.authUser(user, params)
	if rpcErr != nil {
		return rpcErr
	}

	log.Debugf("handleInit: 'init' received from %v for match %v, order %v",
		user, params.MatchID, params.OrderID)

	if len(params.MatchID) != order.MatchIDSize {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "Invalid 'matchid' in 'init' message",
		}
	}

	var matchID order.MatchID
	copy(matchID[:], params.MatchID)
	stepInfo, rpcErr := s.step(user, matchID)
	if rpcErr != nil {
		return rpcErr
	}

	// init requests should only be sent when contracts are still required, in
	// the correct sequence, and by the correct party.
	switch stepInfo.match.Status {
	// These cases can eventually reduced to:
	// case order.NewlyMatched, order.MakerSwapCast: // just continue
	// default: // respond with settlement sequence error
	case order.NewlyMatched:
		if !stepInfo.actor.isMaker {
			// s.step should have returned an error of code SettlementSequenceError.
			panic("handleInit: this stepInformation should be for the maker!")
		}
	case order.MakerSwapCast:
		if stepInfo.actor.isMaker {
			// s.step should have returned an error of code SettlementSequenceError.
			panic("handleInit: this stepInformation should be for the taker!")
		}
	default:
		return &msgjson.Error{
			Code:    msgjson.SettlementSequenceError,
			Message: "swap contract already provided",
		}
	}

	// Validate the coinID and contract script before starting a coin waiter.
	coinStr, err := stepInfo.asset.Backend.ValidateCoinID(params.CoinID)
	if err != nil {
		// TODO: ensure Backends provide sanitized errors or type information to
		// provide more details to the client.
		return &msgjson.Error{
			Code:    msgjson.ContractError,
			Message: "invalid contract coinID or script",
		}
	}
	err = stepInfo.asset.Backend.ValidateContract(params.Contract)
	if err != nil {
		log.Debugf("ValidateContract (asset %v, coin %v) failure: %v", stepInfo.asset.Symbol, coinStr, err)
		// TODO: ensure Backends provide sanitized errors or type information to
		// provide more details to the client.
		return &msgjson.Error{
			Code:    msgjson.ContractError,
			Message: "invalid swap contract",
		}
	}
	// TODO: consider also checking recipient of contract here, but it is also
	// checked in processInit. Note that value cannot be checked as transaction
	// details, which includes the coin/output value, are not yet retrieved.

	s.setLiveWaiter(user, msg)

	// Since we have to consider broadcast latency of the asset's network, run
	// this as a coin waiter.
	s.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(txWaitExpiration),
		TryFunc: func() bool {
			done := s.processInit(msg, params, stepInfo)
			if done == wait.DontTryAgain {
				// It is now either a live acker, or terminally failed.
				s.rmLiveWaiter(user, msg.ID)
			}
			return done
		},
		ExpireFunc: func() {
			s.rmLiveWaiter(user, msg.ID)
			s.respondError(msg.ID, user, msgjson.TransactionUndiscovered,
				fmt.Sprintf("failed to find contract coin %v", coinStr))
		},
	})
	return nil
}

// handleRedeem handles the 'redeem' request from a user. Most of the work is
// performed by processRedeem, but the request is parsed and user is
// authenticated first.
func (s *Swapper) handleRedeem(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	s.handlerMtx.RLock()
	defer s.handlerMtx.RUnlock() // block shutdown until registered with latencyQ
	if s.stop {
		return &msgjson.Error{
			Code:    msgjson.TryAgainLaterError,
			Message: "The swapper is stopping. Try again later.",
		}
	}

	params := new(msgjson.Redeem)

	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "Error decoding 'redeem' method params",
		}
	}

	rpcErr := s.authUser(user, params)
	if rpcErr != nil {
		return rpcErr
	}

	log.Debugf("handleRedeem: 'redeem' received from %v for match %v, order %v",
		user, params.MatchID, params.OrderID)

	if len(params.MatchID) != order.MatchIDSize {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "Invalid 'matchid' in 'redeem' message",
		}
	}

	var matchID order.MatchID
	copy(matchID[:], params.MatchID)
	stepInfo, rpcErr := s.step(user, matchID)
	if rpcErr != nil {
		return rpcErr
	}
	// Validate the redeem coin ID before starting a wait. This does not
	// check the blockchain, but does ensure the CoinID can be decoded for the
	// asset before starting up a coin waiter.
	coinStr, err := stepInfo.asset.Backend.ValidateCoinID(params.CoinID)
	if err != nil {
		// TODO: ensure Backends provide sanitized errors or type information to
		// provide more details to the client.
		return &msgjson.Error{
			Code:    msgjson.ContractError,
			Message: "invalid 'redeem' parameters",
		}
	}

	s.setLiveWaiter(user, msg)

	// Since we have to consider latency, run this as a coin waiter.
	s.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(txWaitExpiration),
		TryFunc: func() bool {
			done := s.processRedeem(msg, params, stepInfo)
			if done == wait.DontTryAgain {
				// It is now either a live acker, or terminally failed.
				s.rmLiveWaiter(user, msg.ID)
			}
			return done
		},
		ExpireFunc: func() {
			s.rmLiveWaiter(user, msg.ID)
			s.respondError(msg.ID, user, msgjson.TransactionUndiscovered,
				fmt.Sprintf("failed to find redeemed coin %v", coinStr))
		},
	})
	return nil
}

// revocationRequests prepares a match revocation RPC request for each client.
// Both the request and the *msgjson.RevokeMatchParams are returned, since they
// cannot be accessed directly from the request (json.RawMessage).
func (s *Swapper) revocationRequests(match *matchTracker) (*msgjson.RevokeMatch, *msgjson.Message, *msgjson.RevokeMatch, *msgjson.Message, error) {
	takerParams := &msgjson.RevokeMatch{
		OrderID: idToBytes(match.Taker.ID()),
		MatchID: idToBytes(match.ID()),
	}
	s.authMgr.Sign(takerParams)
	takerReq, err := msgjson.NewRequest(comms.NextID(), msgjson.RevokeMatchRoute, takerParams)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	makerParams := &msgjson.RevokeMatch{
		OrderID: idToBytes(match.Maker.ID()),
		MatchID: idToBytes(match.ID()),
	}
	s.authMgr.Sign(makerParams)
	makerReq, err := msgjson.NewRequest(comms.NextID(), msgjson.RevokeMatchRoute, makerParams)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return takerParams, takerReq, makerParams, makerReq, nil
}

// revoke revokes the match, sending the 'revoke_match' request to each client
// and processing the acknowledgement. Match Sigs and Status are not accessed.
func (s *Swapper) revoke(match *matchTracker) {
	log.Infof("revoke: sending the 'revoke_match' request to each client for match %v", match.ID())
	// Unlock the maker and taker order coins.
	s.unlockOrderCoins(match.Taker)
	s.unlockOrderCoins(match.Maker)

	sendRevReq := func(user account.AccountID, isMaker bool, req *msgjson.Message, params *msgjson.RevokeMatch) {
		ack := &messageAcker{
			user:    user,
			match:   match,
			params:  params,
			isMaker: isMaker,
			// isAudit: false,
		}
		// Expire function to unregister the outstanding request.
		expireFunc := func() {
			s.rmLiveAckers(req.ID)
			log.Infof("revoke_match request failed for user %v (taker)", user)
		}
		// Register that there is an outstanding request.
		s.setLiveAcker(req, ack)
		s.authMgr.RequestWhenConnected(user, req, func(_ comms.Link, resp *msgjson.Message) {
			s.processAck(resp, ack)
		}, auth.DefaultRequestTimeout, auth.DefaultConnectTimeout, expireFunc)
	}

	takerParams, takerReq, makerParams, makerReq, err := s.revocationRequests(match)
	if err != nil {
		log.Errorf("error creating revocation requests: %v", err)
		return
	}

	sendRevReq(match.Taker.User(), false, takerReq, takerParams)
	sendRevReq(match.Maker.User(), true, makerReq, makerParams)
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

// For the 'match' request, the user returns a msgjson.Acknowledgement array
// with signatures for each match ID. The match acknowledgements were requested
// from each matched user in Negotiate.
func (s *Swapper) processMatchAcks(user account.AccountID, msg *msgjson.Message, matches []*messageAcker) {
	// Remove the live acker from Swapper's tracking.
	defer s.rmLiveAckers(msg.ID)

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

	log.Debugf("processMatchAcks: 'match' ack received from %v for %d matches",
		user, len(matches))

	// Verify the signature of each Acknowledgement, and store the signatures in
	// the matchTracker of each match (messageAcker). The signature will be
	// either a MakerMatch or TakerMatch signature depending on whether the
	// responding user is the maker or taker.
	for i, matchInfo := range matches {
		ack := acks[i]
		matchID := matchInfo.match.ID()
		if !bytes.Equal(ack.MatchID, matchID[:]) {
			s.respondError(msg.ID, user, msgjson.IDMismatchError,
				fmt.Sprintf("unexpected match ID at acknowledgment index %d", i))
			return
		}
		sigMsg := matchInfo.params.Serialize()
		err = s.authMgr.Auth(user, sigMsg, ack.Sig)
		if err != nil {
			log.Warnf("processMatchAcks: 'match' ack for match %v from user %v, "+
				" failed sig verification: %v", matchInfo.match.ID(), user, err)
			s.respondError(msg.ID, user, msgjson.SignatureError,
				fmt.Sprintf("signature validation error: %v", err))
			return
		}

		// Store the signature in the DB.
		storFn := s.storage.SaveMatchAckSigB
		if matchInfo.isMaker {
			storFn = s.storage.SaveMatchAckSigA
		}
		mid := db.MarketMatchID{
			MatchID: matchID,
			Base:    matchInfo.match.Maker.BaseAsset, // same for taker's redeem as BaseAsset refers to the market
			Quote:   matchInfo.match.Maker.QuoteAsset,
		}
		err = storFn(mid, ack.Sig)
		if err != nil {
			log.Errorf("saving match ack signature (match id=%v, maker=%v) failed: %v",
				matchID, matchInfo.isMaker, err)
			s.respondError(msg.ID, matchInfo.user, msgjson.UnknownMarketError,
				"internal server error")
			// TODO: revoke the match without penalties?
			return
		}

		// Store the signature in the matchTracker. These must be collected
		// before the init steps begin and swap contracts are broadcasted.
		log.Debugf("processMatchAcks: storing valid 'match' ack signature from %v (maker=%v) "+
			"for match %v (status %v)", user, matchInfo.isMaker, matchID, matchInfo.match.Status)
		matchInfo.match.mtx.Lock()
		if matchInfo.isMaker {
			matchInfo.match.Sigs.MakerMatch = ack.Sig
		} else {
			matchInfo.match.Sigs.TakerMatch = ack.Sig
		}
		matchInfo.match.mtx.Unlock()
	}
}

// CheckUnspent attempts to verify a coin ID for a given asset by retrieving the
// corresponding asset.Coin. If the coin is not found or spent, an
// asset.CoinNotFoundError is returned.
func (s *Swapper) CheckUnspent(asset uint32, coinID []byte) error {
	backend := s.coins[asset]
	if backend == nil {
		return fmt.Errorf("unknown asset %d", asset)
	}

	return backend.Backend.VerifyUnspentCoin(coinID)
}

// LockOrdersCoins locks the backing coins for the provided orders.
func (s *Swapper) LockOrdersCoins(orders []order.Order) {
	// Separate orders according to the asset of their locked coins.
	assetCoinOrders := make(map[uint32][]order.Order, len(orders))
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

// matchNotifications creates a pair of msgjson.Match from a matchTracker.
func matchNotifications(match *matchTracker) (makerMsg *msgjson.Match, takerMsg *msgjson.Match) {
	// NOTE: If we decide that msgjson.Match should just have a
	// "FeeRateBaseSwap" field, this could be set according to the
	// swapStatus.swapAsset field:
	//
	// base, quote := match.Maker.BaseAsset, match.Maker.QuoteAsset
	// feeRate := func(assetID uint32) uint64 {
	// 	if assetID == match.Maker.BaseAsset {
	// 		return match.FeeRateBase
	// 	}
	// 	return match.FeeRateQuote
	// }
	// FeeRateMakerSwap := feeRate(match.makerStatus.swapAsset)

	stamp := encode.UnixMilliU(match.matchTime)
	return &msgjson.Match{
			OrderID:      idToBytes(match.Maker.ID()),
			MatchID:      idToBytes(match.ID()),
			Quantity:     match.Quantity,
			Rate:         match.Rate,
			Address:      extractAddress(match.Taker),
			ServerTime:   stamp,
			FeeRateBase:  match.FeeRateBase,
			FeeRateQuote: match.FeeRateQuote,
			Side:         uint8(order.Maker),
		}, &msgjson.Match{
			OrderID:      idToBytes(match.Taker.ID()),
			MatchID:      idToBytes(match.ID()),
			Quantity:     match.Quantity,
			Rate:         match.Rate,
			Address:      extractAddress(match.Maker),
			ServerTime:   stamp,
			FeeRateBase:  match.FeeRateBase,
			FeeRateQuote: match.FeeRateQuote,
			Side:         uint8(order.Taker),
		}
}

// readMatches translates a slice of raw matches from the market manager into
// a slice of matchTrackers.
func readMatches(matchSets []*order.MatchSet, feeRates map[uint32]uint64) []*matchTracker {
	// The initial capacity guess here is a minimum, but will avoid a few
	// reallocs.
	nowMs := unixMsNow()
	matches := make([]*matchTracker, 0, len(matchSets))
	for _, matchSet := range matchSets {
		for _, match := range matchSet.Matches( /* consider taking feeRates */ ) {
			maker := match.Maker
			base, quote := maker.BaseAsset, maker.QuoteAsset
			var makerSwapAsset, takerSwapAsset uint32
			if maker.Sell {
				makerSwapAsset = base
				takerSwapAsset = quote
			} else {
				makerSwapAsset = quote
				takerSwapAsset = base
			}

			match.FeeRateBase = feeRates[base]
			match.FeeRateQuote = feeRates[quote]

			matches = append(matches, &matchTracker{
				Match:     match,
				time:      nowMs,
				matchTime: match.Epoch.End(),
				makerStatus: &swapStatus{
					swapAsset:   makerSwapAsset,
					redeemAsset: takerSwapAsset,
				},
				takerStatus: &swapStatus{
					swapAsset:   takerSwapAsset,
					redeemAsset: makerSwapAsset,
				},
			})
		}
	}
	return matches
}

// Negotiate takes ownership of the matches and begins swap negotiation. For
// reliable identification of completed orders when redeem acks are received and
// processed by processAck, BeginMatchAndNegotiate should be called prior to
// matching and order status/amount updates, and EndMatchAndNegotiate should be
// called after Negotiate. This locking sequence allows for orders that may
// already be involved in active swaps to remain unmodified by the
// Matcher/Market until new matches are recorded by the Swapper in Negotiate. If
// this is not done, it is possible that an order may be flagged as completed if
// a swap A completes after Matching and creation of swap B but before Negotiate
// has a chance to record the new swap.
func (s *Swapper) Negotiate(matchSets []*order.MatchSet, finalSwap map[order.OrderID]bool) {
	// If the Swapper is stopping, the Markets should also be stopping, but
	// block this just in case.
	s.handlerMtx.RLock()
	defer s.handlerMtx.RUnlock()
	if s.stop {
		log.Errorf("Negotiate called on stopped swapper. Matches lost!")
		return
	}

	feeRates := make(map[uint32]uint64, 2) // should only be 2, but Negotiate might be called with matches with more than two assets in the future

	getFeeRate := func(assetID uint32) (unsupported bool) {
		if _, found := feeRates[assetID]; found {
			return
		}
		asset := s.coins[assetID]
		if asset == nil {
			return true
		}
		maxFeeRate := asset.Asset.MaxFeeRate
		feeRate, err := asset.Backend.FeeRate()
		if err != nil {
			feeRate = maxFeeRate
			log.Warnf("Unable to determining optimal fee rate for %q, using fallback of %d. Err: %v",
				asset.Symbol, feeRate, err)
		} else {
			log.Debugf("Optimal fee rate for %q: %d", asset.Symbol, feeRate)
			if feeRate > maxFeeRate {
				log.Warnf("Optimal fee rate %d > max fee rate %d, using max fee rate.",
					feeRate, maxFeeRate)
				feeRate = maxFeeRate
			}
		}
		feeRates[assetID] = feeRate
		return
	}

	// Lock trade order coins, and get current optimal fee rates. Also filter
	// out matches with unsupported assets, which should not happen if the
	// Market is behaving, but be defensive.
	supportedMatchSets := matchSets[:0]                    // same buffer, start empty
	swapOrders := make([]order.Order, 0, 2*len(matchSets)) // size guess, with the single maker case
	for _, match := range matchSets {
		// Do the fee rate requests first to verify asset support.
		if base := match.Taker.Base(); getFeeRate(base) {
			log.Errorf("Unsupported asset %d for order %v", base, match.Taker)
			continue
		}
		if quote := match.Taker.Quote(); getFeeRate(quote) {
			log.Errorf("Unsupported asset %d for order %v", quote, match.Taker)
			continue
		}

		supportedMatchSets = append(supportedMatchSets, match)

		if match.Taker.Type() == order.CancelOrderType {
			continue
		}

		swapOrders = append(swapOrders, match.Taker)
		for _, maker := range match.Makers {
			swapOrders = append(swapOrders, maker)
		}
	}
	matchSets = supportedMatchSets

	s.LockOrdersCoins(swapOrders)

	// Set up the matchTrackers, which includes a slice of Matches.
	matches := readMatches(matchSets, feeRates)

	// Record the matches. If any DB updates fail, no swaps proceed. We could
	// let the others proceed, but that could seem selective trickery to the
	// clients.
	for _, match := range matches {
		// Note that matches where the taker order is a cancel will be stored
		// with status MatchComplete, and without the maker or taker swap
		// addresses. The match will also be flagged as inactive since there is
		// no associated swap negotiation.

		// TODO: Initially store cancel matches lacking ack sigs as active, only
		// flagging as inactive when both maker and taker match ack sigs have
		// been received. The client will need a mechanism to provide the ack,
		// perhaps having the server resend missing match ack requests on client
		// connect.
		if err := s.storage.InsertMatch(match.Match); err != nil {
			log.Errorf("InsertMatch (match id=%v) failed: %v", match.ID(), err)
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
		userMatches[acker.user] = append(userMatches[acker.user], acker)
	}

	// Setting length to max possible, which is over-allocating by the number of
	// cancels.
	toMonitor := make([]*matchTracker, 0, len(matches))
	var canceled []*order.LimitOrder
	for _, match := range matches {
		if match.Taker.Type() == order.CancelOrderType {
			// The canceled orders must be flagged after new swaps are counted.
			canceled = append(canceled, match.Maker)

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
			s.orders.incActiveSwapCount(match.Maker, finalSwap[match.Maker.ID()])
			s.orders.incActiveSwapCount(match.Taker, finalSwap[match.Taker.ID()])
		}

		// Create an acker for maker and taker, sharing the same matchTracker.
		makerMsg, takerMsg := matchNotifications(match) // msgjson.Match for each party
		addUserMatch(&messageAcker{
			user:    match.Maker.User(),
			match:   match,
			params:  makerMsg,
			isMaker: true,
			// isAudit: false,
		})
		addUserMatch(&messageAcker{
			user:    match.Taker.User(),
			match:   match,
			params:  takerMsg,
			isMaker: false,
			// isAudit: false,
		})
	}

	// Flag the canceled orders as failed and off-book if they are involved in
	// active swaps from this or previous epochs.
	for _, lo := range canceled {
		s.orders.canceled(lo)
	}

	// Add the matches to the map.
	s.matchMtx.Lock()
	for _, match := range toMonitor {
		s.matches[match.ID()] = match
	}
	s.matchMtx.Unlock()

	// Send the user match notifications.
	for user, matches := range userMatches {
		// msgs is a slice of msgjson.Match created by newMatchAckers
		// (matchNotifications) for all makers and takers.
		msgs := make([]msgjson.Signable, 0, len(matches))
		for _, m := range matches {
			msgs = append(msgs, m.params)
		}

		// Solicit match acknowledgments. Each Match is signed in addUserMatch.
		req, err := msgjson.NewRequest(comms.NextID(), msgjson.MatchRoute, msgs)
		if err != nil {
			log.Errorf("error creating match notification request: %v", err)
			// TODO: prevent user penalty
			continue
		}

		// Copy the loop variables for capture by the match acknowledgement
		// response handler.
		u, m := user, matches
		log.Debugf("Negotiate: sending 'match' ack request to user %v for %d matches",
			u, len(m))

		// Register that there is an outstanding ack request. This is
		// unregistered on completion of processAck or request expiry.
		s.setLiveAckers(req, m)

		// Expire function to unregister the outstanding request.
		expireFunc := func() {
			s.rmLiveAckers(req.ID)
			log.Infof("Failed to send request to %v for match acks.", u)
		}

		// Send the ack request.

		// A user gets responseTimeout to respond to a successfully sent request,
		// and reconnectTimeout to connect and and have the request sent.
		s.authMgr.RequestWhenConnected(u, req, func(_ comms.Link, resp *msgjson.Message) {
			s.processMatchAcks(u, resp, m)
		}, auth.DefaultRequestTimeout, auth.DefaultConnectTimeout, expireFunc)
	}
}

func idToBytes(id [order.OrderIDSize]byte) []byte {
	return id[:]
}
