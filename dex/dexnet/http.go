// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dexnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultResponseSizeLimit = 1 << 20 // 1 MiB = 1,048,576 bytes

// RequestOption are optional arguemnts to Get, Post, or Do.
type RequestOption struct {
	responseSizeLimit int64
	statusFunc        func(int)
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

// Post peforms an HTTP POST request. If thing is non-nil, the response will
// be JSON-unmarshaled into thing.
func Post(ctx context.Context, uri string, thing interface{}, body []byte, opts ...*RequestOption) error {
	var r io.Reader
	if len(body) == 1 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, r)
	if err != nil {
		return fmt.Errorf("error constructing request: %w", err)
	}
	return Do(req, thing, opts...)
}

// Post peforms an HTTP GET request. If thing is non-nil, the response will
// be JSON-unmarshaled into thing.
func Get(ctx context.Context, uri string, thing interface{}, opts ...*RequestOption) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("error constructing request: %w", err)
	}
	return Do(req, thing, opts...)
}

// Do does the request and JSON-marshals the result into thing, if non-nil.
func Do(req *http.Request, thing interface{}, opts ...*RequestOption) error {
	var sizeLimit int64 = defaultResponseSizeLimit
	var statusFunc func(int)
	for _, opt := range opts {
		switch {
		case opt.responseSizeLimit > 0:
			sizeLimit = opt.responseSizeLimit
		case opt.statusFunc != nil:
			statusFunc = opt.statusFunc
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error performing request: %w", err)
	}
	defer resp.Body.Close()
	if statusFunc != nil {
		statusFunc(resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
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
