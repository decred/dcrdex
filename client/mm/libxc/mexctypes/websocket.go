package mexctypes

import "encoding/json"

// Generic websocket message structure
type WsRequest struct {
	Method string   `json:"method"` // e.g., "SUBSCRIPTION", "UNSUBSCRIPTION"
	Params []string `json:"params"` // e.g., ["spot@public.increase.depth.v3.api@BTCUSDT"]
}

// Generic incoming message to determine type
type WsMessage struct {
	Channel string          `json:"c,omitempty"`    // Channel name, e.g., "spot@public.increase.depth.v3.api"
	Symbol  string          `json:"s,omitempty"`    // Symbol, e.g., "BTCUSDT"
	Data    json.RawMessage `json:"d,omitempty"`    // Payload depends on channel
	Ts      int64           `json:"t,omitempty"`    // Timestamp for public streams
	Ping    int64           `json:"ping,omitempty"` // Ping message

	// User data stream specific fields
	ListenKey string `json:"l,omitempty"` // Listen key identifier
	EventType string `json:"e,omitempty"` // Event type for user data (e.g., "spot@private.orders.v3.api", "spot@private.deals.v3.api")
	EventTime int64  `json:"E,omitempty"` // Event timestamp for user data
}

// WsPong is sent in response to a Ping
type WsPong struct {
	Pong int64 `json:"pong"`
}

// --- Market Data Stream Payloads --- //

// WsDepthUpdateData is the payload ('d' field) for spot@public.increase.depth.v3.api
type WsDepthUpdateData struct {
	Version string           `json:"v"` // Version (update ID)
	Bids    [][2]json.Number `json:"b"` // Price, Quantity as strings
	Asks    [][2]json.Number `json:"a"` // Price, Quantity as strings
	Symbol  string           `json:"-"` // Symbol - internal field, not from JSON
}

// --- User Data Stream Payloads --- //

// ListenKeyResponse structure for POST /api/v3/userDataStream.
type ListenKeyResponse struct {
	ListenKey string `json:"listenKey"`
}

// WsOrderUpdateData is the payload ('d' field) for spot@private.orders.v3.api
type WsOrderUpdateData struct {
	Price               string `json:"p"`             // Order price
	Quantity            string `json:"q"`             // Order quantity
	Amount              string `json:"a"`             // Order amount (quote asset quantity)
	Fee                 string `json:"f"`             // Order fee
	FeeCurrency         string `json:"fc"`            // Fee currency
	Side                string `json:"S"`             // Order side (BUY/SELL)
	Type                string `json:"o"`             // Order type (LIMIT/MARKET etc)
	Status              string `json:"s"`             // Order status (NEW/FILLED etc)
	OrderID             string `json:"i"`             // Order ID
	ClientOrderID       string `json:"c"`             // Client Order ID
	IsMaker             bool   `json:"m"`             // Is maker order?
	OrderTime           int64  `json:"O"`             // Order creation timestamp
	TransactionTime     int64  `json:"T"`             // Order transaction timestamp
	IsReduceOnly        bool   `json:"r,omitempty"`   // Futures field, might be present?
	SelfTradePrevention string `json:"stp,omitempty"` // Self trade prevention mode
}

// WsDealUpdateData is the payload ('d' field) for spot@private.deals.v3.api
type WsDealUpdateData struct {
	Price           string `json:"p"`  // Deal price
	Quantity        string `json:"q"`  // Deal quantity
	Amount          string `json:"a"`  // Deal amount (quote asset quantity)
	Fee             string `json:"f"`  // Deal fee
	FeeCurrency     string `json:"fc"` // Fee currency
	IsMaker         bool   `json:"m"`  // Is maker?
	Side            string `json:"S"`  // Order side (BUY/SELL)
	DealID          string `json:"d"`  // Deal/Trade ID
	OrderID         string `json:"i"`  // Order ID
	TransactionTime int64  `json:"T"`  // Deal timestamp
}

// WsAccountUpdateData represents the payload ('d' field) for spot@private.account.v3.api.
type WsAccountUpdateData struct {
	Asset     string `json:"a"` // Asset
	Available string `json:"f"` // Available balance
	Locked    string `json:"l"` // Locked balance
}
