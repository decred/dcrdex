// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build electrumlive

package electrum

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gcash/bchd/wire"
)

func TestServerConn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	addr := "electrum.ltc.xurious.com:51002"
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	rootCAs, _ := x509.SystemCertPool()
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		RootCAs:            rootCAs,
		// MinVersion:         tls.VersionTLS12,
		ServerName: host,
	}
	opts := &ConnectOpts{
		TLSConfig:   tlsConfig,
		DebugLogger: StdoutPrinter,
	}
	sc, err := ConnectServer(ctx, addr, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(sc.proto)

	banner, err := sc.Banner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(banner)

	feats, err := sc.Features(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(feats)
	if feats.Genesis != "4966625a4b2851d9fdee139e56211a0d88575f59ed816ff5e6a63deb4e3e29a0" { // Litecoin Testnet genesis block hash
		t.Fatalf("wrong genesis hash: %v", feats.Genesis)
	}

	txres, err := sc.GetTransaction(ctx, "f4b5fca9e2fa3abfe22ea82eece8e28cdcf3e03bb11c63c327b5c719bfa2df6f")
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(txres)

	var startHeight, count uint32 = 2319765, 11
	hdrsRes, err := sc.BlockHeaders(ctx, startHeight, count) // [2319765, 2319776)
	if err != nil {
		t.Fatal(err)
	}

	hdrReader := hex.NewDecoder(strings.NewReader(hdrsRes.HexConcat))
	var lastHdr *wire.BlockHeader
	timestamps := make([]int64, 0, hdrsRes.Count)
	for i := uint32(0); i < hdrsRes.Count; i++ {
		hdr := &wire.BlockHeader{}
		err = hdr.Deserialize(hdrReader)
		if err != nil {
			if i > 0 {
				t.Fatalf("Failed to deserialize header for block %d: %v",
					startHeight+i, err)
				break // we have at least one time stamp, work with it
			}
			t.Fatal(err)
		}
		timestamps = append(timestamps, hdr.Timestamp.Unix())
		if i == count-1 && count == hdrsRes.Count {
			lastHdr = hdr
		}
	}
	spew.Dump(lastHdr)

	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	medianTimestamp := timestamps[len(timestamps)/2]
	t.Logf("Median time for block %d: %v", startHeight+hdrsRes.Count-1, time.Unix(medianTimestamp, 0))

	/*
			hdrRes, err := sc.SubscribeHeaders(ctx)
			spew.Dump(hdrRes)

			height := uint32(60000)
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
	*/
	sc.Shutdown()

	<-sc.Done()
}
