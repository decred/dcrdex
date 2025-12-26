// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bgtypes

// WebSocket message structures for Bitget API v2

// WsRequest represents a WebSocket subscription/unsubscription request
type WsRequest struct {
	Op   string  `json:"op"` // subscribe, unsubscribe, login
	Args []WsArg `json:"args"`
}

// WsArg represents an argument for WebSocket operations
type WsArg struct {
	InstType string `json:"instType"`         // SPOT, MARGIN, etc.
	Channel  string `json:"channel"`          // books, account, orders, etc.
	InstId   string `json:"instId,omitempty"` // Trading pair symbol or "default" for all
	Coin     string `json:"coin,omitempty"`   // Coin filter for account channel ("default" for all)
}

// WsResponse represents a standard WebSocket response
type WsResponse struct {
	Event string `json:"event,omitempty"` // subscribe, unsubscribe, error, login
	Code  string `json:"code,omitempty"`
	Msg   string `json:"msg,omitempty"`
	Op    string `json:"op,omitempty"`
	Arg   *WsArg `json:"arg,omitempty"`
}

// WsLoginRequest represents a login request for private WebSocket channels
type WsLoginRequest struct {
	Op   string       `json:"op"`
	Args []WsLoginArg `json:"args"`
}

// WsLoginArg represents login credentials
type WsLoginArg struct {
	ApiKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Sign       string `json:"sign"`
}

// WsDataMessage represents a data update message
type WsDataMessage struct {
	Action string   `json:"action,omitempty"` // snapshot, update
	Arg    *WsArg   `json:"arg"`
	Data   []WsData `json:"data"`
	Ts     int64    `json:"ts,omitempty"`
}

// WsData is a generic container for WebSocket data
type WsData interface{}

// WsBookData represents orderbook data from WebSocket
type WsBookData struct {
	Asks     [][]string `json:"asks"` // [price, quantity]
	Bids     [][]string `json:"bids"` // [price, quantity]
	Checksum int64      `json:"checksum,omitempty"`
	Seq      int64      `json:"seq,omitempty"` // Sequence number
	Ts       string     `json:"ts"`
}

// WsOrderData represents order update data from WebSocket
// Per: https://www.bitget.com/api-doc/spot/websocket/private/Order-Channel
type WsOrderData struct {
	InstId           string        `json:"instId"`           // Product ID, e.g. BTCUSDT
	OrderId          string        `json:"orderId"`          // Order ID
	ClientOid        string        `json:"clientOid"`        // Client order ID
	Price            string        `json:"price,omitempty"`  // Order price
	Size             string        `json:"size"`             // Order amount (quote for buy, base for sell)
	NewSize          string        `json:"newSize"`          // Order quantity (base for limit, varies for market)
	Notional         string        `json:"notional"`         // Buy amount for market orders
	OrderType        string        `json:"orderType"`        // "market" or "limit"
	Force            string        `json:"force"`            // GTC, post_only, FOK, IOC
	Side             string        `json:"side"`             // "buy" or "sell"
	FillPrice        string        `json:"fillPrice"`        // Latest filled price
	TradeId          string        `json:"tradeId"`          // Latest transaction ID
	BaseVolume       string        `json:"baseVolume"`       // Latest fill quantity (incremental)
	FillTime         string        `json:"fillTime"`         // Latest transaction time
	FillFee          string        `json:"fillFee"`          // Latest transaction fee (negative)
	FillFeeCoin      string        `json:"fillFeeCoin"`      // Fee currency
	TradeScope       string        `json:"tradeScope"`       // "T"=taker, "M"=maker
	AccBaseVolume    string        `json:"accBaseVolume"`    // Total filled quantity (cumulative)
	PriceAvg         string        `json:"priceAvg"`         // Average filled price
	Status           string        `json:"status"`           // live, partially_filled, filled, cancelled
	CTime            string        `json:"cTime"`            // Order creation time (ms)
	UTime            string        `json:"uTime"`            // Order update time (ms)
	StpMode          string        `json:"stpMode"`          // STP mode
	EnterPointSource string        `json:"enterPointSource"` // Order source
	FeeDetail        []WsFeeDetail `json:"feeDetail"`        // Fee list (can be multiple)
}

// WsFeeDetail represents fee information (simple structure per API)
type WsFeeDetail struct {
	FeeCoin string `json:"feeCoin"` // Transaction fee currency
	Fee     string `json:"fee"`     // Transaction fee amount
}

// WsAccountData represents account balance update from WebSocket
type WsAccountData struct {
	Coin           string `json:"coin"`           // Token name
	Available      string `json:"available"`      // Available coin assets
	Frozen         string `json:"frozen"`         // Frozen when order is placed
	Locked         string `json:"locked"`         // Locked for fiat merchant, etc.
	LimitAvailable string `json:"limitAvailable"` // Restricted for spot copy trading
	UTime          string `json:"uTime"`          // Update time (milliseconds)
}

// WsPingMessage represents a ping message
type WsPingMessage struct {
	Op string `json:"op"` // "ping"
}

// WsPongMessage represents a pong response
type WsPongMessage struct {
	Op string `json:"op"` // "pong"
}

// BookUpdate represents an orderbook update (converted from WsBookData)
type BookUpdate struct {
	Bids         [][]float64
	Asks         [][]float64
	BidsOriginal [][]string // Original string format for checksum calculation
	AsksOriginal [][]string // Original string format for checksum calculation
	IsSnapshot   bool       // true if action='snapshot', false if action='update'
	Checksum     int32      // Bitget's checksum for validation (0 if not provided)
}
