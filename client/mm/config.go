package mm

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

// BotConfig is the configuration for a market making bot.
type BotConfig struct {
	Host       string `json:"host"`
	BaseAsset  uint32 `json:"baseAsset"`
	QuoteAsset uint32 `json:"quoteAsset"`

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
