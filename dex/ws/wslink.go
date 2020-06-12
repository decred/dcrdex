// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

// outBufferSize is the size of the WSLink's buffered channel for outgoing
// messages.
const outBufferSize = 128

const writeWait = 5 * time.Second

// websocket.Upgrader is the preferred method of upgrading a request to a
// websocket connection.
var upgrader = websocket.Upgrader{}

// Error is just a basic error.
type Error string

// Error satisfies the error interface.
func (e Error) Error() string {
	return string(e)
}

// ErrPeerDisconnected will be returned if Send or Request is called on a
// disconnected link.
const ErrPeerDisconnected = Error("peer disconnected")

// Connection represents a websocket connection to a remote peer. In practice,
// it is satisfied by *websocket.Conn. For testing, a stub can be used.
type Connection interface {
	Close() error

	SetReadDeadline(t time.Time) error
	ReadMessage() (int, []byte, error)

	SetWriteDeadline(t time.Time) error
	WriteMessage(int, []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
}

// WSLink is the local, per-connection representation of a DEX peer (client or
// server) connection.
type WSLink struct {
	// ip is the peer's IP address.
	ip string
	// conn is the gorilla websocket.Conn, or a stub for testing.
	conn Connection
	// on is used internally to prevent multiple Close calls on the underlying
	// connections.
	on uint32
	// quit is used to cancel the Context.
	quit context.CancelFunc
	// stopped is closed when quit is called.
	stopped chan struct{}
	// outChan is used to sequence sent messages.
	outChan chan *sendData
	// The WSLink has at least 3 goroutines, one for read, one for write, and
	// one server goroutine to monitor for peer disconnection. The WaitGroup is
	// used to synchronize cleanup on disconnection.
	wg sync.WaitGroup
	// A master message handler.
	handler func(*msgjson.Message) *msgjson.Error
	// pingPeriod is how often to ping the peer.
	pingPeriod time.Duration
}

type sendData struct {
	data []byte
	ret  chan<- error
}

// NewWSLink is a constructor for a new WSLink.
func NewWSLink(addr string, conn Connection, pingPeriod time.Duration, handler func(*msgjson.Message) *msgjson.Error) *WSLink {
	return &WSLink{
		ip:         addr,
		conn:       conn,
		outChan:    make(chan *sendData, outBufferSize),
		pingPeriod: pingPeriod,
		handler:    handler,
	}
}

// Send sends the passed Message to the websocket peer. The actual writing of
// the message on the peer's link occurs asynchronously. As such, a nil error
// only indicates that the link is believed to be up and the message was
// successfully marshalled.
func (c *WSLink) Send(msg *msgjson.Message) error {
	return c.send(msg, nil)
}

// SendNow is like send, but it waits for the message to be written on the
// peer's link, returning any error from the write.
func (c *WSLink) SendNow(msg *msgjson.Message) error {
	writeErrChan := make(chan error, 1)
	if err := c.send(msg, writeErrChan); err != nil {
		return err
	}
	return <-writeErrChan
}

func (c *WSLink) send(msg *msgjson.Message, writeErr chan<- error) error {
	if c.Off() {
		return ErrPeerDisconnected
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// NOTE: Without the stopped chan or access to the Context we are now racing
	// after the c.Off check above.
	select {
	case c.outChan <- &sendData{b, writeErr}:
	case <-c.stopped:
		return ErrPeerDisconnected
	}

	return nil
}

// SendError sends the msgjson.Error to the peer.
func (c *WSLink) SendError(id uint64, rpcErr *msgjson.Error) {
	msg, err := msgjson.NewResponse(id, nil, rpcErr)
	if err != nil {
		log.Errorf("SendError: failed to create message: %v", err)
	}
	err = c.Send(msg)
	if err != nil {
		log.Debug("SendError: failed to send message to peer %s: %v", c.ip, err)
	}
}

// Connect begins processing input and output messages.
func (c *WSLink) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Set the initial read deadline now that the ping ticker is about to be
	// started. The pong handler will set subsequent read deadlines. 2x ping
	// period is a very generous initial pong wait; the readWait provided to
	// NewConnection could be stored and used here (once) instead.
	if !atomic.CompareAndSwapUint32(&c.on, 0, 1) {
		return nil, fmt.Errorf("Attempted to Start a running WSLink")
	}
	linkCtx, quit := context.WithCancel(ctx)
	// Note that there is a brief window where c.on is true but quit and stopped
	// are not set.
	c.quit = quit
	c.stopped = make(chan struct{}) // control signal to block send
	err := c.conn.SetReadDeadline(time.Now().Add(c.pingPeriod * 2))
	if err != nil {
		return nil, fmt.Errorf("Failed to set initial read deadline for %v: %v", c.ip, err)
	}

	log.Tracef("Starting websocket messaging with peer %s", c.ip)
	// Start processing input and output.
	c.wg.Add(3)
	go c.inHandler(linkCtx)
	go c.outHandler(linkCtx)
	go c.pingHandler(linkCtx)
	return &c.wg, nil
}

func (c *WSLink) stop() bool {
	// Flip the switch into the off position and cancel the context.
	if !atomic.CompareAndSwapUint32(&c.on, 1, 0) {
		return false
	}
	// Signal to senders we are done.
	close(c.stopped)
	// Begin shutdown of goroutines, and ultimately connection closure.
	c.quit()
	return true
}

// Disconnect begins shutdown of the WSLink, preventing new messages from
// entering the outgoing queue, and ultimately closing the underlying connection
// when all queued messages have been handled. This shutdown process is complete
// when the WaitGroup returned by Connect is Done.
func (c *WSLink) Disconnect() {
	// Cancel the Context and close the stopped channel if not already done.
	if !c.stop() {
		log.Debugf("Disconnect attempted on stopped WSLink.")
		return
	}
	// NOTE: outHandler closes the c.conn on its return.
}

// inHandler handles all incoming messages for the websocket connection. It must
// be run as a goroutine.
func (c *WSLink) inHandler(ctx context.Context) {
	// Ensure the connection is closed.
	defer c.wg.Done()
	defer c.stop()
out:
	for {
		// Quit when the context is closed.
		if ctx.Err() != nil {
			break out
		}
		// Block until a message is received or an error occurs.
		_, msgBytes, err := c.conn.ReadMessage()
		if err != nil {
			// Log the error if it's not due to disconnecting.
			if !websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				log.Errorf("Websocket receive error from peer %s: %v", c.ip, err)
			}
			break out
		}
		// Attempt to unmarshal the request. Only requests that successfully decode
		// will be accepted by the server, though failure to decode does not force
		// a disconnect.
		msg := new(msgjson.Message)
		err = json.Unmarshal(msgBytes, msg)
		if err != nil {
			c.SendError(1, msgjson.NewError(msgjson.RPCParseError,
				"Failed to parse message: "+err.Error()))
			continue
		}
		if msg.ID == 0 {
			c.SendError(1, msgjson.NewError(msgjson.RPCParseError, "request id cannot be zero"))
			continue
		}
		rpcErr := c.handler(msg)
		if rpcErr != nil {
			// TODO: figure out how to fix this not making sense when the msg is
			// a response, not a request!
			c.SendError(msg.ID, rpcErr)
		}
	}
}

func (c *WSLink) outHandler(ctx context.Context) {
	// Ensure the connection is closed.
	defer c.wg.Done()
	defer c.conn.Close() // close the Conn
	defer c.stop()       // in the event of context cancellation vs Disconnect call

	// Synchronize access to the output queue and the trigger channel.
	var mtx sync.Mutex
	outQueue := make([]*sendData, 0, 128)
	// buffer length 1 since the writer loop triggers itself.
	trigger := make(chan struct{}, 1)

	// Relay a write error to senders waiting for one.
	relayError := func(errChan chan<- error, err error) {
		if errChan != nil {
			errChan <- err
		}
	}

	write := func(sd *sendData) {
		c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		err := c.conn.WriteMessage(websocket.TextMessage, sd.data)
		if err != nil {
			relayError(sd.ret, err)
			// No more Sends should queue messages, and goroutines should return
			// gracefully.
			c.stop()
			return
		}
		if sd.ret != nil {
			close(sd.ret)
		}
	}

	// On shutdown, process any queued senders before closing the connection, if
	// it is still up.
	defer func() {
		log.Infof("Shutting down link for %v with %v queued messages.", c.ip, len(outQueue))
		// Drain the buffered channel of data sent prior to stop, but before it
		// could be put in the outQueue.
	out:
		for {
			select {
			case sd := <-c.outChan:
				outQueue = append(outQueue, sd)
			default:
				break out
			}
		}
		// Attempt sending all queued outgoing messages.
		for _, sd := range outQueue {
			write(sd)
		}
		// NOTE: This also addresses a full trigger channel, but their is no
		// need to drain it, just the outQueue so SendNow never hangs.
	}()

	// Top of defer stack: before clean-up, wait for writer goroutine
	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case <-trigger:
				mtx.Lock()
				// pop front
				sd := outQueue[0]
				//outQueue[0] = nil // allow realloc w/o this element
				//outQueue = outQueue[1:] // reduces length *and* capacity, but no copy now
				// Or, to reduce or eliminate reallocs at the expense of frequent copies:
				copy(outQueue, outQueue[1:])
				outQueue[len(outQueue)-1] = nil
				outQueue = outQueue[:len(outQueue)-1]
				if len(outQueue) > 0 {
					trigger <- struct{}{}
				}
				// len(outQueue) may be longer when we get back here, but only
				// this loop reduces it.
				mtx.Unlock()

				write(sd)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case sd := <-c.outChan:
			mtx.Lock()
			// push back
			outQueue = append(outQueue, sd)
			// If we just repopulated an empty queue, trigger the writer,
			// otherwise the writer will trigger itself until the queue is
			// empty.
			if len(outQueue) == 1 {
				trigger <- struct{}{}
			} // else, len>1 and writer will self trigger
			mtx.Unlock()
		}
	}
}

// pingHandler sends periodic pings to the client.
func (c *WSLink) pingHandler(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.pingPeriod)
	ping := []byte{}
out:
	for {
		// Send any messages ready for send until the quit channel is
		// closed.
		select {
		case <-ticker.C:
			err := c.conn.WriteControl(websocket.PingMessage, ping, time.Now().Add(writeWait))
			if err != nil {
				c.stop()
				// Don't really care what the error is, but log it at debug level.
				log.Debugf("WriteMessage ping error: %v", err)
				break out
			}
		case <-ctx.Done():
			break out
		}
	}

	log.Tracef("Websocket output handler done for peer %s", c.ip)
}

// Off will return true if the link has disconnected.
func (c *WSLink) Off() bool {
	return atomic.LoadUint32(&c.on) == 0
}

// IP is the peer address passed to the constructor.
func (c *WSLink) IP() string {
	return c.ip
}

// NewConnection creates a new Connection by upgrading the http request to a
// websocket.
func NewConnection(w http.ResponseWriter, r *http.Request, readTimeout time.Duration) (Connection, error) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		var hsErr websocket.HandshakeError
		if errors.As(err, &hsErr) {
			log.Errorf("Unexpected websocket error: %v",
				err)
		}
		// TODO: eliminate this http.Error since upgrader.Upgrade already calls
		// http.Error in many paths where err!=nil and it is possible to write.
		http.Error(w, "400 Bad Request.", http.StatusBadRequest)
		return nil, err
	}
	// Configure the pong handler.
	reqAddr := r.RemoteAddr
	ws.SetPongHandler(func(string) error {
		log.Tracef("got pong from %v", reqAddr)
		return ws.SetReadDeadline(time.Now().Add(readTimeout))
	})

	// Do not set an initial read deadline until pinging begins.

	return ws, nil
}
