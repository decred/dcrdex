// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Connects to one Electrum-Firo testnet server and one Electrum-Firo
// mainnet server and tests a subset of electrum-firo commands.
//
// See also:
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html
//
// To also run on Regtest network:
//
// Chain: 					dex/testing/firo/harness.sh
// ElectrumX-Firo server: 	dex/testing/firo/electrumx.sh
// export REGTEST=1

package firo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/electrum"

	"github.com/davecgh/go-spew/spew"
	"github.com/gcash/bchd/wire"
)

type electrum_network string

var regtest electrum_network = "regtest"
var testnet electrum_network = "testnet"
var mainnet electrum_network = "mainnet"

func TestServerConn(t *testing.T) {
	var serverAddr string
	var genesis string
	var randomTx string
	var blockHeight uint32
	var blockCount uint32

	if os.Getenv("REGTEST") != "" {
		serverAddr = "127.0.0.1:50002" // electrumx-firo regtest
		genesis = "a42b98f04cc2916e8adfb5d9db8a2227c4629bc205748ed2f33180b636ee885b"
		randomTx = "7e3d477aaac3534da5624aecf82583a1381508bf672ec18dfc0b9e7b7b0e2dfe"
		blockHeight = 330
		blockCount = 5
		runOneNet(t, regtest, serverAddr, genesis, randomTx, blockHeight, blockCount)
	}

	serverAddr = "95.179.164.13:51002"
	genesis = "aa22adcc12becaf436027ffe62a8fb21b234c58c23865291e5dc52cf53f64fca"
	randomTx = "af5bc24f8f995ff0439af0c9f3ba607995f74a84d8cc06e4a6ba7f3c7692a41a"
	blockHeight = 105793
	blockCount = 3
	runOneNet(t, testnet, serverAddr, genesis, randomTx, blockHeight, blockCount)

	serverAddr = "electrumx.firo.org:50002"
	genesis = "4381deb85b1b2c9843c222944b616d997516dcbd6a964e1eaf0def0830695233"
	randomTx = "3cbf438c07938a5da6293f3a9be9b786e01c14b459d56cd595dcd534db128ed3"
	blockHeight = 676700
	blockCount = 3
	runOneNet(t, mainnet, serverAddr, genesis, randomTx, blockHeight, blockCount)
}

func runOneNet(t *testing.T, electrumNetwork electrum_network,
	addr, genesis, randomTx string, blockHeight, blockCount uint32) {

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	rootCAs, _ := x509.SystemCertPool()
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		RootCAs:            rootCAs,
		MinVersion:         tls.VersionTLS12, // works ok
		ServerName:         host,
	}

	opts := &electrum.ConnectOpts{
		TLSConfig:   tlsConfig,
		DebugLogger: electrum.StdoutPrinter,
	}

	sc, err := electrum.ConnectServer(ctx, addr, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(sc.Proto())

	fmt.Printf("\n\n ** Connected to %s **\n\n", electrumNetwork)

	banner, err := sc.Banner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(banner)

	feats, err := sc.Features(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(feats)

	if feats.Genesis != genesis {
		t.Fatalf("wrong genesis hash for Firo-Electrum on %s: %v",
			feats.Genesis, electrumNetwork)
	}
	t.Log("Genesis correct")

	// All tx hashes will change on each regtest startup. So use
	// listtransactions or listunspent on the harness to get a
	// new random tx.
	txres, err := sc.GetTransaction(ctx, randomTx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(txres)

	// Do not make block count too big or electrumX may throttle response
	// as an anti ddos measure
	hdrsRes, err := sc.BlockHeaders(ctx, blockHeight, blockCount)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(hdrsRes)

	hdrReader := hex.NewDecoder(strings.NewReader(hdrsRes.HexConcat))
	var lastHdr *wire.BlockHeader
	timestamps := make([]int64, 0, hdrsRes.Count)
	for i := uint32(0); i < hdrsRes.Count; i++ {
		hdr := &wire.BlockHeader{}
		err = hdr.Deserialize(hdrReader)
		if err != nil {
			if i > 0 {
				t.Fatalf("Failed to deserialize header for block %d: %v",
					blockHeight+i, err)
				break // we have at least one time stamp, work with it
			}
			t.Fatal(err)
		}
		timestamps = append(timestamps, hdr.Timestamp.Unix())
		if i == blockCount-1 && blockCount == hdrsRes.Count {
			lastHdr = hdr
		}
	}
	spew.Dump(lastHdr)

	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	medianTimestamp := timestamps[len(timestamps)/2]
	t.Logf("Median time for block %d: %v", blockHeight+hdrsRes.Count-1, time.Unix(medianTimestamp, 0))

	hdrRes, _, _ := sc.SubscribeHeaders(ctx)
	spew.Dump(hdrRes)

	height := uint32(blockHeight)
out:
	for {
		select {
		case <-ctx.Done():
			break out
		case <-time.After(5 * time.Second):
			hdr, err := sc.BlockHeader(ctx, height)
			if err != nil {
				t.Log(err)
				break out
			}
			t.Log(hdr)
		}
		height++
	}
	sc.Shutdown()

	<-sc.Done()
}
