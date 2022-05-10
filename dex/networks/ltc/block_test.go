// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package ltc

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

var (
	// Testnet4 block 1821752 is pre-MWEB activation, 3 txns.
	// But version 20000000.
	//go:embed testnet4Block1821752.dat
	block1821752 []byte

	// Block 2215584 is the first with MW txns, a peg-in with witness version 9
	// script, an integ tx with witness version 8 script, block version 20000000
	// (with a MWEB), and 5 txns.
	// 7e35fabe7b3c694ebeb0368a1a1c31e83962f3c5b4cc8dcede3ae94ed3deb306
	//go:embed testnet4Block2215584.dat
	block2215584 []byte

	// Block 2321749 is version 20000000 with a MWEB, 4 txns, the last one being
	// an integration / hogex txn that fails to decode.
	// 57929846db4a92d937eb596354d10949e33c815ee45df0c9b3bbdfb283e15bcd
	//go:embed testnet4Block2321749.dat
	block2321749 []byte

	// Block 2319633 is version 20000000 with a MWEB, 2 txns, one coinbase and
	// one integration.
	// e9fe2c6496aedefa8bf6529bdc5c1f9fd4af565ca4c98cab73e3a1f616fb3502
	//go:embed testnet4Block2319633.dat
	block2319633 []byte

	//go:embed block2215586testnet4.dat
	block2215586 []byte
)

func TestDeserializeBlockBytes(t *testing.T) {
	tests := []struct {
		name       string
		blk        []byte
		wantHash   string
		wantNumTx  int
		wantLastTx string
	}{
		{
			"block 1821752 pre-MWEB activation",
			block1821752,
			"ece484c02e84e4b1c551fbbdde3045e9096c970fbd3e31f2586b68d50dad6b24",
			3,
			"cb4d9d2d7ab7211ddf030a667d320fe499c849623e9d4a130e1901391e9d4947",
		},
		{
			"block 2215584 MWEB",
			block2215584,
			"7e35fabe7b3c694ebeb0368a1a1c31e83962f3c5b4cc8dcede3ae94ed3deb306",
			5,
			"4c86658e64861c2f2b7fbbf26bbf7a6640ae3824d24293a009ad5ea1e8ab4418",
		},
		{
			"block 2215586 MWEB",
			block2215586,
			"3000cc2076a568a8eb5f56a06112a57264446e2c7d2cca28cdc85d91820dfa17",
			37,
			"3a7299f5e6ee9975bdcc2d754ff5de3312d92db177b55c68753a1cdf9ce63a7c",
		},
		{
			"block 2321749 MWEB",
			block2321749,
			"57929846db4a92d937eb596354d10949e33c815ee45df0c9b3bbdfb283e15bcd",
			4,
			"1bad5e78b145947d32eeeb1d24295891ba03359508d5f09921bada3be66bbe17",
		},
		{
			"block 2319633 MWEB",
			block2319633,
			"e9fe2c6496aedefa8bf6529bdc5c1f9fd4af565ca4c98cab73e3a1f616fb3502",
			2,
			"3cd43df64e9382040eff0bf54ba1c2389d5111eb5ab0968ab7af67e3c30cac04",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgBlk, err := DeserializeBlockBytes(tt.blk)
			if err != nil {
				t.Fatal(err)
			}

			blkHash := msgBlk.BlockHash()
			if blkHash.String() != tt.wantHash {
				t.Errorf("Wanted block hash %v, got %v", tt.wantHash, blkHash)
			}

			if len(msgBlk.Transactions) != tt.wantNumTx {
				t.Errorf("Wanted %d txns, found %d", tt.wantNumTx, len(msgBlk.Transactions))
			}

			lastTxHash := msgBlk.Transactions[len(msgBlk.Transactions)-1].TxHash()
			if lastTxHash.String() != tt.wantLastTx {
				t.Errorf("Wanted last tx hash %v, got %v", tt.wantLastTx, lastTxHash)
			}
		})
	}
}

func TestDecodeTransaction(t *testing.T) {
	// pegin 84b7ea499d5650cc220afac8b972527cef10ed402da5a5b000f994199044f450
	// output has witness ver 9
	peginTx, _ := hex.DecodeString("02000000000101d4cfc0df00ced17d9f05460b20be3f8b362e213fbfb22514c5a7e566bd9bd6a10000000000feffffff012a" +
		"c07b050000000022592056961cee5a05f60a40f4730bef10786b0b54e42e460ee9102b2756b69efe37210247304402205f44" +
		"1fa690d41056ab5c635fd8f96427cdb7825ba5cdb4a977758fb09ac2bb2e02204c068a057469fd22176ec45bd1bc780bb6db" +
		"34e299a601ae735b8f3db5abd4b10121029e94825ddd9ed088ca2c270b3ccb5cb5573a8204215912cac1f68f288190270c9f" +
		"ce2100")
	msgTx := &wire.MsgTx{}
	err := msgTx.Deserialize(bytes.NewReader(peginTx))
	if err != nil {
		t.Fatal(err)
	}
	if msgTx.TxOut[0].PkScript[0] != txscript.OP_9 {
		t.Errorf("did not get witness version 9")
	}

	// integ 3cd43df64e9382040eff0bf54ba1c2389d5111eb5ab0968ab7af67e3c30cac04
	// output has witness ver 8
	// tx flag has bit 8 set, indicating hogex
	integTx, _ := hex.DecodeString("02000000000801bba0a561a904465fe2215f77822e04045ca01491c358bb16" +
		"89200d71ada3836b0000000000ffffffff01ec069e659a320000225820399cda16" +
		"3b49fdac1f669f69e63b56756d3bc6f2523eb10615710154959cc1360000000000")
	msgTx = &wire.MsgTx{}
	err = msgTx.Deserialize(bytes.NewReader(integTx))
	if err == nil { // witness tx but flag byte is 08
		t.Fatal("expected error") //fails because of flag
	}
}
