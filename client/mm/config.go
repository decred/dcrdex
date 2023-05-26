package mm

import (
	"fmt"
)

// MarketMakingWithCEXConfig is the configuration for a market
// maker that places orders on both sides of the order book, but
// only if there is profitable counter-trade on the CEX
// order book.
type MarketMakingWithCEXConfig struct {
}

// ArbitrageConfig is the configuration for an arbitrage bot that only places
// when there is a profitable arbitrage opportunity.
type ArbitrageConfig struct {
}

type BalanceType uint8

const (
	Percentage BalanceType = iota
	Amount
)

// BotConfig is the configuration for a market making bot.
// The balance fields are the initial amounts that will be reserved to use for
// this bot. As the bot trades, the amounts reserved for it will be updated.
type BotConfig struct {
	Host       string `json:"host"`
	BaseAsset  uint32 `json:"baseAsset"`
	QuoteAsset uint32 `json:"quoteAsset"`

	BaseBalanceType BalanceType `json:"baseBalanceType"`
	BaseBalance     uint64      `json:"baseBalance"`

	QuoteBalanceType BalanceType `json:"quoteBalanceType"`
	QuoteBalance     uint64      `json:"quoteBalance"`

	// Only one of the following configs should be set
	MMCfg        *MarketMakingConfig        `json:"marketMakingConfig,omitempty"`
	MMWithCEXCfg *MarketMakingWithCEXConfig `json:"marketMakingWithCEXConfig,omitempty"`
	ArbCfg       *ArbitrageConfig           `json:"arbitrageConfig,omitempty"`
}

func (c *BotConfig) requiresPriceOracle() bool {
	if c.MMCfg != nil {
		return c.MMCfg.OracleWeighting != nil && *c.MMCfg.OracleWeighting > 0
	}
	return false
}

func dexMarketID(host string, base, quote uint32) string {
	return fmt.Sprintf("%s-%d-%d", host, base, quote)
}
