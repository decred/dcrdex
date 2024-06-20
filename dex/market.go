// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"math"
	"strings"
)

const (
	// defaultEpochDuration is the spec defined default markets epoch duration
	// in milliseconds.
	defaultEpochDuration uint64 = 20000

	// Parcels are used to track user trading limits. A parcel is a number of
	// lots. How many lots are in a parcel is defined as part of a market's
	// configuration. Markets with low-fee assets might have very small lot
	// sizes, so would set a large parcel size. A market with high-fee assets
	// would have a larger lot size, so would set a small parcel size.

	// Parcels are tracked accross all known markets, and trading limits are
	// assessed globally.

	// When calculating trading limits, each market will report its parcels
	// for the user.

	// Parcel limits are measured in units of "parcel weight", which is kind of
	// like "effective quantity". For makers, parcel weight = quantity. Orders
	// which are likely to be takers have their parcel weight doubled. The
	// quantity in settling matches has the same weight as maker quantity.

	// PerTierBaseParcelLimit is the number of parcels users can trade per tier.
	PerTierBaseParcelLimit = 2
	// ParcelLimitScoreMultiplier defines the maximum parcel limit multiplier
	// that a user achieves with max score. The users parcel limit ranges from
	// the starting value of tier*PerTierBaseParcelLimit to the maximum value of
	// tier*PerTierBaseParcelLimit*ParcelLimitScoreMultiplier. By default,
	// singly-tiered users have max lot limits limited to the range (8,24).
	ParcelLimitScoreMultiplier = 3
)

// MarketInfo specifies a market that the Archiver must support.
type MarketInfo struct {
	Name                   string
	Base                   uint32
	Quote                  uint32
	LotSize                uint64
	ParcelSize             uint32 // number of lots in the base trading limit.
	RateStep               uint64
	EpochDuration          uint64 // msec
	MarketBuyBuffer        float64
	MaxUserCancelsPerEpoch uint32
}

func marketName(base, quote string) string {
	return base + "_" + quote
}

// MarketName creates the string representation of a DEX market (e.g. "dcr_btc")
// given the base and quote asset indexes defined in BIP-0044. See also
// BipIDSymbol.
func MarketName(baseID, quoteID uint32) (string, error) {
	baseSymbol := BipIDSymbol(baseID)
	if baseSymbol == "" {
		return "", fmt.Errorf("base asset %d not found", baseID)
	}
	baseSymbol = strings.ToLower(baseSymbol)

	quoteSymbol := BipIDSymbol(quoteID)
	if quoteSymbol == "" {
		return "", fmt.Errorf("quote asset %d not found", quoteID)
	}
	quoteSymbol = strings.ToLower(quoteSymbol)

	return marketName(baseSymbol, quoteSymbol), nil
}

// NewMarketInfo creates a new market configuration (MarketInfo) from the given
// base and quote asset indexes, order lot size, and epoch duration in
// milliseconds. See also MarketName.
// TODO: Only used in tests. Should just use NewMarketInfoFromSymbols instead.
func NewMarketInfo(base, quote uint32, lotSize, rateStep, epochDuration uint64, marketBuyBuffer float64) (*MarketInfo, error) {
	name, err := MarketName(base, quote)
	if err != nil {
		return nil, err
	}

	// Check for sensible epoch duration.
	if epochDuration == 0 {
		epochDuration = defaultEpochDuration
	}

	return &MarketInfo{
		Name:                   name,
		Base:                   base,
		Quote:                  quote,
		LotSize:                lotSize,
		ParcelSize:             1,
		RateStep:               rateStep,
		EpochDuration:          epochDuration,
		MarketBuyBuffer:        marketBuyBuffer,
		MaxUserCancelsPerEpoch: math.MaxUint32,
	}, nil
}

// NewMarketInfoFromSymbols is like NewMarketInfo, but the base and quote assets
// are identified by their symbols as defined in the
// decred.org/dcrdex/server/asset package.
func NewMarketInfoFromSymbols(base, quote string, lotSize, rateStep, epochDuration uint64, parcelSize uint32, marketBuyBuffer float64) (*MarketInfo, error) {
	base = strings.ToLower(base)
	baseID, found := BipSymbolID(base)
	if !found {
		return nil, fmt.Errorf(`base asset symbol "%s" unrecognized`, base)
	}

	quote = strings.ToLower(quote)
	quoteID, found := BipSymbolID(quote)
	if !found {
		return nil, fmt.Errorf(`quote asset symbol "%s" unrecognized`, quote)
	}

	// Check for sensible epoch duration.
	if epochDuration == 0 {
		epochDuration = defaultEpochDuration
	}

	return &MarketInfo{
		Name:                   marketName(base, quote),
		Base:                   baseID,
		Quote:                  quoteID,
		LotSize:                lotSize,
		ParcelSize:             parcelSize,
		RateStep:               rateStep,
		EpochDuration:          epochDuration,
		MarketBuyBuffer:        marketBuyBuffer,
		MaxUserCancelsPerEpoch: math.MaxUint32,
	}, nil
}

// String returns the market's Name.
func (mi *MarketInfo) String() string {
	return mi.Name
}
