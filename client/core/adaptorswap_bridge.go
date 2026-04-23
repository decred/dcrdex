// Bridge between client/core/Core and client/core/adaptorswap.
// Provides per-match orchestrator lifecycle management plus adapter
// implementations that map the orchestrator's interfaces onto
// bitcoind RPC (BTC side) and the optional XMR wallet (XMR side,
// in a build-tagged companion file).
//
// This file is intentionally self-contained: it does not modify
// core.go. Core plugs into the manager via three methods -
// StartSwap, Handle, and Stop - at match creation, inbound message
// receipt, and teardown time respectively.

package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core/adaptorswap"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// AdaptorSwapManager owns per-match orchestrators for adaptor-swap
// markets and exposes the hooks Core needs to drive them.
// Safe for concurrent use.
type AdaptorSwapManager struct {
	mu            sync.Mutex
	orchestrators map[order.MatchID]*adaptorswap.Orchestrator

	btc     adaptorswap.BTCAssetAdapter
	xmr     adaptorswap.XMRAssetAdapter
	persist adaptorswap.StatePersister
	send    adaptorswap.MessageSender
}

// AdaptorSwapManagerConfig is the set of external dependencies an
// AdaptorSwapManager needs. Either the whole bundle is provided for
// production use, or individual adapters can be mocked for tests.
type AdaptorSwapManagerConfig struct {
	BTC     adaptorswap.BTCAssetAdapter
	XMR     adaptorswap.XMRAssetAdapter
	Persist adaptorswap.StatePersister
	Send    adaptorswap.MessageSender
}

// NewAdaptorSwapManager constructs a manager with the given
// adapters. A no-op persister is substituted if Persist is nil.
func NewAdaptorSwapManager(cfg *AdaptorSwapManagerConfig) *AdaptorSwapManager {
	persist := cfg.Persist
	if persist == nil {
		persist = noopAdaptorPersister{}
	}
	return &AdaptorSwapManager{
		orchestrators: make(map[order.MatchID]*adaptorswap.Orchestrator),
		btc:           cfg.BTC,
		xmr:           cfg.XMR,
		persist:       persist,
		send:          cfg.Send,
	}
}

// StartSwap creates and registers an orchestrator for a new match.
// swapCfg.SendMsg / AssetBTC / AssetXMR / Persist are populated from
// the manager's defaults and the caller's partially-filled config is
// overwritten where those fields are zero-valued.
func (m *AdaptorSwapManager) StartSwap(swapCfg *adaptorswap.Config) (*adaptorswap.Orchestrator, error) {
	if swapCfg == nil {
		return nil, errors.New("nil swap config")
	}
	if swapCfg.AssetBTC == nil {
		swapCfg.AssetBTC = m.btc
	}
	if swapCfg.AssetXMR == nil {
		swapCfg.AssetXMR = m.xmr
	}
	if swapCfg.Persist == nil {
		swapCfg.Persist = m.persist
	}
	if swapCfg.SendMsg == nil {
		swapCfg.SendMsg = m.send
	}
	o, err := adaptorswap.NewOrchestrator(swapCfg)
	if err != nil {
		return nil, fmt.Errorf("new orchestrator: %w", err)
	}
	m.mu.Lock()
	m.orchestrators[swapCfg.MatchID] = o
	m.mu.Unlock()
	if err := o.Start(); err != nil {
		return nil, fmt.Errorf("start orchestrator: %w", err)
	}
	return o, nil
}

// Handle routes an inbound Adaptor* message to the orchestrator for
// the given match. Called from Core's message dispatch for routes
// beginning with "adaptor_".
func (m *AdaptorSwapManager) Handle(route string, matchID order.MatchID, payload any) error {
	m.mu.Lock()
	o, ok := m.orchestrators[matchID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no orchestrator for match %s", matchID)
	}
	evt, err := routeToEvent(route, payload)
	if err != nil {
		return err
	}
	return o.Handle(evt)
}

// Stop removes and tears down an orchestrator. Called at match
// completion or cancellation.
func (m *AdaptorSwapManager) Stop(matchID order.MatchID) {
	m.mu.Lock()
	delete(m.orchestrators, matchID)
	m.mu.Unlock()
}

// adaptorTerminalCallback returns a closure that the orchestrator
// invokes once when reaching a terminal phase. Logs the outcome,
// updates the order's status (best-effort - canceled or executed
// depending on the terminal phase), and removes the orchestrator
// from the manager registry so its memory is reclaimed.
func (c *Core) adaptorTerminalCallback(tracker *trackedTrade, matchID order.MatchID,
	mktName string) func(adaptorswap.Phase) {

	return func(phase adaptorswap.Phase) {
		switch phase {
		case adaptorswap.PhaseComplete:
			c.log.Infof("Adaptor swap complete for match %s on %s", matchID, mktName)
		case adaptorswap.PhaseFailed:
			c.log.Warnf("Adaptor swap failed for match %s on %s", matchID, mktName)
		case adaptorswap.PhasePunish:
			c.log.Warnf("Adaptor swap reached punish branch for match %s on %s "+
				"(participant took BTC; XMR forfeited)", matchID, mktName)
		default:
			return
		}
		// Tear down the per-match orchestrator. The trackedTrade's
		// order status update is best-effort; if no other matches
		// remain on the order, mark it executed so it leaves the
		// active set.
		c.adaptorMgr.Stop(matchID)
		if tracker != nil {
			tracker.mtx.Lock()
			if tracker.metaData != nil &&
				tracker.metaData.Status < order.OrderStatusExecuted {
				tracker.metaData.Status = order.OrderStatusExecuted
			}
			tracker.mtx.Unlock()
		}
	}
}

// spendObserverFor returns a closure suitable for
// adaptorswap.Config.SpendObserver. Invoked by the initiator at
// PhaseSpendPresig with the lock outpoint; the closure spawns a
// goroutine that polls the BTC adapter's ObserveSpend, then feeds
// the witness back via Handle as EventSpendObservedOnChain.
func (c *Core) spendObserverFor(matchID order.MatchID) func(wire.OutPoint, int64) {
	return func(outpoint wire.OutPoint, startHeight int64) {
		m := c.adaptorMgr
		m.mu.Lock()
		o, ok := m.orchestrators[matchID]
		m.mu.Unlock()
		if !ok {
			c.log.Warnf("spendObserverFor: no orchestrator for match %s", matchID)
			return
		}
		btc := o.Cfg().AssetBTC
		if btc == nil {
			c.log.Errorf("spendObserverFor match %s: AssetBTC nil; "+
				"cannot observe spend to recover XMR scalar", matchID)
			return
		}
		go func() {
			c.log.Infof("spendObserverFor match %s: watching outpoint %s from height %d",
				matchID, outpoint, startHeight)
			witness, err := btc.ObserveSpend(outpoint, startHeight)
			if err != nil {
				c.log.Errorf("spendObserverFor match %s: ObserveSpend: %v; "+
					"swap stalled at PhaseSpendPresig", matchID, err)
				return
			}
			c.log.Infof("spendObserverFor match %s: spend observed, feeding witness into orchestrator",
				matchID)
			// Confirm the orchestrator is still registered before
			// dispatching - the swap may have been Stopped.
			m.mu.Lock()
			o, ok := m.orchestrators[matchID]
			m.mu.Unlock()
			if !ok {
				return
			}
			if err := o.Handle(adaptorswap.EventSpendObservedOnChain{Witness: witness}); err != nil {
				c.log.Errorf("spendObserverFor match %s: orchestrator Handle: %v",
					matchID, err)
			}
		}()
	}
}

// OnCounterPartyAddress is the entry point used by Core's
// handleCounterPartyAddressMsg to forward the counterparty's BTC
// payout address to the right orchestrator. Returns handled=false
// when no orchestrator exists for matchID, in which case the caller
// should fall back to the HTLC path. handled=true with err!=nil
// means the address was for an adaptor match but could not be
// applied (decode failure, mismatch with a previously recorded
// value).
func (m *AdaptorSwapManager) OnCounterPartyAddress(matchID order.MatchID,
	addr string, net dex.Network) (handled bool, err error) {

	m.mu.Lock()
	o, ok := m.orchestrators[matchID]
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	// Only the initiator needs the peer's BTC payout (to build the
	// spendTx output). The participant receives the maker's XMR
	// redemption address here, which is irrelevant to the adaptor
	// flow - the participant gets BTC at their own payout address
	// announced via AdaptorSetupPart.
	if o.Role() != adaptorswap.RoleInitiator {
		return true, nil
	}
	script, err := btcAddressToScript(addr, net)
	if err != nil {
		return true, fmt.Errorf("decode peer btc address %q: %w", addr, err)
	}
	return true, o.SetPeerBTCPayoutScript(script)
}

// routeToEvent maps a msgjson Adaptor* route + payload to the
// orchestrator's Event type. Events come from three sources:
// inbound peer messages (the majority), chain observations (fed
// separately), and timeouts. This handles the inbound-peer path.
func routeToEvent(route string, payload any) (adaptorswap.Event, error) {
	switch route {
	case msgjson.AdaptorSetupPartRoute:
		m, ok := payload.(*msgjson.AdaptorSetupPart)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptorswap.EventKeysReceived{Setup: m}, nil
	case msgjson.AdaptorSetupInitRoute:
		m, ok := payload.(*msgjson.AdaptorSetupInit)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptorswap.EventKeysReceived{Setup: m}, nil
	case msgjson.AdaptorRefundPresignedRoute:
		m, ok := payload.(*msgjson.AdaptorRefundPresigned)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptorswap.EventRefundPresignedReceived{
			RefundSig:  m.RefundSig,
			AdaptorSig: m.SpendRefundAdaptorSig,
		}, nil
	case msgjson.AdaptorLockedRoute:
		// AdaptorLocked carries the initiator's confirmed lockTx
		// outpoint + value. In the absence of a participant-side
		// chain watcher (production deployments would add one) we
		// trust the wire claim and fire EventLockConfirmed so the
		// participant proceeds to send XMR. The matching server-
		// side coordinator validates the claim; a malicious
		// initiator who lies here gets caught when the participant
		// later observes the chain or the audit kicks in.
		if _, ok := payload.(*msgjson.AdaptorLocked); !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		// Height isn't carried on AdaptorLocked; participant uses
		// it only to record into LockHeight, which is informational
		// (XMR restore-from height comes from the participant's own
		// XMR wallet, not BTC height). Zero is fine.
		return adaptorswap.EventLockConfirmed{Height: 0}, nil
	case msgjson.AdaptorXmrLockedRoute:
		m, ok := payload.(*msgjson.AdaptorXmrLocked)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptorswap.EventKeysReceived{Setup: m}, nil
	case msgjson.AdaptorSpendPresigRoute:
		m, ok := payload.(*msgjson.AdaptorSpendPresig)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptorswap.EventSpendPresigReceived{
			SpendTx:    m.SpendTx,
			AdaptorSig: m.AdaptorSig,
		}, nil
	case msgjson.AdaptorSpendBroadcastRoute:
		return nil, errPurelyInformational
	case msgjson.AdaptorRefundBroadcastRoute:
		return adaptorswap.EventRefundObservedOnChain{}, nil
	case msgjson.AdaptorCoopRefundRoute:
		return adaptorswap.EventCoopRefundObserved{}, nil
	case msgjson.AdaptorPunishRoute:
		m, ok := payload.(*msgjson.AdaptorPunish)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptorswap.EventPunishObserved{TxID: m.TxID}, nil
	}
	return nil, fmt.Errorf("unknown adaptor route %q", route)
}

// errPurelyInformational is returned for routes whose inbound
// receipt carries no state transition on the receiving orchestrator
// (state advances via a chain event instead). Callers should ignore
// this error rather than surface it as a failure.
var errPurelyInformational = errors.New("informational message; no event")

// startAdaptorMatches kicks off an adaptor-swap orchestrator for
// each msgMatch on an adaptor-market trade. Called from
// negotiateMatches when the market's SwapType is SwapTypeAdaptor.
//
// The Config is built from data available at match time: identity,
// pair assets, amounts, role, and the operator-set CSV window. Asset
// adapters and the message sender come from the AdaptorSwapManager
// defaults.
//
// Wallet-dependent fields - PeerBTCPayoutScript, OwnXMRSweepDest,
// XmrNetTag - are not yet sourced here; the orchestrator is
// constructed without them and will need them populated before it
// can build the lock/spend transactions. Their wiring is the next
// step (collect via the existing CounterPartyAddress route +
// per-asset wallet address lookups).
func (c *Core) startAdaptorMatches(tracker *trackedTrade, msgMatches []*msgjson.Match,
	mkt *msgjson.Market) error {

	for _, m := range msgMatches {
		if len(m.MatchID) != order.MatchIDSize {
			c.log.Errorf("adaptor match: bad matchid length %d for order %s",
				len(m.MatchID), tracker.ID())
			continue
		}
		var matchID order.MatchID
		copy(matchID[:], m.MatchID)

		// Role: under Option 1 enforcement, the BTC-side holder is
		// always the maker. So Side==Maker => initiator.
		role := adaptorswap.RoleParticipant
		if m.Side == uint8(order.Maker) {
			role = adaptorswap.RoleInitiator
		}

		// Pair-asset assignment from the market's scriptable side.
		pairBTC := mkt.ScriptableAsset
		var pairXMR uint32
		if pairBTC == mkt.Base {
			pairXMR = mkt.Quote
		} else {
			pairXMR = mkt.Base
		}

		// Amount conversion: Quantity is in base units.
		var btcAmt int64
		var xmrAmt uint64
		if pairBTC == mkt.Base {
			btcAmt = int64(m.Quantity)
			xmrAmt = calc.BaseToQuote(m.Rate, m.Quantity)
		} else {
			btcAmt = int64(calc.BaseToQuote(m.Rate, m.Quantity))
			xmrAmt = m.Quantity
		}

		var swapID, oid [32]byte
		copy(swapID[:], matchID[:])
		copy(oid[:], tracker.ID().Bytes())

		// Per-role wallet address pre-fetch:
		//  - Initiator (BTC holder) sweeps XMR back to its own
		//    XMR deposit address.
		//  - Participant (XMR holder) is paid BTC at its own BTC
		//    deposit address; the address is sent in
		//    AdaptorSetupPart so the initiator can build the spendTx
		//    output that targets it.
		var ownXMRDest, ownBTCAddr string
		if role == adaptorswap.RoleInitiator {
			ownXMRDest = c.adaptorOwnDepositAddr(pairXMR)
		} else {
			ownBTCAddr = c.adaptorOwnDepositAddr(pairBTC)
		}
		xmrWallet, _ := c.wallet(pairXMR)
		// HACK (adaptor swaps): per-swap BTC/XMR adapters. Prefer
		// adapters pre-installed on the manager (tests inject mocks
		// that way); fall back to env-var BTC RPC + connected XMR
		// wallet otherwise. Until per-wallet adapter plumbing lands
		// (README TODO #5), this is how the orchestrator gets working
		// asset access. Fail fast here instead of letting the
		// orchestrator panic later on a typed-nil interface.
		var btcAdapter adaptorswap.BTCAssetAdapter = c.adaptorMgr.btc
		if btcAdapter == nil {
			if a := buildAdaptorBTCAdapter(c.log); a != nil {
				btcAdapter = a
			}
		}
		if btcAdapter == nil {
			c.log.Errorf("adaptor match %s: BTC adapter unavailable; "+
				"set DCRDEX_ADAPTOR_BTC_{RPC,USER,PASS}", matchID)
			continue
		}
		xmrAdapter := c.adaptorMgr.xmr
		if xmrAdapter == nil {
			xmrAdapter = buildAdaptorXMRAdapter(c.ctx, xmrWallet)
		}
		if xmrAdapter == nil {
			c.log.Errorf("adaptor match %s: XMR adapter unavailable; "+
				"connect the XMR wallet (build with -tags xmr)", matchID)
			continue
		}
		cfg := &adaptorswap.Config{
			SwapID:           swapID,
			OrderID:          oid,
			MatchID:          matchID,
			Role:             role,
			PairBTC:          pairBTC,
			PairXMR:          pairXMR,
			BtcAmount:        btcAmt,
			XmrAmount:        xmrAmt,
			LockBlocks:       mkt.LockBlocks,
			XmrNetTag:        xmrNetTagForNet(c.net),
			OwnXMRSweepDest:  ownXMRDest,
			OwnBTCPayoutAddr: ownBTCAddr,
			AssetBTC:         btcAdapter,
			AssetXMR:         xmrAdapter,
			// DecodeBTCAddr lets the initiator translate the
			// participant's BTCPayoutAddr (received in AdaptorSetupPart)
			// into a pkScript without the orchestrator needing to
			// know about chain params.
			DecodeBTCAddr: func(addr string) ([]byte, error) {
				return btcAddressToScript(addr, c.net)
			},
			// SpendObserver runs the BTC ObserveSpend polling loop
			// in a background goroutine and feeds the witness back
			// via the manager's Handle once seen on-chain. Closes
			// the gap between PhaseSpendPresig (initiator has sent
			// the adaptor sig) and PhaseXmrSwept (initiator has
			// recovered the participant's scalar and swept XMR).
			SpendObserver: c.spendObserverFor(matchID),
			// OnTerminal logs the swap outcome and unregisters the
			// orchestrator from the manager's pool. Order/match
			// status updates are best-effort and live alongside.
			OnTerminal: c.adaptorTerminalCallback(tracker, matchID, mkt.Name),
			// SendMsg is wired per-match to the trade's
			// dexConnection so outbound adaptor_* messages
			// actually reach the server.
			SendMsg: &dcSender{dc: tracker.dc},
		}
		if _, err := c.adaptorMgr.StartSwap(cfg); err != nil {
			c.log.Errorf("AdaptorSwapManager.StartSwap match %s: %v", matchID, err)
			continue
		}
		c.log.Infof("Adaptor swap started for match %s on %s (role=%s, btc=%d, xmr=%d)",
			matchID, mkt.Name, role, btcAmt, xmrAmt)
	}
	return nil
}

// handleAdaptorMsg is the routeHandler for every adaptor_* route in
// Core's noteHandlers map. It unmarshals the payload according to
// msg.Route, extracts the MatchID, and dispatches into Core's
// AdaptorSwapManager. Informational routes (no state change on the
// receiving side) are silently dropped.
//
// dexConnection is unused for now; a future refactor will allow
// orchestrators to send messages back to the same dc, at which point
// the manager's per-swap MessageSender will close over dc and the
// route table can stop indirecting through Core.
func handleAdaptorMsg(c *Core, _ *dexConnection, msg *msgjson.Message) error {
	payload, matchID, err := decodeAdaptorMsg(msg)
	if err != nil {
		return fmt.Errorf("decode %s: %w", msg.Route, err)
	}
	if err := c.adaptorMgr.Handle(msg.Route, matchID, payload); err != nil {
		if IsInformational(err) {
			return nil
		}
		return err
	}
	return nil
}

// decodeAdaptorMsg unmarshals msg's payload into the type appropriate
// for msg.Route and returns it alongside the match ID. Returns an
// error for unknown adaptor routes or malformed payloads.
func decodeAdaptorMsg(msg *msgjson.Message) (any, order.MatchID, error) {
	var matchBytes []byte
	var payload any
	switch msg.Route {
	case msgjson.AdaptorSetupPartRoute:
		p := new(msgjson.AdaptorSetupPart)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorSetupInitRoute:
		p := new(msgjson.AdaptorSetupInit)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorRefundPresignedRoute:
		p := new(msgjson.AdaptorRefundPresigned)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorLockedRoute:
		p := new(msgjson.AdaptorLocked)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorXmrLockedRoute:
		p := new(msgjson.AdaptorXmrLocked)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorSpendPresigRoute:
		p := new(msgjson.AdaptorSpendPresig)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorSpendBroadcastRoute:
		p := new(msgjson.AdaptorSpendBroadcast)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorRefundBroadcastRoute:
		p := new(msgjson.AdaptorRefundBroadcast)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorCoopRefundRoute:
		p := new(msgjson.AdaptorCoopRefund)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorPunishRoute:
		p := new(msgjson.AdaptorPunish)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	default:
		return nil, order.MatchID{}, fmt.Errorf("unknown adaptor route %q", msg.Route)
	}
	if len(matchBytes) != order.MatchIDSize {
		return nil, order.MatchID{}, fmt.Errorf("matchid length %d, want %d",
			len(matchBytes), order.MatchIDSize)
	}
	var matchID order.MatchID
	copy(matchID[:], matchBytes)
	return payload, matchID, nil
}

// IsInformational reports whether an error from Handle is the
// "informational message" sentinel and can be safely ignored.
func IsInformational(err error) bool {
	return errors.Is(err, errPurelyInformational)
}

// ----- BTC adapter (RPC-backed) -----

// BTCRPCAdapter implements adaptorswap.BTCAssetAdapter against a
// btcd-compatible RPC endpoint. Uses internal/adaptorsigs/btc's
// FundBroadcastTaproot + ObserveSpend helpers for the heavy lifting.
type BTCRPCAdapter struct {
	client       *rpcclient.Client
	waitConfirm  func(ctx context.Context, txid *chainhash.Hash) (int64, error)
	observeStart int64 // height to start ObserveSpend scans from
}

// NewBTCRPCAdapter returns an adapter bound to client. waitConfirm
// is called after each lockTx broadcast to wait for confirmation;
// typical implementations mine a block on regtest or poll for a
// confirm on mainnet.
func NewBTCRPCAdapter(client *rpcclient.Client,
	waitConfirm func(context.Context, *chainhash.Hash) (int64, error)) *BTCRPCAdapter {
	return &BTCRPCAdapter{client: client, waitConfirm: waitConfirm}
}

func (b *BTCRPCAdapter) FundBroadcastTaproot(pkScript []byte, value int64) (*wire.MsgTx, uint32, int64, error) {
	ctx := context.Background()
	return btcadaptor.FundBroadcastTaproot(ctx, b.client, pkScript, value, b.waitConfirm)
}

func (b *BTCRPCAdapter) ObserveSpend(outpoint wire.OutPoint, startHeight int64) ([][]byte, error) {
	ctx := context.Background()
	w, err := btcadaptor.ObserveSpend(ctx, b.client, outpoint, startHeight, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return [][]byte(w), nil
}

func (b *BTCRPCAdapter) BroadcastTx(tx *wire.MsgTx) (string, error) {
	// Use RawRequest to avoid the version-detection failure in
	// btcd's SendRawTransaction against Bitcoin Core 28+. The same
	// trick is used by internal/cmd/btcxmrswap.
	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return "", err
	}
	hexTx, err := json.Marshal(hex.EncodeToString(buf.Bytes()))
	if err != nil {
		return "", err
	}
	raw, err := b.client.RawRequest("sendrawtransaction",
		[]json.RawMessage{hexTx})
	if err != nil {
		return "", err
	}
	var txid string
	if err := json.Unmarshal(raw, &txid); err != nil {
		return "", err
	}
	return txid, nil
}

func (b *BTCRPCAdapter) CurrentHeight() (int64, error) {
	return b.client.GetBlockCount()
}

// HACK (adaptor swaps): buildAdaptorBTCAdapter reads simnet BTC RPC
// credentials from the environment and returns a BTCRPCAdapter. The
// adaptor orchestrator needs an *rpcclient.Client that can FundRaw /
// SignRaw / SendRaw, and bisonw's BTC wallet does not expose its own
// client. Side-channel the connection via env vars until per-wallet
// adapter plumbing lands (README TODO #5). Returns nil if unset.
//
// Env vars:
//
//	DCRDEX_ADAPTOR_BTC_RPC  host:port, e.g. 127.0.0.1:20556
//	DCRDEX_ADAPTOR_BTC_USER rpc user
//	DCRDEX_ADAPTOR_BTC_PASS rpc password
func buildAdaptorBTCAdapter(log dex.Logger) *BTCRPCAdapter {
	host := os.Getenv("DCRDEX_ADAPTOR_BTC_RPC")
	user := os.Getenv("DCRDEX_ADAPTOR_BTC_USER")
	pass := os.Getenv("DCRDEX_ADAPTOR_BTC_PASS")
	if host == "" || user == "" || pass == "" {
		return nil
	}
	cl, err := rpcclient.New(&rpcclient.ConnConfig{
		Host:         host,
		User:         user,
		Pass:         pass,
		HTTPPostMode: true,
		DisableTLS:   true,
	}, nil)
	if err != nil {
		log.Errorf("adaptor btc rpc setup: %v", err)
		return nil
	}
	waitConfirm := func(ctx context.Context, txid *chainhash.Hash) (int64, error) {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-tick.C:
			}
			res, err := cl.GetRawTransactionVerbose(txid)
			if err != nil {
				continue
			}
			if res.Confirmations < 1 || res.BlockHash == "" {
				continue
			}
			bh, err := chainhash.NewHashFromStr(res.BlockHash)
			if err != nil {
				return 0, err
			}
			hdr, err := cl.GetBlockHeaderVerbose(bh)
			if err != nil {
				return 0, err
			}
			return int64(hdr.Height), nil
		}
	}
	log.Infof("adaptor btc rpc adapter: host=%s", host)
	return NewBTCRPCAdapter(cl, waitConfirm)
}

// ----- Message router (no-op default) -----

// NoopSender is a message sender that drops outgoing messages. Used
// in tests and as a default when Core has not yet wired up the ws
// message path.
type NoopSender struct{}

func (NoopSender) SendToPeer(route string, payload any) error { return nil }

// dcSender is the production adaptorswap.MessageSender. Outbound
// adaptor_* messages from the orchestrator are wrapped in
// notifications and pushed through the dexConnection's websocket.
// The server-side coordinator validates each message and routes it
// to the matched counterparty (which receives it via its own
// noteHandlers / handleAdaptorMsg path).
type dcSender struct {
	dc *dexConnection
}

func (s *dcSender) SendToPeer(route string, payload any) error {
	// The server's handleAdaptorMsg authenticates every adaptor_*
	// payload against the user's account key. Sign before sending.
	if signable, ok := payload.(msgjson.Signable); ok {
		if s.dc.acct.locked() {
			return fmt.Errorf("cannot sign %s: %s account locked", route, s.dc.acct.host)
		}
		sign(s.dc.acct.privKey, signable)
	}
	msg, err := msgjson.NewNotification(route, payload)
	if err != nil {
		return fmt.Errorf("NewNotification %s: %w", route, err)
	}
	return s.dc.Send(msg)
}

// ----- Persister stubs -----

type noopAdaptorPersister struct{}

func (noopAdaptorPersister) Save(id [32]byte, s *adaptorswap.Snapshot) error { return nil }
func (noopAdaptorPersister) Load(id [32]byte) (*adaptorswap.Snapshot, error) { return nil, nil }

// btcAddressToScript decodes a BTC address string and returns its
// pkScript. chaincfg params are derived from the dex network (a
// later refactor may consult the connected BTC wallet for its
// configured params instead).
func btcAddressToScript(addr string, n dex.Network) ([]byte, error) {
	var params *chaincfg.Params
	switch n {
	case dex.Mainnet:
		params = &chaincfg.MainNetParams
	case dex.Testnet:
		params = &chaincfg.TestNet3Params
	case dex.Simnet:
		params = &chaincfg.RegressionNetParams
	default:
		return nil, fmt.Errorf("unsupported network %v", n)
	}
	a, err := btcutil.DecodeAddress(addr, params)
	if err != nil {
		return nil, fmt.Errorf("DecodeAddress: %w", err)
	}
	if !a.IsForNet(params) {
		return nil, fmt.Errorf("address %q not for network %v", addr, n)
	}
	return txscript.PayToAddrScript(a)
}

// xmrNetTagForNet returns the Monero network tag byte used in
// base58 address encoding for the given dex network.
//
//   - Mainnet (and Regtest, which uses mainnet-shaped addresses
//     under the dex/testing/xmr harness's regtest=1 monerod) -> 18
//   - Testnet -> 24 (stagenet, because monero_c has known address-
//     validation bugs on testnet; the btcxmrswap CLI uses the same
//     workaround)
func xmrNetTagForNet(n dex.Network) uint64 {
	switch n {
	case dex.Mainnet, dex.Simnet:
		return 18
	case dex.Testnet:
		return 24
	}
	return 18
}

// adaptorOwnDepositAddr returns a deposit address from the
// connected wallet for assetID, or empty if the wallet is not
// connected or does not implement asset.NewAddresser. Used to
// populate the orchestrator's OwnXMRSweepDest (initiator) and
// OwnBTCPayoutAddr (participant) at swap-setup time.
func (c *Core) adaptorOwnDepositAddr(assetID uint32) string {
	c.walletMtx.RLock()
	w, ok := c.wallets[assetID]
	c.walletMtx.RUnlock()
	if !ok || !w.connected() {
		return ""
	}
	na, is := w.Wallet.(asset.NewAddresser)
	if !is {
		return ""
	}
	addr, err := na.NewAddress()
	if err != nil {
		c.log.Warnf("NewAddress (asset %d) for adaptor swap: %v", assetID, err)
		return ""
	}
	return addr
}
