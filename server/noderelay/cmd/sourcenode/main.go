// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// sourcenode is the client for a NodeRelay. A NodeRelay is a remote server that
// can request data from the API of a local service, presumably running on a
// private machine which is not accessible from any static IP address or domain.
// In this inverted consumer-provider model, the provider connects to the
// consumer, and then accepts data requests over WebSockets, routing them to the
// local service and responding with the results.

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/noderelay"
)

var log = dex.StdOutLogger("NODESRC", dex.LevelDebug)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() (err error) {
	var (
		// port is a required argument.
		port string
		// optional, but required for e.g. dcrd, which uses an encrypted
		// connection.
		localNodeCert string

		// User can provide NodeRelay server configuration in one of two ways.
		// 1) nexusAddr + relayID + certPath
		nexusAddr string
		relayID   string
		certPath  string // NodeRelay's TLS certificate
		// 2) relayfile, provided by NodeRelay operator
		relayFilepath string
	)

	// required
	flag.StringVar(&port, "port", "", "The port that the local service is listening on")
	// optional. (dcrd needs by default)
	flag.StringVar(&localNodeCert, "localcert", "", "The path to a TLS certificate for the local service") // optional
	// connect either this way...
	flag.StringVar(&relayFilepath, "relayfile", "", "The path to a relay file (provided by the server)")
	// or connect with these parameters
	flag.StringVar(&relayID, "relayid", "", "The relay ID")
	flag.StringVar(&certPath, "certpath", "", "The path to a TLS certificate for the server")
	flag.StringVar(&nexusAddr, "addr", "", "The address to the server")

	flag.Parse()

	if port == "" {
		return errors.New("no local port provided")
	}

	var certB []byte
	if relayFilepath != "" {
		b, err := os.ReadFile(relayFilepath)
		if err != nil {
			return fmt.Errorf("error reading relay file @ %q: %w", relayFilepath, err)
		}
		var relayFile noderelay.RelayFile
		if err := json.Unmarshal(b, &relayFile); err != nil {
			return fmt.Errorf("error parsing relay file: %w", err)
		}
		relayID = relayFile.RelayID
		certB = relayFile.Cert
		nexusAddr = relayFile.Addr
	} else {
		if certPath == "" {
			return errors.New("specify a --certpath")
		}
		var err error
		certB, err = os.ReadFile(certPath)
		if err != nil {
			return fmt.Errorf("error reading server certificate at %q: %v", certPath, err)
		}
	}

	if port == "" {
		return errors.New("specify the --port that the local service is listening on")
	}
	if relayID == "" {
		return errors.New("specify a --relayid")
	}
	if nexusAddr == "" {
		return errors.New("specify a --addr for the server")
	}

	if len(certB) == 0 {
		return errors.New("no server TLS certificate provided")
	}

	registration, err := json.Marshal(&noderelay.RelayedMessage{
		MessageID: 0, // must be zero for registration.
		Body:      []byte(relayID),
	})
	if err != nil {
		return fmt.Errorf("error json-encoding registration message: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		fmt.Println("Shutting down...")
		cancel()
	}()

	// Keep track of some basic stats.
	var stats struct {
		requests uint32
		errors   uint32
		received uint64
		sent     uint64
	}

	// Periodically print the node usage statistics.
	go func() {
		start := time.Now()
		for {
			select {
			case <-time.After(time.Minute * 10):
			case <-ctx.Done():
				return
			}
			log.Infof("%d requests, %.4g MB received, %.4g MB sent, %d errors in %s",
				atomic.LoadUint32(&stats.requests), float64(atomic.LoadUint64(&stats.received))/1e6,
				float64(atomic.LoadUint64(&stats.sent))/1e6, atomic.LoadUint32(&stats.errors),
				time.Since(start))
		}
	}()

	localNodeURL := "http://127.0.0.1" + ":" + port
	httpClient := http.DefaultClient
	if localNodeCert != "" {
		localNodeURL = "https://127.0.0.1" + ":" + port
		pem, err := os.ReadFile(localNodeCert)
		if err != nil {
			return err
		}

		uri, err := url.Parse(localNodeURL)
		if err != nil {
			return fmt.Errorf("error parsing URL: %v", err)
		}

		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			return fmt.Errorf("invalid certificate file: %v", localNodeCert)
		}
		tlsConfig := &tls.Config{
			RootCAs:    pool,
			ServerName: uri.Hostname(),
		}

		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}
	}

	// Define this now, so we can create a ReconnectSync function.
	var cl comms.WsConn

	registerOrReregister := func() {
		if err := cl.SendRaw(registration); err != nil {
			log.Errorf("Error sending registration message: %v", err)
		}
	}

	cl, err = comms.NewWsConn(&comms.WsCfg{
		URL:      "wss://" + nexusAddr,
		PingWait: noderelay.PingPeriod * 2,
		Cert:     certB,
		// On a disconnect, wsConn will attempt to reconnect immediately. If
		// the first attempt is unsuccessful, it will wait 5 seconds for next
		// success, then 10, 15 ... up to a minute.
		// We'll just send the registration on reconnect.
		ReconnectSync:    registerOrReregister,
		ConnectEventFunc: func(s comms.ConnectionStatus) {},
		Logger:           dex.StdOutLogger("CL", dex.LevelDebug),
		RawHandler: func(b []byte) {
			atomic.AddUint64(&stats.received, uint64(len(b)))
			atomic.AddUint32(&stats.requests, 1)
			// Request received from server.
			var msg noderelay.RelayedMessage
			if err := json.Unmarshal(b, &msg); err != nil {
				atomic.AddUint32(&stats.errors, 1)
				log.Errorf("json unmarshal error: %v", err)
				return
			}
			// Prepare mirrored request for local service.
			ctx, cancel := context.WithTimeout(ctx, time.Second*30)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, msg.Method, localNodeURL, bytes.NewReader(msg.Body))
			if err != nil {
				atomic.AddUint32(&stats.errors, 1)
				log.Errorf("Error constructing request: %v", err)
				return
			}
			req.Header = msg.Headers
			// Send request to local service.
			resp, err := httpClient.Do(req)
			if err != nil {
				atomic.AddUint32(&stats.errors, 1)
				log.Errorf("error processing request: %v", err)
				return
			}
			// Read response from local service and encode for the node relay.
			b, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				atomic.AddUint32(&stats.errors, 1)
				log.Errorf("Error reading response: %v", err)
				return
			}
			atomic.AddUint64(&stats.sent, uint64(len(b)))
			encResp, err := json.Marshal(&noderelay.RelayedMessage{
				MessageID: msg.MessageID,
				Body:      b,
				Headers:   resp.Header,
			})
			if err != nil {
				log.Error("Error during json encoding: %v", err)
			}
			if err := cl.SendRaw(encResp); err != nil {
				log.Errorf("SendRaw error: %v", err)
			}

		},
	})

	cm := dex.NewConnectionMaster(cl)
	if err := cm.ConnectOnce(ctx); err != nil {
		return fmt.Errorf("websocketHandler client connect: %v", err)
	}

	// The default read limit is 1024, I think.
	if ws, is := cl.(interface {
		SetReadLimit(limit int64)
	}); is {
		const readLimit = 2_097_152 // 2 MiB
		ws.SetReadLimit(readLimit)
	}

	registerOrReregister()

	cm.Wait()
	return nil
}
