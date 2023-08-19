//go:build vspd && !harness

package dcr

import (
	"encoding/json"
	"fmt"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

func TestHTTPForVSPD(t *testing.T) {
	w := &ExchangeWallet{
		network: dex.Testnet,
		log:     dex.StdOutLogger("TEST", dex.LevelInfo),
	}
	vsps, err := w.ListVSPs()
	if err != nil {
		t.Fatalf("ListVSPs (testnet) error: %v", err)
	}

	fmt.Printf("##### %d testnet VSPs fetched \n", len(vsps))

	w.network = dex.Mainnet
	vsps, err = w.ListVSPs()
	if err != nil {
		t.Fatalf("ListVSPs (mainnet) error: %v", err)
	}
	if len(vsps) == 0 {
		t.Fatalf("no mainnet VSPs listed")
	}

	fmt.Printf("##### %d mainnet VSPs fetched \n", len(vsps))

	var biggestVSP *asset.VotingServiceProvider
	for _, v := range vsps {
		if biggestVSP == nil || v.Voting > biggestVSP.Voting {
			biggestVSP = v
		}
	}

	fmt.Printf("##### VSP with the most voting is %s, with %d tickets \n", biggestVSP.URL, biggestVSP.Voting)

	vspi, err := vspInfo(biggestVSP.URL)
	if err != nil {
		t.Fatalf("vspInfo error: %v", err)
	}

	b, _ := json.MarshalIndent(vspi, "", "    ")
	fmt.Printf("##### VSP Info fetched \n----------------------\n%s\n", string(b))
}
