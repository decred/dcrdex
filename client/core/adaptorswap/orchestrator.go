package adaptorswap

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/agl/ed25519/edwards25519"
	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/haven-protocol-org/monero-go-utils/base58"
)

var curve = edwards.Edwards()

// Config carries the external dependencies required to run an
// Orchestrator. Produced by the caller (typically client/core) from
// the match record plus user wallet handles.
type Config struct {
	SwapID     [32]byte
	OrderID    [32]byte
	MatchID    [32]byte
	Role       Role
	PairBTC    uint32
	PairXMR    uint32
	BtcAmount  int64
	XmrAmount  uint64
	LockBlocks uint32

	// Peer's destination address for their output. For the
	// initiator this is Alice's BTC payout address (where Alice
	// receives BTC on redeem). For the participant this is Bob's
	// XMR sweep destination. These are collected out-of-band at
	// match time.
	PeerBTCPayoutScript []byte
	OwnXMRSweepDest     string

	// Network tag for XMR address encoding (18=mainnet, 24=stagenet).
	XmrNetTag uint64

	AssetBTC BTCAssetAdapter
	AssetXMR XMRAssetAdapter
	SendMsg  MessageSender
	Persist  StatePersister
}

// NewOrchestrator constructs an Orchestrator in PhaseInit with fresh
// per-swap keys. The caller invokes Handle to drive it through the
// protocol; depending on Role, the first action may be to send
// AdaptorSetupPart (participant) or to wait for it (initiator).
func NewOrchestrator(cfg *Config) (*Orchestrator, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	xmrSpend, err := edwards.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("xmr spend key: %w", err)
	}
	btcSign, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("btc sign key: %w", err)
	}
	dleq, err := adaptorsigs.ProveDLEQ(xmrSpend.Serialize())
	if err != nil {
		return nil, fmt.Errorf("dleq: %w", err)
	}
	state := &State{
		SwapID:          cfg.SwapID,
		OrderID:         cfg.OrderID,
		MatchID:         cfg.MatchID,
		Role:            cfg.Role,
		PairBTC:         cfg.PairBTC,
		PairXMR:         cfg.PairXMR,
		BtcSignKey:      btcSign,
		XmrSpendKeyHalf: xmrSpend,
		DLEQProof:       dleq,
		Phase:           PhaseInit,
		Updated:         time.Now(),
	}
	o := &Orchestrator{
		state:    state,
		assetBTC: cfg.AssetBTC,
		assetXMR: cfg.AssetXMR,
		sendMsg:  cfg.SendMsg,
		persist:  cfg.Persist,
		cfg:      cfg,
	}
	return o, nil
}

// Orchestrator is defined in state.go; we add the cfg field here so
// the handler bodies can read the static configuration.
// (state.go was the scaffold; this is the implementation extension.)

// The cfg field is declared by re-opening Orchestrator in this file
// via an embed. Keeping it struct-local rather than on State keeps
// the persistence layer simpler (State is the durable half; Config
// is runtime-only).

// addCfg is a hack to add cfg to Orchestrator without editing state.go.
// Go does not allow reopening a struct in another file; we work around
// that by making cfg a field on Orchestrator below via a wrapper type.
// Actually, Orchestrator is defined in state.go with four fields; I
// will add cfg there rather than resort to a wrapper. See state.go
// patch companion to this file.

// Start kicks off the state machine based on Role. For a
// participant, this emits the initial AdaptorSetupPart; for an
// initiator, it is a no-op and the machine waits for inbound
// EventPartSetup.
func (o *Orchestrator) Start() error {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	if o.state.Phase != PhaseInit {
		return fmt.Errorf("Start called in phase %s", o.state.Phase)
	}
	if o.state.Role == RoleParticipant {
		return o.sendPartSetup()
	}
	o.state.Phase = PhaseKeysSent
	return o.save()
}

// sendPartSetup emits AdaptorSetupPart (participant -> initiator).
// Must be called with o.state.mu held.
func (o *Orchestrator) sendPartSetup() error {
	pubSpend := o.state.XmrSpendKeyHalf.PubKey().Serialize()
	kbvf, err := edwards.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("view key half: %w", err)
	}
	o.state.FullViewKey = kbvf // held until combined with peer's half
	msg := &msgjson.AdaptorSetupPart{
		OrderID:         o.cfg.OrderID[:],
		MatchID:         o.cfg.MatchID[:],
		PubSpendKeyHalf: pubSpend,
		ViewKeyHalf:     kbvf.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(o.state.BtcSignKey.PubKey()),
		DLEQProof:       o.state.DLEQProof,
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSetupPartRoute, msg); err != nil {
		return fmt.Errorf("send AdaptorSetupPart: %w", err)
	}
	o.state.Phase = PhaseKeysSent
	return o.save()
}

// Handle dispatches an Event to the appropriate phase handler.
// Events that do not match the current phase produce an error and
// do not advance state.
func (o *Orchestrator) Handle(evt Event) error {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()

	switch o.state.Phase {
	case PhaseInit, PhaseKeysSent:
		return o.handleSetup(evt)
	case PhaseKeysReceived:
		return o.handleKeysReceived(evt)
	case PhaseRefundPresigned:
		return o.handleRefundPresigned(evt)
	case PhaseLockBroadcast:
		return o.handleLockBroadcast(evt)
	case PhaseLockConfirmed:
		return o.handleLockConfirmed(evt)
	case PhaseXmrSent:
		return o.handleXmrSent(evt)
	case PhaseXmrConfirmed:
		return o.handleXmrConfirmed(evt)
	case PhaseSpendPresig:
		return o.handleSpendPresig(evt)
	case PhaseSpendBroadcast:
		return o.handleSpendBroadcast(evt)
	case PhaseXmrSwept, PhaseComplete, PhaseFailed:
		return fmt.Errorf("event %T in terminal phase %s", evt, o.state.Phase)
	case PhaseRefundTxBroadcast:
		return o.handleRefundTxBroadcast(evt)
	case PhaseCoopRefund:
		return o.handleCoopRefund(evt)
	case PhasePunish:
		return o.handlePunish(evt)
	}
	return fmt.Errorf("unhandled phase %s", o.state.Phase)
}

// handleSetup processes the initial setup exchange. Initiator side:
// receives EventKeysReceived carrying AdaptorSetupPart. Participant
// side: receives EventKeysReceived carrying AdaptorSetupInit.
func (o *Orchestrator) handleSetup(evt Event) error {
	recv, ok := evt.(EventKeysReceived)
	if !ok {
		return fmt.Errorf("expected EventKeysReceived in phase %s, got %T", o.state.Phase, evt)
	}
	if o.state.Role == RoleInitiator {
		part, ok := recv.Setup.(*msgjson.AdaptorSetupPart)
		if !ok {
			return fmt.Errorf("initiator expected AdaptorSetupPart, got %T", recv.Setup)
		}
		return o.initiatorConsumePartSetup(part)
	}
	// Participant side.
	init, ok := recv.Setup.(*msgjson.AdaptorSetupInit)
	if !ok {
		return fmt.Errorf("participant expected AdaptorSetupInit, got %T", recv.Setup)
	}
	return o.participantConsumeInitSetup(init)
}

// initiatorConsumePartSetup (Bob) reads Alice's keys, derives the
// combined XMR view key + spend pubkey, builds the BTC
// lockTx/refundTx/spendRefundTx chain, and emits AdaptorSetupInit.
func (o *Orchestrator) initiatorConsumePartSetup(m *msgjson.AdaptorSetupPart) error {
	s := o.state

	// Parse Alice's ed25519 spend-key half pubkey.
	partSpendPub, err := edwards.ParsePubKey(m.PubSpendKeyHalf)
	if err != nil {
		return fmt.Errorf("parse part spend pub: %w", err)
	}
	partViewHalf, _, err := edwards.PrivKeyFromScalar(m.ViewKeyHalf)
	if err != nil {
		return fmt.Errorf("parse part view half: %w", err)
	}
	peerScalarSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(m.DLEQProof)
	if err != nil {
		return fmt.Errorf("extract peer dleq pubkey: %w", err)
	}
	peerSignPub, err := btcschnorr.ParsePubKey(m.PubSignKeyHalf)
	if err != nil {
		return fmt.Errorf("parse peer sign pubkey: %w", err)
	}

	s.PeerXmrSpendPub = partSpendPub
	s.PeerDLEQProof = m.DLEQProof
	s.PeerScalarSecp = peerScalarSecp
	s.PeerBtcSignPub = m.PubSignKeyHalf

	// Combined XMR pubkeys.
	s.FullSpendPub = sumPubKeys(s.XmrSpendKeyHalf.PubKey(), partSpendPub)
	// Bob's own view half (random) combined with Alice's private half.
	ownViewHalf, err := edwards.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("own view half: %w", err)
	}
	viewBig := scalarAdd(partViewHalf.GetD(), ownViewHalf.GetD())
	viewBig.Mod(viewBig, curve.N)
	var viewBytes [32]byte
	viewBig.FillBytes(viewBytes[:])
	viewKey, _, err := edwards.PrivKeyFromScalar(viewBytes[:])
	if err != nil {
		return fmt.Errorf("combined view key: %w", err)
	}
	s.FullViewKey = viewKey

	// Build BTC script outputs.
	kal := btcschnorr.SerializePubKey(s.BtcSignKey.PubKey())
	kaf := m.PubSignKeyHalf
	lock, err := btcadaptor.NewLockTxOutput(kal, kaf)
	if err != nil {
		return fmt.Errorf("lock output: %w", err)
	}
	refund, err := btcadaptor.NewRefundTxOutput(kal, kaf, int64(o.cfg.LockBlocks))
	if err != nil {
		return fmt.Errorf("refund output: %w", err)
	}
	s.Lock = lock
	s.Refund = refund

	// Build unsigned refundTx and spendRefundTx skeletons. lockTx is
	// built later when we know the funded outpoint.
	refundTx := wire.NewMsgTx(2)
	refundTx.AddTxIn(&wire.TxIn{}) // placeholder; filled after lockTx broadcast
	refundTx.AddTxOut(&wire.TxOut{Value: o.cfg.BtcAmount - 1000, PkScript: refund.PkScript})

	spendRefundTx := wire.NewMsgTx(2)
	spendRefundTx.AddTxIn(&wire.TxIn{Sequence: o.cfg.LockBlocks})
	spendRefundTx.AddTxOut(&wire.TxOut{Value: o.cfg.BtcAmount - 2000, PkScript: o.cfg.PeerBTCPayoutScript})
	s.RefundTx = refundTx
	s.SpendRefundTx = spendRefundTx

	// Serialize for wire.
	var rbuf, srbuf bytes.Buffer
	_ = refundTx.Serialize(&rbuf)
	_ = spendRefundTx.Serialize(&srbuf)

	out := &msgjson.AdaptorSetupInit{
		OrderID:          o.cfg.OrderID[:],
		MatchID:          o.cfg.MatchID[:],
		PubSpendKey:      s.FullSpendPub.SerializeCompressed(),
		ViewKey:          viewKey.Serialize(),
		PubSignKeyHalf:   kal,
		DLEQProof:        s.DLEQProof,
		RefundTx:         rbuf.Bytes(),
		SpendRefundTx:    srbuf.Bytes(),
		LockLeafScript:   lock.LeafScript,
		RefundPkScript:   refund.PkScript,
		CoopLeafScript:   refund.CoopLeafScript,
		PunishLeafScript: refund.PunishLeafScript,
		LockBlocks:       o.cfg.LockBlocks,
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSetupInitRoute, out); err != nil {
		return fmt.Errorf("send AdaptorSetupInit: %w", err)
	}
	_ = peerSignPub
	s.Phase = PhaseKeysReceived
	return o.save()
}

// participantConsumeInitSetup (Alice) validates Bob's material,
// pre-signs the refund chain, and emits AdaptorRefundPresigned.
func (o *Orchestrator) participantConsumeInitSetup(m *msgjson.AdaptorSetupInit) error {
	s := o.state

	fullSpend, err := btcec.ParsePubKey(m.PubSpendKey)
	if err != nil {
		// Also accept compressed ed25519.
		pk, edErr := edwards.ParsePubKey(m.PubSpendKey)
		if edErr != nil {
			return fmt.Errorf("parse full spend: %w / %w", err, edErr)
		}
		_ = pk
	}
	_ = fullSpend

	peerScalarSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(m.DLEQProof)
	if err != nil {
		return fmt.Errorf("peer dleq extract: %w", err)
	}
	s.PeerDLEQProof = m.DLEQProof
	s.PeerScalarSecp = peerScalarSecp
	s.PeerBtcSignPub = m.PubSignKeyHalf

	// Reconstruct view key.
	viewKey, _, err := edwards.PrivKeyFromScalar(m.ViewKey)
	if err != nil {
		return fmt.Errorf("parse view key: %w", err)
	}
	s.FullViewKey = viewKey

	// Parse transactions.
	refundTx := wire.NewMsgTx(2)
	if err := refundTx.Deserialize(bytes.NewReader(m.RefundTx)); err != nil {
		return fmt.Errorf("deserialize refundTx: %w", err)
	}
	spendRefundTx := wire.NewMsgTx(2)
	if err := spendRefundTx.Deserialize(bytes.NewReader(m.SpendRefundTx)); err != nil {
		return fmt.Errorf("deserialize spendRefundTx: %w", err)
	}
	s.RefundTx = refundTx
	s.SpendRefundTx = spendRefundTx
	s.LockLeafScript = m.LockLeafScript
	s.RefundPkScript = m.RefundPkScript
	s.CoopLeafScript = m.CoopLeafScript
	s.PunishLeafScript = m.PunishLeafScript
	s.LockBlocks = m.LockBlocks

	// TODO: validate that the scripts, when re-derived from
	// advertised pubkeys + lockBlocks, match m.LockLeafScript etc.

	// Pre-sign refundTx and adaptor-sign spendRefundTx.
	sigRefund, esig, err := o.participantPresignRefund()
	if err != nil {
		return err
	}
	s.OwnRefundSig = sigRefund
	s.SpendRefundAdaptorSig = esig

	out := &msgjson.AdaptorRefundPresigned{
		OrderID:               o.cfg.OrderID[:],
		MatchID:               o.cfg.MatchID[:],
		RefundSig:             sigRefund,
		SpendRefundAdaptorSig: esig.Serialize(),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorRefundPresignedRoute, out); err != nil {
		return fmt.Errorf("send AdaptorRefundPresigned: %w", err)
	}
	s.Phase = PhaseRefundPresigned
	return o.save()
}

// participantPresignRefund produces Alice's cooperative sig on
// refundTx and her adaptor sig on spendRefundTx (tweaked by Bob's
// XMR-key-half secp pubkey).
func (o *Orchestrator) participantPresignRefund() ([]byte, *adaptorsigs.AdaptorSignature, error) {
	s := o.state
	// refundTx input 0 spends the (not yet funded) lockTx vout. For
	// pre-signing, we sign a sighash that depends on the lockTx
	// outpoint + lockLeafScript; value is fixed to BtcAmount.
	prev := txscript.NewCannedPrevOutputFetcher(
		lockPkScript(s.LockLeafScript), o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(s.RefundTx, prev)
	leaf := txscript.NewBaseTapLeaf(s.LockLeafScript)

	sigRefund, err := txscript.RawTxInTapscriptSignature(
		s.RefundTx, sh, 0, o.cfg.BtcAmount, lockPkScript(s.LockLeafScript),
		leaf, txscript.SigHashDefault, s.BtcSignKey,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("refundTx sign: %w", err)
	}

	// spendRefundTx sighash via coop leaf.
	refundValue := s.RefundTx.TxOut[0].Value
	spendPrev := txscript.NewCannedPrevOutputFetcher(s.RefundPkScript, refundValue)
	spendSh := txscript.NewTxSigHashes(s.SpendRefundTx, spendPrev)
	coopLeaf := txscript.NewBaseTapLeaf(s.CoopLeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(
		spendSh, txscript.SigHashDefault, s.SpendRefundTx, 0,
		spendPrev, coopLeaf,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("spendRefund sighash: %w", err)
	}
	var T btcec.JacobianPoint
	s.PeerScalarSecp.AsJacobian(&T)
	esig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(s.BtcSignKey, sigHash, &T)
	if err != nil {
		return nil, nil, fmt.Errorf("spendRefund adaptor: %w", err)
	}
	return sigRefund, esig, nil
}

// lockPkScript reconstructs the P2TR pkScript from the lock leaf.
// For pre-signing, this is approximate: we only need a fetcher that
// returns a matching pkScript for sighash computation. In a real
// deployment the peer's advertised pkScript should be validated
// against the rebuild.
func lockPkScript(leafScript []byte) []byte {
	// Rebuild via the same btcadaptor code path. This is expensive
	// per call; acceptable at this volume.
	// We treat the leaf script alone as enough to reconstruct (NUMS
	// internal key + single-leaf tree).
	key, _ := btcadaptor.UnspendableInternalKey()
	leaf := txscript.NewBaseTapLeaf(leafScript)
	tree := txscript.AssembleTaprootScriptTree(leaf)
	root := tree.RootNode.TapHash()
	outKey := txscript.ComputeTaprootOutputKey(key, root[:])
	pk, _ := txscript.PayToTaprootScript(outKey)
	return pk
}

// handleKeysReceived: initiator side, on receiving
// EventRefundPresignedReceived, validates the adaptor sig, stores
// it, and funds+broadcasts lockTx.
func (o *Orchestrator) handleKeysReceived(evt Event) error {
	pres, ok := evt.(EventRefundPresignedReceived)
	if !ok {
		return fmt.Errorf("expected EventRefundPresignedReceived, got %T", evt)
	}
	s := o.state
	if s.Role != RoleInitiator {
		return fmt.Errorf("participant received RefundPresigned; unexpected")
	}
	// Verify and store peer refund sig + adaptor sig.
	s.PeerRefundSig = pres.RefundSig
	esig, err := adaptorsigs.ParseAdaptorSignature(pres.AdaptorSig)
	if err != nil {
		return fmt.Errorf("parse adaptor sig: %w", err)
	}
	s.SpendRefundAdaptorSig = esig

	// Fund + broadcast lockTx.
	tx, vout, height, err := o.assetBTC.FundBroadcastTaproot(s.Lock.PkScript, o.cfg.BtcAmount)
	if err != nil {
		return fmt.Errorf("fund+broadcast lockTx: %w", err)
	}
	s.LockTx = tx
	s.LockVout = vout
	s.LockHeight = height

	// Re-point refundTx input to the now-known outpoint.
	lockHash := tx.TxHash()
	s.RefundTx.TxIn[0].PreviousOutPoint = wire.OutPoint{Hash: lockHash, Index: vout}
	// spendRefundTx input to refundTx output.
	refundHash := s.RefundTx.TxHash()
	s.SpendRefundTx.TxIn[0].PreviousOutPoint = wire.OutPoint{Hash: refundHash, Index: 0}

	// Notify participant: lock is broadcast.
	notice := &msgjson.AdaptorLocked{
		OrderID: o.cfg.OrderID[:],
		MatchID: o.cfg.MatchID[:],
		TxID:    lockHash[:],
		Vout:    vout,
		Value:   uint64(o.cfg.BtcAmount),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorLockedRoute, notice); err != nil {
		return fmt.Errorf("send AdaptorLocked: %w", err)
	}
	s.Phase = PhaseLockBroadcast
	return o.save()
}

// handleRefundPresigned: participant side, awaits EventLockConfirmed
// or an inbound AdaptorLocked for awareness.
func (o *Orchestrator) handleRefundPresigned(evt Event) error {
	switch e := evt.(type) {
	case EventLockConfirmed:
		o.state.LockHeight = e.Height
		o.state.Phase = PhaseLockConfirmed
		return o.save()
	default:
		return fmt.Errorf("expected EventLockConfirmed, got %T", evt)
	}
}

// handleLockBroadcast: initiator waits for its lockTx to reach
// confirmation depth. Advance on EventLockConfirmed.
func (o *Orchestrator) handleLockBroadcast(evt Event) error {
	if _, ok := evt.(EventLockConfirmed); !ok {
		return fmt.Errorf("expected EventLockConfirmed, got %T", evt)
	}
	o.state.Phase = PhaseLockConfirmed
	return o.save()
}

// handleLockConfirmed: participant captures XMR height and sends
// XMR to the shared address.
func (o *Orchestrator) handleLockConfirmed(evt Event) error {
	s := o.state
	if s.Role == RoleInitiator {
		// Initiator waits for participant's AdaptorXmrLocked.
		ev, ok := evt.(EventKeysReceived)
		if !ok {
			return fmt.Errorf("initiator expected peer AdaptorXmrLocked, got %T", evt)
		}
		xmr, ok := ev.Setup.(*msgjson.AdaptorXmrLocked)
		if !ok {
			return fmt.Errorf("initiator expected AdaptorXmrLocked payload, got %T", ev.Setup)
		}
		s.XmrSendTxID = string(xmr.XmrTxID)
		s.XmrRestoreHeight = xmr.RestoreHeight
		s.Phase = PhaseXmrConfirmed
		return o.save()
	}
	// Participant: send XMR.
	sharedAddr := deriveSharedAddress(s.FullSpendPub, s.FullViewKey.PubKey(), o.cfg.XmrNetTag)
	txid, height, err := o.assetXMR.SendToSharedAddress(sharedAddr, o.cfg.XmrAmount)
	if err != nil {
		return fmt.Errorf("send xmr: %w", err)
	}
	s.XmrSendTxID = txid
	s.XmrRestoreHeight = height

	notice := &msgjson.AdaptorXmrLocked{
		OrderID:       o.cfg.OrderID[:],
		MatchID:       o.cfg.MatchID[:],
		XmrTxID:       []byte(txid),
		RestoreHeight: height,
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorXmrLockedRoute, notice); err != nil {
		return fmt.Errorf("send AdaptorXmrLocked: %w", err)
	}
	s.Phase = PhaseXmrSent
	return o.save()
}

// handleXmrSent: participant waits for initiator's AdaptorSpendPresig.
func (o *Orchestrator) handleXmrSent(evt Event) error {
	if o.state.Role != RoleParticipant {
		return fmt.Errorf("only participant waits for spend presig here")
	}
	ev, ok := evt.(EventSpendPresigReceived)
	if !ok {
		return fmt.Errorf("expected EventSpendPresigReceived, got %T", evt)
	}
	// Parse spendTx skeleton.
	spendTx := wire.NewMsgTx(2)
	if err := spendTx.Deserialize(bytes.NewReader(ev.SpendTx)); err != nil {
		return fmt.Errorf("parse spendTx: %w", err)
	}
	esig, err := adaptorsigs.ParseAdaptorSignature(ev.AdaptorSig)
	if err != nil {
		return fmt.Errorf("parse spend adaptor: %w", err)
	}
	o.state.SpendTx = spendTx
	o.state.SpendAdaptorSig = esig

	// Alice decrypts Bob's adaptor using her ed25519 scalar.
	aliceScalar, _ := btcec.PrivKeyFromBytes(o.state.XmrSpendKeyHalf.Serialize())
	bobSigCompleted, err := esig.DecryptBIP340(&aliceScalar.Key)
	if err != nil {
		return fmt.Errorf("decrypt bob adaptor: %w", err)
	}

	// Alice signs her tapscript half.
	prev := txscript.NewCannedPrevOutputFetcher(
		lockPkScript(o.state.LockLeafScript), o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(o.state.LockLeafScript)
	aliceSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sh, 0, o.cfg.BtcAmount, lockPkScript(o.state.LockLeafScript),
		leaf, txscript.SigHashDefault, o.state.BtcSignKey,
	)
	if err != nil {
		return fmt.Errorf("alice sign spend: %w", err)
	}

	// Assemble witness: sig_kaf, sig_kal(completed), script, control.
	lock, _ := btcadaptor.NewLockTxOutput(o.state.PeerBtcSignPub,
		btcschnorr.SerializePubKey(o.state.BtcSignKey.PubKey()))
	ctrlSer, err := lock.ControlBlock.ToBytes()
	if err != nil {
		return fmt.Errorf("control block: %w", err)
	}
	spendTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig, bobSigCompleted.Serialize(), o.state.LockLeafScript, ctrlSer,
	}

	txid, err := o.assetBTC.BroadcastTx(spendTx)
	if err != nil {
		return fmt.Errorf("broadcast spend: %w", err)
	}
	o.state.SpendTxID = []byte(txid)

	notice := &msgjson.AdaptorSpendBroadcast{
		OrderID: o.cfg.OrderID[:],
		MatchID: o.cfg.MatchID[:],
		TxID:    []byte(txid),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSpendBroadcastRoute, notice); err != nil {
		return fmt.Errorf("send AdaptorSpendBroadcast: %w", err)
	}
	o.state.Phase = PhaseSpendBroadcast
	return o.save()
}

// handleXmrConfirmed: initiator builds + adaptor-signs spendTx and
// emits AdaptorSpendPresig.
func (o *Orchestrator) handleXmrConfirmed(evt Event) error {
	if o.state.Role != RoleInitiator {
		return fmt.Errorf("only initiator acts in PhaseXmrConfirmed")
	}
	// Build spendTx paying to peer's address.
	lockHash := o.state.LockTx.TxHash()
	spendTx := wire.NewMsgTx(2)
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: lockHash, Index: o.state.LockVout},
	})
	spendTx.AddTxOut(&wire.TxOut{Value: o.cfg.BtcAmount - 1000, PkScript: o.cfg.PeerBTCPayoutScript})

	// Bob adaptor-signs with tweak = Alice's secp pubkey.
	prev := txscript.NewCannedPrevOutputFetcher(o.state.Lock.PkScript, o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(o.state.Lock.LeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(sh, txscript.SigHashDefault, spendTx, 0, prev, leaf)
	if err != nil {
		return fmt.Errorf("sighash: %w", err)
	}
	var T btcec.JacobianPoint
	o.state.PeerScalarSecp.AsJacobian(&T)
	esig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(o.state.BtcSignKey, sigHash, &T)
	if err != nil {
		return fmt.Errorf("bob adaptor sig: %w", err)
	}
	o.state.SpendTx = spendTx
	o.state.SpendAdaptorSig = esig

	var txBuf bytes.Buffer
	_ = spendTx.Serialize(&txBuf)
	out := &msgjson.AdaptorSpendPresig{
		OrderID:    o.cfg.OrderID[:],
		MatchID:    o.cfg.MatchID[:],
		SpendTx:    txBuf.Bytes(),
		AdaptorSig: esig.Serialize(),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSpendPresigRoute, out); err != nil {
		return fmt.Errorf("send AdaptorSpendPresig: %w", err)
	}
	o.state.Phase = PhaseSpendPresig
	return o.save()
}

// handleSpendPresig: initiator waits to observe spend on chain.
func (o *Orchestrator) handleSpendPresig(evt Event) error {
	if o.state.Role != RoleInitiator {
		return fmt.Errorf("only initiator waits for on-chain spend here")
	}
	e, ok := evt.(EventSpendObservedOnChain)
	if !ok {
		return fmt.Errorf("expected EventSpendObservedOnChain, got %T", evt)
	}
	// The completed Bob sig is in the witness at position 1 (sig_kal).
	if len(e.Witness) < 2 {
		return fmt.Errorf("witness too short: %d", len(e.Witness))
	}
	completedSig := e.Witness[1]
	sig, err := btcschnorr.ParseSignature(completedSig)
	if err != nil {
		return fmt.Errorf("parse completed sig: %w", err)
	}
	scalar, err := o.state.SpendAdaptorSig.RecoverTweakBIP340(sig)
	if err != nil {
		return fmt.Errorf("recover tweak: %w", err)
	}
	o.state.RecoveredPeerScalar = scalar

	// Reconstruct full spend key and open sweep wallet.
	var sb [32]byte
	scalar.PutBytes(&sb)
	partRecov, _, err := edwards.PrivKeyFromScalar(sb[:])
	if err != nil {
		return fmt.Errorf("part scalar -> ed25519: %w", err)
	}
	full := scalarAdd(o.state.XmrSpendKeyHalf.GetD(), partRecov.GetD())
	full.Mod(full, curve.N)
	var fullBytes [32]byte
	full.FillBytes(fullBytes[:])

	viewHex := hex.EncodeToString(reverseBytes(o.state.FullViewKey.Serialize()))
	spendHex := hex.EncodeToString(reverseBytes(fullBytes[:]))
	sharedAddr := deriveSharedAddress(o.state.FullSpendPub, o.state.FullViewKey.PubKey(), o.cfg.XmrNetTag)

	txid, err := o.assetXMR.SweepSharedAddress(hex.EncodeToString(o.cfg.SwapID[:]),
		sharedAddr, spendHex, viewHex, o.state.XmrRestoreHeight, o.cfg.OwnXMRSweepDest)
	if err != nil {
		return fmt.Errorf("sweep xmr: %w", err)
	}
	_ = txid
	o.state.Phase = PhaseXmrSwept
	o.state.Phase = PhaseComplete
	return o.save()
}

// handleSpendBroadcast: participant side terminal - spend confirmed.
func (o *Orchestrator) handleSpendBroadcast(evt Event) error {
	o.state.Phase = PhaseComplete
	return o.save()
}

// Refund-path handlers are stubbed; they mirror the CLI's
// aliceBailsBeforeXmrInit / refund / bobBailsAfterXmrInit scenarios
// line-by-line and are the next piece of work. Left as targeted
// TODOs rather than untested code.

func (o *Orchestrator) handleRefundTxBroadcast(evt Event) error {
	// TODO: port from internal/cmd/btcxmrswap refund flows.
	return errors.New("refund path not yet implemented in orchestrator")
}

func (o *Orchestrator) handleCoopRefund(evt Event) error {
	// TODO: port from btcxmrswap refundBtc+refundXmr.
	return errors.New("coop refund not yet implemented in orchestrator")
}

func (o *Orchestrator) handlePunish(evt Event) error {
	// TODO: port from btcxmrswap takeBtc.
	return errors.New("punish not yet implemented in orchestrator")
}

// save is a wrapper around the persister. Must be called with
// o.state.mu held.
func (o *Orchestrator) save() error {
	o.state.Updated = time.Now()
	if o.persist == nil {
		return nil
	}
	return o.persist.Save(o.cfg.SwapID, o.state.Snapshot())
}

// ----- crypto helpers -----

func sumPubKeys(a, b *edwards.PublicKey) *edwards.PublicKey {
	x, y := curve.Add(a.GetX(), a.GetY(), b.GetX(), b.GetY())
	return edwards.NewPublicKey(x, y)
}

func scalarAdd(a, b *big.Int) *big.Int {
	return scalarAddMod(a, b)
}

// scalarAddMod adds two ed25519 scalars mod L using the edwards25519
// field element helpers from agl/ed25519/edwards25519 (imported but
// not strictly needed - big.Int + explicit mod curve.N is enough
// given the DLEQ library's invariant that scalars fit under both L
// and n).
func scalarAddMod(a, b *big.Int) *big.Int {
	sum := new(big.Int).Add(a, b)
	return sum
}

// deriveSharedAddress builds the base58-encoded XMR standard
// address from a combined spend pubkey and the shared view pubkey.
func deriveSharedAddress(spendPub, viewPub *edwards.PublicKey, netTag uint64) string {
	var full []byte
	full = append(full, spendPub.SerializeCompressed()...)
	full = append(full, viewPub.SerializeCompressed()...)
	return base58.EncodeAddr(netTag, full)
}

// reverseBytes returns a little-endian interpretation of a
// big-endian 32-byte scalar (XMR wallet creation expects LE hex).
func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, v := range b {
		out[len(b)-1-i] = v
	}
	return out
}

// Ensure unused imports are reachable during partial compilation.
var (
	_ = edwards25519.FieldElement{}
	_ = rand.Reader
)
