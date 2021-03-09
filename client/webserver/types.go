// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"fmt"
	"strconv"
	"strings"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

// standardResponse is a basic API response when no data needs to be returned.
type standardResponse struct {
	OK   bool   `json:"ok"`
	Msg  string `json:"msg,omitempty"`
	Code *int   `json:"code,omitempty"`
}

// simpleAck is a plain standardResponse with "ok" = true.
func simpleAck() *standardResponse {
	return &standardResponse{
		OK: true,
	}
}

// The initForm is sent by the client to log initialize the DEX.
type initForm struct {
	Pass encode.PassBytes `json:"pass"`
	Seed dex.Bytes        `json:"seed,omitempty"`
}

// The loginForm is sent by the client to log in to a DEX.
type loginForm struct {
	Pass encode.PassBytes `json:"pass"`
}

// registrationForm is used to register a new DEX account.
type registrationForm struct {
	Addr     string           `json:"addr"`
	Cert     string           `json:"cert"`
	Password encode.PassBytes `json:"pass"`
	Fee      uint64           `json:"fee"`
}

// newWalletForm is information necessary to create a new wallet.
type newWalletForm struct {
	AssetID uint32 `json:"assetID"`
	// These are only used if the Decred wallet does not already exist. In that
	// case, these parameters will be used to create the wallet.
	Config map[string]string `json:"config"`
	Pass   encode.PassBytes  `json:"pass"`
	AppPW  encode.PassBytes  `json:"appPass"`
}

// openWalletForm is information necessary to open a wallet.
type openWalletForm struct {
	AssetID uint32           `json:"assetID"`
	Pass    encode.PassBytes `json:"pass"` // Application password.
}

type tradeForm struct {
	Pass  encode.PassBytes `json:"pw"`
	Order *core.TradeForm  `json:"order"`
}

type cancelForm struct {
	Pass    encode.PassBytes `json:"pw"`
	OrderID dex.Bytes        `json:"orderID"`
}

// withdrawForm is sent to initiate a withdraw.
type withdrawForm struct {
	AssetID uint32           `json:"assetID"`
	Value   uint64           `json:"value"`
	Address string           `json:"address"`
	Pass    encode.PassBytes `json:"pw"`
}

type accountExportForm struct {
	Pass encode.PassBytes `json:"pw"`
	Host string           `json:"host"`
}

type accountImportForm struct {
	Pass    encode.PassBytes `json:"pw"`
	Account core.Account     `json:"account"`
}

type accountDisableForm struct {
	Pass encode.PassBytes `json:"pw"`
	Host string           `json:"host"`
}

// orderReader wraps a core.Order and provides methods for info display.
// Whenever possible, add an orderReader methods rather than a template func.
type orderReader struct {
	*core.Order
}

// FromSymbol is the symbol of the asset which will be sent.
func (ord *orderReader) FromSymbol() string {
	if ord.Sell {
		return ord.BaseSymbol
	}
	return ord.QuoteSymbol
}

// FromSymbol is the symbol of the asset which will be received.
func (ord *orderReader) ToSymbol() string {
	if ord.Sell {
		return ord.QuoteSymbol
	}
	return ord.BaseSymbol
}

// FromID is the asset ID of the asset which will be sent.
func (ord *orderReader) FromID() uint32 {
	if ord.Sell {
		return ord.BaseID
	}
	return ord.QuoteID
}

// FromID is the asset ID of the asset which will be received.
func (ord *orderReader) ToID() uint32 {
	if ord.Sell {
		return ord.QuoteID
	}
	return ord.BaseID
}

// Cancelable will be true for standing limit orders in status epoch or booked.
func (ord *orderReader) Cancelable() bool {
	return ord.Type == order.LimitOrderType &&
		ord.TimeInForce == order.StandingTiF &&
		ord.Status <= order.OrderStatusBooked
}

// TypeString combines the order type and side into a single string.
func (ord *orderReader) TypeString() string {
	s := "market"
	if ord.Type == order.LimitOrderType {
		s = "limit"
		if ord.TimeInForce == order.ImmediateTiF {
			s += " (i)"
		}
	}
	if ord.Sell {
		s += " sell"
	} else {
		s += " buy"
	}
	return s
}

// BaseQtyString formats the order quantity in units of the base asset.
func (ord *orderReader) BaseQtyString() string {
	return precision8(ord.Qty)
}

// OfferString formats the order quantity in units of the outgoing asset,
// performing a conversion if necessary.
func (ord *orderReader) OfferString() string {
	if ord.Sell || ord.Type == order.MarketOrderType {
		return precision8(ord.Qty)
	}
	return precision8(calc.BaseToQuote(ord.Rate, ord.Qty))
}

// AskString will print the minimum received amount for the filled limit order.
// The actual settled amount may be more.
func (ord *orderReader) AskString() string {
	if ord.Type == order.MarketOrderType {
		return "market rate"
	}
	if ord.Sell {
		return precision8(calc.BaseToQuote(ord.Rate, ord.Qty))
	}
	return precision8(ord.Qty)
}

// IsMarketBuy is true if this is a market buy order.
func (ord *orderReader) IsMarketBuy() bool {
	return ord.Type == order.MarketOrderType && !ord.Sell
}

// IsMarketOrder is true if this is a market order.
func (ord *orderReader) IsMarketOrder() bool {
	return ord.Type == order.MarketOrderType
}

// SettledFrom is the sum settled incoming asset.
func (ord *orderReader) SettledFrom() string {
	return precision8(ord.sumFrom(settledFilter))
}

// SettledTo is the sum settled of outgoing asset.
func (ord *orderReader) SettledTo() string {
	return precision8(ord.sumTo(settledFilter))
}

// SettledPercent is the percent of the order which has completed settlement.
func (ord *orderReader) SettledPercent() string {
	return ord.percent(settledFilter)
}

// FilledFrom is the sum filled in units of the outgoing asset.
func (ord *orderReader) FilledFrom() string {
	return precision8(ord.sumFrom(filledNonCancelFilter))
}

// FilledTo is the sum filled in units of the incoming asset.
func (ord *orderReader) FilledTo() string {
	return precision8(ord.sumTo(filledNonCancelFilter))
}

// FilledPercent is the percent of the order that has filled, without percent
// sign.
func (ord *orderReader) FilledPercent() string {
	return ord.percent(filledFilter)
}

// SideString is "sell" for sell orders and "buy" for buy orders.
func (ord *orderReader) SideString() string {
	if ord.Sell {
		return "sell"
	}
	return "buy"
}

func (ord *orderReader) percent(filter func(match *core.Match) bool) string {
	var sum uint64
	if ord.Sell {
		sum = ord.sumFrom(filter)
	} else {
		sum = ord.sumTo(filter)
	}
	return strconv.FormatFloat(float64(sum)/float64(ord.Qty)*100, 'f', 1, 64)
}

func settledFilter(match *core.Match) bool {
	if match.IsCancel {
		return false
	}
	return (match.Side == order.Taker && match.Status == order.MatchComplete) ||
		(match.Side == order.Maker && (match.Status == order.MakerRedeemed || match.Status == order.MatchComplete))
}

func settlingFilter(match *core.Match) bool {
	return (match.Side == order.Taker && match.Status < order.MatchComplete) ||
		(match.Side == order.Maker && match.Status < order.MakerRedeemed)
}

func filledFilter(match *core.Match) bool { return true }

func filledNonCancelFilter(match *core.Match) bool {
	return !match.IsCancel
}

// sumFrom will sum the match quantities in units of the outgoing asset.
func (ord *orderReader) sumFrom(filter func(match *core.Match) bool) uint64 {
	var v uint64
	add := func(rate, qty uint64) {
		if ord.Sell {
			v += qty
		} else {
			v += calc.BaseToQuote(rate, qty)
		}
	}
	for _, match := range ord.Matches {
		if filter(match) {
			add(match.Rate, match.Qty)
		}
	}
	return v
}

// sumTo will sum the match quantities in units of the incoming asset.
func (ord *orderReader) sumTo(filter func(match *core.Match) bool) uint64 {
	var v uint64
	add := func(rate, qty uint64) {
		if ord.Sell {
			v += calc.BaseToQuote(rate, qty)
		} else {
			v += qty
		}
	}
	for _, match := range ord.Matches {
		if filter(match) {
			add(match.Rate, match.Qty)
		}
	}
	return v
}

// hasLiveMatches will be true if there are any matches < MakerRedeemed.
func (ord *orderReader) hasLiveMatches() bool {
	for _, match := range ord.Matches {
		if match.Status < order.MakerRedeemed {
			return true
		}
	}
	return false
}

// StatusString is the order status.
func (ord *orderReader) StatusString() string {
	isLive := ord.hasLiveMatches()
	switch ord.Status {
	case order.OrderStatusUnknown:
		return "unknown"
	case order.OrderStatusEpoch:
		return "epoch"
	case order.OrderStatusBooked:
		if ord.Cancelling {
			return "cancelling"
		}
		if isLive {
			return "booked/settling"
		}
		return "booked"
	case order.OrderStatusExecuted:
		if isLive {
			return "settling"
		}
		if ord.Filled == 0 {
			return "no match"
		}
		return "executed"
	case order.OrderStatusCanceled:
		if isLive {
			return "canceled/settling"
		}
		return "canceled"
	case order.OrderStatusRevoked:
		if isLive {
			return "revoked/settling"
		}
		return "revoked"
	}
	return "unknown"
}

// SimpleRateString is the formatted match rate.
func (ord *orderReader) SimpleRateString() string {
	return precision8(ord.Rate)
}

// RateString is a formatted rate with units.
func (ord *orderReader) RateString() string {
	if ord.Type == order.MarketOrderType {
		return "market"
	}
	return fmt.Sprintf("%s %s/%s", precision8(ord.Rate), ord.QuoteSymbol, ord.BaseSymbol)
}

// AverageRateString returns a formatting string containing the average rate of
// the matches that have been filled in an order.
func (ord *orderReader) AverageRateString() string {
	if len(ord.Matches) == 0 {
		return "0"
	}
	var totalQty, totalRate uint64
	for _, match := range ord.Matches {
		totalQty += match.Qty
		totalRate += match.Rate * match.Qty
	}
	averageRate := totalRate / totalQty
	return precision8(averageRate)
}

// MatchReaders creates a slice of *matchReader for the order's matches.
func (ord *orderReader) MatchReaders() []*matchReader {
	readers := make([]*matchReader, 0, len(ord.Matches))
	for _, match := range ord.Matches {
		readers = append(readers, newMatchReader(match, ord))
	}
	return readers
}

// SwapFeesString is a formatted string of the paid swap fees.
func (ord *orderReader) SwapFeesString() string {
	return precision8(ord.FeesPaid.Swap)
}

// RedemptionFeesString is a formatted string of the paid redemption fees.
func (ord *orderReader) RedemptionFeesString() string {
	return precision8(ord.FeesPaid.Redemption)
}

// BaseAssetFees is a formatted string of the fees paid in the base asset.
func (ord *orderReader) BaseAssetFees() string {
	if ord.Sell {
		return ord.SwapFeesString()
	}
	return ord.RedemptionFeesString()
}

// QuoteAssetFees is a formatted string of the fees paid in the quote asset.
func (ord *orderReader) QuoteAssetFees() string {
	if ord.Sell {
		return ord.RedemptionFeesString()
	}
	return ord.SwapFeesString()
}

func (ord *orderReader) FundingCoinIDs() []string {
	ids := make([]string, 0, len(ord.FundingCoins))
	for i := range ord.FundingCoins {
		ids = append(ids, ord.FundingCoins[i].StringID)
	}
	return ids
}

// matchReader wraps a core.Match and provides methods for info display.
// Whenever possible, add an matchReader methods rather than a template func.
type matchReader struct {
	*core.Match
	ord *orderReader
}

func newMatchReader(match *core.Match, ord *orderReader) *matchReader {
	return &matchReader{
		Match: match,
		ord:   ord,
	}
}

// StatusString is a formatted string of the match status.
func (m *matchReader) StatusString() string {
	switch m.Status {
	case order.NewlyMatched:
		return "(0 / 4) Newly Matched"
	case order.MakerSwapCast:
		return "(1 / 4) First Swap Sent"
	case order.TakerSwapCast:
		return "(2 / 4) Second Swap Sent"
	case order.MakerRedeemed:
		if m.Side == order.Maker {
			return "Match Complete"
		}
		return "(3 / 4) Maker Redeemed"
	case order.MatchComplete:
		return "Match Complete"
	}
	return "Unknown Order Status"
}

// RateString is the formatted match rate, with units.
func (m *matchReader) RateString() string {
	return fmt.Sprintf("%s %s/%s", precision8(m.Rate), m.ord.QuoteSymbol, m.ord.BaseSymbol)
}

// FromQuantityString is the formatted quantity.
func (m *matchReader) FromQuantityString() string {
	if m.ord.Sell {
		return precision8(m.Qty)
	}
	return precision8(calc.BaseToQuote(m.Rate, m.Qty))
}

// ToQuantityString is the formatted quantity.
func (m *matchReader) ToQuantityString() string {
	if m.ord.Sell {
		return precision8(calc.BaseToQuote(m.Rate, m.Qty))
	}
	return precision8(m.Qty)
}

// OrderPortion is the percent of the order that this match represents, without
// percent sign.
func (m *matchReader) OrderPortion() string {
	qty := m.ord.Qty
	matchQty := m.Qty
	if m.ord.IsMarketBuy() {
		matchQty = calc.BaseToQuote(matchQty, m.Rate)
	}
	return strconv.FormatFloat((float64(matchQty)/float64(qty))*100, 'f', 1, 64)
}

// TimeString is a formatted string of the match's timestamp.
func (m *matchReader) TimeString() string {
	t := encode.UnixTimeMilli(int64(m.Stamp)).Local()
	return t.Format("Jan 2 2006, 15:04:05 MST")
}

// InSwapCast will be true if the last match step was us broadcasting our swap
// transaction.
func (m *matchReader) InSwapCast() bool {
	if m.Revoked || m.Refund != nil {
		return false
	}
	return (m.Match.Status == order.TakerSwapCast && m.Match.Side == order.Taker) ||
		(m.Match.Status == order.MakerSwapCast && m.Match.Side == order.Maker)
}

// SwapConfirmString returns a string indicating the current confirmation
// progress of the our swap contract, if and only if the counter-party has not
// yet redeemed the swap, otherwise an empty string is returned.
func (m *matchReader) SwapConfirmString() string {
	if !m.InSwapCast() || m.Swap == nil {
		return ""
	}
	confs := m.Swap.Confs
	if confs == nil || confs.Required == 0 {
		return ""
	}
	return fmt.Sprintf("%d / %d confirmations", confs.Count, confs.Required)
}

// InCounterSwapCast will be true if the last match step was the counter-party
// broadcasting their swap transaction.
func (m *matchReader) InCounterSwapCast() bool {
	if m.Revoked || m.Refund != nil {
		return false
	}
	return (m.Match.Status == order.MakerSwapCast && m.Match.Side == order.Taker) ||
		(m.Match.Status == order.TakerSwapCast && m.Match.Side == order.Maker)
}

// CounterSwapConfirmString returns a string indicating the current confirmation
// progress of the other party's swap contract, if and only if we have not yet
// redeemed the swap, otherwise an empty string is returned.
func (m *matchReader) CounterSwapConfirmString() string {
	if !m.InCounterSwapCast() || m.CounterSwap == nil {
		return ""
	}
	confs := m.CounterSwap.Confs
	if confs == nil || confs.Required == 0 {
		return ""
	}
	return fmt.Sprintf("%d / %d confirmations", confs.Count, confs.Required)
}

func precision8(v uint64) string {
	fullPrecision := strconv.FormatFloat(float64(v)/1e8, 'f', 8, 64)
	return strings.TrimRight(strings.TrimRight(fullPrecision, "0"), ".")
}
