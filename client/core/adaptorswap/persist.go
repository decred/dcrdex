package adaptorswap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
)

// fullSnapshot is the on-disk format. Each field is either a fixed
// scalar size or a length-prefixed byte slice; complex types are
// encoded via their existing Serialize methods. JSON encoding gives
// human-inspectability for debugging at modest size cost; a future
// migration to a compact binary format is straightforward without
// changing the State surface.
type fullSnapshot struct {
	// Identity.
	SwapID  [32]byte `json:"swap_id"`
	OrderID [32]byte `json:"order_id"`
	MatchID [32]byte `json:"match_id"`
	Role    Role     `json:"role"`
	PairBTC uint32   `json:"pair_btc"`
	PairXMR uint32   `json:"pair_xmr"`

	// Per-swap fresh keys.
	BtcSignKey      []byte `json:"btc_sign_key"`       // raw 32-byte secp scalar
	XmrSpendKeyHalf []byte `json:"xmr_spend_key_half"` // raw 32-byte ed25519 scalar
	DLEQProof       []byte `json:"dleq_proof"`

	// Counterparty material.
	PeerBtcSignPub  []byte `json:"peer_btc_sign_pub"`
	PeerXmrSpendPub []byte `json:"peer_xmr_spend_pub"` // serialized ed25519 pubkey
	PeerDLEQProof   []byte `json:"peer_dleq_proof"`
	PeerScalarSecp  []byte `json:"peer_scalar_secp"` // serialized compressed secp pubkey

	// XMR full key material.
	FullSpendPub []byte `json:"full_spend_pub"` // ed25519 pubkey
	FullViewKey  []byte `json:"full_view_key"`  // ed25519 scalar (the private view key)

	// BTC scripts + tx artifacts. Lock and Refund are reconstructed
	// from leaf scripts via btcadaptor on Restore.
	LockTx           []byte `json:"lock_tx"`
	LockVout         uint32 `json:"lock_vout"`
	LockHeight       int64  `json:"lock_height"`
	RefundTx         []byte `json:"refund_tx"`
	SpendRefundTx    []byte `json:"spend_refund_tx"`
	SpendTx          []byte `json:"spend_tx,omitempty"`
	LockLeafScript   []byte `json:"lock_leaf_script"`
	RefundPkScript   []byte `json:"refund_pk_script"`
	CoopLeafScript   []byte `json:"coop_leaf_script"`
	PunishLeafScript []byte `json:"punish_leaf_script"`
	LockBlocks       uint32 `json:"lock_blocks"`

	// Sigs.
	OwnRefundSig          []byte `json:"own_refund_sig,omitempty"`
	PeerRefundSig         []byte `json:"peer_refund_sig,omitempty"`
	SpendRefundAdaptorSig []byte `json:"spend_refund_adaptor_sig,omitempty"`
	SpendAdaptorSig       []byte `json:"spend_adaptor_sig,omitempty"`

	// XMR-side bookkeeping.
	XmrSendTxID         string `json:"xmr_send_tx_id"`
	XmrRestoreHeight    uint64 `json:"xmr_restore_height"`
	SpendTxID           []byte `json:"spend_tx_id,omitempty"`
	RecoveredPeerScalar []byte `json:"recovered_peer_scalar,omitempty"`

	// State machine.
	Phase     Phase     `json:"phase"`
	Updated   time.Time `json:"updated"`
	LastError string    `json:"last_error,omitempty"`
}

// FullSnapshot serializes the State to a transportable byte slice.
// Caller must hold s.mu (matches Snapshot semantics).
func (s *State) FullSnapshot() ([]byte, error) {
	fs := &fullSnapshot{
		SwapID:           s.SwapID,
		OrderID:          s.OrderID,
		MatchID:          s.MatchID,
		Role:             s.Role,
		PairBTC:          s.PairBTC,
		PairXMR:          s.PairXMR,
		DLEQProof:        s.DLEQProof,
		PeerBtcSignPub:   s.PeerBtcSignPub,
		PeerDLEQProof:    s.PeerDLEQProof,
		LockVout:         s.LockVout,
		LockHeight:       s.LockHeight,
		LockLeafScript:   s.LockLeafScript,
		RefundPkScript:   s.RefundPkScript,
		CoopLeafScript:   s.CoopLeafScript,
		PunishLeafScript: s.PunishLeafScript,
		LockBlocks:       s.LockBlocks,
		OwnRefundSig:     s.OwnRefundSig,
		PeerRefundSig:    s.PeerRefundSig,
		XmrSendTxID:      s.XmrSendTxID,
		XmrRestoreHeight: s.XmrRestoreHeight,
		SpendTxID:        s.SpendTxID,
		Phase:            s.Phase,
		Updated:          s.Updated,
		LastError:        s.LastError,
	}
	if s.BtcSignKey != nil {
		var b [32]byte
		s.BtcSignKey.Key.PutBytes(&b)
		fs.BtcSignKey = b[:]
	}
	if s.XmrSpendKeyHalf != nil {
		fs.XmrSpendKeyHalf = s.XmrSpendKeyHalf.Serialize()
	}
	if s.PeerXmrSpendPub != nil {
		fs.PeerXmrSpendPub = s.PeerXmrSpendPub.Serialize()
	}
	if s.PeerScalarSecp != nil {
		fs.PeerScalarSecp = s.PeerScalarSecp.SerializeCompressed()
	}
	if s.FullSpendPub != nil {
		fs.FullSpendPub = s.FullSpendPub.Serialize()
	}
	if s.FullViewKey != nil {
		fs.FullViewKey = s.FullViewKey.Serialize()
	}
	if s.LockTx != nil {
		var buf bytes.Buffer
		if err := s.LockTx.Serialize(&buf); err != nil {
			return nil, fmt.Errorf("serialize lockTx: %w", err)
		}
		fs.LockTx = buf.Bytes()
	}
	if s.RefundTx != nil {
		var buf bytes.Buffer
		if err := s.RefundTx.Serialize(&buf); err != nil {
			return nil, fmt.Errorf("serialize refundTx: %w", err)
		}
		fs.RefundTx = buf.Bytes()
	}
	if s.SpendRefundTx != nil {
		var buf bytes.Buffer
		if err := s.SpendRefundTx.Serialize(&buf); err != nil {
			return nil, fmt.Errorf("serialize spendRefundTx: %w", err)
		}
		fs.SpendRefundTx = buf.Bytes()
	}
	if s.SpendTx != nil {
		var buf bytes.Buffer
		if err := s.SpendTx.Serialize(&buf); err != nil {
			return nil, fmt.Errorf("serialize spendTx: %w", err)
		}
		fs.SpendTx = buf.Bytes()
	}
	if s.SpendRefundAdaptorSig != nil {
		fs.SpendRefundAdaptorSig = s.SpendRefundAdaptorSig.Serialize()
	}
	if s.SpendAdaptorSig != nil {
		fs.SpendAdaptorSig = s.SpendAdaptorSig.Serialize()
	}
	if s.RecoveredPeerScalar != nil {
		var b [32]byte
		s.RecoveredPeerScalar.PutBytes(&b)
		fs.RecoveredPeerScalar = b[:]
	}
	return json.Marshal(fs)
}

// RestoreState reconstructs a State from a serialized snapshot.
// The returned State has its mutex unlocked and is ready for use
// by an Orchestrator (after the orchestrator's runtime Config is
// re-supplied separately).
func RestoreState(data []byte) (*State, error) {
	var fs fullSnapshot
	if err := json.Unmarshal(data, &fs); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	s := &State{
		SwapID:           fs.SwapID,
		OrderID:          fs.OrderID,
		MatchID:          fs.MatchID,
		Role:             fs.Role,
		PairBTC:          fs.PairBTC,
		PairXMR:          fs.PairXMR,
		DLEQProof:        fs.DLEQProof,
		PeerBtcSignPub:   fs.PeerBtcSignPub,
		PeerDLEQProof:    fs.PeerDLEQProof,
		LockVout:         fs.LockVout,
		LockHeight:       fs.LockHeight,
		LockLeafScript:   fs.LockLeafScript,
		RefundPkScript:   fs.RefundPkScript,
		CoopLeafScript:   fs.CoopLeafScript,
		PunishLeafScript: fs.PunishLeafScript,
		LockBlocks:       fs.LockBlocks,
		OwnRefundSig:     fs.OwnRefundSig,
		PeerRefundSig:    fs.PeerRefundSig,
		XmrSendTxID:      fs.XmrSendTxID,
		XmrRestoreHeight: fs.XmrRestoreHeight,
		SpendTxID:        fs.SpendTxID,
		Phase:            fs.Phase,
		Updated:          fs.Updated,
		LastError:        fs.LastError,
	}
	if len(fs.BtcSignKey) == 32 {
		s.BtcSignKey, _ = btcec.PrivKeyFromBytes(fs.BtcSignKey)
	}
	if len(fs.XmrSpendKeyHalf) == 32 {
		k, _, err := edwards.PrivKeyFromScalar(fs.XmrSpendKeyHalf)
		if err != nil {
			return nil, fmt.Errorf("xmr spend key: %w", err)
		}
		s.XmrSpendKeyHalf = k
	}
	if len(fs.PeerXmrSpendPub) > 0 {
		pk, err := edwards.ParsePubKey(fs.PeerXmrSpendPub)
		if err != nil {
			return nil, fmt.Errorf("peer xmr spend pub: %w", err)
		}
		s.PeerXmrSpendPub = pk
	}
	if len(fs.PeerScalarSecp) > 0 {
		pk, err := btcec.ParsePubKey(fs.PeerScalarSecp)
		if err != nil {
			return nil, fmt.Errorf("peer scalar secp: %w", err)
		}
		s.PeerScalarSecp = pk
	}
	if len(fs.FullSpendPub) > 0 {
		pk, err := edwards.ParsePubKey(fs.FullSpendPub)
		if err != nil {
			return nil, fmt.Errorf("full spend pub: %w", err)
		}
		s.FullSpendPub = pk
	}
	if len(fs.FullViewKey) == 32 {
		k, _, err := edwards.PrivKeyFromScalar(fs.FullViewKey)
		if err != nil {
			return nil, fmt.Errorf("full view key: %w", err)
		}
		s.FullViewKey = k
	}
	if len(fs.LockTx) > 0 {
		s.LockTx = wire.NewMsgTx(2)
		if err := s.LockTx.Deserialize(bytes.NewReader(fs.LockTx)); err != nil {
			return nil, fmt.Errorf("lockTx: %w", err)
		}
	}
	if len(fs.RefundTx) > 0 {
		s.RefundTx = wire.NewMsgTx(2)
		if err := s.RefundTx.Deserialize(bytes.NewReader(fs.RefundTx)); err != nil {
			return nil, fmt.Errorf("refundTx: %w", err)
		}
	}
	if len(fs.SpendRefundTx) > 0 {
		s.SpendRefundTx = wire.NewMsgTx(2)
		if err := s.SpendRefundTx.Deserialize(bytes.NewReader(fs.SpendRefundTx)); err != nil {
			return nil, fmt.Errorf("spendRefundTx: %w", err)
		}
	}
	if len(fs.SpendTx) > 0 {
		s.SpendTx = wire.NewMsgTx(2)
		if err := s.SpendTx.Deserialize(bytes.NewReader(fs.SpendTx)); err != nil {
			return nil, fmt.Errorf("spendTx: %w", err)
		}
	}
	if len(fs.SpendRefundAdaptorSig) > 0 {
		sig, err := adaptorsigs.ParseAdaptorSignature(fs.SpendRefundAdaptorSig)
		if err != nil {
			return nil, fmt.Errorf("spendRefund adaptor: %w", err)
		}
		s.SpendRefundAdaptorSig = sig
	}
	if len(fs.SpendAdaptorSig) > 0 {
		sig, err := adaptorsigs.ParseAdaptorSignature(fs.SpendAdaptorSig)
		if err != nil {
			return nil, fmt.Errorf("spend adaptor: %w", err)
		}
		s.SpendAdaptorSig = sig
	}
	if len(fs.RecoveredPeerScalar) == 32 {
		var sc btcec.ModNScalar
		sc.SetBytes((*[32]byte)(fs.RecoveredPeerScalar))
		s.RecoveredPeerScalar = &sc
	}
	// Reconstruct Lock and Refund taproot output materials from the
	// leaf scripts. Both sides have these in their state regardless
	// of role, since the participant rebuilds them locally from the
	// initiator's setup message.
	if len(s.LockLeafScript) > 0 && len(s.PeerBtcSignPub) > 0 && s.BtcSignKey != nil {
		// kal/kaf assignment depends on role.
		ownPub := xOnlyPub(s.BtcSignKey)
		var kal, kaf []byte
		if s.Role == RoleInitiator {
			kal, kaf = ownPub, s.PeerBtcSignPub
		} else {
			kal, kaf = s.PeerBtcSignPub, ownPub
		}
		if lock, err := btcadaptor.NewLockTxOutput(kal, kaf); err == nil {
			s.Lock = lock
		}
		if refund, err := btcadaptor.NewRefundTxOutput(kal, kaf, int64(s.LockBlocks)); err == nil {
			s.Refund = refund
		}
	}
	return s, nil
}

// xOnlyPub serializes a private key's pubkey in BIP-340 x-only form.
func xOnlyPub(k *btcec.PrivateKey) []byte {
	pub := k.PubKey().SerializeCompressed()
	return pub[1:] // drop the parity byte
}

// FilePersister is a StatePersister that writes each swap's state
// to a separate file in dir, named by hex(swapID).snap. Suitable
// for low-volume single-process deployments. Production
// installations should use a dcrdex-DB-backed implementation.
type FilePersister struct {
	dir string
}

// NewFilePersister ensures dir exists and returns a persister.
func NewFilePersister(dir string) (*FilePersister, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return &FilePersister{dir: dir}, nil
}

func (p *FilePersister) Save(swapID [32]byte, snap *Snapshot) error {
	// snap is the lightweight Snapshot type the orchestrator emits
	// before each transition; FullSnapshot is what we need for
	// restart resilience. Until callers feed us full state we
	// persist only the phase + updated timestamp.
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return os.WriteFile(p.path(swapID), data, 0o600)
}

func (p *FilePersister) Load(swapID [32]byte) (*Snapshot, error) {
	data, err := os.ReadFile(p.path(swapID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveFull persists a complete State (preferred over Save for
// restart resilience).
func (p *FilePersister) SaveFull(swapID [32]byte, s *State) error {
	data, err := s.FullSnapshot()
	if err != nil {
		return err
	}
	return os.WriteFile(p.fullPath(swapID), data, 0o600)
}

// LoadFull reconstructs a State previously written via SaveFull.
func (p *FilePersister) LoadFull(swapID [32]byte) (*State, error) {
	data, err := os.ReadFile(p.fullPath(swapID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return RestoreState(data)
}

func (p *FilePersister) path(swapID [32]byte) string {
	return filepath.Join(p.dir, fmt.Sprintf("%x.snap", swapID))
}

func (p *FilePersister) fullPath(swapID [32]byte) string {
	return filepath.Join(p.dir, fmt.Sprintf("%x.full", swapID))
}
