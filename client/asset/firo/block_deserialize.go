package firo

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

const (
	// Only blocks mined with progpow are considered.
	// Previous mining algorithms: MTP and Lyra2z ignored as being too early for
	// Firo wallet on Dex ~ late 2023
	ProgpowStartTime        = 1635228000 // Tue Oct 26 2021 06:00:00 UTC+0
	HeaderLength            = 80
	ProgpowExtraLength      = 40
	ProgpowFullHeaderLength = HeaderLength + ProgpowExtraLength
)

// deserializeBlock deserializes the wire bytes passed in as blk and returns the
// header for the network plus any Transparent transactions found parsed into a
// wire.MsgBlock.
//
// Other transaction types are discarded; including coinbase.
func deserializeBlock(net *chaincfg.Params, blk []byte) (*wire.MsgBlock, error) {
	var hdrHash chainhash.Hash
	var hdr *wire.BlockHeader

	// hash header
	var header []byte
	switch net.Name {
	case "mainnet", "testnet3", "testnet":
		header = make([]byte, ProgpowFullHeaderLength)
		copy(header, blk[:ProgpowFullHeaderLength])
	case "regtest":
		header = make([]byte, HeaderLength)
		copy(header, blk[:HeaderLength])
	default:
		return nil, fmt.Errorf("unknown net: %s", net.Name)
	}
	hdrHash = chainhash.DoubleHashH(header)

	fmt.Printf("firo block hash: %s\n", hdrHash.String()) // TODO: delete this

	// make a reader over the full block
	r := bytes.NewReader(blk)

	// deserialize the first 80 bytes of the header
	hdr = &wire.BlockHeader{}
	err := hdr.Deserialize(r)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block header: %w", err)
	}

	if int(hdr.Timestamp.Unix()) < ProgpowStartTime {
		return nil, fmt.Errorf("dex not considering blocks mined before progpow")
	}

	if net.Name != "regtest" {
		// Blocks mined later than progpow start time have 40 extra bytes holding
		// mining info. Skip over!
		var extraBytes = make([]byte, ProgpowExtraLength)
		r.Read(extraBytes)
	}

	// This block's transactions
	txnCount, err := wire.ReadVarInt(r, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction count: %w", err)
	}

	if txnCount == 0 {
		return nil, fmt.Errorf("invalid transaction count 0 -- must at least have a coinbase")
	}

	txns := make([]*wire.MsgTx, 0, int(txnCount))
	for i := 0; i < cap(txns); i++ {
		tx, err := deserializeTransaction(r)

		if err != nil {
			return nil, fmt.Errorf("failed to deserialize transaction %d of %d (type=%d) in block %s: %w",
				i+1, txnCount, tx.txType, hdrHash.String(), err)
		}

		if tx.txType == TransactionNormal {
			txns = append(txns, tx.msgTx)
		}
	}

	return &wire.MsgBlock{
		Header:       *hdr,
		Transactions: txns,
	}, nil
}
