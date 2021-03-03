package main

import "C"
import (
	"encoding/json"
	"fmt"
)

// CallData is the type sent for all golink calls.
type CallData struct {
	Function string          `json:"function"`
	Params   json.RawMessage `json:"params"`
}

func callError(s string, a ...interface{}) *C.char {
	b, _ := json.Marshal(&struct {
		Error string `json:"error"`
	}{
		Error: fmt.Sprintf(s, a...),
	})
	return C.CString(string(b))
}

var adapter = NewCoreAdapter()

// Call is used to invoke a registered function.
//export Call
func Call(msg *C.char) *C.char {
	jsonStr := C.GoString(msg)
	cd := new(CallData)
	err := json.Unmarshal([]byte(jsonStr), cd)
	if err != nil {
		return callError("json Unmarshal error: %v", err)
	}

	res, err := adapter.run(cd)
	if err != nil {
		return callError("%s error: %v", cd.Function, err)
	}
	return C.CString(res)
}

func main() {}
