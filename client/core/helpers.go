// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// OrderReader wraps a Order and provides methods for info display.
// Whenever possible, add an OrderReader methods rather than a template func.
type OrderReader struct {
	*Order
	BaseUnitInfo  dex.UnitInfo
	QuoteUnitInfo dex.UnitInfo
}

// FromSymbol is the symbol of the asset which will be sent.
func (ord *OrderReader) FromSymbol() string {
	if ord.Sell {
		return ord.BaseSymbol
	}
	return ord.QuoteSymbol
}

// FromSymbol is the symbol of the asset which will be received.
func (ord *OrderReader) ToSymbol() string {
	if ord.Sell {
		return ord.QuoteSymbol
	}
	return ord.BaseSymbol
}

// FromID is the asset ID of the asset which will be sent.
func (ord *OrderReader) FromID() uint32 {
	if ord.Sell {
		return ord.BaseID
	}
	return ord.QuoteID
}

// FromID is the asset ID of the asset which will be received.
func (ord *OrderReader) ToID() uint32 {
	if ord.Sell {
		return ord.QuoteID
	}
	return ord.BaseID
}

// Cancelable will be true for standing limit orders in status epoch or booked.
func (ord *OrderReader) Cancelable() bool {
	return ord.Type == order.LimitOrderType &&
		ord.TimeInForce == order.StandingTiF &&
		ord.Status <= order.OrderStatusBooked
}

// TypeString combines the order type and side into a single string.
func (ord *OrderReader) TypeString() string {
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
func (ord *OrderReader) BaseQtyString() string {
	return formatQty(ord.Qty, ord.BaseUnitInfo)
}

// OfferString formats the order quantity in units of the outgoing asset,
// performing a conversion if necessary.
func (ord *OrderReader) OfferString() string {
	if ord.Sell {
		return formatQty(ord.Qty, ord.BaseUnitInfo)
	} else if ord.Type == order.MarketOrderType {
		return formatQty(ord.Qty, ord.QuoteUnitInfo)
	}
	return formatQty(calc.BaseToQuote(ord.Rate, ord.Qty), ord.QuoteUnitInfo)
}

// AskString will print the minimum received amount for the filled limit order.
// The actual settled amount may be more.
func (ord *OrderReader) AskString() string {
	if ord.Type == order.MarketOrderType {
		return "market rate"
	}
	if ord.Sell {
		return formatQty(calc.BaseToQuote(ord.Rate, ord.Qty), ord.QuoteUnitInfo)
	}
	return formatQty(ord.Qty, ord.BaseUnitInfo)
}

// IsMarketBuy is true if this is a market buy order.
func (ord *OrderReader) IsMarketBuy() bool {
	return ord.Type == order.MarketOrderType && !ord.Sell
}

// IsMarketOrder is true if this is a market order.
func (ord *OrderReader) IsMarketOrder() bool {
	return ord.Type == order.MarketOrderType
}

// SettledFrom is the sum settled outgoing asset.
func (ord *OrderReader) SettledFrom() string {
	if ord.Sell {
		return formatQty(ord.sumFrom(settledFilter), ord.BaseUnitInfo)
	}
	return formatQty(ord.sumFrom(settledFilter), ord.QuoteUnitInfo)
}

// SettledTo is the sum settled of incoming asset.
func (ord *OrderReader) SettledTo() string {
	if ord.Sell {
		return formatQty(ord.sumTo(settledFilter), ord.QuoteUnitInfo)
	}
	return formatQty(ord.sumTo(settledFilter), ord.BaseUnitInfo)
}

// SettledPercent is the percent of the order which has completed settlement.
func (ord *OrderReader) SettledPercent() string {
	if ord.Type == order.CancelOrderType {
		return ""
	}
	return ord.percent(settledFilter)
}

// FilledFrom is the sum filled in units of the outgoing asset.
func (ord *OrderReader) FilledFrom() string {
	if ord.Sell {
		return formatQty(ord.sumFrom(filledFilter), ord.BaseUnitInfo)
	}
	return formatQty(ord.sumFrom(filledFilter), ord.QuoteUnitInfo)
}

// FilledTo is the sum filled in units of the incoming asset.
func (ord *OrderReader) FilledTo() string {
	if ord.Sell {
		return formatQty(ord.sumTo(filledFilter), ord.QuoteUnitInfo)
	}
	return formatQty(ord.sumTo(filledFilter), ord.BaseUnitInfo)
}

// FilledPercent is the percent of the order that has filled, without percent
// sign.
func (ord *OrderReader) FilledPercent() string {
	if ord.Type == order.CancelOrderType {
		return ""
	}
	return ord.percent(filledFilter)
}

// SideString is "sell" for sell orders, "buy" for buy orders, and "" for
// cancels.
func (ord *OrderReader) SideString() string {
	if ord.Type == order.CancelOrderType {
		return ""
	}
	if ord.Sell {
		return "sell"
	}
	return "buy"
}

func (ord *OrderReader) percent(filter func(match *Match) bool) string {
	var sum uint64
	if ord.Sell || ord.IsMarketBuy() {
		sum = ord.sumFrom(filter)
	} else {
		sum = ord.sumTo(filter)
	}
	return strconv.FormatFloat(float64(sum)/float64(ord.Qty)*100, 'f', 1, 64)
}

// sumFrom will sum the match quantities in units of the outgoing asset.
func (ord *OrderReader) sumFrom(filter func(match *Match) bool) uint64 {
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
func (ord *OrderReader) sumTo(filter func(match *Match) bool) uint64 {
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
func (ord *OrderReader) hasLiveMatches() bool {
	for _, match := range ord.Matches {
		if match.Active {
			return true
		}
	}
	return false
}

// StatusString is the order status.
func (ord *OrderReader) StatusString() string {
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
		if ord.Filled == 0 && ord.Type != order.CancelOrderType {
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
func (ord *OrderReader) SimpleRateString() string {
	return ord.formatRate(ord.Rate)
}

// RateString is a formatted rate with units.
func (ord *OrderReader) RateString() string {
	if ord.Type == order.MarketOrderType {
		return "market"
	}
	return fmt.Sprintf("%s %s/%s", ord.formatRate(ord.Rate), ord.QuoteSymbol, ord.BaseSymbol)
}

// AverageRateString returns a formatting string containing the average rate of
// the matches that have been filled in an order.
func (ord *OrderReader) AverageRateString() string {
	if len(ord.Matches) == 0 {
		return "0"
	}
	var baseQty, rateProduct uint64
	for _, match := range ord.Matches {
		baseQty += match.Qty
		rateProduct += match.Rate * match.Qty // order ~ 1e16
	}
	return ord.formatRate(rateProduct / baseQty)
}

// SwapFeesString is a formatted string of the paid swap fees.
func (ord *OrderReader) SwapFeesString() string {
	if ord.Sell {
		return formatQty(ord.FeesPaid.Swap, ord.BaseUnitInfo)
	}
	return formatQty(ord.FeesPaid.Swap, ord.QuoteUnitInfo)
}

// RedemptionFeesString is a formatted string of the paid redemption fees.
func (ord *OrderReader) RedemptionFeesString() string {
	if ord.Sell {
		return formatQty(ord.FeesPaid.Swap, ord.QuoteUnitInfo)
	}
	return formatQty(ord.FeesPaid.Swap, ord.BaseUnitInfo)
}

// BaseAssetFees is a formatted string of the fees paid in the base asset.
func (ord *OrderReader) BaseAssetFees() string {
	if ord.Sell {
		return ord.SwapFeesString()
	}
	return ord.RedemptionFeesString()
}

// QuoteAssetFees is a formatted string of the fees paid in the quote asset.
func (ord *OrderReader) QuoteAssetFees() string {
	if ord.Sell {
		return ord.RedemptionFeesString()
	}
	return ord.SwapFeesString()
}

func (ord *OrderReader) FundingCoinIDs() []string {
	ids := make([]string, 0, len(ord.FundingCoins))
	for i := range ord.FundingCoins {
		ids = append(ids, ord.FundingCoins[i].StringID)
	}
	return ids
}

// formatRate formats the specified rate as a conventional rate with trailing
// zeros trimmed.
func (ord *OrderReader) formatRate(msgRate uint64) string {
	r := calc.ConventionalRate(msgRate, ord.BaseUnitInfo, ord.QuoteUnitInfo)
	return trimTrailingZeros(strconv.FormatFloat(r, 'f', 8, 64))
}

// formatQty formats the quantity as a conventional string and trims trailing
// zeros.
func formatQty(qty uint64, unitInfo dex.UnitInfo) string {
	return trimTrailingZeros(unitInfo.ConventionalString(qty))
}

// trimTrailingZeros trims trailing decimal zeros of a formatted float string.
// The number is assumed to be formatted as a float with non-zero decimal
// precision, i.e. there should be a decimal point in there.
func trimTrailingZeros(s string) string {
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func settledFilter(match *Match) bool {
	if match.IsCancel {
		return false
	}
	return (match.Side == order.Taker && match.Status == order.MatchComplete) ||
		(match.Side == order.Maker && (match.Status == order.MakerRedeemed || match.Status == order.MatchComplete))
}

func settlingFilter(match *Match) bool {
	return (match.Side == order.Taker && match.Status < order.MatchComplete) ||
		(match.Side == order.Maker && match.Status < order.MakerRedeemed)
}

func filledFilter(match *Match) bool { return true }

func filledNonCancelFilter(match *Match) bool {
	return !match.IsCancel
}

// matchReader wraps a Match and provides methods for info display.
// Whenever possible, add an matchReader methods rather than a template func.
type matchReader struct {
	*Match
	ord *OrderReader
}

func newMatchReader(match *Match, ord *OrderReader) *matchReader {
	return &matchReader{
		Match: match,
		ord:   ord,
	}
}

// MatchReaders creates a slice of *matchReader for the order's matches.
func (ord *OrderReader) MatchReaders() []*matchReader {
	readers := make([]*matchReader, 0, len(ord.Matches))
	for _, match := range ord.Matches {
		readers = append(readers, newMatchReader(match, ord))
	}
	return readers
}

// StatusString is a formatted string of the match status.
func (m *matchReader) StatusString() string {
	if m.Revoked {
		// When revoked, match status is less important than pending action if
		// still active, or the outcome if inactive.
		switch {
		case m.Active:
			return "Revoked - Refund PENDING" // could auto-redeem too, but we are waiting
		case m.Refund != nil:
			return "Revoked - Refunded"
			// TODO: show refund txid on /order page
		case m.Redeem != nil:
			return "Revoked - Redeemed"
		} // otherwise no txns needed to retire
		return "Revoked - Complete"
	}

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
	return "Unknown Match Status"
}

// RateString is the formatted match rate, with units.
func (m *matchReader) RateString() string {
	return fmt.Sprintf("%s %s/%s", m.ord.formatRate(m.Rate), m.ord.QuoteSymbol, m.ord.BaseSymbol)
}

// FromQuantityString is the formatted quantity.
func (m *matchReader) FromQuantityString() string {
	if m.ord.Sell {
		return formatQty(m.Qty, m.ord.BaseUnitInfo)
	}
	return formatQty(calc.BaseToQuote(m.Rate, m.Qty), m.ord.QuoteUnitInfo)
}

// ToQuantityString is the formatted quantity.
func (m *matchReader) ToQuantityString() string {
	if m.ord.Sell {
		return formatQty(calc.BaseToQuote(m.Rate, m.Qty), m.ord.QuoteUnitInfo)
	}
	return formatQty(m.Qty, m.ord.BaseUnitInfo)
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
	t := time.UnixMilli(int64(m.Stamp)).Local()
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
