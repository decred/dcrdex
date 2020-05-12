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

	mtx  sync.RWMutex
	q    []*readyEpoch
	halt bool

	newQ chan struct{}
}

func newEpochPump() *epochPump {
	return &epochPump{
		ready: make(chan *readyEpoch, 1),
		newQ:  make(chan struct{}, 1),
	}
}

func (ep *epochPump) Run(ctx context.Context) {
	// Context cancellation must cause a graceful shutdown.
	go func() {
		<-ctx.Done()

		// next will close it after it drains the queue.
		ep.mtx.Lock()
		ep.halt = true
		ep.mtx.Unlock()
		ep.newQ <- struct{}{} // final popFront returns nil and next returns closed channel
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
	if ep.halt {
		return nil
	}

	// push: append a new readyEpoch to the closed epoch queue.
	ep.q = append(ep.q, rq)

	// Signal to next in goroutine so slow or missing receiver doesn't block us.
	go func() { ep.newQ <- struct{}{} }()

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

	// Wait for new epoch in queue or halt.
	<-ep.newQ

	ep.mtx.Lock()
	head := ep.popFront()
	ep.mtx.Unlock()

	if head == nil { // pump halted
		close(ready)
		return ready
	}

	// Send next on the returned channel when it becomes ready. If the process
	// dies before goroutine completion, the Market is down anyway.
	go func() {
		<-head.ready // block until preimage collection is complete (close this channel)
		ready <- head
	}()
	return ready
}
