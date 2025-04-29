package firo

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"time"

	"decred.org/dcrdex/client/asset/btc"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// const (
// 	// Only blocks mined with progpow are considered.
// 	// Previous mining algorithms: MTP and Lyra2z ignored as being too early for
// 	// Firo wallet on Dex ~ late 2023
// 	ProgpowStartTime        = 1635228000 // Tue Oct 26 2021 06:00:00 UTC+0
// 	HeaderLength            = 80
// 	ProgpowExtraLength      = 40
// 	ProgpowFullHeaderLength = HeaderLength + ProgpowExtraLength
// )

type firoBlock struct {
	hdrHash chainhash.Hash
	hdr     *wire.BlockHeader
	txns    []*wire.MsgTx
}

// deserializeBlock hashes the header bytes from the serialized Firo block passed
// in blk. Based on block hash re-gets the block as json and returns the header
// plus any Transparent transactions found parsed into a wire.MsgBlock.
//
// The other 10 transaction types are discarded; including coinbase.
func getBlock(c btc.RpcCaller, hash chainhash.Hash) (*wire.MsgBlock, error) {
	// func deserializeBlock(c rpcCaller, net *chaincfg.Params, blk []byte) (*wire.MsgBlock, error) {
	firoBlock := &firoBlock{}

	// // hash header
	// var header []byte
	// switch net.Name {
	// case "mainnet", "testnet3":
	// 	header = make([]byte, ProgpowFullHeaderLength)
	// 	copy(header, blk[:ProgpowFullHeaderLength])
	// case "regtest":
	// 	header = make([]byte, HeaderLength)
	// 	copy(header, blk[:HeaderLength])
	// default:
	// 	return nil, fmt.Errorf("unknown net: %s", net.Name)
	// }
	// firoBlock.hdrHash = chainhash.DoubleHashH(header)

	// fmt.Printf("firo deserialized block hash: %s\n", firoBlock.hdrHash.String())

	// get json block
	jsonBlock, err := getJsonBlock(c, firoBlock.hdrHash)
	if err != nil {
		return nil, err
	}

	// make hdr
	firoBlock.hdr, err = makeHdr(jsonBlock)
	if err != nil {
		return nil, err
	}

	// get txs
	for _, txHash := range jsonBlock.Tx {
		jsonLiteTx, err := getJsonLiteTx(c, txHash)
		if err != nil {
			return nil, err
		}
		// TODO: do we need to check all are v1?
		if jsonLiteTx.Type != 0 {
			// discard this tx
			continue
		}
		// decode a normal transparent tx
		jsonNormalTx, err := getJsonNormalTx(c, txHash)
		if err != nil {
			return nil, err
		}
		// make transaction
		msgTx, err := makeTransaction(jsonNormalTx)
		if err != nil {
			return nil, err
		}
		firoBlock.txns = append(firoBlock.txns, msgTx)
	}

	return &wire.MsgBlock{
		Header:       *firoBlock.hdr,
		Transactions: firoBlock.txns,
	}, nil
}

func getJsonBlock(c btc.RpcCaller, h chainhash.Hash) (*firoBlkResult, error) {
	var blk firoBlkResult
	args := anylist{h.String()}
	args = append(args, true)
	err := c.Call(methodGetBlock, args, &blk)
	if err != nil {
		return nil, err
	}
	return &blk, nil
}

func makeHdr(jsonBlock *firoBlkResult) (*wire.BlockHeader, error) {
	prev, err := hex.DecodeString(jsonBlock.PreviousBlockHash)
	if err != nil {
		return nil, err
	}
	var prevHash = chainhash.Hash{}
	prevHash.SetBytes(prev)

	merkle, err := hex.DecodeString(jsonBlock.Merkleroot)
	if err != nil {
		return nil, err
	}
	var merkleRoot = chainhash.Hash{}
	merkleRoot.SetBytes(merkle)

	bitsB, err := hex.DecodeString(jsonBlock.Bits)
	if err != nil {
		return nil, err
	}

	bits := binary.LittleEndian.Uint32(bitsB[:4])

	return &wire.BlockHeader{
		Version:    int32(jsonBlock.Version),
		PrevBlock:  prevHash,
		MerkleRoot: merkleRoot,
		Timestamp:  time.Unix(int64(jsonBlock.Time), 0).UTC(),
		Bits:       bits,
		Nonce:      uint32(jsonBlock.Nonce),
	}, nil
}

// getJsonLiteTx just unmarshals the first 4 bytes which encode tx version and
// tx type. This is consistent across all 11 tx types.
func getJsonLiteTx(c btc.RpcCaller, txHash string) (*firoTxLiteResult, error) {
	var txLite firoTxLiteResult
	args := anylist{txHash}
	args = append(args, true)
	err := c.Call(methodRawTransaction, args, &txLite)
	if err != nil {
		return nil, err
	}
	return &txLite, nil
}

// getJsonNormalTx unmarshals most interesting fields in a normal (type 0)
// transparent transaction
func getJsonNormalTx(c btc.RpcCaller, txHash string) (*firoNormalTxResult, error) {
	var txNormal firoNormalTxResult
	args := anylist{txHash}
	args = append(args, true)
	err := c.Call(methodRawTransaction, args, &txNormal)
	if err != nil {
		return nil, err
	}
	return &txNormal, nil
}

func makeTransaction(jsonNormalTx *firoNormalTxResult) (*wire.MsgTx, error) {
	msgTx := &wire.MsgTx{
		Version:  int32(jsonNormalTx.Version),
		LockTime: uint32(jsonNormalTx.Locktime),
	}

	for _, inp := range jsonNormalTx.Vin {
		hash, err := chainhash.NewHashFromStr(inp.Txid)
		if err != nil {
			return nil, err
		}
		outPoint := wire.NewOutPoint(hash, uint32(inp.Vout))
		signatureScript, err := hex.DecodeString(inp.ScriptSig.Hex)
		if err != nil {
			return nil, err
		}
		sequence := inp.Sequence
		txIn := &wire.TxIn{
			PreviousOutPoint: *outPoint,
			SignatureScript:  signatureScript,
			Witness:          nil, // Always
			Sequence:         uint32(sequence),
		}
		msgTx.TxIn = append(msgTx.TxIn, txIn)
	}

	for _, outp := range jsonNormalTx.Vout {
		valueSats := int64(math.Round(outp.Value * 1e8))
		pkScript, err := hex.DecodeString(outp.ScriptPubKey.Hex)
		if err != nil {
			return nil, err
		}
		txOut := &wire.TxOut{
			Value:    valueSats,
			PkScript: pkScript,
		}
		msgTx.TxOut = append(msgTx.TxOut, txOut)
	}
	return msgTx, nil
}
