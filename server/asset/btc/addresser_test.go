// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"decred.org/dcrdex/server/asset"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil/hdkeychain"
)

// Dummy keyIndexer since there is no DB or need to coordinate withother
// goroutines.
type keyIndexer struct {
	idx map[string]uint32
}

func newKeyIndexer() *keyIndexer {
	return &keyIndexer{make(map[string]uint32)}
}

func (ki *keyIndexer) KeyIndex(xpub string) (uint32, error) {
	return ki.idx[xpub], nil
}

func (ki *keyIndexer) SetKeyIndex(idx uint32, xpub string) error {
	ki.idx[xpub] = idx
	return nil
}

var _ asset.KeyIndexer = (*keyIndexer)(nil)

func TestAddressDeriver(t *testing.T) {
	var x uint32 = 0x0488b21e
	x2 := binary.BigEndian.Uint32([]byte{0x04, 0x88, 0xb2, 0x1e})
	if x != x2 {
		t.Fatalf("%d != %d", x, x2)
	}

	params := &chaincfg.MainNetParams
	// zpub := "zpub6nKbQwE3qUJUhehpUf2sETXY1v2mm1VM5ou4xdM3PPMudQfuo8XC3DPM3esiGCRhT1JBubsydS2UFM2eX8Uh2tSx6FhDY3GKQGESmbARSmR"
	// xpubWant := "xpub68f4obtDY7DX14KaowTcpHLXfyjssmWMFardPqZGdNc9XD3THpC4o6551ExYGP7rdj4aQegri7KNUmoX5jefSR5kMaJNNDdLrp79zQBSGaK"
	zpub := "zpub6r6v49qBjybMzg17LsqFDYDG3oYssB5GkQ4BdQtjCDkSeBPAgXxHBYXgNGy3LxfHwf5JwvkJTBqHHjkXKdwheZdtLKdcXCU1SMkzryt1evp"
	xpubWant := "xpub6CSPSpVMScWQJ5csgAFzoN2FhsFyyw6GvB1k4d6xSCzgXykiBDd9wRDQKs3sM9MT8NqhSyZBXs8BXAXPtF7g46GgbeEmMNq2tudi5k68MjJ"
	// params := &chaincfg.TestNet3Params
	// zpub := "vpub5UpDTWU6Nuj9uuU5TZnxjS97SAjKwY4UiLvvnYNRC6vmvbDa5toZRG1BkqCnLSRmNYuLuqLDPtdDq6YvELoMUSPjVSFTsX1H42kabkJDwWD"
	// xpubWant := "tpubD8rNb5dcPYmZtJ3AaUMJMXfV7E7spAYY6CZfhtDLJ2SRn5WjfRymopzJ3HCjrkgqyjfs2N3CrKANHrGf4bUcxMDEHiPcisJVBSUH8wNhdoA"
	// params := &chaincfg.RegressionNetParams
	// zpub := "vpub5YqS5eyUTyzmeuQiyW96jgBcRiCNSEMapKkUcF6L8kWSrma29AXi9Zr4e5rxCjk4eZnP8sTpeXNJDdFr6kMGjh8ZqraXpYyyq1XfaaPwt32"
	// xpubWant := "tpubDCsbDE8zUd3BdHyp6QhSMmhz6mavJrqeCBPDXawFEg26iFsBihhvY8qAvXruj419FkYuFQAp6wuSgNyaw12YDbx4e8igfuHBxRFN7kRdZbM"

	net := params.Net

	// P2PWKH extended key prefix is: zpub on mainnet, vpub on regnet/testnet3
	var xpubPrefix, zpubPrefix string
	switch net {
	case wire.MainNet:
		xpubPrefix = "xpub"
		zpubPrefix = "zpub"
	case wire.TestNet, wire.TestNet3:
		xpubPrefix = "tpub"
		zpubPrefix = "vpub"
	}

	key, err := hdkeychain.NewKeyFromString(zpub)
	if err != nil {
		t.Fatalf("error parsing master pubkey: %v", err)
	}
	if key.IsPrivate() {
		t.Fatal("that's a private key")
	}
	if key.IsForNet(params) {
		t.Fatal("btcsuite's hdkeychain recognized our zpub!")
	}

	vers := key.Version()
	prefix := pubVerPrefix(vers, net)
	if prefix == "" {
		t.Fatalf("invalid prefix for version bytes %x", vers)
	}
	if prefix != zpubPrefix {
		t.Fatalf("unexpected prefix %q for version bytes %x", prefix, vers)
	}
	if v := pubPrefixVer(prefix, net); !bytes.Equal(v, vers) {
		t.Fatal("inconsistent version")
	}

	key.SetNet(params) // params uses xpub version bytes

	vers = key.Version()
	prefix = pubVerPrefix(vers, net)
	if prefix == "" {
		t.Fatalf("bad prefix for version bytes %x", vers)
	}
	if prefix != xpubPrefix {
		t.Fatalf("unexpected prefix %q for version bytes %x", prefix, vers)
	}
	if v := pubPrefixVer(prefix, net); !bytes.Equal(v, vers) {
		t.Fatal("inconsistent version")
	}

	xpub := key.String()
	if xpub != xpubWant {
		t.Fatal(xpub, " != ", xpubWant)
	}

	ki := newKeyIndexer()

	// NewAddressDeriver should not like the xpub, and suggest the zpub.
	_, _, err = NewAddressDeriver(xpub, ki, params)
	if err == nil {
		t.Fatal("no error from NewAddressDeriver for an xpub prefixed key")
	}
	if !strings.Contains(err.Error(), zpub) {
		t.Fatalf("NewAddressDeriver should have suggested the corresponding zpub, said: %q", err.Error())
	}

	// Should work with the zpub.
	ad, _, err := NewAddressDeriver(zpub, ki, params)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		addr, err := ad.NextAddress()
		if err != nil {
			t.Fatal(err)
		}
		t.Log(addr)
	}

	if ad.next != 10 {
		t.Fatal(ad.next)
	}
}
