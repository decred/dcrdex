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
		Base                 string  `json:"base"`
		Quote                string  `json:"quote"`
		Duration             uint64  `json:"epochDuration"`
		MBBuffer             float64 `json:"marketBuyBuffer"`
		BookedLotLimit       uint32  `json:"userBookedLotLimit"`
		ProbationaryLotLimit uint32  `json:"probationaryLotLimit"`
		PrivilegedLotLimit   uint32  `json:"privilegedLotLimit"`
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
	log.Debug("                  Base         Quote   EpochDur      Unpriv     Priv")
	for i, mktConf := range conf.Markets {
		log.Debugf("Market %d: % 12s  % 12s  % 8d ms %d %d", i, mktConf.Base, mktConf.Quote,
			mktConf.Duration, mktConf.ProbationaryLotLimit, mktConf.PrivilegedLotLimit)
	}
	log.Debug("")

	log.Debug("ASSETS")
	log.Debug("                  LotSize     RateStep   MaxFeeRate   Network")
	for asset, assetConf := range conf.Assets {
		log.Debugf("%-12s % 12d % 12d % 12d % 9s", asset, assetConf.LotSize,
			assetConf.RateStep, assetConf.MaxFeeRate, assetConf.Network)
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
			baseConf.LotSize, mktConf.Duration, mktConf.MBBuffer)
		if err != nil {
			return nil, nil, err
		}
		if mktConf.BookedLotLimit != 0 {
			mkt.BookedLotLimit = mktConf.BookedLotLimit
		}
		if mktConf.ProbationaryLotLimit != 0 {
			mkt.ProbationaryLots = mktConf.ProbationaryLotLimit
		}
		if mktConf.PrivilegedLotLimit != 0 {
			mkt.PrivilegedLots = mktConf.PrivilegedLotLimit
		}
		markets = append(markets, mkt)
	}

	return markets, assets, nil
}
