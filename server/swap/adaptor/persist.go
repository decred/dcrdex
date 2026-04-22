package adaptor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"decred.org/dcrdex/dex/order"
)

// serverSnapshot is the on-disk representation of a server-side
// State. The server holds only public material (pubkeys, txids,
// scripts) so encoding is straightforward JSON. No private keys
// or adaptor secrets are persisted; even an attacker with disk
// access cannot recover swap funds.
type serverSnapshot struct {
	MatchID         [32]byte `json:"match_id"`
	OrderID         [32]byte `json:"order_id"`
	ScriptableAsset uint32   `json:"scriptable_asset"`
	NonScriptAsset  uint32   `json:"non_script_asset"`
	LockBlocks      uint32   `json:"lock_blocks"`

	ParticipantPubSpendKeyHalf []byte `json:"part_pub_spend_key_half,omitempty"`
	ParticipantPubSignKeyHalf  []byte `json:"part_pub_sign_key_half,omitempty"`
	ParticipantDLEQProof       []byte `json:"part_dleq_proof,omitempty"`
	InitiatorPubSpendKeyHalf   []byte `json:"init_pub_spend_key_half,omitempty"`
	InitiatorPubSignKeyHalf    []byte `json:"init_pub_sign_key_half,omitempty"`
	InitiatorDLEQProof         []byte `json:"init_dleq_proof,omitempty"`

	FullSpendPub []byte `json:"full_spend_pub,omitempty"`
	FullViewKey  []byte `json:"full_view_key,omitempty"`

	LockTxID   []byte `json:"lock_tx_id,omitempty"`
	LockVout   uint32 `json:"lock_vout"`
	LockValue  uint64 `json:"lock_value"`
	LockHeight int64  `json:"lock_height"`

	XmrTxID       []byte `json:"xmr_tx_id,omitempty"`
	XmrSentHeight uint64 `json:"xmr_sent_height"`

	SpendTxID       []byte `json:"spend_tx_id,omitempty"`
	RefundTxID      []byte `json:"refund_tx_id,omitempty"`
	SpendRefundTxID []byte `json:"spend_refund_tx_id,omitempty"`

	Phase         Phase     `json:"phase"`
	Updated       time.Time `json:"updated"`
	LastError     string    `json:"last_error,omitempty"`
	FailOutcome   Outcome   `json:"fail_outcome"`
	PhaseDeadline time.Time `json:"phase_deadline"`
}

// Marshal serializes the State for persistence. Caller must hold
// s.mu (matches the orchestrator's save-from-handler convention).
func (s *State) Marshal() ([]byte, error) {
	ss := &serverSnapshot{
		MatchID:                    s.MatchID,
		OrderID:                    s.OrderID,
		ScriptableAsset:            s.ScriptableAsset,
		NonScriptAsset:             s.NonScriptAsset,
		LockBlocks:                 s.LockBlocks,
		ParticipantPubSpendKeyHalf: s.ParticipantPubSpendKeyHalf,
		ParticipantPubSignKeyHalf:  s.ParticipantPubSignKeyHalf,
		ParticipantDLEQProof:       s.ParticipantDLEQProof,
		InitiatorPubSpendKeyHalf:   s.InitiatorPubSpendKeyHalf,
		InitiatorPubSignKeyHalf:    s.InitiatorPubSignKeyHalf,
		InitiatorDLEQProof:         s.InitiatorDLEQProof,
		FullSpendPub:               s.FullSpendPub,
		FullViewKey:                s.FullViewKey,
		LockTxID:                   s.LockTxID,
		LockVout:                   s.LockVout,
		LockValue:                  s.LockValue,
		LockHeight:                 s.LockHeight,
		XmrTxID:                    s.XmrTxID,
		XmrSentHeight:              s.XmrSentHeight,
		SpendTxID:                  s.SpendTxID,
		RefundTxID:                 s.RefundTxID,
		SpendRefundTxID:            s.SpendRefundTxID,
		Phase:                      s.Phase,
		Updated:                    s.Updated,
		LastError:                  s.LastError,
		FailOutcome:                s.FailOutcome,
		PhaseDeadline:              s.PhaseDeadline,
	}
	return json.Marshal(ss)
}

// Unmarshal reconstructs a State from a previously-marshalled byte
// slice.
func Unmarshal(data []byte) (*State, error) {
	var ss serverSnapshot
	if err := json.Unmarshal(data, &ss); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &State{
		MatchID:                    ss.MatchID,
		OrderID:                    ss.OrderID,
		ScriptableAsset:            ss.ScriptableAsset,
		NonScriptAsset:             ss.NonScriptAsset,
		LockBlocks:                 ss.LockBlocks,
		ParticipantPubSpendKeyHalf: ss.ParticipantPubSpendKeyHalf,
		ParticipantPubSignKeyHalf:  ss.ParticipantPubSignKeyHalf,
		ParticipantDLEQProof:       ss.ParticipantDLEQProof,
		InitiatorPubSpendKeyHalf:   ss.InitiatorPubSpendKeyHalf,
		InitiatorPubSignKeyHalf:    ss.InitiatorPubSignKeyHalf,
		InitiatorDLEQProof:         ss.InitiatorDLEQProof,
		FullSpendPub:               ss.FullSpendPub,
		FullViewKey:                ss.FullViewKey,
		LockTxID:                   ss.LockTxID,
		LockVout:                   ss.LockVout,
		LockValue:                  ss.LockValue,
		LockHeight:                 ss.LockHeight,
		XmrTxID:                    ss.XmrTxID,
		XmrSentHeight:              ss.XmrSentHeight,
		SpendTxID:                  ss.SpendTxID,
		RefundTxID:                 ss.RefundTxID,
		SpendRefundTxID:            ss.SpendRefundTxID,
		Phase:                      ss.Phase,
		Updated:                    ss.Updated,
		LastError:                  ss.LastError,
		FailOutcome:                ss.FailOutcome,
		PhaseDeadline:              ss.PhaseDeadline,
	}, nil
}

// FilePersister stores per-match snapshots as JSON files in dir.
// Suitable for low-volume single-process deployments. Production
// dcrdex servers should use a database-backed implementation that
// hooks into server/db; this file-based one is the minimal viable
// option.
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

// Save writes the state to disk, overwriting any existing snapshot
// for the match.
func (p *FilePersister) Save(matchID order.MatchID, s *State) error {
	if s == nil {
		return errors.New("nil state")
	}
	data, err := s.Marshal()
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(p.path(matchID), data, 0o600)
}

// Load returns the saved state for the match, or (nil, nil) if
// none exists.
func (p *FilePersister) Load(matchID order.MatchID) (*State, error) {
	data, err := os.ReadFile(p.path(matchID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read: %w", err)
	}
	return Unmarshal(data)
}

func (p *FilePersister) path(matchID order.MatchID) string {
	return filepath.Join(p.dir, fmt.Sprintf("%x.snap", matchID[:]))
}
