package mxctypes

import (
	"encoding/json"
	"fmt"
)

// ============================================================================
// Exchange Info & Market Data
// ============================================================================

// ExchangeInfo is the response from MEXC /api/v3/exchangeInfo.
type ExchangeInfo struct {
	Symbols []Symbol `json:"symbols"`
}

// DefaultSymbolsResponse is the response from MEXC /api/v3/defaultSymbols
// This endpoint returns symbols that support API trading.
type DefaultSymbolsResponse struct {
	Data      []string `json:"data"`      // List of tradeable symbols
	Code      int      `json:"code"`      // 0 = success
	Msg       string   `json:"msg"`       // "success"
	Timestamp int64    `json:"timestamp"` // Unix timestamp in ms
}

// Symbol represents a trading symbol with its configuration and filters.
type Symbol struct {
	Symbol                   string   `json:"symbol"`
	BaseAsset                string   `json:"baseAsset"`
	QuoteAsset               string   `json:"quoteAsset"`
	BaseAssetPrecision       int      `json:"baseAssetPrecision"`
	QuotePrecision           int      `json:"quotePrecision"`
	BaseCommissionPrecision  int      `json:"baseCommissionPrecision"`
	QuoteCommissionPrecision int      `json:"quoteCommissionPrecision"`
	Status                   string   `json:"status"` // "1" = ENABLED, "0" = DISABLED
	Filters                  []Filter `json:"filters"`
}

// Filter represents a trading rule filter.
type Filter struct {
	FilterType       string `json:"filterType"`
	MinPrice         string `json:"minPrice,omitempty"`
	MaxPrice         string `json:"maxPrice,omitempty"`
	TickSize         string `json:"tickSize,omitempty"`
	MinQty           string `json:"minQty,omitempty"`
	MaxQty           string `json:"maxQty,omitempty"`
	StepSize         string `json:"stepSize,omitempty"`
	MinNotional      string `json:"minNotional,omitempty"`
	ApplyToMarket    bool   `json:"applyToMarket,omitempty"`
	AvgPriceMins     int    `json:"avgPriceMins,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	MaxNumAlgoOrders int    `json:"maxNumAlgoOrders,omitempty"`
	MaxNumOrders     int    `json:"maxNumOrders,omitempty"`
}

// ============================================================================
// Account & Balance
// ============================================================================

// Account is the response from MEXC /api/v3/account.
type Account struct {
	Balances []Balance `json:"balances"`
}

// Balance represents the balance of an asset.
type Balance struct {
	Asset  string  `json:"asset"`
	Free   float64 `json:"free,string"`
	Locked float64 `json:"locked,string"`
}

// ============================================================================
// Order Book
// ============================================================================

// OrderbookSnapshot represents a full order book snapshot from MEXC REST API
// GET /api/v3/depth
type OrderbookSnapshot struct {
	LastUpdateID uint64           `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"` // [price, quantity]
	Asks         [][2]json.Number `json:"asks"` // [price, quantity]
}

// BookUpdate represents a full order book snapshot from WebSocket
// This corresponds to the protobuf Spot message depth updates
type BookUpdate struct {
	Symbol string
	Bids   [][2]float64
	Asks   [][2]float64
}

// DepthLevel represents a single price level as strings prior to unit conversion.
type DepthLevel struct {
	Price string
	Qty   string
}

// DepthUpdate is a minimal internal struct after decoding protobuf.
type DepthUpdate struct {
	Symbol string
	Bids   []DepthLevel
	Asks   []DepthLevel
	// Optional version/update id if provided
	Version string
}

// ============================================================================
// Trading - Orders
// ============================================================================

// OrderRequest represents a new order request to MEXC.
type OrderRequest struct {
	Symbol           string `json:"symbol"`
	Side             string `json:"side"` // "BUY" or "SELL"
	Type             string `json:"type"` // "LIMIT", "MARKET", etc.
	Quantity         string `json:"quantity,omitempty"`
	QuoteOrderQty    string `json:"quoteOrderQty,omitempty"`
	Price            string `json:"price,omitempty"`
	NewClientOrderID string `json:"newClientOrderId,omitempty"`
	RecvWindow       int64  `json:"recvWindow,omitempty"`
	Timestamp        int64  `json:"timestamp"`
}

// OrderResponse represents the response from placing an order.
type OrderResponse struct {
	Symbol              string `json:"symbol"`
	OrderID             string `json:"orderId"`
	OrderListID         int64  `json:"orderListId"`
	ClientOrderID       string `json:"clientOrderId"`
	TransactTime        int64  `json:"transactTime"`
	Price               string `json:"price"`
	OrigQty             string `json:"origQty"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status              string `json:"status"` // NEW, PARTIALLY_FILLED, FILLED, CANCELED, etc.
	Type                string `json:"type"`
	Side                string `json:"side"`
	Fills               []Fill `json:"fills,omitempty"`
}

// Fill represents a trade fill.
type Fill struct {
	Price           string `json:"price"`
	Qty             string `json:"qty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
}

// OrderStatusResponse represents the response from querying an order's status.
type OrderStatusResponse struct {
	Symbol              string `json:"symbol"`
	OrderID             string `json:"orderId"`
	OrderListID         int64  `json:"orderListId"`
	ClientOrderID       string `json:"clientOrderId"`
	Price               string `json:"price"`
	OrigQty             string `json:"origQty"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status              string `json:"status"`
	TimeInForce         string `json:"timeInForce"`
	Type                string `json:"type"`
	Side                string `json:"side"`
	Time                int64  `json:"time"`
	UpdateTime          int64  `json:"updateTime"`
	IsWorking           bool   `json:"isWorking"`
	OrigQuoteOrderQty   string `json:"origQuoteOrderQty"`
}

// CancelOrderResponse represents the response from canceling an order.
type CancelOrderResponse struct {
	Symbol            string `json:"symbol"`
	OrigClientOrderID string `json:"origClientOrderId"`
	OrderID           string `json:"orderId"`
	ClientOrderID     string `json:"clientOrderId"`
}

// ============================================================================
// Deposits & Withdrawals
// ============================================================================

// DepositAddressResponse represents a single deposit address entry.
// MEXC API returns an array of these (even for single network queries).
type DepositAddressResponse struct {
	Coin    string `json:"coin"`
	Address string `json:"address"`
	Tag     string `json:"tag,omitempty"`     // For assets that require a memo/tag
	Network string `json:"network,omitempty"` // Network name
}

// WithdrawRequest represents a withdrawal request.
type WithdrawRequest struct {
	Coin            string `json:"coin"`
	Network         string `json:"network"`
	Address         string `json:"address"`
	Amount          string `json:"amount"`
	WithdrawOrderID string `json:"withdrawOrderId,omitempty"` // Optional client-provided ID
}

// WithdrawResponse represents the response from a withdrawal request.
type WithdrawResponse struct {
	ID string `json:"id"`
}

// WithdrawHistoryItem represents a single withdrawal in history.
type WithdrawHistoryItem struct {
	ID          string `json:"id"`
	Coin        string `json:"coin"`
	Network     string `json:"network"`
	Address     string `json:"address"`
	Amount      string `json:"amount"`
	TransferFee string `json:"transferFee"`
	Status      int    `json:"status"` // 0: processing, 1: failed, 2: success
	TxID        string `json:"txId"`
	ApplyTime   int64  `json:"applyTime"`
	Info        string `json:"info,omitempty"`
}

// DepositHistoryItem represents a single deposit in history.
type DepositHistoryItem struct {
	Coin       string `json:"coin"`
	Network    string `json:"network"`
	Amount     string `json:"amount"`
	TxID       string `json:"txId"`
	Address    string `json:"address"`
	AddressTag string `json:"addressTag,omitempty"`
	Status     int    `json:"status"` // See MEXC status codes below
	InsertTime int64  `json:"insertTime"`
}

// MEXC Deposit Status Codes:
// 1: Small deposit not credited
// 2: Delayed credit
// 3: Large deposit
// 4: Pending (awaiting confirmations)
// 5: Credited successfully
// 6: Under review
// 7: Rejected
// 8: Forced deposit return
// 9: Pre-credited
// 10: Invalid deposit
// 11: Restricted
// 12: Returned

// CoinInfo represents asset/coin configuration.
type CoinInfo struct {
	Coin           string        `json:"coin"`
	Name           string        `json:"name"`
	NetworkList    []NetworkInfo `json:"networkList"`
	WithdrawEnable bool          `json:"withdrawEnable"`
	DepositEnable  bool          `json:"depositEnable"`
}

// NetworkInfo represents network-specific information for a coin.
type NetworkInfo struct {
	Network                 string `json:"network"`
	Coin                    string `json:"coin"`
	WithdrawEnable          bool   `json:"withdrawEnable"`
	DepositEnable           bool   `json:"depositEnable"`
	WithdrawFee             string `json:"withdrawFee"`
	WithdrawMin             string `json:"withdrawMin"`
	WithdrawMax             string `json:"withdrawMax"`
	MinConfirm              int    `json:"minConfirm"`
	UnlockConfirm           int    `json:"unlockConfirm"`
	WithdrawIntegerMultiple string `json:"withdrawIntegerMultiple,omitempty"`
}

// ============================================================================
// WebSocket Control & Topics
// ============================================================================

// WsRequest is the JSON control message for subscribe/unsubscribe/ping.
type WsRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params,omitempty"`
}

// WsAck is a generic JSON ack from the server for control messages.
type WsAck struct {
	ID   int    `json:"id"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// PublicDealsTopic returns the WebSocket topic for trade updates.
func PublicDealsTopic(symbol string) string {
	return fmt.Sprintf("spot@public.deals.v3.api.pb@%s", symbol)
}

// PublicAggreDealsTopic returns the WebSocket topic for aggregated trades.
func PublicAggreDealsTopic(symbol string, interval string) string {
	return fmt.Sprintf("spot@public.aggre.deals.v3.api.pb@%s@%s", interval, symbol)
}

// PublicLimitDepthTopic returns the WebSocket topic for orderbook snapshots.
func PublicLimitDepthTopic(symbol string, depth string) string {
	return fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@%s", symbol, depth)
}

// ============================================================================
// Error Responses
// ============================================================================

// ErrorResponse represents an error response from MEXC API.
type ErrorResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
