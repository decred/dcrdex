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
	"reflect"
	"strconv"
	"sync/atomic"
	"time"
)

const defaultTimeout = 10 * time.Second

// Client is an Electrum wallet JSON-RPC client.
type Client struct {
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

// NewClient constructs a new Electrum client with the given authorization
// information and endpoint. The endpoint should include the protocol, e.g.
// http://127.0.0.1:4567. To specify a custom http.Client or request timeout,
// the fields may be set after construction.
func NewClient(user, pass, endpoint string) *Client {
	// Prepare the HTTP Basic Authorization request header. This avoids
	// re-encoding it for every request with (*http.Request).SetBasicAuth.
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	return &Client{
		url:        endpoint,
		auth:       auth,
		HTTPClient: &http.Client{},
		Timeout:    defaultTimeout,
	}
}

func (ec *Client) nextID() uint64 {
	return atomic.AddUint64(&ec.reqID, 1)
}

type request struct {
	Jsonrpc string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"` // [] for positional args or {} for named args, no bare types
	ID      interface{}     `json:"id"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (e RPCError) Error() string {
	return fmt.Sprintf("code %d: %q", e.Code, e.Message)
}

type response struct {
	// The "id" and "jsonrpc" fields are ignored.
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

type positional []interface{}

// floatString is for unmarshalling a string with a float like "123.34" directly
// into a float64 instead of a string and then converting later.
type floatString float64

func (fs *floatString) UnmarshalJSON(b []byte) error {
	// Try to strip the string contents out of quotes.
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err // wasn't a string
	}
	fl, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return err // The string didn't contain a float.
	}
	*fs = floatString(fl)
	return nil
}

// Call makes a JSON-RPC request for the given method with the provided
// arguments. args may be a struct or slice that marshalls to JSON. If it is a
// slice, it represents positional arguments. If it is a struct or pointer to a
// struct, it represents "named" parameters in key-value format. Any arguments
// should have their fields appropriately tagged for JSON marshalling. The
// result is marshaled into result if it is non-nil, otherwise the result is
// discarded.
func (ec *Client) Call(method string, args interface{}, result interface{}) error {
	// nil args should marshal as [] instead of null.
	if args == nil {
		args = []json.RawMessage{}
	}

	switch rt := reflect.TypeOf(args); rt.Kind() {
	case reflect.Struct, reflect.Slice:
	case reflect.Ptr: // allow pointer to struct
		if rt.Elem().Kind() != reflect.Struct {
			return fmt.Errorf("invalid arg type %v, must be slice or struct", rt)
		}
	default:
		return fmt.Errorf("invalid arg type %v, must be slice or struct", rt)
	}

	params, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("failed to marshal arguments: %v", err)
	}
	req := &request{
		Jsonrpc: "1.0", // electrum seems to respond with 2.0 regardless
		ID:      ec.nextID(),
		Method:  method,
		Params:  params,
	}
	reqMsg, err := json.Marshal(req)
	if err != nil {
		return err // likely impossible
	}

	bodyReader := bytes.NewReader(reqMsg)
	ctx, cancel := context.WithTimeout(context.Background(), ec.Timeout)
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
