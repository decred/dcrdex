// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/gorilla/websocket"
)

const (
	user    = "user"
	pass    = "pass"
	addr    = "127.0.0.1:5757"
	dexAddr = "127.0.0.1:17273"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
	}
}

func run() error {
	home := dcrutil.AppDataDir("dexc", false)
	certFile := filepath.Join(home, "rpc.cert")
	cfg := &config{
		RPCUser: user,
		RPCPass: pass,
		RPCAddr: addr,
		RPCCert: certFile,
	}
	urlStr := "https://" + cfg.RPCAddr

	// Create a new HTTP client.
	client, err := newHTTPClient(cfg, urlStr)
	if err != nil {
		return err
	}

	rawParams := rpcserver.RawParams{Args: []string{"trade"}}
	msg, err := msgjson.NewRequest(1, "help", rawParams)
	b, err := json.Marshal(msg)
	fmt.Println("http request:", string(b))
	if err != nil {
		return err
	}

	// Send a one shot request over https.
	res, err := sendPostRequest(b, urlStr, cfg, client)
	if err != nil {
		return err
	}
	fmt.Println("response: ", res)

	// Create a new ws client.
	wsc, err := newWSClient(cfg)
	if err != nil {
		return err
	}
	defer wsc.Close()

	// Send a request to the websocket server.
	type marketLoad struct {
		Host  string `json:"host"`
		Base  uint32 `json:"base"`
		Quote uint32 `json:"quote"`
	}
	ml := marketLoad{
		Host:  dexAddr,
		Base:  42,
		Quote: 0,
	}
	msg, err = msgjson.NewRequest(1, "loadmarket", ml)
	b, err = json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Println("ws request:", string(b))
	err = wsc.WriteMessage(1, b)
	if err != nil {
		return err
	}

	// Wait for a notification to come.
	_, r, err := wsc.NextReader()
	if err != nil {
		return err
	}
	notification, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	fmt.Println("ws notification:", string(notification))
	return nil
}

type config struct {
	RPCUser string
	RPCPass string
	RPCAddr string
	RPCCert string
}

// newHTTPClient returns a new HTTP client
func newHTTPClient(cfg *config, urlStr string) (*http.Client, error) {
	// Configure TLS.
	pem, err := ioutil.ReadFile(cfg.RPCCert)
	if err != nil {
		return nil, err
	}

	uri, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pem); !ok {
		return nil, fmt.Errorf("invalid certificate file: %v",
			cfg.RPCCert)
	}
	tlsConfig := &tls.Config{
		RootCAs:    pool,
		ServerName: uri.Hostname(),
	}

	// Create and return the new HTTP client potentially configured with a
	// proxy and TLS.
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	return &client, nil
}

// newWSClient obtains a websocket connection.
func newWSClient(cfg *config) (*websocket.Conn, error) {
	urlStr := "wss://" + cfg.RPCAddr + "/ws"
	// Configure TLS.
	pem, err := ioutil.ReadFile(cfg.RPCCert)
	if err != nil {
		return nil, err
	}

	uri, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pem); !ok {
		return nil, fmt.Errorf("invalid certificate file: %v",
			cfg.RPCCert)
	}
	tlsConfig := &tls.Config{
		RootCAs:    pool,
		ServerName: uri.Hostname(),
	}

	// Configure dialer with tls.
	dialer := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	// Configure basic access authorization.
	authStr := base64.StdEncoding.EncodeToString([]byte(cfg.RPCUser + ":" + cfg.RPCPass))
	header := http.Header{}
	header.Set("Authorization", "Basic "+authStr)
	conn, _, err := dialer.Dial(urlStr, header)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// sendPostRequest sends the marshalled JSON-RPC command using HTTP-POST mode
// to the server described in the passed config struct.  It also attempts to
// unmarshal the response as a msgjson.Message response and returns either the
// response or error.
func sendPostRequest(marshalledJSON []byte, urlStr string, cfg *config, httpClient *http.Client) (*msgjson.Message, error) {
	bodyReader := bytes.NewReader(marshalledJSON)
	httpRequest, err := http.NewRequest("POST", urlStr, bodyReader)
	if err != nil {
		return nil, err
	}
	httpRequest.Close = true
	httpRequest.Header.Set("Content-Type", "application/json")

	// Configure basic access authorization.
	httpRequest.SetBasicAuth(cfg.RPCUser, cfg.RPCPass)

	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	// Read the raw bytes and close the response.
	respBytes, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		err = fmt.Errorf("error reading json reply: %v", err)
		return nil, err
	}

	// Handle unsuccessful HTTP responses
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Generate a standard error to return if the server body is
		// empty.  This should not happen very often, but it's better
		// than showing nothing in case the target server has a poor
		// implementation.
		if len(respBytes) == 0 {
			return nil, fmt.Errorf("%d %s", httpResponse.StatusCode,
				http.StatusText(httpResponse.StatusCode))
		}
		return nil, fmt.Errorf("%s", respBytes)
	}

	// Unmarshal the response.
	var resp *msgjson.Message
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}
