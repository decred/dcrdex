package mexctypes

import "encoding/json"

// ExchangeInfo structure for GET /api/v3/exchangeInfo.
type ExchangeInfo struct {
	Timezone        string       `json:"timezone"`
	ServerTime      int64        `json:"serverTime"`
	RateLimits      []RateLimit  `json:"rateLimits"`      // Define RateLimit if needed
	ExchangeFilters []string     `json:"exchangeFilters"` // Define filters if needed
	Symbols         []SymbolInfo `json:"symbols"`
}

// SymbolInfo defines information about a specific symbol.
type SymbolInfo struct {
	Symbol                   string         `json:"symbol"`
	Status                   string         `json:"status"`
	BaseAsset                string         `json:"baseAsset"`
	BaseAssetPrecision       int            `json:"baseAssetPrecision"`
	QuoteAsset               string         `json:"quoteAsset"`
	QuotePrecision           int            `json:"quotePrecision"`
	QuoteAssetPrecision      int            `json:"quoteAssetPrecision"`
	BaseCommissionPrecision  int            `json:"baseCommissionPrecision"`
	QuoteCommissionPrecision int            `json:"quoteCommissionPrecision"`
	OrderTypes               []string       `json:"orderTypes"`
	IsSpotTradingAllowed     bool           `json:"isSpotTradingAllowed"`
	IsMarginTradingAllowed   bool           `json:"isMarginTradingAllowed"`
	QuoteAmountPrecision     string         `json:"quoteAmountPrecision"` // String in docs, might need conversion
	BaseSizePrecision        string         `json:"baseSizePrecision"`    // String in docs
	PermissionSets           [][]string     `json:"permissionSets"`
	Filters                  []SymbolFilter `json:"filters"`
	MaxQuoteAmount           string         `json:"maxQuoteAmount"`
	MakerCommission          string         `json:"makerCommission"`
	TakerCommission          string         `json:"takerCommission"`
	// Deprecated, use permissionSets
	Permissions []string `json:"permissions,omitempty"`
}

// SymbolFilter defines various filters for a symbol.
// Use map[string]interface{} and extract specific filter types as needed,
// or define structs for each filter type (PRICE_FILTER, LOT_SIZE, etc.).
type SymbolFilter map[string]interface{}

// RateLimit defines API rate limit info.
type RateLimit struct {
	RateLimitType string `json:"rateLimitType"`
	Interval      string `json:"interval"`
	IntervalNum   int    `json:"intervalNum"`
	Limit         int    `json:"limit"`
}

// DepthResponse structure for GET /api/v3/depth.
type DepthResponse struct {
	LastUpdateID int64            `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"` // Price, Quantity as strings
	Asks         [][2]json.Number `json:"asks"` // Price, Quantity as strings
}

// Ticker24hr structure for GET /api/v3/ticker/24hr.
type Ticker24hr struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	PrevClosePrice     string `json:"prevClosePrice"`
	LastPrice          string `json:"lastPrice"`
	BidPrice           string `json:"bidPrice"`
	AskPrice           string `json:"askPrice"`
	OpenPrice          string `json:"openPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	Volume             string `json:"volume"`      // Base asset volume
	QuoteVolume        string `json:"quoteVolume"` // Quote asset volume
	OpenTime           int64  `json:"openTime"`
	CloseTime          int64  `json:"closeTime"`
}

// BookTicker structure for GET /api/v3/ticker/bookTicker
type BookTicker struct {
	Symbol      string `json:"symbol"`
	BidPrice    string `json:"bidPrice"`
	BidQuantity string `json:"bidQty"`
	AskPrice    string `json:"askPrice"`
	AskQuantity string `json:"askQty"`
}

// TickerPrice structure for GET /api/v3/ticker/price endpoint.
type TickerPrice struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}
