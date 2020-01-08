// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"

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
		return nil, fmt.Errorf("decodeAccountInfo: expected 3 data pushes, got %d", len(pushes))
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
	DEXSig []byte
}

// Encode encodes the OrderProof to a versioned blob.
func (p *OrderProof) Encode() []byte {
	return dbBytes{0}.AddData(p.DEXSig)
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
	if len(pushes) != 1 {
		return nil, fmt.Errorf("decodeMatchProof: expected 1 push, got %d", len(pushes))
	}
	return &OrderProof{
		DEXSig: pushes[0],
	}, nil
}

type dbBytes = encode.BuildyBytes

var uint64Bytes = encode.Uint64Bytes
var intCoder = encode.IntCoder
