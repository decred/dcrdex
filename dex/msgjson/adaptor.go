// Adaptor-swap message types.
//
// These messages extend the dcrdex protocol with the wire format for
// BIP-340 adaptor-signature atomic swaps (BTC/XMR and, in principle,
// any scriptable-chain + non-scriptable-chain pair).
//
// The server does not participate in the swap cryptography - it
// routes messages between the two clients, audits the on-chain
// events, and enforces the protocol state machine. This file only
// defines the wire types; dispatch wiring lives in server/comms and
// client/core.
//
// Protocol overview (all BTC-holder-is-maker; Option 1):
//
//   1. Setup (off-chain): participant (XMR holder) sends
//      AdaptorSetupPart with DLEQ proof and pubkeys. Initiator
//      (BTC holder) responds with AdaptorSetupInit, which includes
//      the unsigned refund-tx chain templates.
//
//   2. Refund pre-signing: participant sends AdaptorRefundPresigned
//      with their cooperative refund sig and an adaptor sig on
//      spendRefundTx.
//
//   3. Lock: initiator broadcasts lockTx and sends AdaptorLocked.
//      Server confirms via on-chain observation. Participant sends
//      XMR and emits AdaptorXmrLocked.
//
//   4. Redeem: initiator sends AdaptorSpendPresig (adaptor sig on
//      spendTx). Participant decrypts, broadcasts spendTx, emits
//      AdaptorSpendBroadcast. Initiator observes the completed sig
//      on-chain and recovers the XMR scalar.
//
//   5. Refund/punish: either party broadcasts refundTx and sends
//      AdaptorRefundBroadcast. From there, cooperative refund
//      (AdaptorCoopRefund) or punish (AdaptorPunish) closes the
//      swap.

package msgjson

// Adaptor-swap message route constants.
const (
	// Setup phase. Bi-directional; the server routes these between
	// the two matched clients.
	AdaptorSetupPartRoute       = "adaptor_setup_part"
	AdaptorSetupInitRoute       = "adaptor_setup_init"
	AdaptorRefundPresignedRoute = "adaptor_refund_presigned"

	// Lock phase.
	AdaptorLockedRoute    = "adaptor_locked"
	AdaptorXmrLockedRoute = "adaptor_xmr_locked"

	// Redeem phase.
	AdaptorSpendPresigRoute    = "adaptor_spend_presig"
	AdaptorSpendBroadcastRoute = "adaptor_spend_broadcast"

	// Refund/punish.
	AdaptorRefundBroadcastRoute = "adaptor_refund_broadcast"
	AdaptorCoopRefundRoute      = "adaptor_coop_refund"
	AdaptorPunishRoute          = "adaptor_punish"
)

// AdaptorSetupPart is sent by the participant (XMR holder) to start
// the key exchange. Carries the participant's ed25519 spend-key half
// pubkey, their shared-view-key half private key, their secp256k1
// signing key pubkey for the BTC tapscript, and the DLEQ proof
// linking their ed25519 spend-key scalar to a secp256k1 scalar.
type AdaptorSetupPart struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	// PubSpendKeyHalf is the participant's ed25519 spend-key half.
	PubSpendKeyHalf Bytes `json:"pubspendkeyhalf"`
	// ViewKeyHalf is the participant's ed25519 view-key half, sent
	// as a private scalar so both sides can derive the shared view
	// key.
	ViewKeyHalf Bytes `json:"viewkeyhalf"`
	// PubSignKeyHalf is the participant's secp256k1 x-only pubkey
	// for the BTC 2-of-2 tapscript.
	PubSignKeyHalf Bytes `json:"pubsignkeyhalf"`
	// DLEQProof binds PubSpendKeyHalf (ed25519) to the secp256k1
	// point that the initiator will use as the adaptor tweak.
	DLEQProof Bytes `json:"dleqproof"`
}

var _ Signable = (*AdaptorSetupPart)(nil)

func (m *AdaptorSetupPart) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+
		len(m.PubSpendKeyHalf)+len(m.ViewKeyHalf)+
		len(m.PubSignKeyHalf)+len(m.DLEQProof))
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, m.PubSpendKeyHalf...)
	s = append(s, m.ViewKeyHalf...)
	s = append(s, m.PubSignKeyHalf...)
	return append(s, m.DLEQProof...)
}

// AdaptorSetupInit is the initiator's (BTC holder) response to
// AdaptorSetupPart. Carries the initiator's ed25519 spend-key half,
// the full combined XMR spend pubkey and view key, the initiator's
// secp256k1 signing pubkey, the initiator's DLEQ proof, and the
// unsigned refund transaction chain (refundTx + spendRefundTx) that
// the participant will pre-sign.
type AdaptorSetupInit struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	// PubSpendKey is the full XMR spend pubkey (participant + initiator halves).
	PubSpendKey Bytes `json:"pubspendkey"`
	// ViewKey is the full XMR view key (as private scalar) so the
	// participant can derive the same shared view.
	ViewKey Bytes `json:"viewkey"`
	// PubSignKeyHalf is the initiator's secp256k1 x-only pubkey.
	PubSignKeyHalf Bytes `json:"pubsignkeyhalf"`
	// DLEQProof binds the initiator's ed25519 spend-key half to
	// its secp256k1 pubkey, which the participant will use as the
	// adaptor tweak on spendRefundTx.
	DLEQProof Bytes `json:"dleqproof"`
	// RefundTx is the unsigned refundTx that spends lockTx via the
	// 2-of-2 tapscript into the two-leaf refund output. Both
	// parties pre-sign this; witness assembly happens at broadcast
	// time.
	RefundTx Bytes `json:"refundtx"`
	// SpendRefundTx is the unsigned spendRefundTx. The participant
	// will adaptor-sign it; the initiator will later sign and
	// broadcast to complete a cooperative refund.
	SpendRefundTx Bytes `json:"spendrefundtx"`
	// LockLeafScript and related scripts are sent so the
	// participant can verify the refund chain conforms to the
	// scheme they expect.
	LockLeafScript   Bytes  `json:"lockleafscript"`
	RefundPkScript   Bytes  `json:"refundpkscript"`
	CoopLeafScript   Bytes  `json:"coopleafscript"`
	PunishLeafScript Bytes  `json:"punishleafscript"`
	LockBlocks       uint32 `json:"lockblocks"`
}

var _ Signable = (*AdaptorSetupInit)(nil)

func (m *AdaptorSetupInit) Serialize() []byte {
	buf := make([]byte, 0, 512)
	buf = append(buf, m.OrderID...)
	buf = append(buf, m.MatchID...)
	buf = append(buf, m.PubSpendKey...)
	buf = append(buf, m.ViewKey...)
	buf = append(buf, m.PubSignKeyHalf...)
	buf = append(buf, m.DLEQProof...)
	buf = append(buf, m.RefundTx...)
	buf = append(buf, m.SpendRefundTx...)
	buf = append(buf, m.LockLeafScript...)
	buf = append(buf, m.RefundPkScript...)
	buf = append(buf, m.CoopLeafScript...)
	buf = append(buf, m.PunishLeafScript...)
	return append(buf, uint32Bytes(m.LockBlocks)...)
}

// AdaptorRefundPresigned carries the participant's cooperative
// signature on refundTx and their adaptor signature on
// spendRefundTx. Must be received before the initiator broadcasts
// lockTx - pre-signing is required so refundTx can be broadcast by
// either party if one side goes silent.
type AdaptorRefundPresigned struct {
	Signature
	OrderID               Bytes `json:"orderid"`
	MatchID               Bytes `json:"matchid"`
	RefundSig             Bytes `json:"refundsig"`
	SpendRefundAdaptorSig Bytes `json:"spendrefundadaptorsig"`
}

var _ Signable = (*AdaptorRefundPresigned)(nil)

func (m *AdaptorRefundPresigned) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+
		len(m.RefundSig)+len(m.SpendRefundAdaptorSig))
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, m.RefundSig...)
	return append(s, m.SpendRefundAdaptorSig...)
}

// AdaptorLocked signals that the initiator has broadcast lockTx.
// Analog of an Init message for HTLC swaps. TxID + vout identify
// the taproot lock output for the server's audit and the
// participant's watching.
type AdaptorLocked struct {
	Signature
	OrderID Bytes  `json:"orderid"`
	MatchID Bytes  `json:"matchid"`
	TxID    Bytes  `json:"txid"`
	Vout    uint32 `json:"vout"`
	Value   uint64 `json:"value"`
}

var _ Signable = (*AdaptorLocked)(nil)

func (m *AdaptorLocked) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+len(m.TxID)+12)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, m.TxID...)
	s = append(s, uint32Bytes(m.Vout)...)
	return append(s, uint64Bytes(m.Value)...)
}

// AdaptorXmrLocked signals that the participant has sent XMR to the
// shared address. RestoreHeight is the XMR daemon height at send
// time, needed by the sweep-wallet reconstruction later. TxID is
// included for user-facing reporting; the server cannot validate
// XMR transactions without a monerod, and will typically not.
type AdaptorXmrLocked struct {
	Signature
	OrderID       Bytes  `json:"orderid"`
	MatchID       Bytes  `json:"matchid"`
	XmrTxID       Bytes  `json:"xmrtxid"`
	RestoreHeight uint64 `json:"restoreheight"`
}

var _ Signable = (*AdaptorXmrLocked)(nil)

func (m *AdaptorXmrLocked) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+len(m.XmrTxID)+8)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, m.XmrTxID...)
	return append(s, uint64Bytes(m.RestoreHeight)...)
}

// AdaptorSpendPresig is the initiator's adaptor sig on spendTx,
// forwarded to the participant. The participant completes it with
// their ed25519 scalar and broadcasts spendTx to redeem.
type AdaptorSpendPresig struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	// SpendTx is the unsigned spendTx skeleton; the participant
	// needs it to compute the sighash for adaptor verification and
	// to assemble the final witness.
	SpendTx    Bytes `json:"spendtx"`
	AdaptorSig Bytes `json:"adaptorsig"`
}

var _ Signable = (*AdaptorSpendPresig)(nil)

func (m *AdaptorSpendPresig) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+
		len(m.SpendTx)+len(m.AdaptorSig))
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, m.SpendTx...)
	return append(s, m.AdaptorSig...)
}

// AdaptorSpendBroadcast signals that the participant broadcast
// spendTx. The server records the txid so it can observe the
// completed sig for its own audit; the initiator (on the
// receiving side) does the same to feed RecoverTweakBIP340.
type AdaptorSpendBroadcast struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	TxID    Bytes `json:"txid"`
}

var _ Signable = (*AdaptorSpendBroadcast)(nil)

func (m *AdaptorSpendBroadcast) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+len(m.TxID))
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	return append(s, m.TxID...)
}

// AdaptorRefundBroadcast signals that one of the parties broadcast
// the pre-signed refundTx. After this message the swap enters the
// refund-decision window: if the initiator cooperates, they
// AdaptorCoopRefund; if they stall, the participant eventually
// AdaptorPunishes after CSV.
type AdaptorRefundBroadcast struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	TxID    Bytes `json:"txid"`
	// Broadcaster indicates which party posted the refund. 0 = initiator, 1 = participant.
	Broadcaster uint8 `json:"broadcaster"`
}

var _ Signable = (*AdaptorRefundBroadcast)(nil)

func (m *AdaptorRefundBroadcast) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+len(m.TxID)+1)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, m.TxID...)
	return append(s, m.Broadcaster)
}

// AdaptorCoopRefund signals that the initiator broadcast
// spendRefundTx via the cooperative-refund leaf. The completed
// participant sig on-chain will reveal the initiator's XMR-key-half
// scalar when the participant runs RecoverTweakBIP340.
type AdaptorCoopRefund struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	TxID    Bytes `json:"txid"`
}

var _ Signable = (*AdaptorCoopRefund)(nil)

func (m *AdaptorCoopRefund) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+len(m.TxID))
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	return append(s, m.TxID...)
}

// AdaptorPunish signals that the participant broadcast
// spendRefundTx via the punish leaf after CSV matured. The
// participant gets the BTC; their XMR remains stranded at the
// shared address (the asymmetric cost that disincentivizes
// initiator stalling).
type AdaptorPunish struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	TxID    Bytes `json:"txid"`
}

var _ Signable = (*AdaptorPunish)(nil)

func (m *AdaptorPunish) Serialize() []byte {
	s := make([]byte, 0, len(m.OrderID)+len(m.MatchID)+len(m.TxID))
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	return append(s, m.TxID...)
}
