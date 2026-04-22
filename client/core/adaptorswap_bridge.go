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
	"sync"
	"time"

	"decred.org/dcrdex/client/core/adaptorswap"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
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
		// Locked notification; the initiator's wallet and the
		// participant's chain watcher both feed EventLockConfirmed
		// once confirmation depth is reached. This message itself
		// is informational; the orchestrator advances on the
		// chain event.
		return nil, errPurelyInformational
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

// ----- Message router (no-op default) -----

// NoopSender is a message sender that drops outgoing messages. Used
// in tests and as a default when Core has not yet wired up the ws
// message path.
type NoopSender struct{}

func (NoopSender) SendToPeer(route string, payload any) error { return nil }

// ----- Persister stubs -----

type noopAdaptorPersister struct{}

func (noopAdaptorPersister) Save(id [32]byte, s *adaptorswap.Snapshot) error { return nil }
func (noopAdaptorPersister) Load(id [32]byte) (*adaptorswap.Snapshot, error) { return nil, nil }
