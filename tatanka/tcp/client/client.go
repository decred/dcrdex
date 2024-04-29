// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package client

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type Client struct {
	ctx    context.Context
	log    dex.Logger
	url    *url.URL
	cert   []byte
	cl     comms.WsConn
	cm     *dex.ConnectionMaster
	handle func(*msgjson.Message) *msgjson.Error
}

type Config struct {
	Logger        dex.Logger
	URL           string
	Cert          []byte
	PrivateKey    *secp256k1.PrivateKey
	HandleMessage func(*msgjson.Message) *msgjson.Error
}

func New(cfg *Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %w", err)
	}
	switch u.Scheme {
	case "ws", "wss":
	default:
		return nil, fmt.Errorf("protocol should be 'ws' or 'wss', not %q", u.Scheme)
	}
	return &Client{
		log:    cfg.Logger,
		url:    u,
		cert:   cfg.Cert,
		handle: cfg.HandleMessage,
	}, nil
}

func (c *Client) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	c.ctx = ctx
	if c.cl, err = comms.NewWsConn(&comms.WsCfg{
		URL:      c.url.String(),
		PingWait: 20 * time.Second,
		Cert:     c.cert,
		ReconnectSync: func() {
			fmt.Println("## RECONNECTED RECONNECTED RECONNECTED RECONNECTED ")
		},
		ConnectEventFunc: func(status comms.ConnectionStatus) {
			if status == comms.Disconnected {
				// Remove it from the map.
				c.log.Infof("WebSockets client for %s has disconnected", c.url)
			}
		},
		Logger: c.log.SubLogger("TC"),
	}); err != nil {
		return nil, fmt.Errorf("error creating websockets connection: %w", err)
	}

	var wg sync.WaitGroup

	msgs := c.cl.MessageSource()

	c.cm = dex.NewConnectionMaster(c.cl)
	if err := c.cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to %q: %w", c.url, err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-msgs:
				c.handle(msg)
			case <-ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

func (c *Client) Send(msg *msgjson.Message) error {
	return c.cl.Send(msg)
}

func (c *Client) Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error {
	return c.cl.Request(msg, respHandler)
}
