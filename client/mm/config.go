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

func (cfg *MarketMakingConfig) Copy() *MarketMakingConfig {
	c := &MarketMakingConfig{
		BotConfigs: make([]*BotConfig, len(cfg.BotConfigs)),
		CexConfigs: make([]*CEXConfig, len(cfg.CexConfigs)),
	}
	copy(c.BotConfigs, cfg.BotConfigs)
	copy(c.CexConfigs, cfg.CexConfigs)
	return c
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

// AutoRebalanceConfig determines how the bot will automatically rebalance its
// assets between the CEX and DEX. If the base or quote asset dips below the
// minimum amount, a transfer will take place, but only if both balances can be
// brought above the minimum amount and the transfer amount would be above the
// minimum transfer amount.
type AutoRebalanceConfig struct {
	MinBaseAmt       uint64 `json:"minBaseAmt"`
	MinBaseTransfer  uint64 `json:"minBaseTransfer"`
	MinQuoteAmt      uint64 `json:"minQuoteAmt"`
	MinQuoteTransfer uint64 `json:"minQuoteTransfer"`
}

// BotCEXCfg specifies the CEX that a bot uses, the initial balances
// that should be allocated to the bot on that CEX, and the configuration
// for automatically rebalancing between the CEX and DEX.
type BotCEXCfg struct {
	Name             string               `json:"name"`
	BaseBalanceType  BalanceType          `json:"baseBalanceType"`
	BaseBalance      uint64               `json:"baseBalance"`
	QuoteBalanceType BalanceType          `json:"quoteBalanceType"`
	QuoteBalance     uint64               `json:"quoteBalance"`
	AutoRebalance    *AutoRebalanceConfig `json:"autoRebalance"`
}

// #### IMPORTANT ###
// If non-backwards compatible changes are made to the BotConfig, a new version
// should be created and the event log db should be updated to support both
// versions.

// BotConfig is the configuration for a market making bot.
// The balance fields are the initial amounts that will be reserved to use for
// this bot. As the bot trades, the amounts reserved for it will be updated.
type BotConfig struct {
	Host    string `json:"host"`
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`

	BaseBalanceType BalanceType `json:"baseBalanceType"`
	BaseBalance     uint64      `json:"baseBalance"`

	QuoteBalanceType BalanceType `json:"quoteBalanceType"`
	QuoteBalance     uint64      `json:"quoteBalance"`

	BaseFeeAssetBalanceType  BalanceType `json:"baseFeeAssetBalanceType"`
	BaseFeeAssetBalance      uint64      `json:"baseFeeAssetBalance"`
	QuoteFeeAssetBalanceType BalanceType `json:"quoteFeeAssetBalanceType"`
	QuoteFeeAssetBalance     uint64      `json:"quoteFeeAssetBalance"`

	BaseWalletOptions  map[string]string `json:"baseWalletOptions"`
	QuoteWalletOptions map[string]string `json:"quoteWalletOptions"`

	// Only applicable for arb bots.
	CEXCfg *BotCEXCfg `json:"cexCfg"`

	// Only one of the following configs should be set
	BasicMMConfig        *BasicMarketMakingConfig `json:"basicMarketMakingConfig,omitempty"`
	SimpleArbConfig      *SimpleArbConfig         `json:"simpleArbConfig,omitempty"`
	ArbMarketMakerConfig *ArbMarketMakerConfig    `json:"arbMarketMakingConfig,omitempty"`

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
