// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tanka

import (
	"encoding/hex"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const PeerIDLength = secp256k1.PubKeyBytesLenCompressed

// PeerID is the primary identifier for both clients and servers on Tatanka
// Mesh. The PeerID is the compressed-format serialized secp256k1.PublicKey.
type PeerID [PeerIDLength]byte

func (p PeerID) String() string {
	return hex.EncodeToString(p[:])
}

// func (p *PeerID) MarshalJSON() ([]byte, error) {
// 	return json.Marshal(hex.EncodeToString((*p)[:]))
// }

// func (p *PeerID) UnmarshalJSON(encHex []byte) (err error) {
// 	if len(encHex) < 2 {
// 		return fmt.Errorf("unmarshalled bytes, %q, not valid", string(encHex))
// 	}
// 	if encHex[0] != '"' || encHex[len(encHex)-1] != '"' {
// 		return fmt.Errorf("unmarshalled bytes, %q, not quoted", string(encHex))
// 	}
// 	// DecodeString over-allocates by at least double, and it makes a copy.
// 	src := encHex[1 : len(encHex)-1]
// 	if len(src) != PeerIDLength*2 {
// 		return fmt.Errorf("unmarshalled bytes of wrong length %d", len(src))
// 	}
// 	dst := make([]byte, len(src)/2)
// 	_, err = hex.Decode(dst, src)
// 	if err == nil {
// 		var id PeerID
// 		copy(id[:], dst)
// 		*p = id
// 	}
// 	return err
// }

// Peer is a network peer, which could be a client or a server node.
type Peer struct {
	ID         PeerID               `json:"-"`
	PubKey     *secp256k1.PublicKey `json:"-"`
	Bonds      []*Bond              `json:"-"`
	Reputation *Reputation          `json:"reputation"`
}

// BondTier sums the tier of the current bond set.
func (p *Peer) BondTier() (tier uint64) {
	for _, b := range p.Bonds {
		tier += b.Strength
	}
	return
}

// Sender is an interface that is implemented by all network connections.
type Sender interface {
	Send(*msgjson.Message) error
	SendRaw([]byte) error
	Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error
	RequestRaw(msgID uint64, rawMsg []byte, respHandler func(*msgjson.Message)) error
	SetPeerID(PeerID)
	PeerID() PeerID
	Disconnect()
}
