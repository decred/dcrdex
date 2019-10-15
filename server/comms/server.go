// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"sync/atomic"
)

var idCounter uint64

func NextID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

type RPCClient struct{}
