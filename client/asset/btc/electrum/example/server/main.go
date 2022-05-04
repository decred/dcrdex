// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"decred.org/dcrdex/client/asset/btc/electrum"
	"github.com/btcsuite/btcd/wire"
	"github.com/davecgh/go-spew/spew"
)

// This example app requires a tor proxy listening on 127.0.0.1:9050.

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		fmt.Println("Shutting down...")
		cancel()
	}()

	err := run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func startConn(ctx context.Context, addr, torProxy string, ssl bool) (*electrum.ServerConn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	opts := &electrum.ConnectOpts{
		TorProxy:    torProxy,
		DebugLogger: electrum.StdoutPrinter,
	}
	if ssl {
		rootCAs, _ := x509.SystemCertPool()
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: true, // they are almost all self-signed, most without fqdn
			RootCAs:            rootCAs,
			// MinVersion:         tls.VersionTLS12,
			ServerName: host,
		}
	}
	sc, err := electrum.ConnectServer(ctx, addr, opts)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Connected to Electrum server %v, protocol version %v\n", addr, sc.Proto())

	banner, err := sc.Banner(ctx)
	if err != nil {
		sc.Shutdown()
		return nil, err
	}
	fmt.Println(banner)

	txInfo, err := sc.GetTransaction(ctx, "6356ad5ac46daabfd0a0b607e44ce78b77ad0fd17a3376aaca73ef230303afbf")
	if err != nil {
		sc.Shutdown()
		return nil, err
	}
	spew.Dump(txInfo)

	peers, err := sc.Peers(ctx)
	if err != nil {
		sc.Shutdown()
		return nil, err
	}
	for i, peer := range peers {
		fmt.Printf(" - Peer %3d: %s (%s), features: %v\n", i, peer.Addr, peer.Host, peer.Feats)
	}
	sslPeers, tcpOnionPeers := electrum.SSLPeerAddrs(peers, true)
	fmt.Printf("%d SSL peers / %d no-SSL onion peers\n", len(sslPeers), len(tcpOnionPeers))

	hdrRes, hdrs, err := sc.SubscribeHeaders(ctx)
	if err != nil {
		sc.Shutdown()
		return nil, err
	}
	wireHdr := &wire.BlockHeader{}
	err = wireHdr.Deserialize(hex.NewDecoder(strings.NewReader(hdrRes.Hex)))
	if err != nil {
		sc.Shutdown()
		return nil, err
	}
	fmt.Printf("Best block %v (%d)\n", wireHdr.BlockHash(), hdrRes.Height)

	go func() {
		for hdr := range hdrs {
			wireHdr := &wire.BlockHeader{}
			err := wireHdr.Deserialize(hex.NewDecoder(strings.NewReader(hdr.Hex)))
			if err != nil {
				fmt.Printf("Failed to deserialize block header for block at height %d: %v", hdr.Height, err)
				continue
			}
			fmt.Printf("New best block %v (%d)\n", wireHdr.BlockHash(), hdr.Height)
		}

		sc.Shutdown() // should be shut down already
	}()

	return sc, nil
}

func run(ctx context.Context) error {
	addrs := map[string]bool{ // addr => ssl
		// "127.0.0.1:51002": true,
		"electrum.ltc.xurious.com:51002": true,
		"electrum-ltc.bysh.me:51002":     true,
		"electrum3.hodlister.co:50002":   true,
		"node.degga.net:50002":           true,
		"kittycp2gatrqhlwpmbczk5rblw62enrpo2rzwtkfrrr27hq435d4vid.onion:50001": false,
		"kittycp2gatrqhlwpmbczk5rblw62enrpo2rzwtkfrrr27hq435d4vid.onion:50002": true,
		"hodlers.beer:50002":          true,
		"electrum.neocrypto.io:50001": false,
	}
	for {
		var addr string
		var ssl bool
		for addr, ssl = range addrs {
			break
		}
		sc, err := startConn(ctx, addr, "127.0.0.1:9050", ssl)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Printf("%v, %T\n", err, err)
			time.Sleep(10 * time.Second)
			continue
		}

		<-sc.Done()

		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
