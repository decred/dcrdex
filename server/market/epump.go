package market

import (
	"context"
	"sync"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/matcher"
)

type readyEpoch struct {
	*EpochQueue
	ready          chan struct{} // close this when the struct is ready
	cSum           []byte
	ordersRevealed []*matcher.OrderRevealed
	misses         []order.Order
}

type epochPump struct {
	ready chan *readyEpoch // consumer receives from this

	mtx    sync.RWMutex
	q      []*readyEpoch
	halt   bool
	halted bool
	head   chan *readyEpoch // internal, closed when ready to halt
}

func newEpochPump() *epochPump {
	return &epochPump{
		ready: make(chan *readyEpoch, 1),
		head:  make(chan *readyEpoch, 1),
	}
}

func (ep *epochPump) Run(ctx context.Context) {
	// Context cancellation must cause a graceful shutdown.
	go func() {
		<-ctx.Done()

		ep.mtx.Lock()
		defer ep.mtx.Unlock()

		// gracefully shut down the epoch pump, allowing the queue to be fully
		// drained and all epochs passed on to the consumer.
		if len(ep.q) == 0 {
			// Ready to shutdown.
			close(ep.head) // cause next() to return a closed channel and Run to return.
			ep.halted = true
		} else {
			// next will close it after it drains the queue.
			ep.halt = true
		}
	}()

	defer close(ep.ready)
	for {
		rq, ok := <-ep.next()
		if !ok {
			return
		}
		ep.ready <- rq // consumer should receive this
	}
}

// Insert enqueues an EpochQueue and starts preimage collection immediately.
// Access epoch queues in order and when they have completed preimage collection
// by receiving from the epochPump.ready channel.
func (ep *epochPump) Insert(epoch *EpochQueue) *readyEpoch {
	rq := &readyEpoch{
		EpochQueue: epoch,
		ready:      make(chan struct{}),
	}

	ep.mtx.Lock()
	defer ep.mtx.Unlock()

	if ep.halted || ep.halt {
		// head is closed or about to be.
		return nil
	}

	select {
	case ep.head <- rq: // buffered, so non-blocking when empty and no receiver
	default:
		// push: append a new readyEpoch to the closed epoch queue.
		ep.q = append(ep.q, rq)
	}

	return rq
}

// popFront removes the next readyEpoch from the closed epoch queue, q. It is
// not thread-safe. pop is only used in next to advance the head of the pump.
func (ep *epochPump) popFront() *readyEpoch {
	if len(ep.q) == 0 {
		return nil
	}
	x := ep.q[0]
	ep.q = ep.q[1:]
	return x
}

// next provides a channel for receiving the next readyEpoch when it completes
// preimage collection. next blocks until there is an epoch to send.
func (ep *epochPump) next() <-chan *readyEpoch {
	ready := make(chan *readyEpoch) // next sent on this channel when ready
	next := <-ep.head

	// A closed head channel signals a halted and drained pump.
	if next == nil {
		close(ready)
		return ready
	}

	ep.mtx.Lock()
	defer ep.mtx.Unlock()

	// If the queue is not empty, set new head.
	x := ep.popFront()
	if x != nil {
		ep.head <- x // non-blocking, received in select above
	} else if ep.halt {
		// Only halt the pump once the queue is emptied. The final head is still
		// forwarded to the consumer.
		close(ep.head)
		ep.halted = true
		// continue to serve next, but a closed channel will be returned on
		// subsequent calls.
	}

	// Send next on the returned channel when it becomes ready. If the process
	// dies before goroutine completion, the Market is down anyway.
	go func() {
		<-next.ready // block until preimage collection is complete (close this channel)
		ready <- next
	}()
	return ready
}
