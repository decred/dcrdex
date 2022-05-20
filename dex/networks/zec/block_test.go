package zec

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

var (
	//go:embed test-data/simnet_block_header.dat
	simnetBlockHeader []byte
	//go:embed test-data/block_1624455.dat
	block1624455 []byte
	//go:embed test-data/solution_1624455.dat
	solution1624455 []byte
)

func TestBlock(t *testing.T) {
	// expHash := mustDecodeHex("00000000015c8a406ff880c5be4d2ae2744eab8be02a33d0179d68f47e51ea82")
	const expVersion = 4
	expPrevBlock, _ := chainhash.NewHashFromStr("000000000136717e394d8de1f86257724bb463348993a1fbb651bd3fd3f1a279")
	expMerkleRoot, _ := chainhash.NewHashFromStr("43eec2cfd1487ee70ab0277d7f761d99cf6c19d554d2f73959f4af45d516727f")
	// expHashBlockCommitments := mustDecodeHex("30702d1b320e2ea8f603aa8fb54baea5581761d19fccf5061ac82e81d8fdeea4")
	expNonce := mustDecodeHex("f5a409400000000000000000000300000000000000000000000000000000d0e2")

	expBits := binary.LittleEndian.Uint32(mustDecodeHex("d0aa011c"))
	const expTime = 1649294915

	zecBlock, err := DeserializeBlock(block1624455)
	if err != nil {
		t.Fatalf("decodeBlockHeader error: %v", err)
	}

	hdr := &zecBlock.MsgBlock.Header

	if hdr.Version != expVersion {
		t.Fatalf("wrong version. expected %d, got %d", expVersion, hdr.Version)
	}

	if *expPrevBlock != hdr.PrevBlock {
		t.Fatal("wrong previous block", expPrevBlock, hdr.PrevBlock[:])
	}

	if *expMerkleRoot != hdr.MerkleRoot {
		t.Fatal("wrong merkle root", expMerkleRoot, hdr.MerkleRoot[:])
	}

	// TODO: Find out why this is not right.
	// if !bytes.Equal(zecBlock.HashBlockCommitments[:], expHashBlockCommitments) {
	// 	t.Fatal("wrong hashBlockCommitments", zecBlock.HashBlockCommitments[:], expHashBlockCommitments, h)
	// }

	if hdr.Bits != expBits {
		t.Fatalf("wrong bits")
	}

	if hdr.Timestamp.Unix() != expTime {
		t.Fatalf("wrong timestamp")
	}

	if !bytes.Equal(zecBlock.Nonce[:], expNonce) {
		t.Fatal("wrong nonce", zecBlock.Nonce[:], expNonce)
	}

	if !bytes.Equal(zecBlock.Solution[:], solution1624455) {
		t.Fatal("wrong solution")
	}

	if len(zecBlock.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(zecBlock.Transactions))
	}

	tx := zecBlock.Transactions[0]

	if len(tx.TxIn) != 1 {
		t.Fatalf("wrong number of tx inputs. expected 1, got %d", len(tx.TxIn))
	}

	if len(tx.TxOut) != 4 {
		t.Fatalf("wrong number of tx outputs. expected 4, got %d", len(tx.TxOut))
	}
}

func TestSimnetBlockHeader(t *testing.T) {
	zecBlock := &Block{}
	if err := zecBlock.decodeBlockHeader(bytes.NewReader(simnetBlockHeader)); err != nil {
		t.Fatalf("decodeBlockHeader error: %v", err)
	}
}

func mustDecodeHex(hx string) []byte {
	b, err := hex.DecodeString(hx)
	if err != nil {
		panic("mustDecodeHex: " + err.Error())
	}
	return b
}
