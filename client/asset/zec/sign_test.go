//go:build !harness

package zec

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/txscript"
)

func TestSign(t *testing.T) {
	tests := []struct {
		tx                 []byte
		scriptCode         []byte
		expPreimage        []byte
		expSigHash         []byte
		consensusVersionID [4]byte
	}{
		{ // Test vector 3 from https://github.com/zcash/zips/blob/main/zip-0243.rst
			tx: mustDecodeHex("0400008085202f8901a8c685478265f4c14dada651969c4" +
				"5a65e1aeb8cd6791f2f5bb6a1d9952104d9010000006b483045022100a61e5d557568c" +
				"2ddc1d9b03a7173c6ce7c996c4daecab007ac8f34bee01e6b9702204d38fdc0bcf2728" +
				"a69fde78462a10fb45a9baa27873e6a5fc45fb5c76764202a01210365ffea3efa39089" +
				"18a8b8627724af852fc9b86d7375b103ab0543cf418bcaa7ffeffffff02005a6202000" +
				"000001976a9148132712c3ff19f3a151234616777420a6d7ef22688ac8b95980000000" +
				"0001976a9145453e4698f02a38abdaa521cd1ff2dee6fac187188ac29b0040048b0040" +
				"00000000000000000000000"),
			scriptCode: mustDecodeHex("76a914507173527b4c3318a2aecd793bf1cfed705950cf88ac"),
			expPreimage: mustDecodeHex("0400008085202f89fae31b8dec7b0b77e2c8d6b" +
				"6eb0e7e4e55abc6574c26dd44464d9408a8e33f116c80d37f12d89b6f17ff198723e7d" +
				"b1247c4811d1a695d74d930f99e98418790d2b04118469b7810a0d1cc59568320aad25" +
				"a84f407ecac40b4f605a4e686845400000000000000000000000000000000000000000" +
				"0000000000000000000000000000000000000000000000000000000000000000000000" +
				"0000000000000000000000000000000000000000000000000000000000000000000000" +
				"0000000000029b0040048b00400000000000000000001000000a8c685478265f4c14da" +
				"da651969c45a65e1aeb8cd6791f2f5bb6a1d9952104d9010000001976a914507173527" +
				"b4c3318a2aecd793bf1cfed705950cf88ac80f0fa0200000000feffffff"),
			expSigHash:         mustDecodeHex("f3148f80dfab5e573d5edfe7a850f5fd39234f80b5429d3a57edcc11e34c585b"),
			consensusVersionID: dexzec.ConsensusBranchSapling,
		},
	}

	for _, tt := range tests {
		tx, err := dexzec.DeserializeTx(tt.tx)
		if err != nil {
			t.Fatalf("DeserializeTx error: %v", err)
		}

		cache, err := NewTxSigHashes(tx)
		if err != nil {
			t.Fatalf("NewTxSigHashes error: %v", err)
		}

		amtB, _ := hex.DecodeString("80f0fa0200000000")
		amt := binary.LittleEndian.Uint64(amtB)

		pimg, sigHash, err := signatureHash(tt.scriptCode, cache, txscript.SigHashAll, tx, 0, int64(amt), tt.consensusVersionID)
		if err != nil {
			t.Fatalf("blake2bSignatureHash error: %v", err)
		}

		if !bytes.Equal(pimg, tt.expPreimage) {
			fmt.Printf("expected preimage: %s \n", hex.EncodeToString(tt.expPreimage))
			fmt.Printf("calculated preimage: %s \n", hex.EncodeToString(pimg))
			t.Fatalf("wrong preimage")
		}

		// Sighash from test vector will not be correct because of preimage error.

		if !bytes.Equal(sigHash, tt.expSigHash) {
			fmt.Printf("expected sighash: %s \n", hex.EncodeToString(tt.expSigHash))
			fmt.Printf("calculated sighash: %s \n", hex.EncodeToString(sigHash))
			t.Fatalf("wrong sighash")
		}
	}

}

func mustDecodeHex(hx string) []byte {
	b, err := hex.DecodeString(hx)
	if err != nil {
		panic("mustDecodeHex: " + err.Error())
	}
	return b
}
