package types

import (
	"fmt"
	"strings"

	"github.com/decred/dcrdex/server/asset"
)

// MarketInfo specified a market that the Archiver must support.
type MarketInfo struct {
	Name    string
	Base    uint32
	Quote   uint32
	LotSize uint64
}

func marketName(base, quote string) string {
	return base + "_" + quote
}

// MarketName creates the string representation of a DEX market (e.g. "dcr_btc")
// given the base and quote asset indexes defined in BIP-0044. See also
// github.com/decred/dcrdex/server/asset.BipIDSymbol.
func MarketName(base, quote uint32) (string, error) {
	baseSymbol := asset.BipIDSymbol(base)
	if baseSymbol == "" {
		return "", fmt.Errorf("base asset %d not found", base)
	}
	baseSymbol = strings.ToLower(baseSymbol)

	quoteSymbol := asset.BipIDSymbol(quote)
	if quoteSymbol == "" {
		return "", fmt.Errorf("quote asset %d not found", quote)
	}
	quoteSymbol = strings.ToLower(quoteSymbol)

	return marketName(baseSymbol, quoteSymbol), nil
}

// NewMarketInfo creates a new market configuration (MarketInfo) from the given
// base and quote asset indexes, and order lot size. See also MarketName.
func NewMarketInfo(base, quote uint32, lotSize uint64) (*MarketInfo, error) {
	name, err := MarketName(base, quote)
	if err != nil {
		return nil, err
	}
	return &MarketInfo{
		Name:    name,
		Base:    base,
		Quote:   quote,
		LotSize: lotSize,
	}, nil
}

// NewMarketInfoFromSymbols is like NewMarketInfo, but the base and quote assets
// are identified by their symbols as defined in the
// github.com/decred/dcrdex/server/asset package.
func NewMarketInfoFromSymbols(base, quote string, lotSize uint64) (*MarketInfo, error) {
	baseID, found := asset.BipSymbolID(strings.ToLower(base))
	if !found {
		return nil, fmt.Errorf(`base asset symbol "%s" unrecognized`, base)
	}

	quoteID, found := asset.BipSymbolID(strings.ToLower(quote))
	if !found {
		return nil, fmt.Errorf(`quote asset symbol "%s" unrecognized`, quote)
	}

	return &MarketInfo{
		Name:    marketName(base, quote),
		Base:    baseID,
		Quote:   quoteID,
		LotSize: lotSize,
	}, nil
}

// OrderStatus indicates the state of an order.
type OrderStatus uint16

// There are two general classes of orders: ACTIVE and ARCHIVED. Orders with one
// of the ACTIVE order statuses that follow are likely to be updated.
const (
	// OrderStatusUnknown is a sentinel value to be used when the status of an
	// order cannot be determined.
	OrderStatusUnknown OrderStatus = iota

	// OrderStatusEpoch is for orders that have been received and validated, but
	// not processed yet by the epoch order matcher.
	OrderStatusEpoch

	// OrderStatusBooked is for orders that have been put on the book
	// ("standing" time in force). This includes partially filled orders. As
	// such, when an order with this "booked" status is matched with another
	// order, it should have its filled amount updated, and its status should
	// only be changed to OrderStatusExecuted if the remaining quantity becomes
	// less than the lot size, or perhaps to OrderStatusCanceled if the swap has
	// failed and DEX conduct policy requires that it be removed from the order
	// book.
	OrderStatusBooked

	// OrderStatusExecuted is for orders that have been successfully processed
	// and taken off the book. An order may reach this state if it is (1)
	// matched one or more times and removed from the books, or (2) unmatched in
	// epoch processing and with a time-in-force that forbids the order from
	// entering the books. Orders in the first category (matched and
	// subsequently removed from the book) include: a matched cancel order, a
	// completely filled limit or market order, or a partially filled market buy
	// order. Market and limit orders in the second category necessarily will
	// necessarily be completely unfilled. Partially filled orders that are
	// still on the order book remain in OrderStatusBooked.
	//
	// QUESTION: How do we distinguish a CancelOrder that was not matched from
	// one that was matched? With a limit or market order that was not matched,
	// the filled amount shows this, but cancel orders have no meaningful filled
	// amount. Having to search a matches table is undesirable, especially since
	// it is a worst-case query returning zero rows.
	OrderStatusExecuted

	// OrderStatusCanceled is for orders that were on the book, but matched with
	// a cancel order. This does not mean the order is completely unfilled.
	OrderStatusCanceled

	// QUESTION: What status is for standing limit orders that were matched, but
	// have failed to swap, and do not re-enter the book? Are they forcibly
	// canceled (OrderStatusCanceled) or are they considered executed
	// (OrderStatusExecuted)? Does this also depend on DEX policy regarding the
	// circumstances of swap failure?

	// The above QUESTIONs suggest that a failed status may be needed, although
	// perhaps just internal to the DB backend's implementation, to quickly
	// identify orders that failed to match or failed in the swap stage.
)

var orderStatusNames = map[OrderStatus]string{
	OrderStatusUnknown:  "unknown",
	OrderStatusEpoch:    "epoch",
	OrderStatusBooked:   "booked",
	OrderStatusExecuted: "executed",
	OrderStatusCanceled: "canceled",
}

// String implements Stringer.
func (s OrderStatus) String() string {
	name, ok := orderStatusNames[s]
	if !ok {
		panic("unknown order status!") // programmer error
	}
	return name
}
