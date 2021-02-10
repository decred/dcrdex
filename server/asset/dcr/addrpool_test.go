package dcr

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
)

func TestChildSearch(t *testing.T) {
	feeKey := "spubVWKGn9TGzyo7M4b5xubB5UV4joZ5HBMNBmMyGvYEaoZMkSxVG4opckpmQ26E85iHg8KQxrSVTdex56biddqtXBerG9xMN8Dvb3eNQVFFwpE"
	params := chaincfg.SimNetParams()

	findAddrStr := "SsehuQp6u2rXaKDHiyPoV6PusbatnsE5o96"
	findAddr, _ := dcrutil.DecodeAddress(findAddrStr, params)
	// findIdx := uint32(4271)

	// Get the master extended public key.
	masterKey, err := hdkeychain.NewKeyFromString(feeKey, params)
	if err != nil {
		t.Fatal(err)
	}
	feeKeyBranch, err := masterKey.Child(0) // external branch
	if err != nil {
		t.Fatal(err)
	}

	pool := newPkhPool(feeKeyBranch, 20, 10_000)

	// num := uint32(10_000)
	// addrs := make(map[h160]uint32, num)

	for i := uint32(0); i < 10; i++ {
		hash, _ := pool.get()
		_, err := dcrutil.NewAddressPubKeyHash(hash[:], params, dcrec.STEcdsaSecp256k1)
		if err != nil {
			t.Fatal(err)
		}
	}

	if !pool.owns(*findAddr.Hash160()) {
		t.Error("not found")
	}

	// idx, found := addrs[*findAddr.Hash160()]
	// if !found {
	// 	t.Fatal("not found")
	// }
	// if idx != findIdx {
	// 	t.Errorf("%d != %d", idx, findIdx)
	// }
}
