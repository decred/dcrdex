// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

const defaultWalletTimeout = 10 * time.Second

// WalletClient is an Electrum wallet HTTP JSON-RPC client.
type WalletClient struct {
	reqID uint64
	url   string
	auth  string

	// HTTPClient may be set by the user to a custom http.Client. The
	// constructor sets a vanilla client.
	HTTPClient *http.Client
	// Timeout is the timeout on http requests. A 10 second default is set by
	// the constructor.
	Timeout time.Duration
}

// NewWalletClient constructs a new Electrum wallet RPC client with the given
// authorization information and endpoint. The endpoint should include the
// protocol, e.g. http://127.0.0.1:4567. To specify a custom http.Client or
// request timeout, the fields may be set after construction.
func NewWalletClient(user, pass, endpoint string) *WalletClient {
	// Prepare the HTTP Basic Authorization request header. This avoids
	// re-encoding it for every request with (*http.Request).SetBasicAuth.
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	return &WalletClient{
		url:        endpoint,
		auth:       auth,
		HTTPClient: &http.Client{},
		Timeout:    defaultWalletTimeout,
	}
}

func (ec *WalletClient) nextID() uint64 {
	return atomic.AddUint64(&ec.reqID, 1)
}

// Call makes a JSON-RPC request for the given method with the provided
// arguments. args may be a struct or slice that marshalls to JSON. If it is a
// slice, it represents positional arguments. If it is a struct or pointer to a
// struct, it represents "named" parameters in key-value format. Any arguments
// should have their fields appropriately tagged for JSON marshalling. The
// result is marshaled into result if it is non-nil, otherwise the result is
// discarded.
func (ec *WalletClient) Call(ctx context.Context, method string, args interface{}, result interface{}) error {
	reqMsg, err := prepareRequest(ec.nextID(), method, args)
	if err != nil {
		return err
	}

	bodyReader := bytes.NewReader(reqMsg)
	ctx, cancel := context.WithTimeout(ctx, ec.Timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ec.url, bodyReader)
	if err != nil {
		return err
	}
	httpReq.Close = true
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", ec.auth) // httpReq.SetBasicAuth(ec.user, ec.pass)

	resp, err := ec.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%d: %s", resp.StatusCode, string(b))
	}

	jsonResp := &response{}
	err = json.NewDecoder(resp.Body).Decode(jsonResp)
	if err != nil {
		return err
	}
	if jsonResp.Error != nil {
		return jsonResp.Error
	}

	if result != nil {
		return json.Unmarshal(jsonResp.Result, result)
	}
	return nil
}
