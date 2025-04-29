// To run these unit tests (which do not require API keys or build tags):
// go test -v ./client/mm/libxc
// Or run all unit tests in the project:
// go test -v ./...

package libxc

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/mm/libxc/mexctypes"
	"decred.org/dcrdex/dex"
	"github.com/stretchr/testify/require"
)

// TestStringToSatoshis tests the stringToSatoshis conversion function.
func TestStringToSatoshis(t *testing.T) {
	// Mock logger or use a simple one if needed by the function indirectly
	m := &mexc{log: dex.Disabled} // Assuming stringToSatoshis is a method on *mexc

	tests := []struct {
		name          string
		input         string
		convFactor    float64
		expectedAtoms uint64
		expectError   bool
	}{
		{"empty string", "", 1e8, 0, false},
		{"zero amount", "0", 1e8, 0, false},
		{"zero amount with decimals", "0.000", 1e8, 0, false},
		{"simple btc", "1.23456789", 1e8, 123456789, false},
		{"simple usdt", "123.45", 1e6, 123450000, false},
		{"integer amount", "150", 1e8, 15000000000, false},
		{"max uint64 boundary approx", "184467440.7370955161", 1e8, 18446744073709551, false},
		{"too many decimals", "1.123456789", 1e8, 112345678, false},
		{"factor 1", "12345", 1, 12345, false},
		{"zero factor", "123", 0, 0, false}, // Factor 0 results in 0
		{"invalid amount", "abc", 1e8, 0, true},
		{"negative amount", "-1.0", 1e8, 0, true},
		{"exceeds uint64", "200000000", 1e8, 20000000000000000, false},
		{"scientific notation", "1.2e3", 1e8, 120000000000, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			atoms, err := m.stringToSatoshis(tc.input, tc.convFactor)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedAtoms, atoms)
			}
		})
	}
}

// TestMapMEXCCoinNetworkToDEXSymbol tests the mapping from MEXC coin/network to DEX symbol.
func TestMapMEXCCoinNetworkToDEXSymbol(t *testing.T) {
	// This function doesn't rely on internal state other than logging, which is disabled here.
	m := &mexc{log: dex.Disabled}

	tests := []struct {
		name        string
		mexcCoin    string // Uppercase
		mexcNetwork string
		expectedDex string // Lowercase
	}{
		{"dcr native", "DCR", "DCR", "dcr"},
		{"usdt erc20 (ETH)", "USDT", "ETH", "usdt.erc20"},
		{"usdt erc20 (ERC20)", "USDT", "ERC20", "usdt.erc20"},
		{"usdt polygon", "USDT", "MATIC", "usdt.polygon"},
		{"usdt trc20 (TRX)", "USDT", "TRX", "usdt.trc20"},
		{"usdt trc20 (TRC20)", "USDT", "TRC20", "usdt.trc20"},
		{"usdt bep20", "USDT", "BEP20(BSC)", "usdt.bep20"},
		{"usdt solana", "USDT", "SOLANA", "usdt.sol"},
		{"btc native", "BTC", "BTC", "btc"},
		{"eth native", "ETH", "ETH", "eth"},
		{"unknown network", "USDT", "SOMECHAIN", ""},
		{"mismatched_native", "DCR", "ETH", "dcr.erc20"},
		{"lowercase input coin", "usdt", "ETH", "usdt.erc20"},
		{"lowercase input network", "USDT", "eth", "usdt.erc20"},
		{"empty coin", "", "ETH", ""},
		{"empty network", "USDT", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dexSymbol := m.mapMEXCCoinNetworkToDEXSymbol(tc.mexcCoin, tc.mexcNetwork)
			require.Equal(t, tc.expectedDex, dexSymbol)
		})
	}
}

// TestGenerateClientOrderID tests the client order ID generation.
func TestGenerateClientOrderID(t *testing.T) {
	// Need a mock mexc struct with a prefix
	m := &mexc{
		log:                dex.Disabled,
		tradeIDNoncePrefix: dex.Bytes("testprefix"),
	}

	// Generate a few IDs
	id1 := m.generateClientOrderID()
	time.Sleep(2 * time.Millisecond) // Ensure timestamp potentially changes
	id2 := m.generateClientOrderID()
	id3 := m.generateClientOrderID()

	// Check format (basic check)
	prefix := "dcrdex-testprefix-"
	require.True(t, strings.HasPrefix(id1, prefix), "ID1 has wrong prefix")
	require.True(t, strings.HasPrefix(id2, prefix), "ID2 has wrong prefix")
	require.True(t, strings.HasPrefix(id3, prefix), "ID3 has wrong prefix")

	// Check uniqueness
	require.NotEqual(t, id1, id2, "ID1 and ID2 should be different")
	require.NotEqual(t, id2, id3, "ID2 and ID3 should be different")

	// Check nonce increment (by parsing)
	parseNonce := func(id string) int {
		parts := strings.Split(id, "-")
		require.Len(t, parts, 4, "ID should have 4 parts separated by hyphen")
		nonce, err := strconv.Atoi(parts[3])
		require.NoError(t, err, "Nonce part should be an integer")
		return nonce
	}

	nonce1 := parseNonce(id1)
	nonce2 := parseNonce(id2)
	nonce3 := parseNonce(id3)

	require.Equal(t, nonce1+1, nonce2, "Nonce should increment by 1")
	require.Equal(t, nonce2+1, nonce3, "Nonce should increment by 1")
}

// TestHandleDepthUpdate tests the handleDepthUpdate function.
func TestHandleDepthUpdate(t *testing.T) {
	// Create a mock mexc instance
	logger := dex.StdOutLogger("TEST", dex.LevelDebug)
	m := &mexc{
		log:      logger,
		books:    make(map[string]*mexcOrderBook),
		booksMtx: sync.RWMutex{},
	}

	// Create a mock orderbook for testing
	const testSymbol = "BTCUSDT"
	mockBook := &mexcOrderBook{
		mktSymbol:   testSymbol,
		log:         logger.SubLogger("BOOK-" + testSymbol),
		updateQueue: make(chan *mexctypes.WsDepthUpdateData, 10),
	}
	mockBook.synced.Store(true) // Mark as synced for testing

	// Add the mock book to our mexc instance
	m.booksMtx.Lock()
	m.books[testSymbol] = mockBook
	m.booksMtx.Unlock()

	// Create a test WsMessage with depth update data
	rawBids := [][2]json.Number{
		{json.Number("10000.50"), json.Number("1.5")},
		{json.Number("10000.00"), json.Number("2.5")},
	}
	rawAsks := [][2]json.Number{
		{json.Number("10001.00"), json.Number("1.0")},
		{json.Number("10002.00"), json.Number("2.0")},
	}

	depthData := &mexctypes.WsDepthUpdateData{
		Version: "123456789",
		Bids:    rawBids,
		Asks:    rawAsks,
	}

	depthDataBytes, err := json.Marshal(depthData)
	require.NoError(t, err, "Failed to marshal depth data")

	wsMsg := &mexctypes.WsMessage{
		Channel: "spot@public.increase.depth.v3.api@" + testSymbol,
		Symbol:  testSymbol,
		Data:    depthDataBytes,
	}

	// Create a channel to receive the message from the update queue
	receivedUpdate := make(chan *mexctypes.WsDepthUpdateData, 1)

	// Start a goroutine to read from the update queue
	go func() {
		select {
		case update := <-mockBook.updateQueue:
			receivedUpdate <- update
		case <-time.After(time.Second):
			t.Errorf("Timed out waiting for update to be sent to the queue")
			close(receivedUpdate)
		}
	}()

	// Call handleDepthUpdate
	m.handleDepthUpdate(wsMsg)

	// Check if the update was sent to the book's update queue
	select {
	case update := <-receivedUpdate:
		require.NotNil(t, update, "Update should not be nil")
		require.Equal(t, "123456789", update.Version, "Version should match")
		require.Len(t, update.Bids, 2, "Should have 2 bids")
		require.Len(t, update.Asks, 2, "Should have 2 asks")

		// Check a bid and ask value
		require.Equal(t, "10000.50", string(update.Bids[0][0]), "First bid price should match")
		require.Equal(t, "1.5", string(update.Bids[0][1]), "First bid quantity should match")
		require.Equal(t, "10001.00", string(update.Asks[0][0]), "First ask price should match")
		require.Equal(t, "1.0", string(update.Asks[0][1]), "First ask quantity should match")
	case <-time.After(time.Second):
		t.Fatalf("Timed out waiting for message on receivedUpdate channel")
	}

	// Test with non-existent book
	nonExistentMsg := &mexctypes.WsMessage{
		Channel: "spot@public.increase.depth.v3.api@UNKNOWN",
		Symbol:  "UNKNOWN",
		Data:    depthDataBytes,
	}

	// Call handleDepthUpdate with non-existent book symbol - should not panic
	m.handleDepthUpdate(nonExistentMsg)

	// Test with missing symbol
	noSymbolMsg := &mexctypes.WsMessage{
		Channel: "spot@public.increase.depth.v3.api@",
		Symbol:  "",
		Data:    depthDataBytes,
	}

	// Call handleDepthUpdate with missing symbol - should not panic
	m.handleDepthUpdate(noSymbolMsg)

	// Test with invalid depth data
	invalidDataMsg := &mexctypes.WsMessage{
		Channel: "spot@public.increase.depth.v3.api@" + testSymbol,
		Symbol:  testSymbol,
		Data:    []byte(`{"invalid": "json"`), // Invalid JSON
	}

	// Call handleDepthUpdate with invalid data - should not panic
	m.handleDepthUpdate(invalidDataMsg)
}
