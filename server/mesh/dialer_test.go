package mesh

import (
	"context"
	"sync"

	"decred.org/dcrdex/dex/msgjson"
)

type tPeerConn struct {
	id   uint64
	addr string
	done chan struct{}
	once sync.Once
}

func newTPeerConn() *tPeerConn {
	return &tPeerConn{
		id:   nextConnID(),
		addr: "test-peer",
		done: make(chan struct{}),
	}
}

func (c *tPeerConn) ID() uint64                  { return c.id }
func (c *tPeerConn) Addr() string                { return c.addr }
func (c *tPeerConn) Send(*msgjson.Message) error { return nil }
func (c *tPeerConn) Request(context.Context, string, any, any) error {
	return nil
}
func (c *tPeerConn) Authorize()            {}
func (c *tPeerConn) Done() <-chan struct{} { return c.done }
func (c *tPeerConn) Disconnect() {
	c.once.Do(func() {
		close(c.done)
	})
}

func (c *tPeerConn) requestHello(context.Context, *helloMessage) (*helloResponse, error) {
	return nil, nil
}
func (c *tPeerConn) requestDecision(context.Context, *decisionMessage) error { return nil }
