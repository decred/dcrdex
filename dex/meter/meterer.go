// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meter

import (
	"context"
	"time"
)

// DelayedRelay creates a simple error signal pipeline that delays and
// aggregates the relaying of nil errors. Non-nil errors received on the in
// channel are immediately send on the out channel without delay. If a nil error
// arrives within minDelay of the previous one, it will be scheduled for later
// to respect the configured delay. If multiple arrive within minDelay, they
// will be grouped into a single delayed signal.
func DelayedRelay(ctx context.Context, minDelay time.Duration, n int) (out <-chan error, in chan<- error) {
	inC := make(chan error, n)
	outC := make(chan error, n)

	go func() {
		defer close(outC) // signal shutdown to the consumer

		var last time.Time
		var scheduled *time.Timer
		now := make(chan struct{})

		for {
			select {
			case err := <-inC:
				if err != nil {
					outC <- err
					continue
				}

				if scheduled != nil {
					continue
				}

				if time.Since(last) > minDelay {
					outC <- nil
					last = time.Now().UTC()
					continue
				}

				delay := time.Until(last.Add(minDelay))
				scheduled = time.AfterFunc(delay, func() {
					now <- struct{}{}
				})

			case <-now:
				outC <- nil
				scheduled = nil
				last = time.Now().UTC()

			case <-ctx.Done():
				if scheduled != nil && !scheduled.Stop() {
					<-scheduled.C
				}
				return
			}
		}
	}()

	return outC, inC
}
