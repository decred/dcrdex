// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package coinwaiter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
)

const (
	// These constants are used for a coin waiter to inform the caller whether
	// the waiter should be run again.
	TryAgain     = false
	DontTryAgain = true
)

// Settings is used to define the lifecycle of a waitFunc.
type Settings struct {
	Expiration time.Time
	AccountID  account.AccountID
	Request    *msgjson.Message
	TimeoutErr *msgjson.Error
}

// NewSettings is a constructor for a SEttings with a timeout and a standard
// error message.
func NewSettings(user account.AccountID, msg *msgjson.Message, coinID []byte, txWaitExpiration time.Duration) *Settings {
	return &Settings{
		// must smarten up this expiration value before merge. Where should this
		// come from?
		Expiration: time.Now().Add(txWaitExpiration),
		AccountID:  user,
		Request:    msg,
		TimeoutErr: &msgjson.Error{
			Code:    msgjson.TransactionUndiscovered,
			Message: fmt.Sprintf("failed to find transaction %x", coinID),
		},
	}
}

// A waitFunc is a function that is repeated periodically until a boolean
// true is returned or the expiration time is surpassed. In the latter case a
// timeout error is sent to the client.
type waitFunc struct {
	params *Settings
	f      func() bool
}

// Sender is probably a ws.WsLink.
type Sender func(account.AccountID, *msgjson.Message)

// Waiter is a waitFunc manager. Waiter is used to deal with network latency.
type Waiter struct {
	waiterMtx       sync.RWMutex
	waiters         []*waitFunc
	recheckInterval time.Duration
	send            Sender
}

// New is the constructor for a new Waiter.
func New(recheckInterval time.Duration, sender Sender) *Waiter {
	return &Waiter{
		recheckInterval: recheckInterval,
		waiters:         make([]*waitFunc, 0, 256),
		send:            sender,
	}
}

// waitMempool attempts to run the passed function. If the function returns
// the value DontTryAgain, nothing else is done. If the function returns the
// value TryAgain, the function is queued to run on an interval until it returns
// DontTryAgain, or until an expiration time is exceeded, as specified in the
// Settings.
func (w *Waiter) Wait(params *Settings, f func() bool) {
	if time.Now().After(params.Expiration) {
		log.Error("Swapper.waitMempool: waitSettings given expiration before present")
		return
	}
	// Check to see if it passes right away.
	if f() {
		return
	}
	w.waiterMtx.Lock()
	w.waiters = append(w.waiters, &waitFunc{params: params, f: f})
	w.waiterMtx.Unlock()
}

// Run runs the primary wait loop until the context is canceled.
func (w *Waiter) Run(ctx context.Context) {
	// The latencyTicker triggers a check of all waitFunc functions.
	latencyTicker := time.NewTicker(w.recheckInterval)
	defer latencyTicker.Stop()

	runWaiters := func() {
		w.waiterMtx.Lock()
		defer w.waiterMtx.Unlock()
		agains := make([]*waitFunc, 0)
		// Grab new waiters
		tNow := time.Now()
		for _, mFunc := range w.waiters {
			if !mFunc.f() {
				// If this waiter has expired, issue the timeout error to the client
				// and do not append to the agains slice.
				if mFunc.params.Expiration.Before(tNow) {
					p := mFunc.params
					resp, err := msgjson.NewResponse(p.Request.ID, nil, p.TimeoutErr)
					if err != nil {
						log.Error("NewResponse error in (Swapper).loop: %v", err)
						continue
					}
					w.send(p.AccountID, resp)
					continue
				}
				agains = append(agains, mFunc)
			} // End if !mFunc.f(). nothing to do if mFunc returned dontTryAgain=true
		}
		w.waiters = agains
	}
out:
	for {
		select {
		case <-latencyTicker.C:
			runWaiters()
		case <-ctx.Done():
			break out
		}
	}
}
