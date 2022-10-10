//go:build !spvlive

package btc

import (
	"bytes"
	_ "embed"
	"testing"

	"github.com/btcsuite/btcd/wire"
)

//go:embed bitcoin-block-757922.dat
var block757922 []byte

//go:embed bitcoin-tx-7393096d97bfee8660f4100ffd61874d62f9a65de9fb6acf740c4c386990ef73.dat
var tx7393096d9 []byte

func TestBigWitness(t *testing.T) {
	msgBlock := &wire.MsgBlock{}
	err := msgBlock.Deserialize(bytes.NewReader(block757922))
	if err != nil {
		t.Fatal(err)
	}
	wantHash := "0000000000000000000400a35a007e223a7fb8a622dc7b5aa5eaace6824291fb"
	if h := msgBlock.BlockHash().String(); h != wantHash {
		t.Errorf("got %v, wanted %v", h, wantHash)
	}

	msgTx := &wire.MsgTx{}
	err = msgTx.Deserialize(bytes.NewReader(tx7393096d9))
	if err != nil {
		t.Fatal(err)
	}
	wantHash = "7393096d97bfee8660f4100ffd61874d62f9a65de9fb6acf740c4c386990ef73"
	if h := msgTx.TxHash().String(); h != wantHash {
		t.Errorf("got %v, wanted %v", h, wantHash)
	}
}
