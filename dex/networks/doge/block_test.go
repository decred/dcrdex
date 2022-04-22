// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package doge

import (
	_ "embed"
	"testing"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

var (
	// Block 299983 is version 2 with no AuxPOW section, and two txns.
	//go:embed block299983.dat
	block299983 []byte

	// Block 371027 is version 6422530 with and AuxPOW section, and five txns.
	//go:embed block371027.dat
	block371027 []byte

	// Block 371469 is version 6422786 with and AuxPOW section, and three txns.
	//go:embed block371469.dat
	block371469 []byte

	// Block 4193723 is version 6422788 with an AuxPOW section, and two txns.
	//go:embed block4193723.dat
	block4193723 []byte
)

func TestDeserializeBlock(t *testing.T) {
	tests := []struct {
		name       string
		blk        []byte
		wantHash   string
		wantNumTx  int
		wantLastTx string
	}{
		{
			"block 299983 ver 2 no AuxPOW",
			block299983,
			"1cf943b386ffb79595ef7587deef02419e9d0af6a0e3a1e826d8f34f89c678db",
			2,
			"51a9edf6eccf76c09f7ee150634d9425498fbf70108b1c05d39b71f32574ffe7",
		},
		{
			"block 371027 ver 6422530 AuxPOW",
			block371027,
			"f498b4d866dd602749fb5bb2765333d09ae9d807a0c72434994d617ad38a4197",
			5,
			"9813c190f5733e12030f2bc0b6581ebfbba2a039ceb8432b4d73a159e90f08ea",
		},
		{
			"block 371469 ver 6422786 AuxPOW",
			block371469,
			"99c426b4c1b3f6c62f7d6fd1ccf8554a046b0156eef1ea2fe98daf53a3f7f184",
			3,
			"446ef44f9ec21695f481af8b60a3c84560faca40c9cc82a460eee87b9ea8aba1",
		},
		{
			"block 4193723 ver 6422788 AuxPOW",
			block4193723,
			"7395d83c7a7acdaa69b08af0c3bc1b8f57a1102a5f4090bee9380985833c3682",
			2,
			"31f6c10a2afcd2759de7cd9cc1247e6994e62bda3f202bd87cff1cb14fab5934",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgBlk, err := DeserializeBlock(tt.blk)
			if err != nil {
				t.Fatal(err)
			}

			wantHash, err := chainhash.NewHashFromStr(tt.wantHash)
			if err != nil {
				t.Fatal(err)
			}

			blkHash := msgBlk.BlockHash()
			if blkHash != *wantHash {
				t.Errorf("Wanted block hash %v, got %v", wantHash, blkHash)
			}

			if len(msgBlk.Transactions) != tt.wantNumTx {
				t.Errorf("Wanted %d txns, found %d", tt.wantNumTx, len(msgBlk.Transactions))
			}

			wantLastTx, err := chainhash.NewHashFromStr(tt.wantLastTx)
			if err != nil {
				t.Fatal(err)
			}

			lastTxHash := msgBlk.Transactions[len(msgBlk.Transactions)-1].TxHash()
			if lastTxHash != *wantLastTx {
				t.Errorf("Wanted last block hash %v, got %v", wantLastTx, lastTxHash)
			}
		})
	}
}
