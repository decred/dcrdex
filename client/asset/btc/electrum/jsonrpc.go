// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

type positional []interface{}

type request struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"` // [] for positional args or {} for named args, no bare types
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Error satisfies the error interface.
func (e RPCError) Error() string {
	return fmt.Sprintf("code %d: %q", e.Code, e.Message)
}

type response struct {
	// The "jsonrpc" fields is ignored.
	ID     uint64          `json:"id"`     // response to request
	Method string          `json:"method"` // notification for subscription
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

type ntfn = request // weird but true
type ntfnData struct {
	Params json.RawMessage `json:"params"`
}

func prepareRequest(id uint64, method string, args interface{}) ([]byte, error) {
	// nil args should marshal as [] instead of null.
	if args == nil {
		args = []json.RawMessage{}
	}

	switch rt := reflect.TypeOf(args); rt.Kind() {
	case reflect.Struct, reflect.Slice:
	case reflect.Ptr: // allow pointer to struct
		if rt.Elem().Kind() != reflect.Struct {
			return nil, fmt.Errorf("invalid arg type %v, must be slice or struct", rt)
		}
	default:
		return nil, fmt.Errorf("invalid arg type %v, must be slice or struct", rt)
	}

	params, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %v", err)
	}
	req := &request{
		Jsonrpc: "2.0", // electrum wallet seems to respond with 2.0 regardless
		ID:      id,
		Method:  method,
		Params:  params,
	}
	return json.Marshal(req)
}

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
