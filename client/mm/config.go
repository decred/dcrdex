package mm

import (
	"fmt"
)

type BalanceType uint8

const (
	Percentage BalanceType = iota
	Amount
)

// MarketMakingConfig is the overall configuration of the market maker.
type MarketMakingConfig struct {
	BotConfigs []*BotConfig `json:"botConfigs"`
	CexConfigs []*CEXConfig `json:"cexConfigs"`
}

// CEXConfig is a configuration for connecting to a CEX API.
type CEXConfig struct {
	// Name is the name of the cex.
	Name string `json:"name"`
	// APIKey is the API key for the CEX.
	APIKey string `json:"apiKey"`
	// APISecret is the API secret for the CEX.
	APISecret string `json:"apiSecret"`
}

// BotCEXCfg specifies the CEX that a bot uses and the initial balances
// that should be allocated to the bot on that CEX.
type BotCEXCfg struct {
	Name             string      `json:"name"`
	BaseBalanceType  BalanceType `json:"baseBalanceType"`
	BaseBalance      uint64      `json:"baseBalance"`
	QuoteBalanceType BalanceType `json:"quoteBalanceType"`
	QuoteBalance     uint64      `json:"quoteBalance"`
}

// BotConfig is the configuration for a market making bot.
// The balance fields are the initial amounts that will be reserved to use for
// this bot. As the bot trades, the amounts reserved for it will be updated.
type BotConfig struct {
	Host             string      `json:"host"`
	BaseAsset        uint32      `json:"baseAsset"`
	QuoteAsset       uint32      `json:"quoteAsset"`
	BaseBalanceType  BalanceType `json:"baseBalanceType"`
	BaseBalance      uint64      `json:"baseBalance"`
	QuoteBalanceType BalanceType `json:"quoteBalanceType"`
	QuoteBalance     uint64      `json:"quoteBalance"`

	// Only applicable for arb bots.
	CEXCfg *BotCEXCfg `json:"cexCfg"`

	// Only one of the following configs should be set
	BasicMMConfig        *BasicMarketMakingConfig `json:"basicMarketMakingConfig,omitempty"`
	SimpleArbConfig      *SimpleArbConfig         `json:"simpleArbConfig,omitempty"`
	ArbMarketMakerConfig *ArbMarketMakerConfig    `json:"arbMarketMakerConfig,omitempty"`

	Disabled bool `json:"disabled"`
}

func (c *BotConfig) requiresPriceOracle() bool {
	if c.BasicMMConfig != nil {
		return c.BasicMMConfig.OracleWeighting != nil && *c.BasicMMConfig.OracleWeighting > 0
	}
	return false
}

func (c *BotConfig) requiresCEX() bool {
	return c.SimpleArbConfig != nil || c.ArbMarketMakerConfig != nil
}

func dexMarketID(host string, base, quote uint32) string {
	return fmt.Sprintf("%s-%d-%d", host, base, quote)
}
