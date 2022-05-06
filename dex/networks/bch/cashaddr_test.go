// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bch

import (
	"testing"

	"decred.org/dcrdex/dex/encode"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
	"github.com/gcash/bchutil"
)

func TestCashAddr(t *testing.T) {
	lowB := make([]byte, 20)
	highB := make([]byte, 20)
	for i := range highB {
		highB[i] = 255
	}

	lowPubKey := make([]byte, 33)
	lowPubKey[0] = 2
	lowPubKey[32] = 1

	checkHash := func(net *chaincfg.Params, h []byte) {
		t.Helper()

		switch len(h) {
		case 20:
			var bchAddr bchutil.Address
			bchAddr, err := bchutil.NewAddressPubKeyHash(h, convertParams(net))
			if err != nil {
				t.Fatalf("bchutil.AddressScriptHash error: %v", err)
			}
			testRoundTripFromBCH(t, withPrefix(bchAddr, net), net)

			bchAddr, err = bchutil.NewAddressScriptHashFromHash(h, convertParams(net))
			if err != nil {
				t.Fatalf("bchutil.AddressScriptHash error: %v", err)
			}
			testRoundTripFromBCH(t, withPrefix(bchAddr, net), net)

			var btcAddr btcutil.Address
			btcAddr, err = btcutil.NewAddressPubKeyHash(h, net)
			if err != nil {
				t.Fatalf("btcutil.NewAddressPubkeyHash error: %v", err)
			}

			testRoundTripFromBTC(t, btcAddr.String(), net)

			btcAddr, err = btcutil.NewAddressScriptHashFromHash(h, net)
			if err != nil {
				t.Fatalf("btcutil.NewAddressPubkeyHash error: %v", err)
			}
			testRoundTripFromBTC(t, btcAddr.String(), net)

		case 33, 65: // See btcec.PubKeyBytesLen(Un)Compressed
			var bchAddr bchutil.Address
			bchAddr, err := bchutil.NewAddressPubKey(h, convertParams(net))
			if err != nil {
				t.Fatalf("bchutil.NewAddressPubKey error: %v", err)
			}
			testRoundTripFromBCH(t, withPrefix(bchAddr, net), net)

			var btcAddr btcutil.Address
			btcAddr, err = btcutil.NewAddressPubKey(h, net)
			if err != nil {
				t.Fatalf("btcutil.NewAddressPubKey error: %v", err)
			}

			testRoundTripFromBTC(t, btcAddr.String(), net)

		default:
			t.Fatalf("unknown address data length %d", len(h))
		}

	}

	nets := []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams}
	for _, net := range nets {
		// Check the lowest and highest possible hashes.
		checkHash(net, lowB)
		checkHash(net, highB)
		// Check a bunch of random addresses.
		for i := 0; i < 1000; i++ {
			checkHash(net, encode.RandomBytes(20))
		}
		// Check Pubkey addresses. These just encode to the hex encoding of the
		// serialized pubkey for both bch and btc, so there's little that can go
		// wrong.
		checkHash(net, lowPubKey)
		for i := 0; i < 100; i++ {
			_, pubKey := btcec.PrivKeyFromBytes(btcec.S256(), encode.RandomBytes(33))
			checkHash(net, pubKey.SerializeUncompressed())
			checkHash(net, pubKey.SerializeCompressed())
		}

	}
}

func testRoundTripFromBCH(t *testing.T, bchAddrStr string, net *chaincfg.Params) {
	t.Helper()

	btcAddr, err := DecodeCashAddress(bchAddrStr, net)
	if err != nil {
		t.Fatalf("DecodeCashAddress error: %v", err)
	}

	reAddr, err := RecodeCashAddress(btcAddr.String(), net)
	if err != nil {
		t.Fatalf("RecodeCashAddr error: %v", err)
	}

	if reAddr != bchAddrStr {
		t.Fatalf("Recoded address mismatch: %s != %s", reAddr, bchAddrStr)
	}
}

func testRoundTripFromBTC(t *testing.T, btcAddrStr string, net *chaincfg.Params) {
	t.Helper()

	bchAddrStr, err := RecodeCashAddress(btcAddrStr, net)
	if err != nil {
		t.Fatalf("RecodeCashAddr error: %v", err)
	}

	btcAddr, err := DecodeCashAddress(bchAddrStr, net)
	if err != nil {
		t.Fatalf("DecodeCashAddress error: %v", err)
	}

	reAddr := btcAddr.String()
	if reAddr != btcAddrStr {
		t.Fatalf("Decoded address mismatch: %s != %s", reAddr, btcAddrStr)
	}
}
