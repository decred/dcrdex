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
	var acctInfo *AccountInfo
	err := encode.VersionedDecode("decodeAccountInfo", b, map[byte]encode.ByteDecoder{
		0: func(pushes [][]byte) error {
			if len(pushes) != 4 {
				return fmt.Errorf("decodeAccountInfo: expected 3 data pushes, got %d", len(pushes))
			}
			urlB, keyB, dexB, coinB := pushes[0], pushes[1], pushes[2], pushes[3]
			pk, err := secp256k1.ParsePubKey(dexB)
			if err != nil {
				return err
			}
			acctInfo = &AccountInfo{
				URL:       string(urlB),
				EncKey:    keyB,
				DEXPubKey: pk,
				FeeCoin:   coinB,
			}
			return nil
		},
	})
	return acctInfo, err
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

// MatchSignatures holds the DEX signatures associated with each step of the
// swap negotiation process.
type MatchSignatures struct {
	Match      []byte
	Init       []byte
	Audit      []byte
	Redeem     []byte
	Redemption []byte
}

// MatchProof is information related to the progression of the swap negotiation
// process.
type MatchProof struct {
	MakerSwap   order.CoinID
	MakerRedeem order.CoinID
	TakerSwap   order.CoinID
	TakerRedeem order.CoinID
	Sigs        MatchSignatures
}

// Encode encodes the MatchProof to a versioned blob.
func (p *MatchProof) Encode() []byte {
	sigs := p.Sigs
	return dbBytes{0}.
		AddData(p.MakerSwap).
		AddData(p.MakerRedeem).
		AddData(p.TakerSwap).
		AddData(p.TakerRedeem).
		AddData(sigs.Match).
		AddData(sigs.Init).
		AddData(sigs.Audit).
		AddData(sigs.Redeem).
		AddData(sigs.Redemption)
}

// DecodeMatchProof decodes the versioned blob to a *MatchProof.
func DecodeMatchProof(b []byte) (*MatchProof, error) {
	var proof *MatchProof
	err := encode.VersionedDecode("decodeMatchProof", b, map[byte]encode.ByteDecoder{
		0: func(pushes [][]byte) error {
			if len(pushes) != 9 {
				return fmt.Errorf("decodeMatchProof: expected 9 pushes, got %d", len(pushes))
			}
			proof = &MatchProof{
				MakerSwap:   pushes[0],
				MakerRedeem: pushes[1],
				TakerSwap:   pushes[2],
				TakerRedeem: pushes[3],
				Sigs: MatchSignatures{
					Match:      pushes[4],
					Init:       pushes[5],
					Audit:      pushes[6],
					Redeem:     pushes[7],
					Redemption: pushes[8],
				},
			}
			return nil
		},
	})
	return proof, err
}

// OrderProof is information related to order authentication and matching.
type OrderProof struct {
	DEXSig []byte
}

// Encode encodes the OrderProof to a versioned blob.
func (p *OrderProof) Encode() []byte {
	return dbBytes{0}.
		AddData(p.DEXSig)
}

// DecodeOrderProof decodes the versioned blob to an *OrderProof.
func DecodeOrderProof(b []byte) (*OrderProof, error) {
	var proof *OrderProof
	err := encode.VersionedDecode("decodeOrderProof", b, map[byte]encode.ByteDecoder{
		0: func(pushes [][]byte) error {
			if len(pushes) != 1 {
				return fmt.Errorf("decodeMatchProof: expected 1 push, got %d", len(pushes))
			}
			proof = &OrderProof{
				DEXSig: pushes[0],
			}
			return nil
		},
	})
	return proof, err
}

type dbBytes = encode.BuildyBytes
