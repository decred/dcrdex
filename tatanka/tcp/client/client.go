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

type ConnectionStatus uint32

const (
	Disconnected ConnectionStatus = iota
	Connected
	InvalidCert
)

type Client struct {
	ctx              context.Context
	log              dex.Logger
	url              *url.URL
	cert             []byte
	cl               comms.WsConn
	cm               *dex.ConnectionMaster
	handle           func(*msgjson.Message, func(*msgjson.Message))
	connectEventFunc func(ConnectionStatus)
}

type Config struct {
	Logger           dex.Logger
	URL              string
	Cert             []byte
	PrivateKey       *secp256k1.PrivateKey
	HandleMessage    func(*msgjson.Message, func(*msgjson.Message))
	ConnectEventFunc func(ConnectionStatus)
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
		log:              cfg.Logger,
		url:              u,
		cert:             cfg.Cert,
		handle:           cfg.HandleMessage,
		connectEventFunc: cfg.ConnectEventFunc,
	}, nil
}

func (c *Client) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	c.ctx = ctx
	if c.cl, err = comms.NewWsConn(&comms.WsCfg{
		URL:                  c.url.String(),
		PingWait:             20 * time.Second,
		Cert:                 c.cert,
		DisableAutoReconnect: true,
		ReconnectSync: func() {
			fmt.Println("## RECONNECTED RECONNECTED RECONNECTED RECONNECTED ")
		},
		ConnectEventFunc: func(status comms.ConnectionStatus) {
			if c.connectEventFunc == nil {
				return
			}

			switch status {
			case comms.Disconnected:
				c.connectEventFunc(Disconnected)
			case comms.Connected:
				c.connectEventFunc(Connected)
			case comms.InvalidCert:
				c.connectEventFunc(InvalidCert)
			default:
				c.log.Errorf("Unknown connection status: %d", status)
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

	sendResponse := func(msg *msgjson.Message) {
		c.log.Infof("Sending response: %v", msg)
		err = c.cl.Send(msg)
		if err != nil {
			c.log.Errorf("Error sending response: %v", err)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-msgs:
				if msg == nil {
					c.log.Errorf("Received nil message")
					continue
				}
				c.log.Infof("Received message: %v", msg)
				c.handle(msg, sendResponse)
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
