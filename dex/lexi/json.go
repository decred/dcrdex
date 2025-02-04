// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"encoding"
	"encoding/json"
)

// lexiJSON is just a wrapper for something that is JSON-encodable.
type lexiJSON struct {
	thing any
}

type BinaryMarshal interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

// JSON can be used to encode JSON-encodable things.
func JSON(thing any) BinaryMarshal {
	return &lexiJSON{thing}
}

// UnJSON can be used in index entry generator functions for some syntactic
// sugar.
func UnJSON(thing any) interface{} {
	if lj, is := thing.(*lexiJSON); is {
		return lj.thing
	}
	return struct{}{}
}

func (p *lexiJSON) MarshalBinary() ([]byte, error) {
	return json.Marshal(p.thing)
}

func (p *lexiJSON) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, p.thing)
}
