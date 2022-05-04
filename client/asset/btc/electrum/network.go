// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package electrum provides a client for an ElectrumX server. Not all methods
// are implemented. For the methods and their request and response types, see
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html.
package electrum

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/go-socks/socks"
)

// Printer is a function with the signature of a logger method.
type Printer func(format string, params ...interface{})

var (
	// StdoutPrinter is a DebugLogger that uses fmt.Printf.
	StdoutPrinter = Printer(func(format string, params ...interface{}) {
		fmt.Printf(format+"\n", params...) // discard the returns
	})
	// StderrPrinter is a DebugLogger that uses fmt.Fprintf(os.Stderr, ...).
	StderrPrinter = Printer(func(format string, params ...interface{}) {
		fmt.Fprintf(os.Stderr, format+"\n", params...)
	})

	disabledPrinter = Printer(func(string, ...interface{}) {})
)

const pingInterval = 10 * time.Second

// ServerConn represents a connection to an Electrum server e.g. ElectrumX. It
// is a single use type that must be replaced if the connection is lost. Use
// ConnectServer to construct a ServerConn and connect to the server.
type ServerConn struct {
	conn   net.Conn
	cancel context.CancelFunc
	done   chan struct{}
	proto  string
	debug  Printer

	reqID uint64

	respHandlersMtx sync.Mutex
	respHandlers    map[uint64]chan *response // reqID => requestor

	ntfnHandlersMtx sync.RWMutex
	ntfnHandlers    map[string][]chan []byte // method => subscribers
}

func (sc *ServerConn) nextID() uint64 {
	return atomic.AddUint64(&sc.reqID, 1)
}

const newline = byte('\n')

func (sc *ServerConn) listen(ctx context.Context) {
	// listen is charged with sending on the response and notification channels.
	// As such, only listen should close these channels, and only after the read
	// loop has finished.
	defer sc.cancelRequests()      // close the response chans
	defer sc.deleteSubscriptions() // close the ntfn chans

	reader := bufio.NewReader(io.LimitReader(sc.conn, 1<<18))

	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := reader.ReadBytes(newline)
		if err != nil {
			if ctx.Err() == nil { // unexpected
				sc.debug("ReadBytes: %v", err)
			}
			sc.cancel()
			return
		}

		var jsonResp response
		err = json.Unmarshal(msg, &jsonResp)
		if err != nil {
			sc.debug("response Unmarshal error: %v", err)
			continue
		}

		if jsonResp.Method != "" { // notification
			var ntfnParams ntfnData // the ntfn payload is in the params field of a request object (!)
			err = json.Unmarshal(msg, &ntfnParams)
			if err != nil {
				sc.debug("notification Unmarshal error: %v", err)
				continue
			}
			for _, c := range sc.subChans(jsonResp.Method) {
				select {
				case c <- ntfnParams.Params:
				default: // non-blocking, but consider deleting sub and closing chan
				}
			}
			continue
		}
		// sc.debug(string(msg))

		c := sc.responseChan(jsonResp.ID)
		if c == nil {
			sc.debug("Received response for unknown request ID %d", jsonResp.ID)
			continue
		}
		c <- &jsonResp // buffered and single use => cannot block
	}
}

func (sc *ServerConn) pinger(ctx context.Context) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()

	for {
		// listen => ReadBytes cannot wait forever. Reset the read deadline for
		// the next ping's response, as the ping loop is running.
		err := sc.conn.SetReadDeadline(time.Now().Add(pingInterval * 5 / 4))
		if err != nil {
			sc.debug("SetReadDeadline: %v", err) // just dropped conn, but for debugging...
			sc.cancel()
			return
		}
		if err = sc.Ping(ctx); err != nil {
			sc.debug("Ping: %v", err)
			sc.cancel()
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// negotiateVersion should only be called once, and before starting the listen
// read loop. As such, this does not use the Request method.
func (sc *ServerConn) negotiateVersion() (string, error) {
	reqMsg, err := prepareRequest(sc.nextID(), "server.version", positional{"Electrum", "1.4"})
	if err != nil {
		return "", err
	}
	reqMsg = append(reqMsg, newline)

	if err = sc.send(reqMsg); err != nil {
		return "", err
	}

	err = sc.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(io.LimitReader(sc.conn, 1<<18))
	msg, err := reader.ReadBytes(newline)
	if err != nil {
		return "", err
	}

	var jsonResp response
	err = json.Unmarshal(msg, &jsonResp)
	if err != nil {
		return "", err
	}

	var vers []string // [server_software_version, protocol_version]
	err = json.Unmarshal(jsonResp.Result, &vers)
	if err != nil {
		return "", err
	}
	if len(vers) != 2 {
		return "", fmt.Errorf("unexpected version response: %v", vers)
	}
	return vers[1], nil
}

type ConnectOpts struct {
	TLSConfig   *tls.Config // nil means plain
	TorProxy    string
	DebugLogger Printer
}

// ConnectServer connects to the electrum server at the given address. To close
// the connection and shutdown ServerConn, either cancel the context or use the
// Shutdown method, then wait on the channel from Done() to ensure a clean
// shutdown (connection closed and all requests handled). There is no automatic
// reconnection functionality, as the caller should handle dropped connections
// by potentially cycling to a different server.
func ConnectServer(ctx context.Context, addr string, opts *ConnectOpts) (*ServerConn, error) {
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	if opts.TorProxy != "" {
		proxy := &socks.Proxy{
			Addr: opts.TorProxy,
		}
		dial = proxy.DialContext
	} else {
		dial = new(net.Dialer).DialContext
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := dial(dialCtx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	if opts.TLSConfig != nil {
		conn = tls.Client(conn, opts.TLSConfig)
		err = conn.(*tls.Conn).HandshakeContext(ctx)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	logger := opts.DebugLogger
	if logger == nil {
		logger = disabledPrinter
	}

	sc := &ServerConn{
		conn:         conn,
		done:         make(chan struct{}),
		debug:        logger,
		respHandlers: make(map[uint64]chan *response),
		ntfnHandlers: make(map[string][]chan []byte),
	}

	// Wrap the context with a cancel function for internal shutdown, and so the
	// user can use Shutdown, instead of cancelling the parent context.
	ctx, sc.cancel = context.WithCancel(ctx)

	// Negotiate protocol version.
	sc.proto, err = sc.negotiateVersion()
	if err != nil {
		conn.Close()
		return nil, err // e.g. code 1: "unsupported protocol version: 1.4"
	}

	sc.debug("Connected to server %s using negotiated protocol version %s",
		addr, sc.proto)

	go sc.listen(ctx) // must be running to receive response
	go sc.pinger(ctx)

	go func() {
		<-ctx.Done()
		conn.Close()
		close(sc.done)
	}()

	return sc, nil
}

// Proto returns the electrum protocol of the connected server. e.g. "1.4.2".
func (sc *ServerConn) Proto() string {
	return sc.proto
}

// Shutdown begins shutting down the connection and request handling goroutines.
// Receive on the channel from Done() to wait for shutdown to complete.
func (sc *ServerConn) Shutdown() {
	sc.cancel()
}

// Done returns a channel that is closed when the ServerConn is fully shutdown.
func (sc *ServerConn) Done() <-chan struct{} {
	return sc.done
}

func (sc *ServerConn) send(msg []byte) error {
	err := sc.conn.SetWriteDeadline(time.Now().Add(7 * time.Second))
	if err != nil {
		return err
	}
	_, err = sc.conn.Write(msg)
	return err
}

func (sc *ServerConn) registerRequest(id uint64) chan *response {
	c := make(chan *response, 1)
	sc.respHandlersMtx.Lock()
	sc.respHandlers[id] = c
	sc.respHandlersMtx.Unlock()
	return c
}

func (sc *ServerConn) responseChan(id uint64) chan *response {
	sc.respHandlersMtx.Lock()
	defer sc.respHandlersMtx.Unlock()
	c := sc.respHandlers[id]
	delete(sc.respHandlers, id)
	return c
}

// cancelRequests deletes all response handlers from the respHandlers map and
// closes all of the channels. As such, this method MUST be called from the same
// goroutine that sends on the channel.
func (sc *ServerConn) cancelRequests() {
	sc.respHandlersMtx.Lock()
	defer sc.respHandlersMtx.Unlock()
	for id, c := range sc.respHandlers {
		close(c) // requester receives nil immediately
		delete(sc.respHandlers, id)
	}
}

func (sc *ServerConn) registerSub(method string) <-chan []byte {
	c := make(chan []byte, 1)
	sc.ntfnHandlersMtx.Lock()
	sc.ntfnHandlers[method] = append(sc.ntfnHandlers[method], c)
	sc.ntfnHandlersMtx.Unlock()
	return c
}

func (sc *ServerConn) subChans(method string) []chan []byte {
	sc.ntfnHandlersMtx.RLock()
	defer sc.ntfnHandlersMtx.RUnlock()
	return sc.ntfnHandlers[method]
}

// deleteSubscriptions deletes all subscriptions from the ntfnHandlers map and
// closes all of the channels. As such, this method MUST be called from the same
// goroutine that sends on the channel.
func (sc *ServerConn) deleteSubscriptions() {
	sc.ntfnHandlersMtx.Lock()
	defer sc.ntfnHandlersMtx.Unlock()
	for method, cs := range sc.ntfnHandlers {
		for _, c := range cs {
			close(c) // sub handler loop receives nil immediately
		}
		delete(sc.ntfnHandlers, method)
	}
}

// Request performs a request to the remote server for the given method using
// the provided arguments, which may either be positional (e.g.
// []interface{arg1, arg2}), named (any struct), or nil if there are no
// arguments. args may not be any other basic type. The the response does not
// include an error, the result will be unmarshalled into result, unless the
// provided result is nil in which case the response payload will be ignored.
func (sc *ServerConn) Request(ctx context.Context, method string, args interface{}, result interface{}) error {
	id := sc.nextID()
	reqMsg, err := prepareRequest(id, method, args)
	if err != nil {
		return err
	}
	reqMsg = append(reqMsg, newline)

	c := sc.registerRequest(id)

	if err = sc.send(reqMsg); err != nil {
		sc.cancel()
		return err
	}

	var resp *response
	select {
	case <-ctx.Done():
		return ctx.Err() // either timeout or canceled
	case resp = <-c:
	}

	if resp == nil { // channel closed
		return errors.New("connection terminated")
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// Ping pings the remote server. This can be used as a connectivity test on
// demand, although a ServerConn started with ConnectServer will launch a pinger
// goroutine to keep the connection alive.
func (sc *ServerConn) Ping(ctx context.Context) error {
	return sc.Request(ctx, "server.ping", nil, nil)
}

// Banner retrieves the server's banner, which is any announcement set by the
// server operator. It should be interpreted with caution as the content is
// untrusted.
func (sc *ServerConn) Banner(ctx context.Context) (string, error) {
	var resp string
	err := sc.Request(ctx, "server.banner", nil, &resp)
	if err != nil {
		return "", err
	}
	return resp, nil
}

// ServerFeatures represents the result of a server features requests.
type ServerFeatures struct {
	Genesis  string                       `json:"genesis_hash"`
	Hosts    map[string]map[string]uint32 `json:"hosts"` // e.g. {"host.com": {"tcp_port": 51001, "ssl_port": 51002}}, may be unset!
	ProtoMax string                       `json:"protocol_max"`
	ProtoMin string                       `json:"protocol_min"`
	Pruning  interface{}                  `json:"pruning,omitempty"` // supposedly an integer, but maybe a string or even JSON null
	Version  string                       `json:"server_version"`    // server software version, not proto
	HashFunc string                       `json:"hash_function"`     // e.g. sha256
	// Services []string                     `json:"services,omitempty"` // e.g. ["tcp://host.com:51001", "ssl://host.com:51002"]
}

// Features requests the features claimed by the server. The caller should check
// the Genesis hash field to ensure it is the intended network.
func (sc *ServerConn) Features(ctx context.Context) (*ServerFeatures, error) {
	var feats ServerFeatures
	err := sc.Request(ctx, "server.features", nil, &feats)
	if err != nil {
		return nil, err
	}
	return &feats, nil
}

// PeersResult represents the results of a peers server request.
type PeersResult struct {
	Addr  string // IP address or .onion name
	Host  string
	Feats []string
}

// Peers requests the known peers from a server (other servers). See
// SSLPeerAddrs to assist parsing useable peers.
func (sc *ServerConn) Peers(ctx context.Context) ([]*PeersResult, error) {
	// Note that the Electrum exchange wallet type does not currently use this
	// method since it follows the Electrum wallet server peer or one of the
	// wallets other servers. See (*electrumWallet).connect and
	// (*WalletClient).GetServers. We might wish to in the future though.

	// [["ip", "host", ["featA", "featB", ...]], ...]
	// [][]interface{}{string, string, []interface{}{string, ...}}
	var resp [][]interface{}
	err := sc.Request(ctx, "server.peers.subscribe", nil, &resp) // not really a subscription!
	if err != nil {
		return nil, err
	}
	peers := make([]*PeersResult, 0, len(resp))
	for _, peer := range resp {
		if len(peer) != 3 {
			sc.debug("bad peer data: %v (%T)", peer, peer)
			continue
		}
		addr, ok := peer[0].(string)
		if !ok {
			sc.debug("bad peer IP data: %v (%T)", peer[0], peer[0])
			continue
		}
		host, ok := peer[1].(string)
		if !ok {
			sc.debug("bad peer hostname: %v (%T)", peer[1], peer[1])
			continue
		}
		featsI, ok := peer[2].([]interface{})
		if !ok {
			sc.debug("bad peer feature data: %v (%T)", peer[2], peer[2])
			continue
		}
		feats := make([]string, len(featsI))
		for i, featI := range featsI {
			feat, ok := featI.(string)
			if !ok {
				sc.debug("bad peer feature data: %v (%T)", featI, featI)
				continue
			}
			feats[i] = feat
		}
		peers = append(peers, &PeersResult{
			Addr:  addr,
			Host:  host,
			Feats: feats,
		})
	}
	return peers, nil
}

// SSLPeerAddrs filters the peers slice and returns the addresses in a
// "host:port" format in separate slices for SSL-enabled servers and optionally
// TCP-only hidden services (.onion host names). Note that if requesting to
// include onion hosts, the SSL slice may include onion hosts that also use SSL.
func SSLPeerAddrs(peers []*PeersResult, includeOnion bool) (ssl, tcpOnlyOnion []string) {
peerloop:
	for _, peer := range peers {
		isOnion := strings.HasSuffix(peer.Addr, ".onion")
		if isOnion && !includeOnion {
			continue
		}
		var tcpOnion string // host to accept if no ssl
		for _, feat := range peer.Feats {
			// We require a port set after the transport letter. The default
			// port depends on the asset network, so we could consider providing
			// that as an input in the future, but most servers set a port.
			if len(feat) < 2 {
				continue
			}
			switch []rune(feat)[0] {
			case 't':
				if !isOnion {
					continue
				}
				port := feat[1:]
				if _, err := strconv.Atoi(port); err != nil {
					continue
				}
				tcpOnion = net.JoinHostPort(peer.Host, port) // hang onto this if there is no ssl
			case 's':
				port := feat[1:]
				if _, err := strconv.Atoi(port); err != nil {
					continue
				}
				addr := net.JoinHostPort(peer.Host, port)
				ssl = append(ssl, addr) // we know the first rune is one byte
				continue peerloop
			}
		}
		if tcpOnion != "" {
			tcpOnlyOnion = append(tcpOnlyOnion, tcpOnion)
		}
	}
	return
}

// SigScript represents the signature script in a Vin returned by a transaction
// request.
type SigScript struct {
	Asm string `json:"asm"` // this is not the sigScript you're looking for
	Hex string `json:"hex"`
}

// Vin represents a transaction input in a requested transaction.
type Vin struct {
	TxID      string     `json:"txid"`
	Vout      uint32     `json:"vout"`
	SigScript *SigScript `json:"scriptsig"`
	Witness   []string   `json:"txinwitness,omitempty"`
	Sequence  uint32     `json:"sequence"`
	Coinbase  string     `json:"coinbase,omitempty"`
}

// PkScript represents the pkScript/scriptPubKey of a transaction output
// returned by a transaction requests.
type PkScript struct {
	Asm       string   `json:"asm"`
	Hex       string   `json:"hex"`
	ReqSigs   uint32   `json:"reqsigs"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses,omitempty"`
}

// Vout represents a transaction output in a requested transaction.
type Vout struct {
	Value    float64  `json:"value"`
	N        uint32   `json:"n"`
	PkScript PkScript `json:"scriptpubkey"`
}

// GetTransactionResult is the data returned by a transaction request.
type GetTransactionResult struct {
	TxID string `json:"txid"`
	// Hash          string `json:"hash"` // ??? don't use, not always the txid! witness not stripped?
	Version       uint32 `json:"version"`
	Size          uint32 `json:"size"`
	VSize         uint32 `json:"vsize"`
	Weight        uint32 `json:"weight"`
	LockTime      uint32 `json:"locktime"`
	Hex           string `json:"hex"`
	Vin           []Vin  `json:"vin"`
	Vout          []Vout `json:"vout"`
	BlockHash     string `json:"blockhash,omitempty"`
	Confirmations int32  `json:"confirmations,omitempty"` // probably uint32 ok because it seems to be omitted, but could be -1?
	Time          int64  `json:"time,omitempty"`
	BlockTime     int64  `json:"blocktime,omitempty"` // same as Time?
	// Merkel // proto 1.5+
}

// GetTransaction requests a transaction.
func (sc *ServerConn) GetTransaction(ctx context.Context, txid string) (*GetTransactionResult, error) {
	var resp GetTransactionResult
	err := sc.Request(ctx, "blockchain.transaction.get", positional{txid, true}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// BlockHeader requests the block header at the given height, returning
// hexadecimal encoded serialized header.
func (sc *ServerConn) BlockHeader(ctx context.Context, height uint32) (string, error) {
	var resp string
	err := sc.Request(ctx, "blockchain.block.header", positional{height}, &resp)
	if err != nil {
		return "", err
	}
	return resp, nil
}

// GetBlockHeadersResult represent the result of a batch request for block
// headers via the BlockHeaders method. The serialized block headers are
// concatenated in the HexConcat field, which contains Count headers.
type GetBlockHeadersResult struct {
	Count     uint32 `json:"count"`
	HexConcat string `json:"hex"`
	Max       uint32 `json:"max"`
}

// BlockHeaders requests a batch of block headers beginning at the given height.
// The sever may respond with a different number of headers, so the caller
// should check the Count field of the result.
func (sc *ServerConn) BlockHeaders(ctx context.Context, startHeight, count uint32) (*GetBlockHeadersResult, error) {
	var resp GetBlockHeadersResult
	err := sc.Request(ctx, "blockchain.block.headers", positional{startHeight, count}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// SubscribeHeadersResult is the contents of a block header notification.
type SubscribeHeadersResult struct {
	Height int32  `json:"height"`
	Hex    string `json:"hex"`
}

// SubscribeHeaders subscribes for block header notifications. There seems to be
// no guarantee that we will be notified of all new blocks, such as when there
// are blocks in rapid succession.
func (sc *ServerConn) SubscribeHeaders(ctx context.Context) (*SubscribeHeadersResult, <-chan *SubscribeHeadersResult, error) {
	const method = "blockchain.headers.subscribe"
	c := sc.registerSub(method)

	var resp SubscribeHeadersResult
	err := sc.Request(ctx, method, nil, &resp)
	if err != nil {
		return nil, nil, err
	}

	ntfnChan := make(chan *SubscribeHeadersResult, 10)

	go func() {
		defer close(ntfnChan)

		for data := range c {
			var res []*SubscribeHeadersResult
			err = json.Unmarshal(data, &res)
			if err != nil {
				sc.debug("SubscribeHeaders - unmarshal ntfn data: %v", err)
				continue
			}

			for _, r := range res { // should just be one, but the params are a slice...
				ntfnChan <- r
			}
		}
	}()

	return &resp, ntfnChan, nil
}
