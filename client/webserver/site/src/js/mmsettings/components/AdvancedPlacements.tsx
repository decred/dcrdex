import React from 'react'
import { GapStrategy, OrderPlacement, ArbMarketMakingPlacement } from '../../registry'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import PlacementsChartWrapper from './PlacementsChartWrapper'
import { PlacementsPanelHeader } from './QuickPlacements'
import { FormLabel, NumberInput, IconButton, ErrorMessage } from './FormComponents'
import { useBootstrapBreakpoints } from '../hooks/PageSizeBreakpoints'

type UnifiedPlacement = OrderPlacement | ArbMarketMakingPlacement;

const gapStrategies = {
  'percent-plus': {
    label: 'Percent Plus',
    description: 'Places orders at percentages above and below the market price.',
    factorLabel: 'Percent',
    checkRange: (value: number) => (value <= 0 || value > 10) ? 'Percent must be between 0 and 10' : null,
    convert: (value: number) => value / 100
  },
  'percent': {
    label: 'Percent',
    description: 'Places orders at fixed percentages from the market price.',
    factorLabel: 'Percent',
    checkRange: (value: number) => (value <= 0 || value > 10) ? 'Percent must be between 0 and 10' : null,
    convert: (value: number) => value / 100
  },
  'absolute-plus': {
    label: 'Absolute Plus',
    description: 'Places orders at fixed amounts above and below the market price.',
    factorLabel: 'Rate',
    checkRange: (value: number) => (value <= 0) ? 'Rate must be greater than 0' : null,
    convert: (value: number) => value
  },
  'absolute': {
    label: 'Absolute',
    description: 'Places orders at fixed amounts from the market price.',
    factorLabel: 'Rate',
    checkRange: (value: number) => (value <= 0) ? 'Rate must be greater than 0' : null,
    convert: (value: number) => value
  },
  'multiplier': {
    label: 'Multiplier',
    description: 'Uses a multiplier to determine order placement.',
    factorLabel: 'Multiplier',
    checkRange: (value: number) => (value < 1 || value > 100) ? 'Multiplier must be between 1 and 100' : null,
    convert: (value: number) => value
  }
} as const

const GapStrategySelector: React.FC = () => {
  const { botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  // Only render for basic market making bots
  if (!botConfig.basicMarketMakingConfig) return null

  const gapStrategy = botConfig.basicMarketMakingConfig.gapStrategy

  const handleGapStrategyChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    dispatch({ type: 'SET_GAP_STRATEGY', payload: e.target.value as GapStrategy })
  }

  return (
    <div className="w-100">
        <FormLabel text="Gap Strategy" className="text-nowrap me-2 mb-1" />
        <select
          className="form-select fs18"
          value={gapStrategy}
          onChange={handleGapStrategyChange}
        >
          {Object.entries(gapStrategies).map(([value, { label }]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </select>

      {gapStrategy && (
        <div className="w-100 pb-2 border-bottom fs16 text-justify">
          Strategy: {gapStrategies[gapStrategy].description}
        </div>
      )}
    </div>
  )
}

const ProfitSelector: React.FC = () => {
  const { botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  const [errorMessage, setErrorMessage] = React.useState<string>('')

  // Only render for arbitrage market making bots
  if (!botConfig.arbMarketMakingConfig) return null

  const profit = botConfig.arbMarketMakingConfig.profit
  const handleProfitChange = (value: number) => {
    setErrorMessage('')
    if (value <= 0) {
      setErrorMessage('Profit must be greater than 0')
      return
    }
    dispatch({ type: 'SET_PROFIT', payload: value / 100 })
  }

  return (
    <div className="d-flex align-items-stretch py-2 my-2 border-bottom">
      <div className="flex-grow-1 d-flex flex-column pe-2">
        <FormLabel text="Profit Threshold" description="Minimum profit required for arbitrage opportunities." isBold={false} />
        {errorMessage && <ErrorMessage message={errorMessage} onClear={() => setErrorMessage('')} />}
      </div>
      <div className="w-100 d-flex flex-column align-items-stretch">
        <div className="flex-center p-3 border-left">
          <NumberInput
            value={profit * 100}
            onChange={handleProfitChange}
            precision={2}
            className="p-2 text-center fs24 me-1"
            suffix="%"
          />
        </div>
      </div>
    </div>
  )
}

interface PlacementRowProps {
  index: number
  isFirst: boolean
  isLast: boolean
  lots: number
  gapFactor: number
  gapStrategy: GapStrategy
  onMoveUp: () => void
  onMoveDown: () => void
  onRemove: () => void
}

const PlacementRow: React.FC<PlacementRowProps> = ({
  index,
  isFirst,
  isLast,
  lots,
  gapFactor,
  gapStrategy,
  onMoveUp,
  onMoveDown,
  onRemove
}) => {
  const { botConfig } = useBotConfigState()
  const isBasicMM = !!botConfig.basicMarketMakingConfig

  const isPercent = gapStrategy === 'percent' || gapStrategy === 'percent-plus'
  const displayFactor = isPercent ? gapFactor * 100 : gapFactor

  return (
    <tr>
      { isBasicMM ? <td>{index + 1}</td> : null }
      <td>{lots}</td>
      <td>
        {displayFactor}
        {isPercent && '%'}
      </td>
      <td className="no-stretch text-start text-nowrap">
        <IconButton iconClass="ico-cross sellcolor" onClick={onRemove} ariaLabel="Remove placement" />
        {!isFirst && <IconButton iconClass="ico-arrowup" onClick={onMoveUp} ariaLabel="Move up" />}
        {!isLast && <IconButton iconClass="ico-arrowdown" onClick={onMoveDown} ariaLabel="Move down" />}
      </td>
    </tr>
  )
}

interface PlacementsProps {
  isSell: boolean
}

const Placements: React.FC<PlacementsProps> = ({ isSell }) => {
  const { botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()
  const [lots, setLots] = React.useState<number | undefined>(undefined)
  const [gapFactor, setGapFactor] = React.useState<number | undefined>(undefined)
  const [errorMessage, setErrorMessage] = React.useState('')

  const isBasicMM = !!botConfig.basicMarketMakingConfig
  const gapStrategy: GapStrategy = botConfig.basicMarketMakingConfig?.gapStrategy ?? 'multiplier'
  const title = isSell ? 'Sell Placements' : 'Buy Placements'
  const placements: UnifiedPlacement[] = isSell
    ? (botConfig.basicMarketMakingConfig?.sellPlacements || botConfig.arbMarketMakingConfig?.sellPlacements || [])
    : (botConfig.basicMarketMakingConfig?.buyPlacements || botConfig.arbMarketMakingConfig?.buyPlacements || [])

  const getFactor = (placement: UnifiedPlacement) => 'gapFactor' in placement ? placement.gapFactor : placement.multiplier

  const validateAndAddPlacement = () => {
    // Validate lots
    if (!lots || lots <= 0 || !Number.isInteger(lots)) {
      setErrorMessage('Lots must be a whole number greater than 0')
      return
    }

    // Validate gap factor
    if (!gapFactor) {
      setErrorMessage('Gap factor must be a valid number')
      return
    }

    const rangeError = gapStrategies[gapStrategy].checkRange(gapFactor)
    if (rangeError) {
      setErrorMessage(rangeError)
      return
    }

    // Convert gap factor for storage if needed
    const storageGapFactor = gapStrategies[gapStrategy].convert(gapFactor)

    // Check for duplicate gap factors (use epsilon for floating-point comparison)
    const duplicateExists = placements.some(placement => Math.abs(getFactor(placement) - storageGapFactor) < 1e-9)

    if (duplicateExists) {
      setErrorMessage(`A placement with ${gapFactor}${(gapStrategy === 'percent' || gapStrategy === 'percent-plus') ? '%' : ''} already exists`)
      return
    }

    dispatch({
      type: 'ADD_PLACEMENT',
      payload: { sell: isSell, lots, gapFactor: storageGapFactor }
    })

    setLots(undefined)
    setGapFactor(undefined)
    setErrorMessage('')
  }

  const handleMoveUp = (index: number) => {
    if (index > 0) {
      dispatch({
        type: 'REORDER_PLACEMENTS',
        payload: { sell: isSell, fromIndex: index, toIndex: index - 1 }
      })
    }
  }

  const handleMoveDown = (index: number) => {
    if (index < placements.length - 1) {
      dispatch({
        type: 'REORDER_PLACEMENTS',
        payload: { sell: isSell, fromIndex: index, toIndex: index + 1 }
      })
    }
  }

  const handleRemovePlacement = (index: number) => {
    dispatch({
      type: 'REMOVE_PLACEMENT',
      payload: { sell: isSell, index }
    })
  }

  return (
    <div>
      <div className="d-flex align-items-center fs18 mt-3 demi">
        {title}
      </div>
      <div className="d-flex flex-column">
        <table className="cell-border compact">
          <thead>
            <tr>
              { isBasicMM ? <th className="no-stretch">Priority</th> : null }
              <th>Lots</th>
              <th>{gapStrategies[gapStrategy].factorLabel}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {placements.map((placement, index) => (
              <PlacementRow
                key={`${placement.lots}-${getFactor(placement)}-${index}`}
                index={index}
                isFirst={index === 0}
                isLast={index === placements.length - 1}
                lots={placement.lots}
                gapFactor={getFactor(placement)}
                gapStrategy={gapStrategy}
                onMoveUp={() => handleMoveUp(index)}
                onMoveDown={() => handleMoveDown(index)}
                onRemove={() => handleRemovePlacement(index)}
              />
            ))}
            <tr>
              { isBasicMM ? <td></td> : null }
              <td>
                <NumberInput
                  value={lots}
                  onChange={setLots}
                  className="lots-input p-2"
                  precision={0}
                />
              </td>
              <td>
                <NumberInput
                  value={gapFactor}
                  onChange={setGapFactor}
                  className="gap-factor-input p-2"
                  precision={2}
                />
              </td>
              <td className="no-stretch text-start">
                <IconButton iconClass="ico-plus buycolor" onClick={validateAndAddPlacement} ariaLabel="Add placement" />
              </td>
            </tr>
            {errorMessage && (
              <tr>
                <td colSpan={isBasicMM ? 4 : 3}>
                  <ErrorMessage message={errorMessage} onClear={() => setErrorMessage('')} />
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const headerDescription = 'Manually configure each order placement.'

export const AdvancedPlacements: React.FC = () => {
  const pageSize = useBootstrapBreakpoints(['lg'])

  const header = <PlacementsPanelHeader description={headerDescription} />

  // One column layout for small screens
  if (pageSize === 'xs') {
    return (
      <div>
        {header}
        <GapStrategySelector />
        <ProfitSelector />
        <Placements isSell={false} />
        <Placements isSell={true} />
        <PlacementsChartWrapper />
      </div>
    )
  }

  // Two column layout for large screens
  return (
    <div>
      {header}
      <div className="row">
        {/* LEFT COLUMN */}
        <div className="col-15">
          <GapStrategySelector />
          <ProfitSelector />
          <Placements isSell={false} />
          <Placements isSell={true} />
        </div>

        {/* RIGHT COLUMN */}
        <div className="col-9">
          <PlacementsChartWrapper />
        </div>
      </div>
    </div>
  )
}
