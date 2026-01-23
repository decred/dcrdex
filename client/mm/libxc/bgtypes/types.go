// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bgtypes

import "encoding/json"

// Bitget API v2 response structures

// APIResponse is the standard wrapper for all Bitget API responses
type APIResponse struct {
	Code        string          `json:"code"`
	Msg         string          `json:"msg"`
	RequestTime int64           `json:"requestTime"`
	Data        json.RawMessage `json:"data"`
}

// CoinInfo represents information about a coin/asset
type CoinInfo struct {
	Coin     string       `json:"coin"`
	Transfer string       `json:"transfer"` // YES or NO
	Chains   []*ChainInfo `json:"chains"`
}

// ChainInfo represents blockchain network information for a coin
type ChainInfo struct {
	Chain                   string `json:"chain"`
	NeedTag                 string `json:"needTag"`      // YES or NO
	Withdrawable            string `json:"withdrawable"` // YES or NO
	Rechargeable            string `json:"rechargeable"` // YES or NO
	WithdrawFee             string `json:"withdrawFee"`
	ExtraWithdrawFee        string `json:"extraWithdrawFee"`
	DepositConfirm          string `json:"depositConfirm"`
	WithdrawConfirm         string `json:"withdrawConfirm"`
	MinDepositAmount        string `json:"minDepositAmount"`
	MinWithdrawAmount       string `json:"minWithdrawAmount"`
	BrowserUrl              string `json:"browserUrl"`
	ContractAddress         string `json:"contractAddress"`
	WithdrawStep            string `json:"withdrawStep"`
	CongestionLevel         string `json:"congestionLevel"`
	EstimatedArrivalTime    int    `json:"estimatedArrivalTime"`
	Label                   string `json:"label"`
	WithdrawIntegerMultiple string `json:"withdrawIntegerMultiple"`
}

// SymbolInfo represents trading symbol/market information
type SymbolInfo struct {
	Symbol              string `json:"symbol"`
	BaseCoin            string `json:"baseCoin"`
	QuoteCoin           string `json:"quoteCoin"`
	MinTradeAmount      string `json:"minTradeAmount"`
	MaxTradeAmount      string `json:"maxTradeAmount"`
	TakerFeeRate        string `json:"takerFeeRate"`
	MakerFeeRate        string `json:"makerFeeRate"`
	PricePrecision      string `json:"pricePrecision"`
	QuantityPrecision   string `json:"quantityPrecision"`
	QuotePrecision      string `json:"quotePrecision"`
	MinTradeUSDT        string `json:"minTradeUSDT"`
	Status              string `json:"status"` // live, halt, etc.
	BuyLimitPriceRatio  string `json:"buyLimitPriceRatio"`
	SellLimitPriceRatio string `json:"sellLimitPriceRatio"`
}

// TickerData represents 24hr ticker data
type TickerData struct {
	Symbol       string `json:"symbol"`
	High24h      string `json:"high24h"`
	Low24h       string `json:"low24h"`
	Close        string `json:"close"`
	QuoteVol     string `json:"quoteVol"`
	BaseVol      string `json:"baseVol"`
	UsdtVol      string `json:"usdtVol"`
	Ts           string `json:"ts"`
	BidPr        string `json:"bidPr"`
	AskPr        string `json:"askPr"`
	BidSz        string `json:"bidSz"`
	AskSz        string `json:"askSz"`
	OpenUtc      string `json:"openUtc"`
	ChangeUtc24h string `json:"changeUtc24h"`
	Change24h    string `json:"change24h"`
}

// OrderbookSnapshot represents a full orderbook snapshot from REST API
type OrderbookSnapshot struct {
	Asks [][]string `json:"asks"` // [price, quantity]
	Bids [][]string `json:"bids"` // [price, quantity]
	Ts   string     `json:"ts"`
}

// AssetBalance represents account balance for an asset
type AssetBalance struct {
	Coin           string `json:"coin"`
	Available      string `json:"available"`
	Frozen         string `json:"frozen"`
	Locked         string `json:"locked"`
	LimitAvailable string `json:"limitAvailable"`
	UTime          string `json:"uTime"`
}

// OrderRequest represents a new order request
type OrderRequest struct {
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`            // buy or sell
	OrderType string `json:"orderType"`       // limit, market
	Force     string `json:"force,omitempty"` // GTC, IOC, FOK, POST_ONLY
	Price     string `json:"price,omitempty"`
	Size      string `json:"size"`                // base currency quantity for limit orders
	QuoteSize string `json:"quoteSize,omitempty"` // quote currency amount for market buy
	ClientOid string `json:"clientOid"`
}

// OrderResponse represents the response when placing an order
type OrderResponse struct {
	OrderId   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// CancelOrderRequest represents a request to cancel an order
type CancelOrderRequest struct {
	Symbol    string `json:"symbol"`
	OrderId   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

// OrderDetail represents detailed order information
type OrderDetail struct {
	UserId           string `json:"userId"`
	Symbol           string `json:"symbol"`
	OrderId          string `json:"orderId"`
	ClientOid        string `json:"clientOid"`
	Price            string `json:"price"`
	Size             string `json:"size"`
	OrderType        string `json:"orderType"`
	Side             string `json:"side"`
	Status           string `json:"status"` // live, partially_filled, filled, cancelled
	PriceAvg         string `json:"priceAvg"`
	BaseVolume       string `json:"baseVolume"`  // filled base quantity
	QuoteVolume      string `json:"quoteVolume"` // filled quote quantity
	EnterPointSource string `json:"enterPointSource"`
	FeeDetail        string `json:"feeDetail"`
	OrderSource      string `json:"orderSource"`
	CTime            string `json:"cTime"`
	UTime            string `json:"uTime"`
}

// FillDetail represents a single fill/trade execution
type FillDetail struct {
	Fee     string `json:"fee"`
	FeeCoin string `json:"feeCoin"`
}

// DepositAddress represents a deposit address for an asset
type DepositAddress struct {
	Coin    string `json:"coin"`
	Chain   string `json:"chain"`
	Address string `json:"address"`
	Tag     string `json:"tag"`
	Url     string `json:"url"`
}

// ModifyDepositAccountRequest represents a request to modify deposit account type
type ModifyDepositAccountRequest struct {
	Coin        string `json:"coin"`
	AccountType string `json:"accountType"` // spot, funding, coin-futures, usdt-futures, usdc-futures
}

// WithdrawalRequest represents a withdrawal request
type WithdrawalRequest struct {
	Coin         string `json:"coin"`
	TransferType string `json:"transferType"` // "on_chain" for blockchain withdrawals, "internal_transfer" for internal transfers
	Chain        string `json:"chain"`
	Address      string `json:"address"`
	Tag          string `json:"tag,omitempty"`
	Size         string `json:"size"`
	Remark       string `json:"remark,omitempty"`
	ClientOid    string `json:"clientOid,omitempty"`
}

// WithdrawalResponse represents the response to a withdrawal request
type WithdrawalResponse struct {
	OrderId   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// WithdrawalRecord represents a withdrawal record
type WithdrawalRecord struct {
	OrderId   string `json:"orderId"`
	TradeId   string `json:"tradeId"`
	ClientOid string `json:"clientOid"`
	Coin      string `json:"coin"`
	Chain     string `json:"chain"`
	Address   string `json:"address"`
	Tag       string `json:"tag"`
	Size      string `json:"size"`
	Fee       string `json:"fee"`
	Status    string `json:"status"` // pending, success, failed, etc.
	TxId      string `json:"txId"`
	CTime     string `json:"cTime"`
	UTime     string `json:"uTime"`
}

// DepositRecord represents a deposit record
type DepositRecord struct {
	OrderId string `json:"orderId"`
	TradeId string `json:"tradeId"`
	Coin    string `json:"coin"`
	Chain   string `json:"chain"`
	Address string `json:"address"`
	Tag     string `json:"tag"`
	Size    string `json:"size"`
	Status  string `json:"status"` // pending, success, failed, etc.
	TxId    string `json:"txId"`
	CTime   string `json:"cTime"`
	UTime   string `json:"uTime"`
}

// Market represents processed market information for internal use
type Market struct {
	Symbol            string
	BaseCoin          string
	QuoteCoin         string
	MinTradeAmount    float64
	MaxTradeAmount    float64
	PricePrecision    int
	QuantityPrecision int
	MinTradeUSDT      float64
	PriceStep         uint64 // Message rate step
	LotSize           uint64 // Minimum quantity step in base asset atoms
	MinQty            uint64
	MaxQty            uint64
	MinNotional       uint64 // in quote asset atoms
}
