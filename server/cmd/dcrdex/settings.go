// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"decred.org/dcrdex/dex"
	dexsrv "decred.org/dcrdex/server/dex"
)

type marketConfig struct {
	Markets []*struct {
		Base           string  `json:"base"`
		Quote          string  `json:"quote"`
		LotSize        uint64  `json:"lotSize"`
		RateStep       uint64  `json:"rateStep"`
		Duration       uint64  `json:"epochDuration"`
		MBBuffer       float64 `json:"marketBuyBuffer"`
		BookedLotLimit uint32  `json:"userBookedLotLimit"`
	} `json:"markets"`
	Assets map[string]*dexsrv.AssetConf `json:"assets"`
}

func loadMarketConfFile(network dex.Network, marketsJSON string) ([]*dex.MarketInfo, []*dexsrv.AssetConf, error) {
	src, err := os.Open(marketsJSON)
	if err != nil {
		return nil, nil, err
	}
	defer src.Close()
	return loadMarketConf(network, src)
}

func loadMarketConf(network dex.Network, src io.Reader) ([]*dex.MarketInfo, []*dexsrv.AssetConf, error) {
	settings, err := ioutil.ReadAll(src)
	if err != nil {
		return nil, nil, err
	}

	var conf marketConfig
	err = json.Unmarshal(settings, &conf)
	if err != nil {
		return nil, nil, err
	}

	log.Debug("-------------------- BEGIN parsed markets.json --------------------")
	log.Debug("MARKETS")
	log.Debug("                  Base         Quote    LotSize     EpochDur")
	for i, mktConf := range conf.Markets {
		if mktConf.LotSize == 0 {
			return nil, nil, fmt.Errorf("market (%s, %s) has NO lot size specified (was an asset setting)",
				mktConf.Base, mktConf.Quote)
		}
		if mktConf.RateStep == 0 {
			return nil, nil, fmt.Errorf("market (%s, %s) has NO rate step specified (was an asset setting)",
				mktConf.Base, mktConf.Quote)
		}
		log.Debugf("Market %d: % 12s  % 12s   %6de8  % 8d ms",
			i, mktConf.Base, mktConf.Quote, mktConf.LotSize/1e8, mktConf.Duration)
	}
	log.Debug("")

	log.Debug("ASSETS")
	log.Debug("             MaxFeeRate   SwapConf   Network")
	for asset, assetConf := range conf.Assets {
		if assetConf.LotSizeOLD > 0 {
			return nil, nil, fmt.Errorf("asset %s has a lot size (%d) specified, "+
				"but this is now a market setting", asset, assetConf.LotSizeOLD)
		}
		if assetConf.RateStepOLD > 0 {
			return nil, nil, fmt.Errorf("asset %s has a rate step (%d) specified, "+
				"but this is now a market setting", asset, assetConf.RateStepOLD)
		}
		log.Debugf("%-12s % 10d  % 9d % 9s", asset, assetConf.MaxFeeRate, assetConf.SwapConf, assetConf.Network)
	}
	log.Debug("--------------------- END parsed markets.json ---------------------")

	// Normalize the asset names to lower case.
	var assets []*dexsrv.AssetConf
	for assetName, assetConf := range conf.Assets {
		net, err := dex.NetFromString(assetConf.Network)
		if err != nil {
			return nil, nil, fmt.Errorf("unrecognized network %s for asset %s",
				assetConf.Network, assetName)
		}
		if net != network {
			continue
		}

		symbol := strings.ToLower(assetConf.Symbol)
		_, found := dex.BipSymbolID(symbol)
		if !found {
			return nil, nil, fmt.Errorf("asset %q symbol %q unrecognized", assetName, assetConf.Symbol)
		}

		if assetConf.MaxFeeRate == 0 {
			return nil, nil, fmt.Errorf("max fee rate of 0 is invalid for asset %q", assetConf.Symbol)
		}

		assets = append(assets, assetConf)
	}

	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Symbol < assets[j].Symbol
	})

	var markets []*dex.MarketInfo
	for _, mktConf := range conf.Markets {
		baseConf, ok := conf.Assets[mktConf.Base]
		if !ok {
			return nil, nil, fmt.Errorf("Missing configuration for asset %s", mktConf.Base)
		}
		quoteConf, ok := conf.Assets[mktConf.Quote]
		if !ok {
			return nil, nil, fmt.Errorf("Missing configuration for asset %s", mktConf.Quote)
		}

		baseNet, err := dex.NetFromString(baseConf.Network)
		if err != nil {
			return nil, nil, fmt.Errorf("Unrecognized network %s", baseConf.Network)
		}
		quoteNet, err := dex.NetFromString(quoteConf.Network)
		if err != nil {
			return nil, nil, fmt.Errorf("Unrecognized network %s", quoteConf.Network)
		}

		if baseNet != quoteNet {
			return nil, nil, fmt.Errorf("Assets are for different networks (%s and %s)",
				baseConf.Network, quoteConf.Network)
		}

		if baseNet != network {
			continue
		}

		mkt, err := dex.NewMarketInfoFromSymbols(baseConf.Symbol, quoteConf.Symbol,
			mktConf.LotSize, mktConf.RateStep, mktConf.Duration, mktConf.MBBuffer)
		if err != nil {
			return nil, nil, err
		}
		if mktConf.BookedLotLimit != 0 {
			mkt.BookedLotLimit = mktConf.BookedLotLimit
		}
		markets = append(markets, mkt)
	}

	return markets, assets, nil
}
