import React from 'react'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import PlacementsChartWrapper from './PlacementsChartWrapper'
import { NumberInput, PanelHeader } from './FormComponents'
import Tooltip from './Tooltip'
import { useBootstrapBreakpoints } from '../hooks/PageSizeBreakpoints'
import {
  prep,
  ID_MM_PRICE_LEVELS_PER_SIDE,
  ID_MM_PRICE_LEVELS_TOOLTIP,
  ID_MM_LOTS_PER_LEVEL,
  ID_MM_USD_PER_SIDE,
  ID_MM_LOTS_USD_TOOLTIP,
  ID_MM_PROFIT_THRESHOLD,
  ID_MM_PROFIT_THRESHOLD_DESC,
  ID_MM_PROFIT_THRESHOLD_MM_TOOLTIP,
  ID_MM_PRICE_INCREMENT,
  ID_MM_PRICE_INCREMENT_TOOLTIP,
  ID_MM_MATCH_BUFFER,
  ID_MM_MATCH_BUFFER_TOOLTIP,
  ID_MM_QUICK_PLACEMENTS,
  ID_MM_ADVANCED_PLACEMENTS,
  ID_MM_SWITCH_TO_ADVANCED,
  ID_MM_SWITCH_TO_QUICK,
  ID_MM_QUICK_PLACEMENTS_DESC
} from '../../locales'

const LevelsPerSideSelector: React.FC = () => {
  const { quickPlacements } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const handleChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'priceLevelsPerSide', value } })
  }

  return (
    <div className="d-flex align-items-center justify-content-between">
      <span className="fs16">
        {prep(ID_MM_PRICE_LEVELS_PER_SIDE)}
        <Tooltip content={prep(ID_MM_PRICE_LEVELS_TOOLTIP)}>
          <span className="ico-info fs13 ms-1"></span>
        </Tooltip>
      </span>
      <div style={{ width: '6rem' }}>
        <NumberInput
          onChange={handleChange}
          value={quickPlacements.priceLevelsPerSide}
          precision={0}
          className="p-1 text-center fs16"
          onIncrement={() => handleChange(quickPlacements.priceLevelsPerSide + 1)}
          onDecrement={() => handleChange(Math.max(1, quickPlacements.priceLevelsPerSide - 1))}
        />
      </div>
    </div>
  )
}

const LotsOrUsdSelector: React.FC = () => {
  const { quickPlacements, dexMarket: { lotSize, baseAsset }, fiatRatesMap } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const USD_RATE = fiatRatesMap[baseAsset.id] || 0
  const [isLotsMode, setIsLotsMode] = React.useState(true)

  const formatUSD = (value: number): string => {
    return value.toFixed(2)
  }

  const lotsToUSD = (): number => {
    const convFactor = baseAsset.unitInfo.conventional.conversionFactor
    const totalLots = quickPlacements.lotsPerLevel * quickPlacements.priceLevelsPerSide
    const convLotSize = lotSize / convFactor
    return totalLots * USD_RATE * convLotSize
  }

  const usdToLots = (usd: number) => {
    const convFactor = baseAsset.unitInfo.conventional.conversionFactor
    const convLotSize = lotSize / convFactor
    const usdPerLot = USD_RATE * convLotSize
    if (usdPerLot === 0 || quickPlacements.priceLevelsPerSide === 0) return 1
    return Math.floor(usd / usdPerLot / quickPlacements.priceLevelsPerSide)
  }

  const handleQuickConfigChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'lotsPerLevel', value } })
  }

  const handleChange = (value: number) => {
    if (!isLotsMode) handleQuickConfigChange(usdToLots(value))
    else handleQuickConfigChange(value)
  }

  const handleIncrement = () => {
    handleQuickConfigChange(quickPlacements.lotsPerLevel + 1)
  }

  const handleDecrement = () => {
    handleQuickConfigChange(Math.max(1, quickPlacements.lotsPerLevel - 1))
  }

  return (
    <div>
      <div className="d-flex align-items-center justify-content-between">
        <div className="fs16">
          {isLotsMode ? prep(ID_MM_LOTS_PER_LEVEL) : prep(ID_MM_USD_PER_SIDE)}
          <Tooltip content={prep(ID_MM_LOTS_USD_TOOLTIP)}>
            <span className="ico-info fs13 ms-1"></span>
          </Tooltip>
          <span
            className="fs12 lh1 grey p-1 ms-1 hoverbg pointer"
            onClick={() => { setIsLotsMode(!isLotsMode) }}
          >
            <span className="ico-arrowleft"></span><span className="ico-arrowright"></span>
          </span>
        </div>
        <div style={{ width: '6rem' }}>
          <NumberInput
            value={isLotsMode ? quickPlacements.lotsPerLevel : lotsToUSD()}
            onChange={handleChange}
            precision={isLotsMode ? 0 : 2}
            className="p-1 text-center fs14"
            onIncrement={handleIncrement}
            onDecrement={handleDecrement}
          />
        </div>
      </div>
      <div className="fs13 grey text-end">
        ~{isLotsMode ? formatUSD(lotsToUSD()) : String(quickPlacements.lotsPerLevel)} {isLotsMode ? 'USD per side' : 'Lots per level'}
      </div>
    </div>
  )
}

const ProfitThresholdEntry: React.FC = () => {
  const { quickPlacements, botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const isArbMM = !!botConfig.arbMarketMakingConfig
  const tooltipText = isArbMM
    ? prep(ID_MM_PROFIT_THRESHOLD_DESC)
    : prep(ID_MM_PROFIT_THRESHOLD_MM_TOOLTIP)

  const handleQuickConfigChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'profitThreshold', value } })
  }

  return (
    <div className="d-flex align-items-center">
      <div className="fs16 me-3 flex-shrink-0">
        {prep(ID_MM_PROFIT_THRESHOLD)}
        <Tooltip content={tooltipText}>
          <span className="ico-info fs13 ms-1"></span>
        </Tooltip>
      </div>
      <NumberInput
        sliderPosition="inline"
        className="p-1 text-center fs14"
        min={0.1}
        max={10}
        precision={2}
        value={quickPlacements.profitThreshold * 100}
        onChange={(value) => handleQuickConfigChange(value / 100)}
        suffix="%"
        withSlider={true}
      />
    </div>
  )
}

const PriceIncrementEntry: React.FC = () => {
  const { quickPlacements } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const handleQuickConfigChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'priceIncrement', value } })
  }

  return (
    <div className="d-flex align-items-center">
      <div className="fs16 me-3 flex-shrink-0">
        {prep(ID_MM_PRICE_INCREMENT)}
        <Tooltip content={prep(ID_MM_PRICE_INCREMENT_TOOLTIP)}>
          <span className="ico-info fs13 ms-1"></span>
        </Tooltip>
      </div>
      <NumberInput
        sliderPosition="inline"
        className="p-1 text-center fs14"
        min={0.1}
        max={2}
        precision={2}
        value={quickPlacements.priceIncrement * 100}
        onChange={(value) => handleQuickConfigChange(value / 100)}
        disabled={quickPlacements.priceLevelsPerSide === 1}
        suffix="%"
        withSlider={true}
      />
    </div>
  )
}

const MatchBufferEntry: React.FC = () => {
  const { quickPlacements } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const handleQuickConfigChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'matchBuffer', value } })
  }

  return (
    <div className="d-flex align-items-center">
      <div className="fs16 me-3 flex-shrink-0">
        {prep(ID_MM_MATCH_BUFFER)}
        <Tooltip content={prep(ID_MM_MATCH_BUFFER_TOOLTIP)}>
          <span className="ico-info fs13 ms-1"></span>
        </Tooltip>
      </div>
      <NumberInput
        sliderPosition="inline"
        className="p-1 text-center fs14"
        min={0}
        max={100}
        precision={2}
        value={quickPlacements.matchBuffer * 100}
        onChange={(value) => handleQuickConfigChange(value / 100)}
        suffix="%"
        withSlider={true}
      />
    </div>
  )
}

interface PlacementsPanelHeaderProps {
  description: string
}

export const PlacementsPanelHeader: React.FC<PlacementsPanelHeaderProps> = ({
  description
}) => {
  const { quickPlacements } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  const isUsingQuickPlacements = !!quickPlacements

  const title = isUsingQuickPlacements ? prep(ID_MM_QUICK_PLACEMENTS) : prep(ID_MM_ADVANCED_PLACEMENTS)
  const buttonText = isUsingQuickPlacements ? prep(ID_MM_SWITCH_TO_ADVANCED) : prep(ID_MM_SWITCH_TO_QUICK)

  const handleSwitch = () => {
    dispatch({ type: 'USE_QUICK_PLACEMENTS', payload: !isUsingQuickPlacements })
  }

  return (
    <PanelHeader
      title={title}
      description={description}
      buttonText={buttonText}
      onClick={handleSwitch}
    />
  )
}

const headerDescription = prep(ID_MM_QUICK_PLACEMENTS_DESC)

export const QuickPlacements: React.FC = () => {
  const pageSize = useBootstrapBreakpoints(['md', 'lg'])
  const { quickPlacements, botConfig } = useBotConfigState()
  if (!quickPlacements) return null

  // Determine bot type
  const isBasicMM = !!botConfig.basicMarketMakingConfig
  const isArbMM = !!botConfig.arbMarketMakingConfig

  const header = <PlacementsPanelHeader description={headerDescription} />

  const inputs = (
    <div className="border rounded p-3">
      <div className="d-flex flex-column gap-3">
        <LevelsPerSideSelector />
        <LotsOrUsdSelector />
      </div>
      <hr className="my-3" />
      <div className="d-flex flex-column gap-2">
        <ProfitThresholdEntry />
        {isBasicMM && <PriceIncrementEntry />}
        {isArbMM && <MatchBufferEntry />}
      </div>
    </div>
  )

  // Small screens: everything stacked
  if (pageSize === 'xs') {
    return (
      <div>
        {header}
        {inputs}
        <div className="mt-3">
          <PlacementsChartWrapper />
        </div>
      </div>
    )
  }

  // Medium+ screens: inputs and chart side by side
  return (
    <div>
      {header}
      <div className="row">
        <div className="col-24 col-lg-15">
          {inputs}
        </div>
        <div className="col-24 col-lg-9">
          <PlacementsChartWrapper />
        </div>
      </div>
    </div>
  )
}
