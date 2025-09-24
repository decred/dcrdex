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

func (p PeerID) PublicKey() (*secp256k1.PublicKey, error) {
	return secp256k1.ParsePubKey(p[:])
}

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

var (
	SimnetTatankaPriv, _ = hex.DecodeString("4122f61ac48667b3d21c624adca3b390ed0ce17e231cf642e4ac993241fd01af")
	SimnetTatankaPeerID  = PeerID{0x02, 0x2e, 0x3d, 0x03, 0x9d, 0xd0, 0x48, 0x1b, 0xcd, 0x6e, 0x71, 0xcc, 0x79, 0x0e, 0x47, 0x2e, 0xea, 0xc7, 0x4c, 0x90, 0x0c, 0x5d, 0x96, 0x6a, 0xd0, 0xcd, 0xe1, 0x3e, 0xeb, 0xec, 0xe2, 0xe1, 0xbe}
)
