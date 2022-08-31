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
	BaseUnitInfo        dex.UnitInfo
	BaseFeeUnitInfo     dex.UnitInfo
	BaseFeeAssetSymbol  string
	QuoteUnitInfo       dex.UnitInfo
	QuoteFeeUnitInfo    dex.UnitInfo
	QuoteFeeAssetSymbol string
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

// FromFeeSymbol is the symbol of the asset used to pay swap fees.
func (ord *OrderReader) FromFeeSymbol() string {
	if ord.Sell {
		return ord.BaseFeeAssetSymbol
	}
	return ord.QuoteFeeAssetSymbol
}

// ToFeeSymbol is the symbol of the asset used to pay redeem fees.
func (ord *OrderReader) ToFeeSymbol() string {
	if ord.Sell {
		return ord.QuoteFeeAssetSymbol
	}
	return ord.BaseFeeAssetSymbol
}

// BaseFeeSymbol is the symbol of the asset used to pay the base asset's
// network fees.
func (ord *OrderReader) BaseFeeSymbol() string {
	return ord.BaseFeeAssetSymbol
}

// QuoteFeeSymbol is the symbol of the asset used to pay the quote asset's
// network fees.
func (ord *OrderReader) QuoteFeeSymbol() string {
	return ord.QuoteFeeAssetSymbol
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

// FilledFrom is the sum filled in units of the outgoing asset. Excludes cancel
// matches.
func (ord *OrderReader) FilledFrom() string {
	if ord.Sell {
		return formatQty(ord.sumFrom(filledNonCancelFilter), ord.BaseUnitInfo)
	}
	return formatQty(ord.sumFrom(filledNonCancelFilter), ord.QuoteUnitInfo)
}

// FilledTo is the sum filled in units of the incoming asset. Excludes cancel
// matches.
func (ord *OrderReader) FilledTo() string {
	if ord.Sell {
		return formatQty(ord.sumTo(filledNonCancelFilter), ord.QuoteUnitInfo)
	}
	return formatQty(ord.sumTo(filledNonCancelFilter), ord.BaseUnitInfo)
}

// FilledPercent is the percent of the order that has filled, without percent
// sign. Excludes cancel matches.
func (ord *OrderReader) FilledPercent() string {
	if ord.Type == order.CancelOrderType {
		return ""
	}
	return ord.percent(filledNonCancelFilter)
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
		return formatQty(ord.FeesPaid.Swap, ord.BaseFeeUnitInfo)
	}
	return formatQty(ord.FeesPaid.Swap, ord.QuoteFeeUnitInfo)
}

// RedemptionFeesString is a formatted string of the paid redemption fees.
func (ord *OrderReader) RedemptionFeesString() string {
	if ord.Sell {
		return formatQty(ord.FeesPaid.Redemption, ord.QuoteFeeUnitInfo)
	}
	return formatQty(ord.FeesPaid.Redemption, ord.BaseFeeUnitInfo)
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
	return (match.Side == order.Taker && match.Status >= order.MatchComplete) ||
		(match.Side == order.Maker && (match.Status >= order.MakerRedeemed))
}

func settlingFilter(match *Match) bool {
	return (match.Side == order.Taker && match.Status < order.MatchComplete) ||
		(match.Side == order.Maker && match.Status < order.MakerRedeemed)
}

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

// IsMaker returns if the user is the maker in this match.
func (m *matchReader) IsMaker() bool {
	return m.Side == order.Maker
}

// MakerSwapID returns the ID of the asset used in the maker's swap.
func (m *matchReader) MakerSwapID() uint32 {
	if m.Side == order.Maker {
		return m.ord.FromID()
	}
	return m.ord.ToID()
}

// TakerSwapID returns the ID of the asset used in the taker's swap.
func (m *matchReader) TakerSwapID() uint32 {
	if m.Side == order.Maker {
		return m.ord.ToID()
	}
	return m.ord.FromID()
}

// MakerSwapSymbol returns the symbol of the asset used in the maker's swap.
func (m *matchReader) MakerSwapSymbol() string {
	if m.Side == order.Maker {
		return m.ord.FromSymbol()
	}
	return m.ord.ToSymbol()
}

// TakerSwapSymbol returns the symbol of the asset used in the taker's swap.
func (m *matchReader) TakerSwapSymbol() string {
	if m.Side == order.Maker {
		return m.ord.ToSymbol()
	}
	return m.ord.FromSymbol()
}

// MakerSwap returns the coin created by the maker's swap transaction.
func (m *matchReader) MakerSwap() *Coin {
	if m.Side == order.Maker {
		return m.Swap
	}
	return m.CounterSwap
}

// TakerSwap returns the coin created by the taker's swap transaction.
func (m *matchReader) TakerSwap() *Coin {
	if m.Side == order.Maker {
		return m.CounterSwap
	}
	return m.Swap
}

// MakerRedeem returns the coin created by the maker's redeem transaction.
func (m *matchReader) MakerRedeem() *Coin {
	if m.Side == order.Maker {
		return m.Redeem
	}
	return m.CounterRedeem
}

// TakerRedeem returns the coin created by the taker's redeem transaction.
func (m *matchReader) TakerRedeem() *Coin {
	if m.Side == order.Maker {
		return m.CounterRedeem
	}
	return m.Redeem
}

// ShowMakerSwap returns whether or not to display the maker swap section
// on the match card.
func (m *matchReader) ShowMakerSwap() bool {
	return (m.MakerSwap() != nil || !m.Revoked) && !m.IsCancel
}

// ShowTakerSwap returns whether or not to display the taker swap section
// on the match card.
func (m *matchReader) ShowTakerSwap() bool {
	return (m.TakerSwap() != nil || !m.Revoked) && !m.IsCancel
}

// ShowMakerRedeem returns whether or not to display the maker redeem section
// on the match card.
func (m *matchReader) ShowMakerRedeem() bool {
	return (m.MakerRedeem() != nil || !m.Revoked) && !m.IsCancel
}

// ShowTakerRedeem returns whether or not to display the taker redeem section
// on the match card.
func (m *matchReader) ShowTakerRedeem() bool {
	return !m.IsMaker() && (m.TakerRedeem() != nil || !m.Revoked) && !m.IsCancel
}

// ShowRefund returns whether or not to display the refund section on the match
// card.
func (m *matchReader) ShowRefund() bool {
	return m.Refund != nil || (m.Revoked && m.Active)
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
		return "Newly Matched"
	case order.MakerSwapCast:
		return "Maker Swap Sent"
	case order.TakerSwapCast:
		return "Taker Swap Sent"
	case order.MakerRedeemed:
		if m.Side == order.Maker {
			return "Redemption Sent"
		}
		return "Maker Redeemed"
	case order.MatchComplete:
		return "Redemption Sent"
	case order.MatchConfirmed:
		return "Redemption Confirmed"
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

// InMakerSwapCast will be true if the last match step was the maker
// broadcasting their swap transaction.
func (m *matchReader) InMakerSwapCast() bool {
	if m.Revoked || m.Refund != nil {
		return false
	}

	return m.Match.Status == order.MakerSwapCast
}

// InTakerSwapCast will be true if the last match step was the taker
// broadcasting their swap transaction.
func (m *matchReader) InTakerSwapCast() bool {
	if m.Revoked || m.Refund != nil {
		return false
	}

	return m.Match.Status == order.TakerSwapCast
}

// MakerSwapConfirmString returns a string indicating the current confirmation
// progress of the maker's swap contract.
func (m *matchReader) MakerSwapConfirmString() string {
	makerSwap := m.MakerSwap()
	if !m.InMakerSwapCast() || makerSwap == nil {
		return ""
	}
	confs := makerSwap.Confs
	if confs == nil || confs.Required == 0 {
		return ""
	}
	if confs.Count < 0 {
		return "confirmations unknown"
	}
	return fmt.Sprintf("%d / %d confirmations", confs.Count, confs.Required)
}

// TakerSwapConfirmString returns a string indicating the current confirmation
// progress of the taker's swap contract.
func (m *matchReader) TakerSwapConfirmString() string {
	takerSwap := m.TakerSwap()
	if !m.InTakerSwapCast() || takerSwap == nil {
		return ""
	}
	confs := takerSwap.Confs
	if confs == nil || confs.Required == 0 {
		return ""
	}
	if confs.Count < 0 {
		return "confirmations unknown"
	}
	return fmt.Sprintf("%d / %d confirmations", confs.Count, confs.Required)
}

// ConfirmingMakerRedeem returns true if the user is the maker and the maker's
// redemption is being confirmed.
func (m *matchReader) ConfirmingMakerRedeem() bool {
	if m.Revoked || m.Refund != nil {
		return false
	}

	return m.Match.Side == order.Maker &&
		m.Match.Status < order.MatchConfirmed && m.Match.Status >= order.MakerRedeemed
}

// ConfirmingMakerRedeem returns true if the user is the taker and the taker's
// redemption is being confirmed.
func (m *matchReader) ConfirmingTakerRedeem() bool {
	if m.Revoked || m.Refund != nil {
		return false
	}

	return m.Match.Side == order.Taker &&
		m.Match.Status < order.MatchConfirmed && m.Match.Status >= order.MatchComplete
}

// InConfirmingRedeem returns true if the match has completed but the user's
// redemption has not yet been confirmed.
func (m *matchReader) InConfirmingRedeem() bool {
	return m.ConfirmingMakerRedeem() || m.ConfirmingTakerRedeem()
}

// ConfirmRedeemString returns a string indicating the current confirmation
// progress of the user's redemption. An empty string is returned if the
// user has not yet submit their redemption, or if it is already confirmed.
func (m *matchReader) ConfirmRedeemString() string {
	if !m.InConfirmingRedeem() || m.Redeem == nil {
		return ""
	}

	confs := m.Redeem.Confs
	if confs == nil || confs.Required == 0 {
		return ""
	}

	return fmt.Sprintf("%d / %d confirmations", confs.Count, confs.Required)
}
