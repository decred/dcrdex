// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

// AccountInfo is information about an account on a Decred DEX. The database
// is designed for one account per server.
type AccountInfo struct {
	URL string
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
		AddData([]byte(ai.URL)).
		AddData(ai.EncKey).
		AddData(ai.DEXPubKey.Serialize()).
		AddData(ai.FeeCoin)
}

// DecodeAccountInfo decodes the versioned blob into an *AccountInfo.
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
	if len(pushes) != 4 {
		return nil, fmt.Errorf("decodeAccountInfo: expected 4 data pushes, got %d", len(pushes))
	}
	urlB, keyB, dexB, coinB := pushes[0], pushes[1], pushes[2], pushes[3]
	pk, err := secp256k1.ParsePubKey(dexB)
	if err != nil {
		return nil, err
	}
	return &AccountInfo{
		URL:       string(urlB),
		EncKey:    keyB,
		DEXPubKey: pk,
		FeeCoin:   coinB,
	}, nil
}

// Account proof is information necessary to prove that the DEX server accepted
// the account's fee payment. The fee coin is not part of the proof, since it
// is already stored as part of the AccountInfo blob.
type AccountProof struct {
	URL   string
	Stamp uint64
	Sig   []byte
}

// Encode encodes the AccountProof to a versioned blob.
func (p *AccountProof) Encode() []byte {
	return dbBytes{0}.
		AddData([]byte(p.URL)).
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
	urlB, stampB := pushes[0], pushes[1]
	return &AccountProof{
		URL:   string(urlB),
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
	// DEX is the URL of the server that this order is associated with.
	DEX string
	// Proof is the signatures and other verification-related data for the order.
	Proof OrderProof
}

// MetaMatch is the match and its metadata.
type MetaMatch struct {
	// MetaData is important auxiliary information about the match.
	MetaData *MatchMetaData
	// Match is the match info.
	Match *order.UserMatch
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
}

// MatchSignatures holds the DEX signatures and timestamps associated with
// the messages in the negotiation process.
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
	CounterScript []byte
	SecretHash    []byte
	SecretKey     []byte
	InitStamp     uint64
	RedeemStamp   uint64
	MakerSwap     order.CoinID
	MakerRedeem   order.CoinID
	TakerSwap     order.CoinID
	TakerRedeem   order.CoinID
	Auth          MatchAuth
}

// Encode encodes the MatchProof to a versioned blob.
func (p *MatchProof) Encode() []byte {
	auth := p.Auth
	return dbBytes{0}.
		AddData(p.CounterScript).
		AddData(p.SecretHash).
		AddData(p.SecretKey).
		AddData(p.MakerSwap).
		AddData(p.MakerRedeem).
		AddData(p.TakerSwap).
		AddData(p.TakerRedeem).
		AddData(auth.MatchSig).
		AddData(uint64Bytes(auth.MatchStamp)).
		AddData(auth.InitSig).
		AddData(uint64Bytes(auth.InitStamp)).
		AddData(auth.AuditSig).
		AddData(uint64Bytes(auth.AuditStamp)).
		AddData(auth.RedeemSig).
		AddData(uint64Bytes(auth.RedeemStamp)).
		AddData(auth.RedemptionSig).
		AddData(uint64Bytes(auth.RedemptionStamp))
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
	}
	return nil, fmt.Errorf("unknown MatchProof version %d", ver)
}

func decodeMatchProof_v0(pushes [][]byte) (*MatchProof, error) {
	if len(pushes) != 17 {
		return nil, fmt.Errorf("DecodeMatchProof: expected 17 pushes, got %d", len(pushes))
	}
	return &MatchProof{
		CounterScript: pushes[0],
		SecretHash:    pushes[1],
		SecretKey:     pushes[2],
		MakerSwap:     pushes[3],
		MakerRedeem:   pushes[4],
		TakerSwap:     pushes[5],
		TakerRedeem:   pushes[6],
		Auth: MatchAuth{
			MatchSig:        pushes[7],
			MatchStamp:      intCoder.Uint64(pushes[8]),
			InitSig:         pushes[9],
			InitStamp:       intCoder.Uint64(pushes[10]),
			AuditSig:        pushes[11],
			AuditStamp:      intCoder.Uint64(pushes[12]),
			RedeemSig:       pushes[13],
			RedeemStamp:     intCoder.Uint64(pushes[14]),
			RedemptionSig:   pushes[15],
			RedemptionStamp: intCoder.Uint64(pushes[16]),
		},
	}, nil
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
		return nil, fmt.Errorf("decodeMatchProof: expected 2 push, got %d", len(pushes))
	}
	return &OrderProof{
		DEXSig:   pushes[0],
		Preimage: pushes[1],
	}, nil
}

// Wallet is information necessary to create an asset.Wallet.
type Wallet struct {
	AssetID     uint32
	Account     string
	INIPath     string
	Balance     uint64
	BalUpdate   time.Time
	EncryptedPW []byte
	Address     string
}

// Encode encodes the Wallet to a versioned blob.
func (w *Wallet) Encode() []byte {
	return dbBytes{0}.
		AddData(uint32Bytes(w.AssetID)).
		AddData([]byte(w.Account)).
		AddData([]byte(w.INIPath)).
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
	if len(pushes) != 5 {
		return nil, fmt.Errorf("decodeWallet_v0: expected 5 pushes, got %d", len(pushes))
	}
	idB, acctB, iniB := pushes[0], pushes[1], pushes[2]
	keyB, addressB := pushes[3], pushes[4]
	return &Wallet{
		AssetID:     intCoder.Uint32(idB),
		Account:     string(acctB),
		INIPath:     string(iniB),
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
		AddData([]byte(acct.URL)).
		AddData(acct.EncKey).
		AddData(acct.DEXPubKey.Serialize())
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
		ai.URL = string(pushes[0])
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
	return ioutil.WriteFile(path, backup, 0644)
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
