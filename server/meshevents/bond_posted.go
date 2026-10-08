// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/server/account"
)

// BondPostedAccount is the account identity on a bond_posted event.
type BondPostedAccount struct {
	AccountID account.AccountID `json:"accountid"`
	Pubkey    dex.Bytes         `json:"pubkey"`
}

// Bond is a fidelity bond on a bond_posted event.
type Bond struct {
	Version  uint16
	AssetID  uint32
	CoinID   []byte
	Amount   int64  // bond value in AssetID's atomic units
	Strength uint32 // Amount / <bond increment at time of acceptance>
	LockTime int64  // bond output locktime, Unix seconds
}

// BondPostedEvent is the payload of a bond_posted event, containing the
// accepted bond and its account.
type BondPostedEvent struct {
	Account *BondPostedAccount `json:"account"`
	Bond    *Bond              `json:"bond"`
}

// NewBondPostedEvent builds a bond_posted event for an account and its bond.
func NewBondPostedEvent(acct *account.Account, bond *Bond) *BondPostedEvent {
	return &BondPostedEvent{
		Account: &BondPostedAccount{
			AccountID: acct.ID,
			Pubkey:    acct.PubKey.SerializeCompressed(),
		},
		Bond: bond,
	}
}

// Kind identifies the mesh event kind.
func (e *BondPostedEvent) Kind() string { return EventKindBondPosted }

// Encode returns the event's JSON payload.
func (e *BondPostedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeBondPostedEvent decodes and validates a bond_posted wire payload.
func DecodeBondPostedEvent(payload []byte) (*BondPostedEvent, error) {
	return decodeEvent[BondPostedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *BondPostedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil bond posted event")
	}
	if e.Bond == nil {
		return fmt.Errorf("nil posted bond")
	}
	_, err := e.PostedAccount()
	return err
}

// PostedAccount parses the account identity carried by the event, verifying
// the replicated pubkey matches the replicated account id.
func (e *BondPostedEvent) PostedAccount() (*account.Account, error) {
	if e.Account == nil {
		return nil, fmt.Errorf("nil bond posted account")
	}
	acct, err := account.NewAccountFromPubKey(e.Account.Pubkey)
	if err != nil {
		return nil, fmt.Errorf("error parsing replicated account pubkey: %w", err)
	}
	if acct.ID != e.Account.AccountID {
		return nil, fmt.Errorf("replicated account id mismatch")
	}
	return acct, nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for a bond_posted event.
func (e *BondPostedEvent) EventTxData() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	bond := e.Bond
	bondData := encode.BuildyBytes{0}.
		AddData(encode.Uint16Bytes(bond.Version)).
		AddData(encode.Uint32Bytes(bond.AssetID)).
		AddData(bond.CoinID).
		AddData(encode.Uint64Bytes(uint64(bond.Amount))).
		AddData(encode.Uint32Bytes(bond.Strength)).
		AddData(encode.Uint64Bytes(uint64(bond.LockTime)))
	return encode.BuildyBytes{0}.
		AddData(e.Account.AccountID[:]).
		AddData(e.Account.Pubkey).
		AddData(bondData), nil
}
