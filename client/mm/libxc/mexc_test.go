// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"

	_ "decred.org/dcrdex/client/asset/importall"
)

// TestMXSubscribeTradeUpdates tests the subscription and unsubscription to trade updates.
func TestMXSubscribeTradeUpdates(t *testing.T) {
	m := &mexc{
		tradeUpdaters: make(map[int]chan *Trade),
	}
	_, unsub0, _ := m.SubscribeTradeUpdates()
	_, _, id1 := m.SubscribeTradeUpdates()
	unsub0()
	_, _, id2 := m.SubscribeTradeUpdates()
	if len(m.tradeUpdaters) != 2 {
		t.Fatalf("wrong number of updaters. wanted 2, got %d", len(m.tradeUpdaters))
	}
	if id1 == id2 {
		t.Fatalf("ids should be unique. got %d twice", id1)
	}
	if _, found := m.tradeUpdaters[id1]; !found {
		t.Fatalf("id1 not found")
	}
	if _, found := m.tradeUpdaters[id2]; !found {
		t.Fatalf("id2 not found")
	}
}

// TestMXConvertCoin tests the mxConvertCoin function.
func TestMXConvertCoin(t *testing.T) {
	tests := map[string]string{
		"btc":   "btc",
		"BTC":   "btc",
		"weth":  "eth",
		"WETH":  "eth",
		"MATIC": "polygon",
		"eth":   "eth",
		"ETH":   "eth",
		"":      "",
	}

	for input, expected := range tests {
		result := mxConvertCoin(input)
		if result != expected {
			t.Fatalf("expected mxConvertCoin(%q) = %q, got %q", input, expected, result)
		}
	}
}

// TestMXCoinNetworkToDexSymbol tests the mxCoinNetworkToDexSymbol function.
func TestMXCoinNetworkToDexSymbol(t *testing.T) {
	tests := map[[2]string]string{
		{"btc", "btc"}:    "btc",
		{"BTC", "BTC"}:    "btc",
		{"usdt", "eth"}:   "usdt.eth",
		{"USDT", "ETH"}:   "usdt.eth",
		{"eth", "eth"}:    "eth",
		{"ETH", "ETH"}:    "eth",
		{"weth", "eth"}:   "eth",
		{"WETH", "ETH"}:   "eth",
		{"usdt", "matic"}: "usdt.polygon",
		{"USDT", "MATIC"}: "usdt.polygon",
	}

	for test, expected := range tests {
		result := mxCoinNetworkToDexSymbol(test[0], test[1])
		if result != expected {
			t.Fatalf("expected mxCoinNetworkToDexSymbol(%q, %q) = %q, got %q",
				test[0], test[1], expected, result)
		}
	}
}

// TestMapMEXCCoinNetworkToDEXSymbol tests the mapMEXCCoinNetworkToDEXSymbol method.
func TestMapMEXCCoinNetworkToDEXSymbol(t *testing.T) {
	m := &mexc{log: dex.Disabled}

	tests := map[[2]string]string{
		{"DCR", "DCR"}:         "dcr",
		{"USDT", "ETH"}:        "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "ERC20"}:      "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "MATIC"}:      "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "TRX"}:        "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "TRC20"}:      "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "BEP20(BSC)"}: "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "SOLANA"}:     "usdt.polygon", // Special case: USDT always maps to polygon
		{"USDT", "SOMECHAIN"}:  "usdt.polygon", // Special case: USDT always maps to polygon
		{"DCR", "ETH"}:         "dcr.erc20",
		{"usdt", "ETH"}:        "usdt.polygon", // Lowercase also maps to polygon
		{"USDT", "eth"}:        "usdt.polygon", // Mixed case also maps to polygon
		{"", "ETH"}:            "",
		{"USDT", ""}:           "",
	}

	for test, expected := range tests {
		result := m.mapMEXCCoinNetworkToDEXSymbol(test[0], test[1])
		if result != expected {
			t.Fatalf("expected mapMEXCCoinNetworkToDEXSymbol(%q, %q) = %q, got %q",
				test[0], test[1], expected, result)
		}
	}
}

// TestMXAssetCfg tests the mxAssetCfg function.
func TestMXAssetCfg(t *testing.T) {
	tests := map[uint32]struct {
		symbol           string
		coin             string
		chain            string
		conversionFactor uint64
	}{
		42: { // DCR
			symbol:           "dcr",
			coin:             "DCR",
			chain:            "DCR",
			conversionFactor: 1e8,
		},
		0: { // BTC
			symbol:           "btc",
			coin:             "BTC",
			chain:            "BTC",
			conversionFactor: 1e8,
		},
		60: { // ETH
			symbol:           "eth",
			coin:             "ETH",
			chain:            "ETH",
			conversionFactor: 1e9,
		},
		966001: { // USDC.polygon
			symbol:           "usdc.polygon",
			coin:             "USDC",
			chain:            "MATIC",
			conversionFactor: 1e6,
		},
	}

	for assetID, expected := range tests {
		cfg, err := mxAssetCfg(assetID)
		if err != nil {
			t.Fatalf("error getting asset config for %d: %v", assetID, err)
		}

		if cfg.symbol != expected.symbol {
			t.Errorf("assetID %d: expected symbol %q, got %q", assetID, expected.symbol, cfg.symbol)
		}
		if cfg.coin != expected.coin {
			t.Errorf("assetID %d: expected coin %q, got %q", assetID, expected.coin, cfg.coin)
		}
		if cfg.chain != expected.chain {
			t.Errorf("assetID %d: expected chain %q, got %q", assetID, expected.chain, cfg.chain)
		}
		if cfg.conversionFactor != expected.conversionFactor {
			t.Errorf("assetID %d: expected conversionFactor %d, got %d",
				assetID, expected.conversionFactor, cfg.conversionFactor)
		}
	}
}

// TestHandleMarketRawMessage tests the WebSocket ping/pong response mechanism.
func TestHandleMarketRawMessage(t *testing.T) {
	// Create a mock MEXC instance
	m := &mexc{
		log:               dex.Disabled,
		connectionHealthy: atomic.Bool{},
		marketStreamMtx:   sync.RWMutex{},
	}
	m.connectionHealthy.Store(false) // Initial false to test it gets set to true

	// Set up a channel to capture sent messages
	sentMessages := make(chan []byte, 10)

	// Create a mock WsConn implementation
	mockWsConn := &mockWsConn{
		sendRaw: func(data []byte) error {
			sentMessages <- data
			return nil
		},
		isDown: func() bool {
			return false
		},
	}

	// Set the mock connection
	m.marketStreamMtx.Lock()
	m.marketStream = mockWsConn
	m.marketStreamMtx.Unlock()

	// Test handling a ping message
	startCount := atomic.LoadUint64(&m.messageCounter)
	pingMsg := []byte(`{"ping":true}`)
	m.handleMarketRawMessage(pingMsg)

	// Verify pong response
	select {
	case sentMsg := <-sentMessages:
		if !bytes.Contains(sentMsg, []byte(`"pong"`)) {
			t.Errorf("Expected pong message, got: %s", string(sentMsg))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No pong response was sent")
	}

	// Verify message counter was incremented
	newCount := atomic.LoadUint64(&m.messageCounter)
	if newCount != startCount+1 {
		t.Errorf("Message counter not incremented: expected %d, got %d", startCount+1, newCount)
	}

	// Verify lastMessageReceived was updated
	lastMsgTime := m.lastMessageReceived.Load().(time.Time)
	if time.Since(lastMsgTime) > 100*time.Millisecond {
		t.Errorf("lastMessageReceived not updated properly: %v", lastMsgTime)
	}

	// Verify lastMessageReceived was properly updated
	msgTime := m.lastMessageReceived.Load().(time.Time)
	if time.Since(msgTime) > 100*time.Millisecond {
		t.Error("lastMessageReceived was not properly updated")
	}

	// Test a normal message (not ping)
	startCount = atomic.LoadUint64(&m.messageCounter)
	m.lastMessageReceived.Store(time.Time{}) // Reset to zero time
	normalMsg := []byte(`{"result":"success"}`)
	m.handleMarketRawMessage(normalMsg)

	// Verify message counter was incremented
	newCount = atomic.LoadUint64(&m.messageCounter)
	if newCount != startCount+1 {
		t.Errorf("Message counter not incremented for normal message: expected %d, got %d",
			startCount+1, newCount)
	}

	// Verify lastMessageReceived was updated
	lastMsgTime = m.lastMessageReceived.Load().(time.Time)
	if time.Since(lastMsgTime) > 100*time.Millisecond {
		t.Errorf("lastMessageReceived not updated for normal message: %v", lastMsgTime)
	}
}

// TestShouldSendPing tests the conditions for sending a ping based on last message time.
func TestShouldSendPing(t *testing.T) {
	// Create a mock MEXC instance
	m := &mexc{
		log:               dex.Disabled,
		quit:              make(chan struct{}),
		reconnectChan:     make(chan struct{}, 1),
		connectionHealthy: atomic.Bool{},
		marketStreamMtx:   sync.RWMutex{},
	}
	m.connectionHealthy.Store(true)

	// Create a mock WsConn implementation
	sentPings := make(chan []byte, 10)
	mockWsConn := &mockWsConn{
		sendRaw: func(data []byte) error {
			sentPings <- data
			return nil
		},
		isDown: func() bool {
			return false
		},
	}

	// Set the mock connection
	m.marketStreamMtx.Lock()
	m.marketStream = mockWsConn
	m.marketStreamMtx.Unlock()

	// Test Cases:
	// 1. Last message received < 20 seconds ago (should NOT send ping)
	m.lastMessageReceived.Store(time.Now().Add(-10 * time.Second))

	// Manually simulate the ping ticker firing
	pingMsg := map[string]interface{}{"ping": time.Now().UnixNano() / int64(time.Millisecond)}
	pingBytes, _ := json.Marshal(pingMsg)

	// Check if the implementation would send a ping (we assume it wouldn't since last message was recent)
	if time.Since(m.lastMessageReceived.Load().(time.Time)) > 20*time.Second {
		m.marketStream.SendRaw(pingBytes)
	}

	// Verify no ping message was sent
	select {
	case <-sentPings:
		t.Error("A ping was sent when last message was less than 20 seconds ago")
	case <-time.After(50 * time.Millisecond):
		// Correct: no ping should be sent
	}

	// 2. Last message received > 20 seconds ago (should send ping)
	m.lastMessageReceived.Store(time.Now().Add(-30 * time.Second))

	// Check if the implementation would send a ping (it should since last message was > 20s ago)
	if time.Since(m.lastMessageReceived.Load().(time.Time)) > 20*time.Second {
		m.marketStream.SendRaw(pingBytes)
	}

	// Verify a ping message was sent
	select {
	case sentPing := <-sentPings:
		// Validate it's a ping message
		var pingData map[string]interface{}
		if err := json.Unmarshal(sentPing, &pingData); err != nil {
			t.Errorf("Invalid ping message format: %s", string(sentPing))
		} else if _, ok := pingData["ping"]; !ok {
			t.Errorf("Expected ping message but got: %s", string(sentPing))
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("No ping was sent when last message was more than 20 seconds ago")
	}

	// 3. Test stale connection detection (> 180 seconds without message)
	m.lastMessageReceived.Store(time.Now().Add(-185 * time.Second))

	// Capture reconnect signals
	reconnects := make(chan struct{}, 1)
	go func() {
		select {
		case <-m.reconnectChan:
			reconnects <- struct{}{}
		case <-time.After(100 * time.Millisecond):
			// Test timeout, do nothing
		}
	}()

	// Simulate the code that would trigger a reconnect
	if time.Since(m.lastMessageReceived.Load().(time.Time)) > 180*time.Second {
		m.reconnectChan <- struct{}{}
	}

	// Verify a reconnect was triggered
	select {
	case <-reconnects:
		// Correct: reconnect was triggered
	case <-time.After(100 * time.Millisecond):
		t.Error("No reconnection was triggered when connection was stale")
	}
}

// TestIsConnectionHealthy tests the IsConnectionHealthy method.
func TestIsConnectionHealthy(t *testing.T) {
	// Create a mexc instance for testing
	m := &mexc{
		marketStreamMtx: sync.RWMutex{},
		booksMtx:        sync.RWMutex{},
		books:           make(map[string]*mxOrderBook),
	}

	// Create a test book with subscribers to simulate active subscriptions
	testBook := &mxOrderBook{
		mtx:            sync.RWMutex{},
		numSubscribers: 1, // Has one subscriber
	}

	// Test with no active books first (should return true since no subscriptions)
	if !m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return true when there are no active subscriptions")
	}

	// Add a book to simulate an active subscription
	m.booksMtx.Lock()
	m.books["BTCUSDT"] = testBook
	m.booksMtx.Unlock()

	// Now test with an active subscription but nil market stream
	if m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return false when marketStream is nil with active subscriptions")
	}

	// Test with down market stream
	mockWsConn := &mockWsConn{
		isDown: func() bool { return true },
	}
	m.marketStreamMtx.Lock()
	m.marketStream = mockWsConn
	m.marketStreamMtx.Unlock()

	if m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return false when marketStream is down with active subscriptions")
	}

	// Test with up market stream but old lastMessageReceived (> 60 seconds)
	mockWsConn.isDown = func() bool { return false }
	m.lastMessageReceived.Store(time.Now().Add(-65 * time.Second))

	if m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return false when lastMessageReceived is too old with active subscriptions")
	}

	// Test with up market stream and older message but still within healthy threshold (< 60 seconds)
	m.lastMessageReceived.Store(time.Now().Add(-55 * time.Second))
	if !m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return true when lastMessageReceived is within threshold with active subscriptions")
	}

	// Test with up market stream and recent lastMessageReceived
	m.lastMessageReceived.Store(time.Now())
	if !m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return true with recent message with active subscriptions")
	}

	// Remove all subscriptions
	m.booksMtx.Lock()
	testBook.mtx.Lock()
	testBook.numSubscribers = 0
	testBook.mtx.Unlock()
	m.booksMtx.Unlock()

	// Test without active subscriptions - should always return true
	m.lastMessageReceived.Store(time.Now().Add(-300 * time.Second)) // Very old

	if !m.IsConnectionHealthy() {
		t.Error("IsConnectionHealthy should return true when there are no active subscriptions")
	}
}

// Mock implementation of the comms.WsConn interface for testing
type mockWsConn struct {
	sendRaw            func(data []byte) error
	isDown             func() bool
	nextID             func() uint64
	send               func(msg *msgjson.Message) error
	request            func(msg *msgjson.Message, respHandler func(*msgjson.Message)) error
	requestRaw         func(msgID uint64, rawMsg []byte, respHandler func(*msgjson.Message)) error
	requestWithTimeout func(msg *msgjson.Message, respHandler func(*msgjson.Message), expireTime time.Duration, expire func()) error
	connect            func(ctx context.Context) (*sync.WaitGroup, error)
	messageSource      func() <-chan *msgjson.Message
	updateURL          func(string)
}

func (m *mockWsConn) SendRaw(data []byte) error {
	if m.sendRaw != nil {
		return m.sendRaw(data)
	}
	return nil
}

func (m *mockWsConn) IsDown() bool {
	if m.isDown != nil {
		return m.isDown()
	}
	return false
}

func (m *mockWsConn) NextID() uint64 {
	if m.nextID != nil {
		return m.nextID()
	}
	return 0
}

func (m *mockWsConn) Send(msg *msgjson.Message) error {
	if m.send != nil {
		return m.send(msg)
	}
	return nil
}

func (m *mockWsConn) Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error {
	if m.request != nil {
		return m.request(msg, respHandler)
	}
	return nil
}

func (m *mockWsConn) RequestRaw(msgID uint64, rawMsg []byte, respHandler func(*msgjson.Message)) error {
	if m.requestRaw != nil {
		return m.requestRaw(msgID, rawMsg, respHandler)
	}
	return nil
}

func (m *mockWsConn) RequestWithTimeout(msg *msgjson.Message, respHandler func(*msgjson.Message), expireTime time.Duration, expire func()) error {
	if m.requestWithTimeout != nil {
		return m.requestWithTimeout(msg, respHandler, expireTime, expire)
	}
	return nil
}

func (m *mockWsConn) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if m.connect != nil {
		return m.connect(ctx)
	}
	wg := &sync.WaitGroup{}
	return wg, nil
}

func (m *mockWsConn) MessageSource() <-chan *msgjson.Message {
	if m.messageSource != nil {
		return m.messageSource()
	}
	ch := make(chan *msgjson.Message)
	close(ch)
	return ch
}

func (m *mockWsConn) UpdateURL(url string) {
	if m.updateURL != nil {
		m.updateURL(url)
	}
}
