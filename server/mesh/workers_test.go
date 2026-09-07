// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
)

func waitServiceWorkers(t testing.TB, svc *Service, desc string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		svc.runWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", desc)
	}
}

// startTestWorkerSupervisor starts the supervisor as Service.startup does.
func startTestWorkerSupervisor(svc *Service, ctx context.Context) {
	svc.workers = newWorkerSupervisor(svc.masterWorkers, svc.log, svc.ensureLoaded, svc.handleMasterWorkerReadiness)
	svc.workers.startSupervisor(ctx, &svc.runWG)
}

func TestServiceMasterWorkersSingleServer(t *testing.T) {
	started := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	svc, err := NewService(&ServiceConfig{
		EventLogReader: &testEventLogReader{},
		OnHalt:         func(error) {},
		MasterWorkers: []MasterWorker{{
			Name: "test",
			Run: func(ctx context.Context, reportReady func(error)) {
				reportReady(nil)
				started <- struct{}{}
				<-ctx.Done()
				stopped <- struct{}{}
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runServiceForTest(ctx, svc)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("master worker did not start in single-server mode")
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatalf("master worker did not stop")
	}
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatalf("service did not stop")
	}
}

func TestServiceMasterWorkersStartOnce(t *testing.T) {
	started := make(chan struct{}, 4)
	var starts atomic.Uint32
	svc := &Service{
		loaded:    newReadiness(),
		log:       dex.Disabled,
		transport: newSingleServerTransport(),
		masterWorkers: []MasterWorker{{
			Name: "test",
			Run: func(ctx context.Context, reportReady func(error)) {
				reportReady(nil)
				starts.Add(1)
				started <- struct{}{}
				<-ctx.Done()
			},
		}},
		ready: newReadiness(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.lifeCtx = ctx
	svc.lifeCancel = cancel
	startTestWorkerSupervisor(svc, ctx)

	svc.workers.startWorkers()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("master worker did not start")
	}

	svc.workers.startWorkers()
	select {
	case <-started:
		t.Fatalf("master worker started twice")
	case <-time.After(50 * time.Millisecond):
	}
	if starts.Load() != 1 {
		t.Fatalf("worker starts = %d, want 1", starts.Load())
	}

	cancel()
	done := make(chan struct{})
	go func() {
		svc.runWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("master worker did not stop after service shutdown")
	}
}

func TestServiceMasterWorkersStartSequentiallyInConfigOrder(t *testing.T) {
	firstReady := make(chan struct{})
	secondReady := make(chan struct{})
	started := make(chan string, 3)
	svc, err := NewService(&ServiceConfig{
		EventLogReader: &testEventLogReader{},
		OnHalt:         func(error) {},
		MasterWorkers: []MasterWorker{
			{
				Name: "z-first",
				Run: func(ctx context.Context, reportReady func(error)) {
					started <- "first"
					select {
					case <-firstReady:
						reportReady(nil)
					case <-ctx.Done():
						return
					}
					<-ctx.Done()
				},
			},
			{
				Name: "a-second",
				Run: func(ctx context.Context, reportReady func(error)) {
					started <- "second"
					select {
					case <-secondReady:
						reportReady(nil)
					case <-ctx.Done():
						return
					}
					<-ctx.Done()
				},
			},
			{
				Name: "m-third",
				Run: func(ctx context.Context, reportReady func(error)) {
					started <- "third"
					reportReady(nil)
					<-ctx.Done()
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runServiceForTest(ctx, svc)

	requireStarted := func(want string) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("started worker = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("worker %q did not start", want)
		}
	}
	requireNoStart := func(msg string) {
		t.Helper()
		select {
		case got := <-started:
			t.Fatalf("%s: worker %q started", msg, got)
		case <-time.After(50 * time.Millisecond):
		}
	}

	requireStarted("first")
	requireNoStart("second worker started before first worker was ready")
	close(firstReady)
	requireStarted("second")
	requireNoStart("third worker started before second worker was ready")
	close(secondReady)
	requireStarted("third")
	if err := svc.WaitUntilReadyForComms(context.Background()); err != nil {
		t.Fatalf("WaitUntilReadyForComms error: %v", err)
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatalf("service did not stop")
	}
}
