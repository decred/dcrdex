// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dexnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/decred/go-socks/socks"
)

const defaultResponseSizeLimit = 1 << 20 // 1 MiB = 1,048,576 bytes

// Client is the default HTTP client used for requests.
var Client = &http.Client{
	Timeout: 20 * time.Second, // 20 seconds
}

// ProxyDialContext returns a DialContext function that routes connections
// through a SOCKS5 proxy at addr (host:port). If addr is empty, nil is
// returned.
func ProxyDialContext(addr string) func(ctx context.Context, network, address string) (net.Conn, error) {
	if addr == "" {
		return nil
	}
	proxy := &socks.Proxy{Addr: addr}
	return proxy.DialContext
}

// ProxyTransport returns an *http.Transport that routes all connections through
// a SOCKS5 proxy at addr (host:port).
func ProxyTransport(addr string) *http.Transport {
	return &http.Transport{
		DialContext:           ProxyDialContext(addr),
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// ProxyHTTPClient returns an *http.Client that routes all requests through a
// SOCKS5 proxy at addr (host:port).
func ProxyHTTPClient(addr string) *http.Client {
	return &http.Client{
		Transport: ProxyTransport(addr),
		Timeout:   20 * time.Second,
	}
}

var setProxyOnce sync.Once

// SetProxy configures the global Client to route all requests through a SOCKS5
// proxy at the given address (host:port). Only the first call takes effect;
// subsequent calls are no-ops.
func SetProxy(addr string) {
	setProxyOnce.Do(func() {
		Client.Transport = ProxyTransport(addr)
	})
}

// RequestOption are optional arguments to Get, Post, or Do.
type RequestOption struct {
	responseSizeLimit int64
	statusFunc        func(int)
	header            *[2]string
	errThing          any
}

// WithSizeLimit sets a size limit for a response. See defaultResponseSizeLimit
// for the default.
func WithSizeLimit(limit int64) *RequestOption {
	return &RequestOption{responseSizeLimit: limit}
}

// WithStatusFunc calls a function with the status code after the request is
// performed.
func WithStatusFunc(f func(int)) *RequestOption {
	return &RequestOption{statusFunc: f}
}

// WithRequestHeader adds a header entry to the request.
func WithRequestHeader(k, v string) *RequestOption {
	h := [2]string{k, v}
	return &RequestOption{header: &h}
}

// WithErrorParsing adds parsing of response bodies for HTTP error responses.
func WithErrorParsing(thing any) *RequestOption {
	return &RequestOption{errThing: thing}
}

// Post performs an HTTP POST request. If thing is non-nil, the response will
// be JSON-unmarshaled into thing.
func Post(ctx context.Context, uri string, thing any, body []byte, opts ...*RequestOption) error {
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, r)
	if err != nil {
		return fmt.Errorf("error constructing request: %w", err)
	}
	return Do(req, thing, opts...)
}

// Post performs an HTTP GET request. If thing is non-nil, the response will
// be JSON-unmarshaled into thing.
func Get(ctx context.Context, uri string, thing any, opts ...*RequestOption) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("error constructing request: %w", err)
	}
	return Do(req, thing, opts...)
}

// Do does the request and JSON-marshals the result into thing, if non-nil.
func Do(req *http.Request, thing any, opts ...*RequestOption) error {
	var sizeLimit int64 = defaultResponseSizeLimit
	var statusFunc func(int)
	var errThing any
	for _, opt := range opts {
		switch {
		case opt.responseSizeLimit > 0:
			sizeLimit = opt.responseSizeLimit
		case opt.statusFunc != nil:
			statusFunc = opt.statusFunc
		case opt.header != nil:
			h := *opt.header
			k, v := h[0], h[1]
			req.Header.Add(k, v)
		case opt.errThing != nil:
			errThing = opt.errThing
		}
	}
	resp, err := Client.Do(req)
	if err != nil {
		return fmt.Errorf("error performing request: %w", err)
	}
	defer resp.Body.Close()
	if statusFunc != nil {
		statusFunc(resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		if errThing != nil {
			reader := io.LimitReader(resp.Body, sizeLimit)
			if err = json.NewDecoder(reader).Decode(errThing); err != nil {
				return fmt.Errorf("HTTP error: %q (code %d). error encountered parsing error body: %w", resp.Status, resp.StatusCode, err)
			}
		}
		return fmt.Errorf("HTTP error: %q (code %d)", resp.Status, resp.StatusCode)
	}
	if thing == nil {
		return nil
	}
	reader := io.LimitReader(resp.Body, sizeLimit)
	if err = json.NewDecoder(reader).Decode(thing); err != nil {
		return fmt.Errorf("error decoding request: %w", err)
	}
	return nil
}
