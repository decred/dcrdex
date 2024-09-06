package bntypes

import "encoding/json"

type Market struct {
	Symbol              string   `json:"symbol"`
	Status              string   `json:"status"`
	BaseAsset           string   `json:"baseAsset"`
	BaseAssetPrecision  int      `json:"baseAssetPrecision"`
	QuoteAsset          string   `json:"quoteAsset"`
	QuoteAssetPrecision int      `json:"quoteAssetPrecision"`
	OrderTypes          []string `json:"orderTypes"`
}

type Balance struct {
	Asset  string  `json:"asset"`
	Free   float64 `json:"free,string"`
	Locked float64 `json:"locked,string"`
}

type Account struct {
	Balances []*Balance `json:"balances"`
}

type NetworkInfo struct {
	// AddressRegex            string  `json:"addressRegex"`
	Coin          string `json:"coin"`
	DepositEnable bool   `json:"depositEnable"`
	// IsDefault               bool    `json:"isDefault"`
	// MemoRegex               string  `json:"memoRegex"`
	// MinConfirm              int     `json:"minConfirm"`
	// Name                    string  `json:"name"`
	Network string `json:"network"`
	// ResetAddressStatus      bool    `json:"resetAddressStatus"`
	// SpecialTips             string  `json:"specialTips"`
	// UnLockConfirm           int     `json:"unLockConfirm"`
	WithdrawEnable bool    `json:"withdrawEnable"`
	WithdrawFee    float64 `json:"withdrawFee,string"`
	// WithdrawIntegerMultiple float64 `json:"withdrawIntegerMultiple,string"`
	// WithdrawMax             float64 `json:"withdrawMax,string"`
	WithdrawMin float64 `json:"withdrawMin,string"`
	// SameAddress             bool    `json:"sameAddress"`
	// EstimatedArrivalTime    int     `json:"estimatedArrivalTime"`
	// Busy                    bool    `json:"busy"`
}

type CoinInfo struct {
	Coin string `json:"coin"`
	// DepositAllEnable  bool           `json:"depositAllEnable"`
	// Free              float64        `json:"free,string"`
	// Freeze            float64        `json:"freeze,string"`
	// Ipoable           float64        `json:"ipoable,string"`
	// Ipoing            float64        `json:"ipoing,string"`
	// IsLegalMoney      bool           `json:"isLegalMoney"`
	// Locked            float64        `json:"locked,string"`
	// Name              string         `json:"name"`
	// Storage           float64        `json:"storage,string"`
	// Trading           bool           `json:"trading"`
	// WithdrawAllEnable bool           `json:"withdrawAllEnable"`
	// Withdrawing       float64        `json:"withdrawing,string"`
	NetworkList []*NetworkInfo `json:"networkList"`
}

type OrderbookSnapshot struct {
	LastUpdateID uint64           `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"`
	Asks         [][2]json.Number `json:"asks"`
}

type BookUpdate struct {
	FirstUpdateID uint64           `json:"U"`
	LastUpdateID  uint64           `json:"u"`
	Bids          [][2]json.Number `json:"b"`
	Asks          [][2]json.Number `json:"a"`
}

type BookNote struct {
	StreamName string      `json:"stream"`
	Data       *BookUpdate `json:"data"`
}

type WSBalance struct {
	Asset  string  `json:"a"`
	Free   float64 `json:"f,string"`
	Locked float64 `json:"l,string"`
}

type StreamUpdate struct {
	Asset              string       `json:"a"`
	EventType          string       `json:"e"`
	ClientOrderID      string       `json:"c"`
	CurrentOrderStatus string       `json:"X"`
	Balances           []*WSBalance `json:"B"`
	BalanceDelta       float64      `json:"d,string"`
	Filled             float64      `json:"z,string"`
	QuoteFilled        float64      `json:"Z,string"`
	OrderQty           float64      `json:"q,string"`
	QuoteOrderQty      float64      `json:"Q,string"`
	CancelledOrderID   string       `json:"C"`
	E                  int64        `json:"E"`
	ListenKey          string       `json:"listenKey"`
}

type RateLimit struct {
	RateLimitType string `json:"rateLimitType"`
	Interval      string `json:"interval"`
	IntervalNum   int64  `json:"intervalNum"`
	Limit         int64  `json:"limit"`
}

type DataStreamKey struct {
	ListenKey string `json:"listenKey"`
}

type ExchangeInfo struct {
	Timezone   string       `json:"timezone"`
	ServerTime int64        `json:"serverTime"`
	RateLimits []*RateLimit `json:"rateLimits"`
	Symbols    []*Market    `json:"symbols"`
}

type StreamSubscription struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     uint64   `json:"id"`
}

type PendingDeposit struct {
	Amount  float64 `json:"amount,string"`
	Coin    string  `json:"coin"`
	Network string  `json:"network"`
	Status  int     `json:"status"`
	TxID    string  `json:"txId"`
}

const (
	DepositStatusPending            = 0
	DepositStatusSuccess            = 1
	DepositStatusCredited           = 6
	DepositStatusWrongDeposit       = 7
	DepositStatusWaitingUserConfirm = 8
)

type OrderResponse struct {
	Symbol             string  `json:"symbol"`
	Price              float64 `json:"price,string"`
	OrigQty            float64 `json:"origQty,string"`
	OrigQuoteQty       float64 `json:"origQuoteOrderQty,string"`
	ExecutedQty        float64 `json:"executedQty,string"`
	CumulativeQuoteQty float64 `json:"cummulativeQuoteQty,string"`
	Status             string  `json:"status"`
}

type BookedOrder struct {
	Symbol             string  `json:"symbol"`
	OrderID            int64   `json:"orderId"`
	ClientOrderID      string  `json:"clientOrderId"`
	Price              float64 `json:"price,string"`
	OrigQty            float64 `json:"origQty,string"`
	OrigQuoteQty       float64 `json:"origQuoteOrderQty,string"`
	ExecutedQty        float64 `json:"executedQty,string"`
	CumulativeQuoteQty float64 `json:"cummulativeQuoteQty,string"`
	Status             string  `json:"status"`
	TimeInForce        string  `json:"timeInForce"`
	Side               string  `json:"side"`
}

type MarketTicker24 struct {
	Symbol             string  `json:"symbol"`
	PriceChange        float64 `json:"priceChange,string"`
	PriceChangePercent float64 `json:"priceChangePercent,string"`
	WeightedAvgPrice   float64 `json:"weightedAvgPrice,string"`
	PrevClosePrice     float64 `json:"prevClosePrice,string"`
	LastPrice          float64 `json:"lastPrice,string"`
	LastQty            float64 `json:"lastQty,string"`
	BidPrice           float64 `json:"bidPrice,string"`
	BidQty             float64 `json:"bidQty,string"`
	AskPrice           float64 `json:"askPrice,string"`
	AskQty             float64 `json:"askQty,string"`
	OpenPrice          float64 `json:"openPrice,string"`
	HighPrice          float64 `json:"highPrice,string"`
	LowPrice           float64 `json:"lowPrice,string"`
	Volume             float64 `json:"volume,string"`
	QuoteVolume        float64 `json:"quoteVolume,string"`
	OpenTime           int64   `json:"openTime"`
	CloseTime          int64   `json:"closeTime"`
	FirstId            int64   `json:"firstId"`
	LastId             int64   `json:"lastId"`
	Count              int64   `json:"count"`
}
