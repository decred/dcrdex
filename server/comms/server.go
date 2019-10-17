// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"sync/atomic"

	"github.com/decred/dcrdex/server/comms/msgjson"
)

var idCounter uint64

func NextID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

// Link is an interface for a communication channel with an API client. The
// reference implemenatation of a Link-satisfying type is the wsLink, which
// passes messages over a websocket connection.
type Link interface {
	// ID will return a unique ID by which this connection can be identified.
	ID() uint64
	// Send sends the msgjson.Message to the client.
	Send(msg *msgjson.Message) error
	// Request sends the Request-type msgjson.Message to the client and registers
	// a handler for the response.
	Request(msg *msgjson.Message, f func(Link, *msgjson.Message)) error
	// Banish closes the link and quarantines the client.
	Banish()
}
