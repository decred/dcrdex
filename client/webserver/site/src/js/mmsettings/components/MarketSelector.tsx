import React, { useState } from 'react'
import Doc from '../../doc'
import { CEXDisplayInfos } from '../../mmutil'
import { renderSymbol, AvailableMarket } from './MMSettings'
import { prep, ID_MM_SELECT_MARKET, ID_MM_SEARCH_MARKETS, ID_MM_MARKET_HEADER, ID_MM_HOST_HEADER, ID_MM_ARB_HEADER } from '../../locales'

interface MarketData {
  baseSymbol: string;
  quoteSymbol: string;
  baseID: number;
  quoteID: number;
  host: string;
  hasArb: boolean;
  supportedCexes: string[];
}

interface CexMarketSupportChecker {
  (baseID: number, quoteID: number, cexName: string, directOnly: boolean): boolean;
}

interface MarketSelectorProps {
  markets?: AvailableMarket[];
  exchangesRequiringRegistration?: string[];
  cexes?: Record<string, import('../../registry').MMCEXStatus>;
  checkCexMarketSupport?: CexMarketSupportChecker;
  onClose: () => void;
  handleMarketSelected: (host: string, baseID: number, quoteID: number, baseSymbol: string, quoteSymbol: string) => void;
}

const MarketSelector: React.FC<MarketSelectorProps> = ({
  markets = [],
  exchangesRequiringRegistration = [],
  cexes = {},
  checkCexMarketSupport,
  onClose,
  handleMarketSelected
}) => {
  const [filterText, setFilterText] = useState('')

  // Check which CEXes support arbitrage for each market
  const getSupportedCexes = (market: AvailableMarket) => {
    const supportedCexes: string[] = []
    if (checkCexMarketSupport) {
      for (const cexName of Object.keys(cexes)) {
        if (checkCexMarketSupport(market.baseID, market.quoteID, cexName, false)) {
          supportedCexes.push(cexName)
        }
      }
    }
    return supportedCexes
  }

  // Convert AvailableMarket data to MarketData format for the component
  const marketData: MarketData[] = markets.map(market => ({
    baseSymbol: market.baseSymbol,
    quoteSymbol: market.quoteSymbol,
    host: market.host,
    hasArb: market.hasArb,
    baseID: market.baseID,
    quoteID: market.quoteID,
    supportedCexes: getSupportedCexes(market)
  }))

  // Filter markets based on search text
  const filteredMarkets = marketData.filter(market =>
    market.baseSymbol.toLowerCase().includes(filterText.toLowerCase()) ||
    market.quoteSymbol.toLowerCase().includes(filterText.toLowerCase()) ||
    market.host.toLowerCase().includes(filterText.toLowerCase())
  )

  const handleMarketClick = (market: MarketData) => {
    handleMarketSelected(market.host, market.baseID, market.quoteID, market.baseSymbol, market.quoteSymbol)
  }

  return (
    <div id="forms" className="stylish-overflow flex-center">
      <form id="marketSelectForm" className="flex-stretch-column position-relative">
        {/* Form Closer */}
        <div className="form-closer">
          <span className="ico-cross pointer" onClick={onClose}></span>
        </div>

        {/* Form Content */}
        <div>
          {/* Header */}
          <header className="d-flex align-items-center mb-3">
            <span className="me-2 ico-robot fs35 pb-1"></span>
            <span className="fs26">{prep(ID_MM_SELECT_MARKET)}</span>
          </header>

          {/* Market Filter Box */}
          <div id="marketFilterBox" className="flex-center mb-3">
            <div className="position-relative">
              <input
                id="marketFilterInput"
                type="text"
                placeholder={prep(ID_MM_SEARCH_MARKETS)}
                value={filterText}
                onChange={(e) => setFilterText(e.target.value)}
              />
              <span id="marketFilterIcon" className="fs22 ico-search"></span>
            </div>
          </div>

      {/* Market Selection Table */}
      <div id="marketSelectionTable">
        <table className="row-border w-100">
          <thead>
            <tr>
              <th>{prep(ID_MM_MARKET_HEADER)}</th>
              <th>{prep(ID_MM_HOST_HEADER)}</th>
              <th>{prep(ID_MM_ARB_HEADER)}</th>
            </tr>
          </thead>
          <tbody id="marketSelect">
            {filteredMarkets.map((market, index) => (
              <tr
                key={`${market.baseSymbol}-${market.quoteSymbol}-${market.host}-${index}`}
                className="hoverbg pointer"
                onClick={() => handleMarketClick(market)}
              >
                <td>
                  <div className="d-flex align-items-center">
                    <img
                      src={Doc.logoPath(market.baseSymbol)}
                      className="mini-icon me-1"
                    />
                    <img
                      src={Doc.logoPath(market.quoteSymbol)}
                      className="mini-icon me-1"
                    />
                    {renderSymbol(market.baseID, market.baseSymbol)}-
                    {renderSymbol(market.quoteID, market.quoteSymbol)}
                  </div>
                </td>
                <td>{market.host}</td>
                <td>
                  <div className="d-flex flex-wrap gap-1">
                    {market.supportedCexes.map(cexName => {
                      const cexInfo = CEXDisplayInfos[cexName]
                      return cexInfo
                        ? (
                        <img
                          key={cexName}
                          src={cexInfo.logo}
                          className="mini-icon"
                        />
                          )
                        : null
                    })}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* No Markets Message */}
      {filteredMarkets.length === 0 && (
        <div id="noMarkets" className="flex-center py-4">
          No markets available
        </div>
      )}

          {/* Registration Required Buttons */}
          {exchangesRequiringRegistration.map(host => (
            <div key={host} id="needRegBox" className="flex-stretch-column pt-0">
              <button
                type="button"
                id="needRegTmpl"
                className="mt-3"
                onClick={() => {
                  // Navigate to register page with host and backTo parameters
                  window.location.href = `/register?host=${encodeURIComponent(host)}&backTo=mmsettings`
                }}
              >
                Register {host} to enable market making
              </button>
            </div>
          ))}
        </div>
      </form>
    </div>
  )
}

export default MarketSelector
