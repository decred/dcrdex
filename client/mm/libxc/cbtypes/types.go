package cbtypes

import "time"

type SubscriptionMessage struct {
	Channel     string    `json:"channel"`
	ClientID    string    `json:"client_id"`
	Timestamp   time.Time `json:"timestamp"`
	SequenceNum uint64    `json:"sequence_num"`
	Events      []struct {
		Subscriptions map[string][]string `json:"subscriptions"`
	} `json:"events"`
}

type OrderbookUpdate struct {
	Side        string    `json:"side"` // "bid" or "offer"
	EventTime   time.Time `json:"event_time"`
	PriceLevel  float64   `json:"price_level,string"`
	NewQuantity float64   `json:"new_quantity,string"`
}

type Level2Message struct {
	Events []*struct {
		Type      string             `json:"type"`
		ProductID string             `json:"product_id"`
		Updates   []*OrderbookUpdate `json:"updates"`
	} `json:"events"`
}

type UserMessageOrder struct {
	OrderID       string  `json:"order_id"`
	CumulativeQty float64 `json:"cumulative_quantity,string"`
	FilledValue   float64 `json:"filled_value,string"`
	TotalFees     float64 `json:"total_fees,string"`
	Status        string  `json:"status"`
}

type UserMessage struct {
	Events []*struct {
		Orders []*UserMessageOrder `json:"orders"`
	} `json:"events"`
}

type CancelMessage struct {
	OrderIDs []string `json:"order_ids"`
}

type CancelResponse struct {
	Results []struct {
		Success       bool   `json:"success"`
		FailureReason string `json:"failure_reason"`
		OrderID       string `json:"order_id"`
	} `json:"results"`
}

type OrderRequest struct {
	ClientOrderID      string `json:"client_order_id"`
	ProductID          string `json:"product_id"`
	Side               string `json:"side"` // "BUY" or "SELL"
	OrderConfiguration struct {
		Limit struct {
			BaseSize   string `json:"base_size"`
			LimitPrice string `json:"limit_price"`
			PostOnly   bool   `json:"post_only"`
		} `json:"limit_limit_gtc"`
	} `json:"order_configuration"`
}

type OrderResponse struct {
	Success         bool   `json:"success"`
	FailureReason   string `json:"failure_reason"`
	SuccessResponse struct {
		OrderID       string `json:"order_id"`
		ProductID     string `json:"product_id"`
		Side          string `json:"side"`
		ClientOrderID string `json:"client_order_id"`
	} `json:"success_response"`
	ErrorResponse struct {
		Error                 string `json:"error"`
		Message               string `json:"message"`
		ErrorDetails          string `json:"error_details"`
		PreviewFailureReason  string `json:"preview_failure_reason"`
		NewOrderFailureReason string `json:"new_order_failure_reason"`
	} `json:"error_response"`
}

type DepositAddressResponse struct {
	Data struct {
		Address string `json:"address"`
	} `json:"data"`
}

type Transaction struct {
	ID     string `json:"id"`
	Amount struct {
		Amount float64 `json:"amount,string"`
	} `json:"amount"`
	Network struct {
		Hash string `json:"hash"`
	} `json:"network"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type TransactionResponse struct {
	Data *Transaction `json:"data"`
}

type ListTransactionsResponse struct {
	Pagination struct {
		NextURI string `json:"next_uri"`
	} `json:"pagination"`
	Data []*Transaction `json:"data"`
}

type SendTransactionRequest struct {
	Type     string `json:"type"`
	To       string `json:"to"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Network  string `json:"network"`
}

type TradeStatusResponse struct {
	Order struct {
		Status              string  `json:"status"`
		FilledSize          float64 `json:"filled_size,string"`
		FilledValue         float64 `json:"filled_value,string"`
		TotalFees           float64 `json:"total_fees,string"`
		TotalValueAfterFees float64 `json:"total_value_after_fees,string"`
		Side                string  `json:"side"`
		Config              struct {
			LimitGTC struct {
				BaseSize   float64 `json:"base_size,string"`
				LimitPrice float64 `json:"limit_price,string"`
			} `json:"limit_limit_gtc"`
		} `json:"order_configuration"`
	} `json:"order"`
}

type AssetBalance struct {
	Value    float64 `json:"value,string"`
	Currency string  `json:"currency"`
}

type Account struct {
	UUID             string       `json:"uuid"`
	Name             string       `json:"name"`
	Currency         string       `json:"currency"`
	AvailableBalance AssetBalance `json:"available_balance"`
	Default          bool         `json:"default"`
	Active           bool         `json:"active"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	DeletedAt        *time.Time   `json:"deleted_at"`
	Type             string       `json:"type"`
	Ready            bool         `json:"ready"`
	Hold             AssetBalance `json:"hold"`
}

type AccountsResult struct {
	Accounts []*Account `json:"accounts"`
	HasNext  bool       `json:"has_next"`
	Cursor   string     `json:"cursor"`
}

type Market struct {
	ProductID             string `json:"product_id"`
	Price                 string `json:"price"`
	DayPriceChangePctStr  string `json:"price_percentage_change_24h"`
	Volume                string `json:"volume_24h"`
	DayVolumeChangePctStr string `json:"volume_percentage_change_24h"`
	BaseIncrement         string `json:"base_increment"`
	// QuoteIncrement           float64 `json:"quote_increment,string"`
	// QuoteMinSize             float64 `json:"quote_min_size,string"`
	// QuoteMaxSize             float64 `json:"quote_max_size,string"`
	BaseMinSize              string `json:"base_min_size"`
	BaseMaxSize              string `json:"base_max_size"`
	BaseName                 string `json:"base_name"`
	QuoteName                string `json:"quote_name"`
	Watched                  bool   `json:"watched"`
	IsDisabled               bool   `json:"is_disabled"`
	New                      bool   `json:"new"`
	Status                   string `json:"status"`
	CancelOnly               bool   `json:"cancel_only"`
	LimitOnly                bool   `json:"limit_only"`
	PostOnly                 bool   `json:"post_only"`
	TradingDisabled          bool   `json:"trading_disabled"`
	AuctionMode              bool   `json:"auction_mode"`
	ProductType              string `json:"product_type"`
	QuoteCurrencyID          string `json:"quote_currency_id"`
	BaseCurrencyID           string `json:"base_currency_id"`
	FCMTradingSessionDetails struct {
		IsSessionOpen bool      `json:"is_session_open"`
		OpenTime      time.Time `json:"open_time"`
		CloseTime     time.Time `json:"close_time"`
	} `json:"fcm_trading_session_details"`
	MidMarketPrice     string   `json:"mid_market_price"`
	Alias              string   `json:"alias"`
	AliastTo           []string `json:"alias_to"`
	BaseDisplaySymbol  string   `json:"base_display_symbol"`
	QuoteDisplaySymbol string   `json:"quote_display_symbol"`
	ViewOnly           bool     `json:"view_only"`
	PriceIncrement     string   `json:"price_increment"`
	// FutureProductDetails struct { ... } `json:"future_product_details"`

	// These are not in the response, but calculated.
	RateStep uint64
	LotSize  uint64
	MaxQty   uint64
	MinQty   uint64
}

type ProductsResult struct {
	Products []*Market `json:"products"`
}
