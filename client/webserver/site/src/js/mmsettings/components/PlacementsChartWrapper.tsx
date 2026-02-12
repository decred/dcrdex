import React from 'react'
import { useBotConfigState } from '../utils/BotConfig'
import { PlacementsChart, PlacementChartConfig } from '../../mmutil'
import { OrderPlacement } from '../../registry'

const PlacementsChartWrapper: React.FC = () => {
  const chartRef = React.useRef<HTMLDivElement>(null)
  const chartInstanceRef = React.useRef<PlacementsChart | null>(null)
  const { botConfig, dexMarket, quickPlacements, fiatRatesMap } = useBotConfigState()

  React.useEffect(() => {
    if (chartRef.current && !chartInstanceRef.current) {
      chartInstanceRef.current = new PlacementsChart(chartRef.current)
    }
    return () => {
      chartInstanceRef.current = null
    }
  }, [])

  React.useEffect(() => {
    if (chartInstanceRef.current && botConfig && dexMarket) {
      const isBasicMM = !!botConfig.basicMarketMakingConfig
      const isArbMM = !!botConfig.arbMarketMakingConfig

      let buyPlacements: OrderPlacement[] = []
      let sellPlacements: OrderPlacement[] = []
      let profit = 0

      if (isBasicMM && botConfig.basicMarketMakingConfig) {
        buyPlacements = [...botConfig.basicMarketMakingConfig.buyPlacements]
        sellPlacements = [...botConfig.basicMarketMakingConfig.sellPlacements]
        // For basic MM, profit comes from quick config or defaults to 0
        profit = quickPlacements?.profitThreshold || 0
      } else if (isArbMM && botConfig.arbMarketMakingConfig) {
        buyPlacements = botConfig.arbMarketMakingConfig.buyPlacements.map(placement => ({
          lots: placement.lots,
          gapFactor: placement.multiplier
        }))
        sellPlacements = botConfig.arbMarketMakingConfig.sellPlacements.map(placement => ({
          lots: placement.lots,
          gapFactor: placement.multiplier
        }))
        profit = botConfig.arbMarketMakingConfig.profit || 0
      }

      buyPlacements.sort((a, b) => a.gapFactor - b.gapFactor)
      sellPlacements.sort((a, b) => a.gapFactor - b.gapFactor)

      const chartConfig: PlacementChartConfig = {
        cexName: botConfig.cexName,
        botType: isBasicMM ? 'basicMM' : isArbMM ? 'arbMM' : 'basicArb',
        baseFiatRate: fiatRatesMap[dexMarket.baseID] || 1,
        dict: {
          profit,
          buyPlacements,
          sellPlacements
        }
      }

      chartInstanceRef.current.setMarket(chartConfig)
    }
  }, [botConfig, dexMarket, quickPlacements, fiatRatesMap])

  return (
      <div
        ref={chartRef}
        className="p-1 placements-chart-wrapper"
      />
  )
}

export default PlacementsChartWrapper
