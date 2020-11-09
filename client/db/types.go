// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"golang.org/x/crypto/blake2s"
)

// Severity indicates the level of required action for a notification. The DEX
// db only stores notifications with Severity >= Success.
type Severity uint8

const (
	Ignorable Severity = iota
	// Data notifications are not meant for display to the user. These
	// notifications are used only for communication of information necessary for
	// UI updates or other high-level state changes.
	Data
	// Poke notifications are not persistent across sessions. These should be
	// displayed if the user has a live notification feed. They are not stored in
	// the database.
	Poke
	// Success and higher are stored and can be recalled using DB.NotificationsN.
	Success
	WarningLevel
	ErrorLevel
)

// String satisfies fmt.Stringer for Severity.
func (s Severity) String() string {
	switch s {
	case Ignorable:
		return "ignore"
	case Data:
		return "data"
	case Poke:
		return "poke"
	case WarningLevel:
		return "warning"
	case ErrorLevel:
		return "error"
	case Success:
		return "success"
	}
	return "unknown severity"
}

// AccountInfo is information about an account on a Decred DEX. The database
// is designed for one account per server.
type AccountInfo struct {
	Host string
	Cert []byte
	// EncKey should be an encrypted private key. The database itself does not
	// handle encryption (yet?).
	EncKey    []byte
	DEXPubKey *secp256k1.PublicKey
	FeeCoin   []byte
	// Paid will be set on retrieval based on whether there is an AccountProof
	// set.
	Paid bool
}

// Encode the AccountInfo as bytes.
func (ai *AccountInfo) Encode() []byte {
	return dbBytes{0}.
		AddData([]byte(ai.Host)).
		AddData(ai.EncKey).
		AddData(ai.DEXPubKey.SerializeCompressed()).
		AddData(ai.FeeCoin).
		AddData(ai.Cert)
}

// DecodeAccountInfo decodes the versioned blob into an *AccountInfo. The byte
// slice fields of AccountInfo reference the underlying buffer of the the input.
func DecodeAccountInfo(b []byte) (*AccountInfo, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeAccountInfo_v0(pushes)
	}
	return nil, fmt.Errorf("unknown AccountInfo version %d", ver)
}

func decodeAccountInfo_v0(pushes [][]byte) (*AccountInfo, error) {
	if len(pushes) != 5 {
		return nil, fmt.Errorf("decodeAccountInfo: expected 5 data pushes, got %d", len(pushes))
	}
	hostB, keyB, dexB := pushes[0], pushes[1], pushes[2]
	coinB, certB := pushes[3], pushes[4]
	pk, err := secp256k1.ParsePubKey(dexB)
	if err != nil {
		return nil, err
	}
	return &AccountInfo{
		Host:      string(hostB),
		EncKey:    keyB,
		DEXPubKey: pk,
		FeeCoin:   coinB,
		Cert:      certB,
	}, nil
}

// Account proof is information necessary to prove that the DEX server accepted
// the account's fee payment. The fee coin is not part of the proof, since it
// is already stored as part of the AccountInfo blob.
type AccountProof struct {
	Host  string
	Stamp uint64
	Sig   []byte
}

// Encode encodes the AccountProof to a versioned blob.
func (p *AccountProof) Encode() []byte {
	return dbBytes{0}.
		AddData([]byte(p.Host)).
		AddData(uint64Bytes(p.Stamp)).
		AddData(p.Sig)
}

// DecodeAccountProof decodes the versioned blob to a *MatchProof.
func DecodeAccountProof(b []byte) (*AccountProof, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeAccountProof_v0(pushes)
	}
	return nil, fmt.Errorf("unknown AccountProof version %d", ver)
}

func decodeAccountProof_v0(pushes [][]byte) (*AccountProof, error) {
	if len(pushes) != 3 {
		return nil, fmt.Errorf("decodeAccountProof_v0: expected 3 pushes, got %d", len(pushes))
	}
	hostB, stampB := pushes[0], pushes[1]
	return &AccountProof{
		Host:  string(hostB),
		Stamp: intCoder.Uint64(stampB),
		Sig:   pushes[2],
	}, nil
}

// MetaOrder is an order and its metadata.
type MetaOrder struct {
	// MetaData is important auxiliary information about the order.
	MetaData *OrderMetaData
	// Order is the order.
	Order order.Order
}

// OrderMetaData is important auxiliary information about an order.
type OrderMetaData struct {
	// Status is the last known order status.
	Status order.OrderStatus
	// Host is the hostname of the server that this order is associated with.
	Host string
	// Proof is the signatures and other verification-related data for the order.
	Proof OrderProof
	// ChangeCoin is a change coin from a match. Change coins are "daisy-chained"
	// for matches. All funding coins go into the first match, and the change coin
	// from the initiation transaction is used to fund the next match. The
	// change from that matches ini tx funds the next match, etc.
	ChangeCoin order.CoinID
	// LinkedOrder is used to specify the cancellation order for a trade, or
	// vice-versa.
	LinkedOrder order.OrderID
	// SwapFeesPaid is the sum of the actual fees paid for all swaps.
	SwapFeesPaid uint64
	// RedemptionFeesPaid is the sum of the actual fees paid for all
	// redemptions.
	RedemptionFeesPaid uint64
}

// MetaMatch is the match and its metadata.
type MetaMatch struct {
	// MetaData is important auxiliary information about the match.
	MetaData *MatchMetaData
	// Match is the match info.
	Match *order.UserMatch
}

// SetStatus sets the match status in both the UserMatch and the MatchMetaData.
func (m *MetaMatch) SetStatus(status order.MatchStatus) {
	m.MetaData.Status = status
	m.Match.Status = status
}

// ID is a unique ID for the match-order pair.
func (m *MetaMatch) ID() []byte {
	return hashKey(append(m.Match.MatchID[:], m.Match.OrderID[:]...))
}

// MatchMetaData is important auxiliary information about the match.
type MatchMetaData struct {
	// Status is the last known match status.
	Status order.MatchStatus
	// Proof is the signatures and other verification-related data for the match.
	Proof MatchProof
	// DEX is the URL of the server that this match is associated with.
	DEX string
	// Base is the base asset of the exchange market.
	Base uint32
	// Quote is the quote asset of the exchange market.
	Quote uint32
	// Stamp is the match time (ms UNIX), according to the server's 'match'
	// request timestamp.
	Stamp uint64
}

// MatchAuth holds the DEX signatures and timestamps associated with the
// messages in the negotiation process.
type MatchAuth struct {
	MatchSig        []byte
	MatchStamp      uint64
	InitSig         []byte
	InitStamp       uint64
	AuditSig        []byte
	AuditStamp      uint64
	RedeemSig       []byte
	RedeemStamp     uint64
	RedemptionSig   []byte
	RedemptionStamp uint64
}

// MatchProof is information related to the progression of the swap negotiation
// process.
type MatchProof struct {
	Script        []byte
	CounterScript []byte
	SecretHash    []byte
	Secret        []byte
	MakerSwap     order.CoinID
	MakerRedeem   order.CoinID
	TakerSwap     order.CoinID
	TakerRedeem   order.CoinID
	RefundCoin    order.CoinID
	Auth          MatchAuth
	ServerRevoked bool
	SelfRevoked   bool
}

// Encode encodes the MatchProof to a versioned blob.
func (p *MatchProof) Encode() []byte {
	auth := p.Auth
	srvRevoked := encode.ByteFalse
	if p.ServerRevoked {
		srvRevoked = encode.ByteTrue
	}
	selfRevoked := encode.ByteFalse
	if p.SelfRevoked {
		selfRevoked = encode.ByteTrue
	}

	return dbBytes{1}.
		AddData(p.Script).
		AddData(p.CounterScript).
		AddData(p.SecretHash).
		AddData(p.Secret).
		AddData(p.MakerSwap).
		AddData(p.MakerRedeem).
		AddData(p.TakerSwap).
		AddData(p.TakerRedeem).
		AddData(p.RefundCoin).
		AddData(auth.MatchSig).
		AddData(uint64Bytes(auth.MatchStamp)).
		AddData(auth.InitSig).
		AddData(uint64Bytes(auth.InitStamp)).
		AddData(auth.AuditSig).
		AddData(uint64Bytes(auth.AuditStamp)).
		AddData(auth.RedeemSig).
		AddData(uint64Bytes(auth.RedeemStamp)).
		AddData(auth.RedemptionSig).
		AddData(uint64Bytes(auth.RedemptionStamp)).
		AddData(srvRevoked).
		AddData(selfRevoked)
}

// DecodeMatchProof decodes the versioned blob to a *MatchProof.
func DecodeMatchProof(b []byte) (*MatchProof, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeMatchProof_v0(pushes)
	case 1:
		return decodeMatchProof_v1(pushes)
	}
	return nil, fmt.Errorf("unknown MatchProof version %d", ver)
}

func decodeMatchProof_v0(pushes [][]byte) (*MatchProof, error) {
	pushes = append(pushes, encode.ByteFalse)
	return decodeMatchProof_v1(pushes)
}

func decodeMatchProof_v1(pushes [][]byte) (*MatchProof, error) {
	if len(pushes) != 21 {
		return nil, fmt.Errorf("DecodeMatchProof: expected 21 pushes, got %d", len(pushes))
	}
	return &MatchProof{
		Script:        pushes[0],
		CounterScript: pushes[1],
		SecretHash:    pushes[2],
		Secret:        pushes[3],
		MakerSwap:     pushes[4],
		MakerRedeem:   pushes[5],
		TakerSwap:     pushes[6],
		TakerRedeem:   pushes[7],
		RefundCoin:    pushes[8],
		Auth: MatchAuth{
			MatchSig:        pushes[9],
			MatchStamp:      intCoder.Uint64(pushes[10]),
			InitSig:         pushes[11],
			InitStamp:       intCoder.Uint64(pushes[12]),
			AuditSig:        pushes[13],
			AuditStamp:      intCoder.Uint64(pushes[14]),
			RedeemSig:       pushes[15],
			RedeemStamp:     intCoder.Uint64(pushes[16]),
			RedemptionSig:   pushes[17],
			RedemptionStamp: intCoder.Uint64(pushes[18]),
		},
		ServerRevoked: bytes.Equal(pushes[19], encode.ByteTrue),
		SelfRevoked:   bytes.Equal(pushes[20], encode.ByteTrue),
	}, nil
}

// IsRevoked is true if either ServerRevoked or SelfRevoked is true.
func (p *MatchProof) IsRevoked() bool {
	return p.ServerRevoked || p.SelfRevoked
}

// OrderProof is information related to order authentication and matching.
type OrderProof struct {
	DEXSig   []byte
	Preimage []byte
}

// Encode encodes the OrderProof to a versioned blob.
func (p *OrderProof) Encode() []byte {
	return dbBytes{0}.AddData(p.DEXSig).AddData(p.Preimage)
}

// DecodeOrderProof decodes the versioned blob to an *OrderProof.
func DecodeOrderProof(b []byte) (*OrderProof, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeOrderProof_v0(pushes)
	}
	return nil, fmt.Errorf("unknown OrderProof version %d", ver)
}

func decodeOrderProof_v0(pushes [][]byte) (*OrderProof, error) {
	if len(pushes) != 2 {
		return nil, fmt.Errorf("decodeOrderProof: expected 2 push, got %d", len(pushes))
	}
	return &OrderProof{
		DEXSig:   pushes[0],
		Preimage: pushes[1],
	}, nil
}

// encodeAssetBalance serializes an asset.Balance.
func encodeAssetBalance(bal *asset.Balance) []byte {
	return dbBytes{0}.
		AddData(uint64Bytes(bal.Available)).
		AddData(uint64Bytes(bal.Immature)).
		AddData(uint64Bytes(bal.Locked))
}

// decodeAssetBalance deserializes an asset.Balance.
func decodeAssetBalance(b []byte) (*asset.Balance, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeAssetBalance_v0(pushes)
	}
	return nil, fmt.Errorf("unknown Balance version %d", ver)
}

func decodeAssetBalance_v0(pushes [][]byte) (*asset.Balance, error) {
	if len(pushes) != 3 {
		return nil, fmt.Errorf("decodeBalance_v0: expected 3 push, got %d", len(pushes))
	}
	return &asset.Balance{
		Available: intCoder.Uint64(pushes[0]),
		Immature:  intCoder.Uint64(pushes[1]),
		Locked:    intCoder.Uint64(pushes[2]),
	}, nil
}

// Balance represents a wallet's balance in various contexts.
type Balance struct {
	asset.Balance
	Stamp time.Time `json:"stamp"`
}

// Encode encodes the Balance to a versioned blob.
func (b *Balance) Encode() []byte {
	return dbBytes{0}.
		AddData(encodeAssetBalance(&b.Balance)).
		AddData(uint64Bytes(encode.UnixMilliU(b.Stamp)))
}

// DecodeBalance decodes the versioned blob to a *Balance.
func DecodeBalance(b []byte) (*Balance, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeBalance_v0(pushes)
	}
	return nil, fmt.Errorf("unknown Balance version %d", ver)
}

func decodeBalance_v0(pushes [][]byte) (*Balance, error) {
	if len(pushes) < 2 {
		return nil, fmt.Errorf("decodeBalances_v0: expected >= 2 pushes. got %d", len(pushes))
	}
	if len(pushes)%2 != 0 {
		return nil, fmt.Errorf("decodeBalances_v0: expected an even number of pushes, got %d", len(pushes))
	}
	bal, err := decodeAssetBalance(pushes[0])
	if err != nil {
		return nil, fmt.Errorf("decodeBalances_v0: error decoding zero conf balance: %w", err)
	}

	return &Balance{
		Balance: *bal,
		Stamp:   encode.UnixTimeMilli(int64(intCoder.Uint64(pushes[1]))),
	}, nil
}

// Wallet is information necessary to create an asset.Wallet.
type Wallet struct {
	AssetID     uint32
	Settings    map[string]string
	Balance     *Balance
	EncryptedPW []byte
	Address     string
}

// Encode encodes the Wallet to a versioned blob.
func (w *Wallet) Encode() []byte {
	return dbBytes{0}.
		AddData(uint32Bytes(w.AssetID)).
		AddData(config.Data(w.Settings)).
		AddData(w.EncryptedPW).
		AddData([]byte(w.Address))
}

// DecodeWallet decodes the versioned blob to a *Wallet.
func DecodeWallet(b []byte) (*Wallet, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeWallet_v0(pushes)
	}
	return nil, fmt.Errorf("unknown DecodeWallet version %d", ver)
}

func decodeWallet_v0(pushes [][]byte) (*Wallet, error) {
	if len(pushes) != 4 {
		return nil, fmt.Errorf("decodeWallet_v0: expected 4 pushes, got %d", len(pushes))
	}
	idB, settingsB, keyB, addressB := pushes[0], pushes[1], pushes[2], pushes[3]
	settings, err := config.Parse(settingsB)
	if err != nil {
		return nil, fmt.Errorf("unable to decode wallet settings")
	}
	return &Wallet{
		AssetID:     intCoder.Uint32(idB),
		Settings:    settings,
		EncryptedPW: keyB,
		Address:     string(addressB),
	}, nil
}

// ID is the byte-encoded asset ID for this wallet.
func (w *Wallet) ID() []byte {
	return uint32Bytes(w.AssetID)
}

// SID is a string respresentation of the wallet's asset ID.
func (w *Wallet) SID() string {
	return strconv.Itoa(int(w.AssetID))
}

type dbBytes = encode.BuildyBytes

var uint64Bytes = encode.Uint64Bytes
var uint32Bytes = encode.Uint32Bytes
var intCoder = encode.IntCoder

// AccountBackup represents a user account backup.
type AccountBackup struct {
	KeyParams []byte
	Accounts  []*AccountInfo
}

// encodeDEXAccount serializes the details needed to backup a dex account.
func encodeDEXAccount(acct *AccountInfo) []byte {
	return dbBytes{0}.
		AddData([]byte(acct.Host)).
		AddData(acct.EncKey).
		AddData(acct.DEXPubKey.SerializeCompressed())
}

// decodeDEXAccount decodes the versioned blob into an AccountInfo.
func decodeDEXAccount(acctB []byte) (*AccountInfo, error) {
	ver, pushes, err := encode.DecodeBlob(acctB)
	if err != nil {
		return nil, err
	}

	switch ver {
	case 0:
		if len(pushes) != 3 {
			return nil, fmt.Errorf("expected 3 pushes, got %d", len(pushes))
		}

		var ai AccountInfo
		ai.Host = string(pushes[0])
		ai.EncKey = pushes[1]
		ai.DEXPubKey, err = secp256k1.ParsePubKey(pushes[2])
		if err != nil {
			return nil, err
		}
		return &ai, nil
	}
	return nil, fmt.Errorf("unknown DEX account backup version %d", ver)
}

// Serialize encodes an account backup as bytes.
func (ab *AccountBackup) Serialize() []byte {
	backup := dbBytes{0}.AddData(ab.KeyParams)
	for _, acct := range ab.Accounts {
		backup = backup.AddData(encodeDEXAccount(acct))
	}
	return backup
}

// decodeAccountBackup decodes the versioned blob into an *AccountBackup.
func decodeAccountBackup(b []byte) (*AccountBackup, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		keyParams := pushes[0]
		accts := make([]*AccountInfo, 0, len(pushes[1:]))
		for _, push := range pushes[1:] {
			ai, err := decodeDEXAccount(push)
			if err != nil {
				return nil, err
			}
			accts = append(accts, ai)
		}

		return &AccountBackup{
			KeyParams: keyParams,
			Accounts:  accts,
		}, nil
	}
	return nil, fmt.Errorf("unknown AccountBackup version %d", ver)
}

// Save persists an account backup to file.
func (ab *AccountBackup) Save(path string) error {
	backup := ab.Serialize()
	return ioutil.WriteFile(path, backup, 0o600)
}

// RestoreAccountBackup generates a user account from a backup file.
func RestoreAccountBackup(path string) (*AccountBackup, error) {
	backup, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ab, err := decodeAccountBackup(backup)
	if err != nil {
		return nil, err
	}
	return ab, nil
}

// Notification is information for the user that is typically meant for display,
// and is persisted for recall across sessions.
type Notification struct {
	NoteType    string    `json:"type"`
	SubjectText string    `json:"subject"`
	DetailText  string    `json:"details"`
	Severeness  Severity  `json:"severity"`
	TimeStamp   uint64    `json:"stamp"`
	Ack         bool      `json:"acked"`
	Id          dex.Bytes `json:"id"`
}

// NewNotification is a constructor for a Notification.
func NewNotification(noteType, subject, details string, severity Severity) Notification {
	note := Notification{
		NoteType:    noteType,
		SubjectText: subject,
		DetailText:  details,
		Severeness:  severity,
	}
	note.Stamp()
	return note
}

// ID is a unique ID based on a hash of the notification data.
func (n *Notification) ID() dex.Bytes {
	return noteKey(n.Encode())
}

// Type is the notification type.
func (n *Notification) Type() string {
	return n.NoteType
}

// Subject is a short description of the notification contents.
func (n *Notification) Subject() string {
	return n.SubjectText
}

// Details should contain more detailed information.
func (n *Notification) Details() string {
	return n.DetailText
}

// Severity is the notification severity.
func (n *Notification) Severity() Severity {
	return n.Severeness
}

// Time is the notification timestamp. The timestamp is set in NewNotification.
func (n *Notification) Time() uint64 {
	return n.TimeStamp
}

// Acked is true if the user has seen the notification. Acknowledgement is
// recorded with DB.AckNotification.
func (n *Notification) Acked() bool {
	return n.Ack
}

// Stamp sets the notification timestamp. If NewNotification is used to
// construct the Notification, the timestamp will already be set.
func (n *Notification) Stamp() {
	n.TimeStamp = encode.UnixMilliU(time.Now())
	n.Id = n.ID()
}

// DBNote is a function to return the *Notification itself. It  should really be
// defined on the concrete types in core, but is ubiquitous so defined here for
// convenience.
func (n *Notification) DBNote() *Notification {
	return n
}

// String generates a compact human-readable representation of the Notification
// that is suitable for logging. For example:
//   |SUCCESS| (fee payment) Fee paid - Waiting for 2 confirmations before trading at https://superdex.tld:7232
//   |DATA| (boring event) Subject without details
func (n *Notification) String() string {
	// In case type and/or detail or empty strings, adjust the formatting to
	// avoid extra whitespace.
	var format strings.Builder
	format.WriteString("|%s| (%s)") // always nil error
	if len(n.DetailText) > 0 || len(n.SubjectText) > 0 {
		format.WriteString(" ")
	}
	format.WriteString("%s")
	if len(n.DetailText) > 0 && len(n.SubjectText) > 0 {
		format.WriteString(" - ")
	}
	format.WriteString("%s")

	severity := strings.ToUpper(n.Severity().String())
	return fmt.Sprintf(format.String(), severity, n.NoteType, n.SubjectText, n.DetailText)
}

// DecodeWallet decodes the versioned blob to a *Wallet.
func DecodeNotification(b []byte) (*Notification, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeNotification_v0(pushes)
	}
	return nil, fmt.Errorf("unknown DecodeNotification version %d", ver)
}

func decodeNotification_v0(pushes [][]byte) (*Notification, error) {
	if len(pushes) != 5 {
		return nil, fmt.Errorf("decodeNotification_v0: expected 5 pushes, got %d", len(pushes))
	}
	if len(pushes[3]) != 1 {
		return nil, fmt.Errorf("decodeNotification_v0: severity push is supposed to be length 1. got %d", len(pushes[2]))
	}

	return &Notification{
		NoteType:    string(pushes[0]),
		SubjectText: string(pushes[1]),
		DetailText:  string(pushes[2]),
		Severeness:  Severity(pushes[3][0]),
		TimeStamp:   intCoder.Uint64(pushes[4]),
	}, nil
}

// Encode encodes the Notification to a versioned blob.
func (n *Notification) Encode() []byte {
	return dbBytes{0}.
		AddData([]byte(n.NoteType)).
		AddData([]byte(n.SubjectText)).
		AddData([]byte(n.DetailText)).
		AddData([]byte{byte(n.Severeness)}).
		AddData(uint64Bytes(n.TimeStamp))
}

// OrderFilter is used to limit the results returned by a query to (DB).Orders.
type OrderFilter struct {
	// N is the number of orders to return in the set.
	N int
	// Offset can be used to shift the window of the time-sorted orders such
	// that any orders that would sort to index <= the order specified by Offset
	// will be rejected.
	Offset order.OrderID
	// Hosts is a list of acceptable hosts. A zero-length Hosts means all
	// hosts are accepted.
	Hosts []string
	// Assets is a list of BIP IDs for acceptable assets. A zero-length Assets
	// means all assets are accepted.
	Assets []uint32
	// Statuses is a list of acceptable statuses. A zero-length Statuses means
	// all statuses are accepted.
	Statuses []order.OrderStatus
}

// noteKeySize must be <= 32.
const noteKeySize = 8

// noteKey creates a unique key from the hash of the supplied bytes.
func noteKey(b []byte) []byte {
	h := blake2s.Sum256(b)
	return h[:noteKeySize]
}

// hashKey creates a unique key from the hash of the supplied bytes.
func hashKey(b []byte) []byte {
	h := blake2s.Sum256(b)
	return h[:]
}
