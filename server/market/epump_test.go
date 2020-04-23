package market

import (
	"context"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestMarket_epochPumpHalt(t *testing.T) {
	// This tests stopping the epochPump.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ePump := newEpochPump()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ePump.Run(ctx)
	}()

	eq0 := NewEpoch(123413513, 1000)
	rq0 := ePump.Insert(eq0)

	eq1 := NewEpoch(123413514, 1000)
	rq1 := ePump.Insert(eq1) // testing ep.q append

	close(rq1.ready)

	var rq0Out *readyEpoch
	select {
	case rq0Out = <-ePump.ready:
		t.Fatalf("readyQueue provided out of order, got epoch %d", rq0Out.Epoch)
	default:
		// good, nothing was supposed to come out yet
	}

	close(rq0.ready)

	rq0Out = <-ePump.ready
	if rq0Out.EpochQueue.Epoch != eq0.Epoch {
		t.Errorf("expected epoch %d, got %d", eq0.Epoch, rq0Out.EpochQueue.Epoch)
	}

	cancel() // testing len(ep.q) == 0 (Ready to shutdown), with rq1 in head
	wg.Wait()

	// pump should be done, but the final readyEpoch should have been sent on
	// the buffered ready chan.
	rq1Out := <-ePump.ready
	if rq1Out.EpochQueue.Epoch != eq1.Epoch {
		t.Errorf("expected epoch %d, got %d", eq1.Epoch, rq1Out.EpochQueue.Epoch)
	}

	// Test shutdown with multiple queued epochs.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	ePump = newEpochPump()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ePump.Run(ctx)
	}()

	eq0 = NewEpoch(123413513, 1000)
	rq0 = ePump.Insert(eq0)
	eq1 = NewEpoch(123413514, 1000)
	rq1 = ePump.Insert(eq1)
	eq2 := NewEpoch(123413515, 1000)
	rq2 := ePump.Insert(eq2)

	cancel()          // testing len(ep.q) != 0 (stop after it drains the queue).
	runtime.Gosched() // let Run start shutting things down
	time.Sleep(50 * time.Millisecond)

	// Make sure epochs come out in order.
	close(rq0.ready)
	rq0Out = <-ePump.ready
	if rq0Out.EpochQueue.Epoch != eq0.Epoch {
		t.Errorf("expected epoch %d, got %d", eq0.Epoch, rq0Out.EpochQueue.Epoch)
	}

	close(rq1.ready)
	rq1Out = <-ePump.ready
	if rq1Out.EpochQueue.Epoch != eq1.Epoch {
		t.Errorf("expected epoch %d, got %d", eq1.Epoch, rq1Out.EpochQueue.Epoch)
	}

	close(rq2.ready)
	rq2Out := <-ePump.ready
	if rq2Out.EpochQueue.Epoch != eq2.Epoch {
		t.Errorf("expected epoch %d, got %d", eq2.Epoch, rq2Out.EpochQueue.Epoch)
	}

	select {
	case _, ok := <-ePump.ready:
		if ok {
			t.Errorf("ready channel should be closed now (Run returned), but something came through")
		}
	case <-time.After(time.Second):
		t.Errorf("ready channel should be closed by now (Run returned)")
	}

	rqX := ePump.Insert(eq2)
	if rqX != nil {
		t.Errorf("halted epoch pump allowed insertion of new epoch queue")
	}

	wg.Wait()
}

func Test_epochPump_next(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ep := newEpochPump()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ep.Run(ctx)
		wg.Done()
	}()

	waitTime := int64(5 * time.Second)
	if testing.Short() {
		waitTime = int64(1 * time.Second)
	}

	var epochStart, epochDur, numEpochs int64 = 123413513, 1_000, 100
	epochs := make([]*readyEpoch, numEpochs)
	for i := int64(0); i < numEpochs; i++ {
		rq := ep.Insert(NewEpoch(i+epochStart, epochDur))
		epochs[i] = rq

		// Simulate preimage collection, randomly making the queues ready.
		go func() {
			wait := time.Duration(rand.Int63n(waitTime))
			time.Sleep(wait)
			close(rq.ready)
		}()
	}

	// Receive all the ready epochs, verifying they come in order.
	for i := epochStart; i < numEpochs+epochStart; i++ {
		rq := <-ep.ready
		if rq.Epoch != i {
			t.Errorf("Received epoch %d, expected %d", rq.Epoch, i)
		}
	}
	// All fake preimage collection goroutines are done now.

	cancel()
	wg.Wait()
}
