// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	clientcomms "decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
)

// linkWrapper wraps a comms.Link to create a tanka.Sender.
type linkWrapper struct {
	comms.Link
}

func (l *linkWrapper) Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error {
	const requestTimeout = time.Second * 30
	return l.Link.Request(msg, func(_ comms.Link, respMsg *msgjson.Message) {
		respHandler(respMsg)
	}, requestTimeout, func() { // expire
		errMsg := mj.MustResponse(msg.ID, nil, msgjson.NewError(mj.ErrTimeout, "request timed out"))
		respHandler(errMsg)
	})
}

func (l *linkWrapper) RequestRaw(msgID uint64, rawMsg []byte, respHandler func(*msgjson.Message)) error {
	const requestTimeout = time.Second * 30
	return l.Link.RequestRaw(msgID, rawMsg, func(_ comms.Link, respMsg *msgjson.Message) {
		respHandler(respMsg)
	}, requestTimeout, func() { // expire
		errMsg := mj.MustResponse(msgID, nil, msgjson.NewError(mj.ErrTimeout, "request timed out"))
		respHandler(errMsg)
	})
}

func (l *linkWrapper) PeerID() (peerID tanka.PeerID) {
	copy(peerID[:], []byte(l.CustomID()))
	return
}

func (l *linkWrapper) SetPeerID(peerID tanka.PeerID) {
	l.SetCustomID(string(peerID[:]))
}

type wsConnWrapper struct {
	clientcomms.WsConn
	cm *dex.ConnectionMaster
	id atomic.Value
}

func newWsConnWrapper(cl clientcomms.WsConn, cm *dex.ConnectionMaster) *wsConnWrapper {
	w := &wsConnWrapper{
		WsConn: cl,
		cm:     cm,
	}
	w.id.Store(tanka.PeerID{})
	return w
}

func (cl *wsConnWrapper) Disconnect() {
	cl.cm.Disconnect()
	cl.cm.Wait()
}

func (cl *wsConnWrapper) SetPeerID(id tanka.PeerID) {
	cl.id.Store(id)
}

func (cl *wsConnWrapper) PeerID() tanka.PeerID {
	return cl.id.Load().(tanka.PeerID)
}

// Server handles HTTP, HTTPS, WebSockets, and Tor communications protocols.
type Server struct {
	t   TankaCore
	wg  *sync.WaitGroup
	srv *comms.Server
	log dex.Logger
}

type TankaCore interface {
	// Config() *mj.TatankaConfig
	Routes() []string
	HandleMessage(tanka.Sender, *msgjson.Message) *msgjson.Error
}

func NewServer(cfg *comms.RPCConfig, t TankaCore, log dex.Logger) (*Server, error) {
	srv, err := comms.NewServer(cfg)
	if err != nil {
		return nil, fmt.Errorf("NewServer error: %w", err)
	}

	s := &Server{
		t:   t,
		srv: srv,
		log: log,
	}

	for _, r := range t.Routes() {
		srv.Route(r, func(l comms.Link, msg *msgjson.Message) *msgjson.Error {
			return t.HandleMessage(&linkWrapper{l}, msg)
		})
	}

	return s, nil
}

func (s *Server) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	s.wg = &wg

	// Start WebSocket server
	runner := dex.NewStartStopWaiter(s.srv)
	runner.Start(ctx) // stopped with Stop

	wg.Add(1)
	go func() {
		<-ctx.Done()
		runner.Stop()
		wg.Done()
	}()

	return &wg, nil
}

type RemoteNodeConfig struct {
	URL  string `json:"url"`
	Cert []byte `json:"cert"`
}

func (s *Server) ConnectBootNode(
	ctx context.Context,
	rawCfg json.RawMessage,
	handleMessage func(cl tanka.Sender, msg *msgjson.Message),
	disconnect func(),
) (tanka.Sender, error) {

	var n RemoteNodeConfig
	if err := json.Unmarshal(rawCfg, &n); err != nil {
		return nil, fmt.Errorf("error reading boot node configuration: %w", err)
	}

	uri, err := url.Parse(n.URL)
	if err != nil {
		return nil, fmt.Errorf("url parse error: %w", err)
	}

	if uri.Path != "/ws" {
		uri.Path = "/ws"
	}

	cl, err := clientcomms.NewWsConn(&clientcomms.WsCfg{
		URL:      uri.String(),
		PingWait: 20 * time.Second,
		Cert:     n.Cert,
		ConnectEventFunc: func(status clientcomms.ConnectionStatus) {
			if status == clientcomms.Disconnected {
				disconnect()
			}
		},
		Logger:               dex.StdOutLogger(fmt.Sprintf("CL[%s]", n.URL), dex.LevelDebug),
		DisableAutoReconnect: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to bootnode %q: %v", n.URL, err)
	}

	cm := dex.NewConnectionMaster(cl)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to boot node %q. error: %w", n.URL, err)
	}

	sender := newWsConnWrapper(cl, cm)
	go s.handleOutboudTatankaConnection(ctx, sender, handleMessage)

	return sender, nil
}

func (s *Server) handleOutboudTatankaConnection(ctx context.Context, cl *wsConnWrapper, handleMessage func(cl tanka.Sender, msg *msgjson.Message)) {
	msgs := cl.MessageSource()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case msg, ok := <-msgs:
			if !ok {
				// Connection has been closed.
				return
			}
			handleMessage(cl, msg)
		case <-ctx.Done():
			return
		}
	}
}
