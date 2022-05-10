// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package ltc

import (
	"bytes"
	_ "embed"
	"testing"
)

var (
	// peg-in txn 8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03
	// MEMPOOL, not in block, 2203 bytes, version 2
	// with:
	//  - flag 0x9
	//  - 1 regular input / 1 regular output
	//  - 0 mw inputs / 2 mw outputs
	//  - 1 kernel with peg-in amt and fee amt and stealth pubkey
	//go:embed tx8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03_mp.dat
	tx8b83439mp []byte

	// peg-in txn 8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03
	// This is the same tx as above, but now in the block stripped of mw tx
	// data and with the flag with the mw bit unset.
	// IN BLOCK, 203 bytes, version 2
	// with:
	//  - flag 0x1 (mw removed)
	//  - 1 regular input / 1 regular output
	//go:embed tx8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03.dat
	tx8b83439 []byte

	// Pure MW-only txn (no canonical inputs or outputs).
	// MEMPOOL, not in block, 2203 bytes, version 2
	// with:
	//  - flag 0x8
	//  - 0 regular inputs / 0 regular outputs
	//  - 1 mw input / 2 mw outputs
	//  - 1 kernel with fee amt and stealth pubkey
	//go:embed txe6f8fdb15b27e603adcfa5aa4107a0e7a7b07e6dc76d2ba95a33710db02a0049_mp.dat
	txe6f8fdbmp []byte

	// Pure MW-only txn (no canonical inputs or outputs).
	// MEMPOOL, not in block, 2987 bytes, version 2
	// with:
	//  - flag 0x8
	//  - 0 regular inputs / 0 regular outputs
	//  - 5 mw inputs / 2 mw outputs
	//  - 1 kernel with fee amt and stealth pubkey
	//go:embed tx62e17562a697e3167e2b68bce5a927ac355249ef5751800c4dd927ddf57d9db2_mp.dat
	tx62e1756mp []byte

	// Pure MW-only txn (no canonical inputs or outputs).
	// MEMPOOL, not in block, 1333 bytes, version 2
	// with:
	//  - flag 0x8
	//  - 0 regular inputs / 0 regular outputs
	//  - 1 mw inputs / 1 mw outputs
	//  - 1 kernel with fee amt and pegouts and stealth pubkey
	//go:embed txcc202c44db4d6faec57f42566c6b1e03139f924eaf685ae964b3076594d65349_mp.dat
	txcc202c4mp []byte

	// HogEx with multiple inputs and outputs, IN BLOCK 2269979.
	// Flag 0x8 (omit witness data), null mw tx (but not omitted), 169 bytes.
	//go:embed txde22f4de7116b8482a691cc5e552c4212f0ae77e3c8d92c9cb85c29f4dc1f47c.dat
	txde22f4d []byte
)

func TestDeserializeTx(t *testing.T) {
	tests := []struct {
		name         string
		tx           []byte
		wantHash     string
		wantLockTime uint32 // last field after any mw tx data is the ideal check
	}{
		{
			"mempool peg-in tx 8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03",
			tx8b83439mp,
			"8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03",
			2269978,
		},
		{
			"block peg-in tx 8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03",
			tx8b83439,
			"8b8343978dbef95d54da796977e9a254565c0dc9ce54917d9111267547fcde03",
			2269978,
		},
		{
			"mempool MW tx e6f8fdb15b27e603adcfa5aa4107a0e7a7b07e6dc76d2ba95a33710db02a0049",
			txe6f8fdbmp,
			"e6f8fdb15b27e603adcfa5aa4107a0e7a7b07e6dc76d2ba95a33710db02a0049",
			2269917,
		},
		{
			"mempool MW tx 62e17562a697e3167e2b68bce5a927ac355249ef5751800c4dd927ddf57d9db2",
			tx62e1756mp,
			"62e17562a697e3167e2b68bce5a927ac355249ef5751800c4dd927ddf57d9db2",
			2269919,
		},
		{
			"mempool MW tx cc202c44db4d6faec57f42566c6b1e03139f924eaf685ae964b3076594d65349",
			txcc202c4mp,
			"cc202c44db4d6faec57f42566c6b1e03139f924eaf685ae964b3076594d65349",
			2269977,
		},
		{
			"HogEx tx de22f4de7116b8482a691cc5e552c4212f0ae77e3c8d92c9cb85c29f4dc1f47c",
			txde22f4d,
			"de22f4de7116b8482a691cc5e552c4212f0ae77e3c8d92c9cb85c29f4dc1f47c",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgTx, err := DeserializeTx(bytes.NewReader(tt.tx))
			if err != nil {
				t.Fatal(err)
			}
			if msgTx.LockTime != tt.wantLockTime {
				t.Errorf("want locktime %d, got %d", tt.wantLockTime, msgTx.LockTime)
			}

			txHash := msgTx.TxHash()
			if txHash.String() != tt.wantHash {
				t.Errorf("Wanted tx hash %v, got %v", tt.wantHash, txHash)
			}
		})
	}
}
