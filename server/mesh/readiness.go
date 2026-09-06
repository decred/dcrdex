// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"sync"
)

// readiness records a single outcome and lets callers wait for it.
type readiness struct {
	once sync.Once
	err  error
	done chan struct{}
}

// newReadiness returns an unresolved readiness.
func newReadiness() *readiness {
	return &readiness{done: make(chan struct{})}
}

// resolve records the outcome. Only the first call has any effect.
func (r *readiness) resolve(err error) {
	r.once.Do(func() {
		r.err = err
		close(r.done)
	})
}

// resolved returns a channel that is closed when the outcome is recorded.
func (r *readiness) resolved() <-chan struct{} {
	return r.done
}

// result returns the recorded outcome and whether it is recorded yet.
func (r *readiness) result() (error, bool) {
	select {
	case <-r.done:
		return r.err, true
	default:
		return nil, false
	}
}

// wait blocks until the outcome is recorded or ctx is done, and returns the
// outcome or the context error.
func (r *readiness) wait(ctx context.Context) error {
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
