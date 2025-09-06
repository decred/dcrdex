import React, { useState } from 'react';
import Doc from '../../doc';
import { app, MarketWithHost, MMCEXStatus } from '../../registry';
import { CEXDisplayInfos } from '../../mmutil';
import { renderSymbol } from './MMSettings';
import CEXConfigForm from './CEXConfigForm';

interface CexMarketSupportChecker {
  (baseID: number, quoteID: number, cexName: string): boolean;
}

interface BotTypeSelectorProps {
  selectedMarket: MarketWithHost;
  cexes?: Record<string, MMCEXStatus>;
  checkCexMarketSupport?: CexMarketSupportChecker;
  onClose?: () => void;
  onBotTypeSelected: (botType: 'basicMM' | 'arbMM' | 'basicArb', cexName?: string) => void;
  onChangeMarket?: () => void;
}

interface CEXIconProps {
  marketBaseID: number;
  marketQuoteID: number;
  cexName: string;
  isConfigured: boolean;
  isSelected: boolean;
  checkCexMarketSupport?: CexMarketSupportChecker;
  onSelect: () => void;
  onConfigure: () => void;
  onReconfigure: (e: React.MouseEvent) => void;
}

const CEXIcon: React.FC<CEXIconProps> = ({
  marketBaseID,
  marketQuoteID,
  cexName,
  isConfigured,
  isSelected,
  checkCexMarketSupport,
  onSelect,
  onConfigure,
  onReconfigure
}) => {
  const cexInfo = CEXDisplayInfos[cexName];
  const supportsArbitrage = isConfigured && checkCexMarketSupport ?
    checkCexMarketSupport(marketBaseID, marketQuoteID, cexName) : false;

  const handleClick = () => {
    if (isConfigured) {
      onSelect();
    } else {
      onConfigure();
    }
  };

  return (
    <div
      key={cexName}
      id="cexOptTmpl"
      className={`position-relative p-2 col-11 flex-center flex-column border rounded3 cex-selector ${isSelected ? 'selected' : ''}`}
      onClick={handleClick}
    >
      {/* Reconfigure icon for configured CEXes */}
      {isConfigured && (
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

      {/* Status indicators */}
      {!isConfigured && (
        <span className="fs14 grey flex-center">
          <span className="ico-settings fs12 me-1"></span>
          <span>Configure</span>
        </span>
      )}

      {isConfigured && !supportsArbitrage && (
        <span className="fs14 grey">Market not available</span>
      )}
    </div>
  );
};


const BotTypeSelector: React.FC<BotTypeSelectorProps> = ({
  selectedMarket,
  cexes = {},
  checkCexMarketSupport,
  onClose,
  onBotTypeSelected,
  onChangeMarket
}) => {
  const [selectedBotType, setSelectedBotType] = useState<'basicMM' | 'arbMM' | 'basicArb'>('basicMM');
  const [selectedCex, setSelectedCex] = useState<string>('');
  const [configuringCex, setConfiguringCex] = useState<string | null>(null);

  const handleBotTypeSelect = (botType: 'basicMM' | 'arbMM' | 'basicArb') => {
    setSelectedBotType(botType);

    // Auto-select a configured CEX for arbitrage bot types
    if ((botType === 'arbMM' || botType === 'basicArb') && configuredCexes.length > 0) {
      const supportedCex = findSupportedCex();
      if (supportedCex) {
        setSelectedCex(supportedCex);
      }
    }
  };

  const handleCexSelect = (cexName: string) => {
    setSelectedCex(cexName);
  };

  const handleConfigureCex = (cexName: string) => {
    setConfiguringCex(cexName);
  };

  const handleCexConfigSubmit = (cexName: string, apiKey: string, apiSecret: string) => {
    console.log('CEX Config submitted:', cexName, apiKey, apiSecret);
    // Here you would typically submit the CEX configuration
    setConfiguringCex(null);
  };

  const handleCexConfigClose = () => {
    setConfiguringCex(null);
  };

  const handleSubmit = () => {
    if (selectedBotType) {
      onBotTypeSelected(selectedBotType, selectedCex || undefined);
    }
  };

  const availableCexes = Object.keys(CEXDisplayInfos);
  const configuredCexes = Object.keys(cexes);

  // Check if a CEX supports arbitrage on this market
  const checkCexArbitrageSupport = (cexName: string): boolean => {
    if (!checkCexMarketSupport) return false;
    return checkCexMarketSupport(selectedMarket.baseID, selectedMarket.quoteID, cexName);
  };

  // Find the first configured CEX that supports arbitrage on this market
  const findSupportedCex = (): string | null => {
    for (const cexName of configuredCexes) {
      if (checkCexArbitrageSupport(cexName)) {
        return cexName;
      }
    }
    return null;
  };

  const baseSymbol = app().assets[selectedMarket.baseID].symbol;
  const quoteSymbol = app().assets[selectedMarket.quoteID].symbol;

  return (
    <div id="forms" className="stylish-overflow flex-center">
        <form id="botTypeForm" className="position-relative mw-425 stylish-overflow" autoComplete="off">
        {/* Form Closer */}
        <div className="form-closer">
            <span className="ico-cross pointer" onClick={onClose}></span>
        </div>

        {/* Header */}
        <header className="d-flex align-items-center mb-3">
            <div className="d-flex align-items-center">
                <img
                    className="mini-icon me-1"
                    src={Doc.logoPath(baseSymbol)}
                />
                <img
                    className="mini-icon me-1 ms-1"
                    src={Doc.logoPath(quoteSymbol)}
                />
            </div>
            {renderSymbol(selectedMarket.baseID, baseSymbol)}-
            {renderSymbol(selectedMarket.quoteID, quoteSymbol)}
            <span
                className="p-2 fs14 ico-edit hoverbg pointer"
                onClick={onChangeMarket}
            ></span>
        </header>

        {/* Title */}
        <div className="flex-center mb-3">
            <span className="fs35 ico-robot me-2"></span>
            <span className="fs22 pt-1">Choose Your Bot</span>
        </div>

        {/* No CEXes Configured Message */}
        {availableCexes.length === 0 && (
            <div className="fs18 mb-3">
            Only basic market making is available. Configure CEXes to enable more options.
            </div>
        )}

        {/* Bot Selection */}
        <div id="botSelect">
            {/* Basic Market Maker */}
            <div
            id="botTypeBasicMM"
            data-bot-type="basicMM"
            className={`bot-type-selector mt-2 ${selectedBotType === 'basicMM' ? 'selected' : ''}`}
            onClick={() => handleBotTypeSelect('basicMM')}
            >
            <div className="flex-center fs24 p-2">Basic Market Maker</div>
            <div className="flex-center fs16 px-3 pb-2 d-hide">
                Keep orders on both sides of a DEX market with a configurable spread. If a buy and a sell order both match with minimal market movement in between, you profit.
            </div>
            </div>

            {/* MM + Arb */}
            {availableCexes.length > 0 && (
            <div
                id="botTypeARbMM"
                data-bot-type="arbMM"
                className={`bot-type-selector mt-2 ${selectedBotType === 'arbMM' ? 'selected' : ''}`}
                onClick={() => handleBotTypeSelect('arbMM')}
            >
                <div className="flex-center fs24 p-2">MM + Arbitrage</div>
                <div className="flex-center fs16 px-3 pb-2 d-hide">
                Maintain orders on a DEX market placed at positions calculated for matching profitable existing orders on CEX. When the DEX order matches, the CEX order is placed immediately for a profit.
                </div>
            </div>
            )}

            {/* Basic Arbitrage */}
            {availableCexes.length > 0 && (
            <div
                id="botTypeBasicArb"
                data-bot-type="basicArb"
                className={`bot-type-selector mt-2 ${selectedBotType === 'basicArb' ? 'selected' : ''}`}
                onClick={() => handleBotTypeSelect('basicArb')}
            >
                <div className="flex-center fs24 p-2">Basic Arbitrage</div>
                <div className="flex-center fs16 px-3 pb-2 d-hide">
                Watch both CEX and DEX order books and wait for conditions favorable to place a pair of orders such that when both match, a profit is made.
                </div>
            </div>
            )}
        </div>

        {/* CEX Selection */}
        {(selectedBotType === 'arbMM' || selectedBotType === 'basicArb') && availableCexes.length > 0 && (
            <div id="cexSelection" className="d-flex flex-wrap justify-content-between mt-3">
            {availableCexes.map(cexName => {
                const isSelected = selectedCex === cexName;
                const isConfigured = configuredCexes.includes(cexName);

                return (
                    <CEXIcon
                        key={cexName}
                        marketBaseID={selectedMarket.baseID}
                        marketQuoteID={selectedMarket.quoteID}
                        cexName={cexName}
                        isConfigured={isConfigured}
                        isSelected={isSelected}
                        checkCexMarketSupport={checkCexMarketSupport}
                        onSelect={() => handleCexSelect(cexName)}
                        onConfigure={() => handleConfigureCex(cexName)}
                        onReconfigure={(e) => {
                            e.stopPropagation();
                            handleConfigureCex(cexName);
                        }}
                    />
                );
            })}
            </div>
        )}

        {/* Error Message */}
        <div id="botTypeErr" className="flex-center text-danger d-none"></div>

        {/* Submit Button */}
        <div className="flex-stretch-column">
            <button
            id="botTypeSubmit"
            type="button"
            className="feature"
            disabled={!selectedBotType || ((selectedBotType === 'arbMM' || selectedBotType === 'basicArb') && !selectedCex)}
            onClick={handleSubmit}
            >
            Submit
            </button>
        </div>
        </form>

        {/* CEX Configuration Form */}
        {configuringCex && (
            <CEXConfigForm
                cexName={configuringCex}
                onClose={handleCexConfigClose}
                onSubmit={handleCexConfigSubmit}
            />
        )}
    </div>
  );
};

export default BotTypeSelector;
