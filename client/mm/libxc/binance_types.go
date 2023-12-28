// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import "encoding/json"

type binanceMarket struct {
	Symbol              string   `json:"symbol"`
	Status              string   `json:"status"`
	BaseAsset           string   `json:"baseAsset"`
	BaseAssetPrecision  int      `json:"baseAssetPrecision"`
	QuoteAsset          string   `json:"quoteAsset"`
	QuoteAssetPrecision int      `json:"quoteAssetPrecision"`
	OrderTypes          []string `json:"orderTypes"`
}

type binanceNetworkInfo struct {
	AddressRegex            string  `json:"addressRegex"`
	Coin                    string  `json:"coin"`
	DepositEnable           bool    `json:"depositEnable"`
	IsDefault               bool    `json:"isDefault"`
	MemoRegex               string  `json:"memoRegex"`
	MinConfirm              int     `json:"minConfirm"`
	Name                    string  `json:"name"`
	Network                 string  `json:"network"`
	ResetAddressStatus      bool    `json:"resetAddressStatus"`
	SpecialTips             string  `json:"specialTips"`
	UnLockConfirm           int     `json:"unLockConfirm"`
	WithdrawEnable          bool    `json:"withdrawEnable"`
	WithdrawFee             float64 `json:"withdrawFee,string"`
	WithdrawIntegerMultiple float64 `json:"withdrawIntegerMultiple,string"`
	WithdrawMax             float64 `json:"withdrawMax,string"`
	WithdrawMin             float64 `json:"withdrawMin,string"`
	SameAddress             bool    `json:"sameAddress"`
	EstimatedArrivalTime    int     `json:"estimatedArrivalTime"`
	Busy                    bool    `json:"busy"`
}

type binanceCoinInfo struct {
	Coin              string                `json:"coin"`
	DepositAllEnable  bool                  `json:"depositAllEnable"`
	Free              float64               `json:"free,string"`
	Freeze            float64               `json:"freeze,string"`
	Ipoable           float64               `json:"ipoable,string"`
	Ipoing            float64               `json:"ipoing,string"`
	IsLegalMoney      bool                  `json:"isLegalMoney"`
	Locked            float64               `json:"locked,string"`
	Name              string                `json:"name"`
	Storage           float64               `json:"storage,string"`
	Trading           bool                  `json:"trading"`
	WithdrawAllEnable bool                  `json:"withdrawAllEnable"`
	Withdrawing       float64               `json:"withdrawing,string"`
	NetworkList       []*binanceNetworkInfo `json:"networkList"`
}

type binanceOrderbookSnapshot struct {
	LastUpdateID uint64           `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"`
	Asks         [][2]json.Number `json:"asks"`
}

type binanceBookUpdate struct {
	FirstUpdateID uint64           `json:"U"`
	LastUpdateID  uint64           `json:"u"`
	Bids          [][2]json.Number `json:"b"`
	Asks          [][2]json.Number `json:"a"`
}

type binanceBookNote struct {
	StreamName string             `json:"stream"`
	Data       *binanceBookUpdate `json:"data"`
}

type binanceWSBalance struct {
	Asset  string  `json:"a"`
	Free   float64 `json:"f,string"`
	Locked float64 `json:"l,string"`
}

type binanceStreamUpdate struct {
	Asset              string              `json:"a"`
	EventType          string              `json:"e"`
	ClientOrderID      string              `json:"c"`
	CurrentOrderStatus string              `json:"X"`
	Balances           []*binanceWSBalance `json:"B"`
	BalanceDelta       float64             `json:"d,string"`
	Filled             float64             `json:"z,string"`
	QuoteFilled        float64             `json:"Z,string"`
	OrderQty           float64             `json:"q,string"`
	QuoteOrderQty      float64             `json:"Q,string"`
	CancelledOrderID   string              `json:"C"`
	E                  json.RawMessage     `json:"E"`
}
