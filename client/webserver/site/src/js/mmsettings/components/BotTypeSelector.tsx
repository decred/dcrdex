import React, { useState, useCallback, useEffect } from 'react'
import Doc from '../../doc'
import { app, MarketWithHost, MMCEXStatus } from '../../registry'
import { CEXDisplayInfos } from '../../mmutil'
import { renderSymbol } from './MMSettings'
import CEXConfigForm from './CEXConfigForm'
import {
  prep,
  ID_MM_CONFIGURE,
  ID_MM_FIX_ERRORS,
  ID_MM_MARKET_NOT_AVAILABLE,
  ID_MM_CHOOSE_BOT,
  ID_MM_BASIC_MARKET_MAKER,
  ID_MM_MM_PLUS_ARB,
  ID_MM_BASIC_ARBITRAGE,
  ID_MM_SUBMIT
} from '../../locales'

interface CexMarketSupportChecker {
  (baseID: number, quoteID: number, cexName: string, directOnly: boolean): boolean;
}

interface BotTypeSelectorProps {
  selectedMarket: MarketWithHost;
  cexes?: Record<string, MMCEXStatus>;
  checkCexMarketSupport?: CexMarketSupportChecker;
  onClose?: () => void;
  onBotTypeSelected: (botType: 'basicMM' | 'arbMM' | 'basicArb', cexName?: string) => void;
  onChangeMarket?: () => void;
  handleCEXesUpdated: () => void;
}

interface CEXIconProps {
  cexName: string;
  isSelected: boolean;
  cexStatus: MMCEXStatus | null;
  supportsArbitrage: boolean;
  onSelect: () => void;
  onConfigure: () => void;
  onReconfigure: (e: React.MouseEvent) => void;
}

const CEXIcon: React.FC<CEXIconProps> = ({
  cexName,
  isSelected,
  cexStatus,
  supportsArbitrage,
  onSelect,
  onConfigure,
  onReconfigure
}) => {
  const cexInfo = CEXDisplayInfos[cexName]
  const isConfigured = cexStatus !== null

  const handleClick = () => {
    if (isConfigured && !cexStatus?.connectErr) {
      onSelect()
    } else {
      onConfigure()
    }
  }

  const statusIndicator = () : React.ReactNode => {
    if (!isConfigured) {
      return (
      <span className="fs14 grey flex-center">
        <span className="ico-settings fs12 me-1"></span>
        <span>{prep(ID_MM_CONFIGURE)}</span>
      </span>
      )
    }

    if (cexStatus.connectErr) {
      return (
      <span className="flex-center">
        <span className="ico-disconnected fs16 text-danger me-1"></span>
        <span className="fs14 text-danger">{prep(ID_MM_FIX_ERRORS)}</span>
      </span>
      )
    }

    if (!supportsArbitrage) {
      return (
      <span className="fs14 grey">{prep(ID_MM_MARKET_NOT_AVAILABLE)}</span>
      )
    }

    return null
  }

  return (
    <div
      key={cexName}
      className={`position-relative p-2 col-11 flex-center flex-column border rounded3 cex-selector ${isSelected ? 'selected' : ''}`}
      onClick={handleClick}
    >
      {isConfigured && !cexStatus?.connectErr && (
        <div
          className="fs14 ico-settings p-2 hoverbg pointer reconfig position-absolute top-0 end-0"
          onClick={onReconfigure}
        >
        </div>
      )}

      <div className="flex-center lh1">
        <img
          className="mini-icon me-1 xclogo medium-icon"
          src={cexInfo?.logo || '/img/coins/question.png'}
          alt={cexName}
        />
        <span className="me-1 fs20 text-nowrap">{cexName}</span>
      </div>

      {statusIndicator()}
    </div>
  )
}

const BotTypeSelector: React.FC<BotTypeSelectorProps> = ({
  selectedMarket,
  cexes = {},
  checkCexMarketSupport,
  onClose,
  onBotTypeSelected,
  onChangeMarket,
  handleCEXesUpdated
}) => {
  const checkCexArbitrageSupport = useCallback((cexName: string, directOnly: boolean): boolean => {
    if (!checkCexMarketSupport) return false
    return checkCexMarketSupport(selectedMarket.baseID, selectedMarket.quoteID, cexName, directOnly)
  }, [checkCexMarketSupport, selectedMarket.baseID, selectedMarket.quoteID])

  const findSupportedCex = useCallback((directOnly: boolean): string => {
    for (const cexName of Object.keys(cexes || {})) {
      if (checkCexArbitrageSupport(cexName, directOnly)) {
        return cexName
      }
    }
    return ''
  }, [cexes, checkCexArbitrageSupport])

  const [selectedBotType, setSelectedBotType] = useState<'basicMM' | 'arbMM' | 'basicArb'>('basicMM')
  const [selectedCex, setSelectedCex] = useState<string>('')
  const [configuringCex, setConfiguringCex] = useState<string | null>(null)

  useEffect(() => {
    const directOnly = selectedBotType === 'basicArb'
    const supportedCex = findSupportedCex(directOnly)
    if (!selectedCex || !checkCexArbitrageSupport(selectedCex, directOnly)) {
      setSelectedCex(supportedCex)
    }
  }, [findSupportedCex, selectedCex, checkCexArbitrageSupport, selectedBotType])

  const handleBotTypeSelect = (botType: 'basicMM' | 'arbMM' | 'basicArb') => {
    setSelectedBotType(botType)

    if (botType === 'basicMM') {
      setSelectedCex('')
      return
    }

    if (botType === 'arbMM' || botType === 'basicArb') {
      const directOnly = botType === 'basicArb'
      const supportedCex = findSupportedCex(directOnly)
      setSelectedCex(supportedCex)
    }
  }

  const handleCexSelect = (cexName: string) => {
    setSelectedCex(cexName)
  }

  const handleConfigureCex = (cexName: string) => {
    setConfiguringCex(cexName)
  }

  const handleCexConfigClose = () => {
    setConfiguringCex(null)
  }

  const handleSubmit = () => {
    if (selectedBotType) {
      let cex : string | undefined = selectedCex
      if (selectedBotType === 'basicMM' || cex === '') cex = undefined
      onBotTypeSelected(selectedBotType, cex)
    }
  }

  const baseSymbol = app().assets[selectedMarket.baseID]?.symbol ?? ''
  const quoteSymbol = app().assets[selectedMarket.quoteID]?.symbol ?? ''
  const availableCexes = Object.keys(CEXDisplayInfos)
  const isArbBotSelected = selectedBotType === 'arbMM' || selectedBotType === 'basicArb'
  const isSubmitDisabled = !selectedBotType || (isArbBotSelected && !selectedCex)

  return (
    <div id="forms" className="stylish-overflow flex-center">
      <form id="botTypeForm" className="position-relative mw-425 stylish-overflow" autoComplete="off">
        <div className="form-closer">
            <span className="ico-cross pointer" onClick={onClose}></span>
        </div>

        <header className="d-flex align-items-center mb-3">
          <div className="d-flex align-items-center">
            <img className="mini-icon me-1" src={Doc.logoPath(baseSymbol)} />
            <img className="mini-icon me-1 ms-1" src={Doc.logoPath(quoteSymbol)} />
          </div>
          {renderSymbol(selectedMarket.baseID, baseSymbol)}-
          {renderSymbol(selectedMarket.quoteID, quoteSymbol)}
          <span className="p-2 fs14 ico-edit hoverbg pointer" onClick={onChangeMarket}></span>
        </header>

        <div className="flex-center mb-3">
          <span className="fs35 ico-robot me-2"></span>
          <span className="fs22 pt-1">{prep(ID_MM_CHOOSE_BOT)}</span>
        </div>

        {availableCexes.length === 0 && (
          <div className="fs18 mb-3">
            Only basic market making is available. Configure CEXes to enable more options.
          </div>
        )}

        <div id="botSelect">
          <div
            id="botTypeBasicMM"
            data-bot-type="basicMM"
            className={`bot-type-selector mt-2 ${selectedBotType === 'basicMM' ? 'selected' : ''}`}
            onClick={() => handleBotTypeSelect('basicMM')}
          >
            <div className="flex-center fs24 p-2">{prep(ID_MM_BASIC_MARKET_MAKER)}</div>
            <div className="flex-center fs16 px-3 pb-2 d-hide">
                Keep orders on both sides of a DEX market with a configurable spread. If a buy and a sell order both match with minimal market movement in between, you profit.
            </div>
          </div>

          {availableCexes.length > 0 && (
            <>
              <div
                id="botTypeARbMM"
                data-bot-type="arbMM"
                className={`bot-type-selector mt-2 ${selectedBotType === 'arbMM' ? 'selected' : ''}`}
                onClick={() => handleBotTypeSelect('arbMM')}
              >
                <div className="flex-center fs24 p-2">{prep(ID_MM_MM_PLUS_ARB)}</div>
                <div className="flex-center fs16 px-3 pb-2 d-hide">
                  Maintain orders on a DEX market placed at positions calculated for matching profitable existing orders on CEX. When the DEX order matches, the CEX order is placed immediately for a profit.
                </div>
              </div>

              <div
                id="botTypeBasicArb"
                data-bot-type="basicArb"
                className={`bot-type-selector mt-2 ${selectedBotType === 'basicArb' ? 'selected' : ''}`}
                onClick={() => handleBotTypeSelect('basicArb')}
              >
                <div className="flex-center fs24 p-2">{prep(ID_MM_BASIC_ARBITRAGE)}</div>
                <div className="flex-center fs16 px-3 pb-2 d-hide">
                  Watch both CEX and DEX order books and wait for conditions favorable to place a pair of orders such that when both match, a profit is made.
                </div>
              </div>
            </>
          )}
        </div>

        {isArbBotSelected && availableCexes.length > 0 && (
          <div id="cexSelection" className="d-flex flex-wrap justify-content-between mt-3">
            {availableCexes.map(cexName => {
              const cexStatus = cexes[cexName] || null
              const directOnly = selectedBotType === 'basicArb'
              const supportsArbitrage = cexStatus !== null && checkCexArbitrageSupport(cexName, directOnly)
              return (
                <CEXIcon
                  key={cexName}
                  cexName={cexName}
                  isSelected={selectedCex === cexName}
                  cexStatus={cexStatus}
                  supportsArbitrage={supportsArbitrage}
                  onSelect={() => handleCexSelect(cexName)}
                  onConfigure={() => handleConfigureCex(cexName)}
                  onReconfigure={(e) => {
                    e.stopPropagation()
                    handleConfigureCex(cexName)
                  }}
                />
              )
            })}
          </div>
        )}

        <div id="botTypeErr" className="flex-center text-danger d-none"></div>

        <div className="flex-stretch-column">
          <button
            id="botTypeSubmit"
            type="button"
            className="feature"
            disabled={isSubmitDisabled}
            onClick={handleSubmit}
          >
            {prep(ID_MM_SUBMIT)}
          </button>
        </div>
      </form>

      {configuringCex && (
        <CEXConfigForm
          cexStatus={cexes[configuringCex] || null}
          cexName={configuringCex}
          onClose={handleCexConfigClose}
          onCEXUpdated={handleCEXesUpdated}
        />
      )}
    </div>
  )
}

export default BotTypeSelector
