import React from 'react'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import PlacementsChartWrapper from './PlacementsChartWrapper'
import { FormLabel, NumberInput, PanelHeader } from './FormComponents'
import { useBootstrapBreakpoints } from '../hooks/PageSizeBreakpoints'

const LevelsPerSideSelector: React.FC = () => {
  const { quickPlacements } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const handleChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'priceLevelsPerSide', value } })
  }

  const handleIncrement = () => {
    handleChange(quickPlacements.priceLevelsPerSide + 1)
  }

  const handleDecrement = () => {
    handleChange(Math.max(1, quickPlacements.priceLevelsPerSide - 1))
  }

  return (
    <NumberInput
      header={<FormLabel text="Price levels per side" className="fs17" isBold={false} />}
      onChange={handleChange}
      value={quickPlacements.priceLevelsPerSide}
      precision={0}
      className="fs18 text-center p-2 fs20 flex-grow-1"
      onIncrement={handleIncrement}
      onDecrement={handleDecrement}
    />
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
    <NumberInput
      header={
        <div className="d-flex align-items-center mb-1">
          <FormLabel text={isLotsMode ? 'Lots per level' : 'USD per side'} className="fs17" isBold={false} />
          <span
            className="fs12 lh1 grey p-1 ms-1 hoverbg pointer"
            onClick={() => { setIsLotsMode(!isLotsMode) }}
          >
            <span className="ico-arrowleft"></span><span className="ico-arrowright"></span>
          </span>
        </div>
      }
      value={isLotsMode ? quickPlacements.lotsPerLevel : lotsToUSD()}
      onChange={handleChange}
      precision={isLotsMode ? 0 : 2}
      className="fs18 text-center p-2 fs20 flex-grow-1"
      onIncrement={handleIncrement}
      onDecrement={handleDecrement}
      bottomContent={
        <span className="fs15 grey mt-1">
          <span>~</span>
          <span>{isLotsMode ? formatUSD(lotsToUSD()) : String(quickPlacements.lotsPerLevel)}</span>
          <span>{isLotsMode ? 'USD per side' : 'Lots per level'}</span>
        </span>
      }
    />
  )
}

const ProfitThresholdEntry: React.FC = () => {
  const { quickPlacements } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  if (!quickPlacements) return null

  const handleQuickConfigChange = (value: number) => {
    dispatch({ type: 'UPDATE_QUICK_CONFIG', payload: { field: 'profitThreshold', value } })
  }

  return (
    <NumberInput
      header={<FormLabel text="Profit threshold" className="fs17" isBold={false} />}
      min={0.1}
      max={10}
      precision={2}
      value={quickPlacements.profitThreshold * 100}
      onChange={(value) => handleQuickConfigChange(value / 100)}
      suffix='%'
      withSlider={true}
    />
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
    <NumberInput
      header={<FormLabel text="Price increment" className="fs17" isBold={false} />}
      min={0.1}
      max={2}
      precision={2}
      value={quickPlacements.priceIncrement * 100}
      onChange={(value) => handleQuickConfigChange(value / 100)}
      disabled={quickPlacements.priceLevelsPerSide === 1}
      suffix='%'
      withSlider={true}
    />
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
    <NumberInput
      header={<FormLabel text="Match buffer" className="fs17" isBold={false} />}
      min={0}
      max={100}
      precision={2}
      value={quickPlacements.matchBuffer * 100}
      onChange={(value) => handleQuickConfigChange(value / 100)}
      suffix='%'
      withSlider={true}
    />
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

  const title = isUsingQuickPlacements ? 'Quick Placements' : 'Advanced Placements'
  const buttonText = isUsingQuickPlacements ? 'Advanced config' : 'Quick config'

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

const headerDescription = 'Configure the price levels of the placements on both sides of the order book.'

export const QuickPlacements: React.FC = () => {
  const pageSize = useBootstrapBreakpoints(['lg'])
  const { quickPlacements, botConfig } = useBotConfigState()
  if (!quickPlacements) return null

  // Determine bot type
  const isBasicMM = !!botConfig.basicMarketMakingConfig
  const isArbMM = !!botConfig.arbMarketMakingConfig

  const header = <PlacementsPanelHeader description={headerDescription} />

  if (pageSize === 'xs') {
    return (<div>
      {header}

      <div className="row">
        <div className="col-12 pe-1">
          <LevelsPerSideSelector />
        </div>
        <div className="col-12 ps-1">
          <LotsOrUsdSelector />
        </div>
      </div>

      <div className="row">
        <div className="col-12 pe-1">
          <ProfitThresholdEntry />
        </div>

        {isBasicMM && (
          <div className="col-12 ps-1">
            <PriceIncrementEntry />
          </div>
        )}

        {isArbMM && (
          <div className="col-12 ps-1">
            <MatchBufferEntry />
          </div>
        )}
      </div>

      <div className="row m-2">
        <PlacementsChartWrapper />
      </div>
    </div>)
  }

  return (
    <div>
      {header}

      <div className="row">
        <div className="col-15">
          <div className="row">
            <div className="col-12 pe-1">
              <LevelsPerSideSelector />
            </div>
            <div className="col-12 ps-1">
              <LotsOrUsdSelector />
            </div>
          </div>

          <hr className="my-2" />

          <div className="row">
            <div className="col-12 pe-1">
              <ProfitThresholdEntry />
            </div>

            {isBasicMM && (
              <div className="col-12 ps-1">
                <PriceIncrementEntry />
              </div>
            )}

            {isArbMM && (
              <div className="col-12 ps-1">
                <MatchBufferEntry />
              </div>
            )}
          </div>
        </div>
        <div className="col-9">
          <PlacementsChartWrapper />
        </div>
      </div>
    </div>
  )
}
