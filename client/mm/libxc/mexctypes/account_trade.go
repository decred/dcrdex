package mexctypes

// Types related to Account and Trading endpoints

// AccountInfo structure for GET /api/v3/account.
type AccountInfo struct {
	MakerCommission  int64            `json:"makerCommission"`
	TakerCommission  int64            `json:"takerCommission"`
	BuyerCommission  int64            `json:"buyerCommission"`
	SellerCommission int64            `json:"sellerCommission"`
	CanTrade         bool             `json:"canTrade"`
	CanWithdraw      bool             `json:"canWithdraw"`
	CanDeposit       bool             `json:"canDeposit"`
	UpdateTime       int64            `json:"updateTime"` // Not present in example, check docs
	AccountType      string           `json:"accountType"`
	Balances         []AccountBalance `json:"balances"`
	Permissions      []string         `json:"permissions"` // e.g., ["SPOT"]
}

// AccountBalance defines the balance for a single asset.
type AccountBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`   // Available balance as string
	Locked string `json:"locked"` // Locked balance as string
}

// Order structure for responses from POST /api/v3/order, GET /api/v3/order, GET /api/v3/openOrders, etc.
type Order struct {
	Symbol              string `json:"symbol"`
	OrderID             string `json:"orderId"`       // MEXC's order ID
	OrderListID         int64  `json:"orderListId"`   // -1 unless part of an OCO order
	ClientOrderID       string `json:"clientOrderId"` // User-provided ID
	Price               string `json:"price"`
	OrigQuantity        string `json:"origQty"`             // Original quantity
	ExecutedQuantity    string `json:"executedQty"`         // Executed quantity
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"` // Filled quote asset quantity
	Status              string `json:"status"`              // NEW, FILLED, CANCELED, etc.
	TimeInForce         string `json:"timeInForce"`
	Type                string `json:"type"`                // LIMIT, MARKET, etc.
	Side                string `json:"side"`                // BUY, SELL
	StopPrice           string `json:"stopPrice,omitempty"` // Only for STOP_LOSS, TAKE_PROFIT orders
	IcebergQuantity     string `json:"icebergQty,omitempty"`
	Time                int64  `json:"time"`                        // Order creation time
	UpdateTime          int64  `json:"updateTime"`                  // Last update time
	IsWorking           bool   `json:"isWorking"`                   // Appears in some responses
	OrigQuoteOrderQty   string `json:"origQuoteOrderQty,omitempty"` // Original quote order quantity for market orders
}

// CanceledOrder is the response type for DELETE /api/v3/order.
type CanceledOrder Order // Structure is the same as Order

// NewOrderResponse is the response type for POST /api/v3/order.
type NewOrderResponse struct {
	Symbol        string `json:"symbol"`
	OrderID       string `json:"orderId"`
	OrderListID   int64  `json:"orderListId"` // Should be -1 for non-OCO
	ClientOrderID string `json:"clientOrderId"`
	TransactTime  int64  `json:"transactTime"` // Timestamp of the order creation

	// These fields might be included for non-MARKET orders, needs confirmation
	Price               string `json:"price,omitempty"`
	OrigQuantity        string `json:"origQty,omitempty"`
	ExecutedQuantity    string `json:"executedQty,omitempty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty,omitempty"`
	Status              string `json:"status,omitempty"`
	TimeInForce         string `json:"timeInForce,omitempty"`
	Type                string `json:"type,omitempty"`
	Side                string `json:"side,omitempty"`
}

// Trade structure for GET /api/v3/myTrades response.
type Trade struct {
	Symbol          string `json:"symbol"`
	ID              string `json:"id"` // Trade ID
	OrderID         string `json:"orderId"`
	OrderListID     int64  `json:"orderListId"`
	Price           string `json:"price"`
	Quantity        string `json:"qty"`
	QuoteQuantity   string `json:"quoteQty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	Time            int64  `json:"time"`
	IsBuyer         bool   `json:"isBuyer"`
	IsMaker         bool   `json:"isMaker"`
	IsBestMatch     bool   `json:"isBestMatch"` // Seems specific to Binance, check MEXC docs
}

// OrderResponse is an alias for NewOrderResponse
type OrderResponse NewOrderResponse

// CancelOrderResponse is an alias for CanceledOrder
type CancelOrderResponse CanceledOrder

// MyTrade is an alias for Trade
type MyTrade Trade
