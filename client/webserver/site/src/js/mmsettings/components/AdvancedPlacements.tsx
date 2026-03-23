import React from 'react'
import { GapStrategy, OrderPlacement, ArbMarketMakingPlacement } from '../../registry'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import PlacementsChartWrapper from './PlacementsChartWrapper'
import { PlacementsPanelHeader } from './QuickPlacements'
import { NumberInput, IconButton, ErrorMessage } from './FormComponents'
import Tooltip from './Tooltip'
import {
  prep,
  ID_MM_GAP_STRATEGY,
  ID_MM_PERCENT_PLUS, ID_MM_PERCENT_PLUS_DESC,
  ID_MM_PERCENT, ID_MM_PERCENT_DESC,
  ID_MM_ABSOLUTE_PLUS, ID_MM_ABSOLUTE_PLUS_DESC,
  ID_MM_ABSOLUTE, ID_MM_ABSOLUTE_DESC,
  ID_MM_MULTIPLIER, ID_MM_MULTIPLIER_DESC,
  ID_MM_FACTOR_LABEL_PERCENT, ID_MM_FACTOR_LABEL_RATE, ID_MM_FACTOR_LABEL_MULTIPLIER,
  ID_MM_PROFIT_THRESHOLD, ID_MM_PROFIT_ADV_DESC,
  ID_MM_PRIORITY, ID_MM_PRIORITY_TOOLTIP,
  ID_MM_LOTS,
  ID_MM_BUY_PLACEMENTS, ID_MM_SELL_PLACEMENTS,
  ID_MM_ADV_PLACEMENTS_DESC,
  ID_MM_REMOVE_PLACEMENT, ID_MM_MOVE_UP, ID_MM_MOVE_DOWN, ID_MM_ADD_PLACEMENT
} from '../../locales'

type UnifiedPlacement = OrderPlacement | ArbMarketMakingPlacement;

interface GapStrategyConfig {
  label: string
  description: string
  factorLabel: string
  checkRange: (value: number) => string | null
  convert: (value: number) => number
}

function gapStrategies (): Record<GapStrategy, GapStrategyConfig> {
  return {
    'percent-plus': {
      label: prep(ID_MM_PERCENT_PLUS),
      description: prep(ID_MM_PERCENT_PLUS_DESC),
      factorLabel: prep(ID_MM_FACTOR_LABEL_PERCENT),
      checkRange: (value: number) => (value <= 0 || value > 10) ? 'Percent must be between 0 and 10' : null,
      convert: (value: number) => value / 100
    },
    'percent': {
      label: prep(ID_MM_PERCENT),
      description: prep(ID_MM_PERCENT_DESC),
      factorLabel: prep(ID_MM_FACTOR_LABEL_PERCENT),
      checkRange: (value: number) => (value <= 0 || value > 10) ? 'Percent must be between 0 and 10' : null,
      convert: (value: number) => value / 100
    },
    'absolute-plus': {
      label: prep(ID_MM_ABSOLUTE_PLUS),
      description: prep(ID_MM_ABSOLUTE_PLUS_DESC),
      factorLabel: prep(ID_MM_FACTOR_LABEL_RATE),
      checkRange: (value: number) => (value <= 0) ? 'Rate must be greater than 0' : null,
      convert: (value: number) => value
    },
    'absolute': {
      label: prep(ID_MM_ABSOLUTE),
      description: prep(ID_MM_ABSOLUTE_DESC),
      factorLabel: prep(ID_MM_FACTOR_LABEL_RATE),
      checkRange: (value: number) => (value <= 0) ? 'Rate must be greater than 0' : null,
      convert: (value: number) => value
    },
    'multiplier': {
      label: prep(ID_MM_MULTIPLIER),
      description: prep(ID_MM_MULTIPLIER_DESC),
      factorLabel: prep(ID_MM_FACTOR_LABEL_MULTIPLIER),
      checkRange: (value: number) => (value < 1 || value > 100) ? 'Multiplier must be between 1 and 100' : null,
      convert: (value: number) => value
    }
  }
}

const GapStrategySelector: React.FC = () => {
  const { botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  // Only render for basic market making bots
  if (!botConfig.basicMarketMakingConfig) return null

  const gapStrategy = botConfig.basicMarketMakingConfig.gapStrategy
  const strategies = gapStrategies()

  const handleGapStrategyChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    dispatch({ type: 'SET_GAP_STRATEGY', payload: e.target.value as GapStrategy })
  }

  return (
    <div className="border rounded p-3 mb-3">
      <div className="d-flex align-items-center justify-content-between mb-2">
        <span className="fs16 demi">{prep(ID_MM_GAP_STRATEGY)}</span>
        <select
          className="form-select fs14"
          style={{ width: 'auto' }}
          value={gapStrategy}
          onChange={handleGapStrategyChange}
        >
          {Object.entries(strategies).map(([value, { label }]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </select>
      </div>
      {gapStrategy && (
        <div className="fs14 grey">
          {strategies[gapStrategy].description}
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
    <div className="border rounded p-3 mb-3">
      <div className="d-flex align-items-center justify-content-between">
        <div>
          <span className="fs16 demi">{prep(ID_MM_PROFIT_THRESHOLD)}</span>
          <div className="fs14 grey">{prep(ID_MM_PROFIT_ADV_DESC)}</div>
        </div>
        <div style={{ width: '6rem' }}>
          <NumberInput
            value={profit * 100}
            onChange={handleProfitChange}
            precision={2}
            className="p-1 text-center fs16"
            suffix="%"
          />
        </div>
      </div>
      {errorMessage && <ErrorMessage message={errorMessage} onClear={() => setErrorMessage('')} />}
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
        <IconButton iconClass="ico-cross sellcolor" onClick={onRemove} ariaLabel={prep(ID_MM_REMOVE_PLACEMENT)} />
        {!isFirst && <IconButton iconClass="ico-arrowup" onClick={onMoveUp} ariaLabel={prep(ID_MM_MOVE_UP)} />}
        {!isLast && <IconButton iconClass="ico-arrowdown" onClick={onMoveDown} ariaLabel={prep(ID_MM_MOVE_DOWN)} />}
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
  const strategies = gapStrategies()

  const isBasicMM = !!botConfig.basicMarketMakingConfig
  const gapStrategy: GapStrategy = botConfig.basicMarketMakingConfig?.gapStrategy ?? 'multiplier'
  const title = isSell ? prep(ID_MM_SELL_PLACEMENTS) : prep(ID_MM_BUY_PLACEMENTS)
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

    const rangeError = strategies[gapStrategy].checkRange(gapFactor)
    if (rangeError) {
      setErrorMessage(rangeError)
      return
    }

    // Convert gap factor for storage if needed
    const storageGapFactor = strategies[gapStrategy].convert(gapFactor)

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
    <div className="border rounded p-3 h-100">
      <div className="fs16 demi mb-2">{title}</div>
      <table className="cell-border compact w-100">
        <thead>
          <tr>
            { isBasicMM
              ? (
                <th className="no-stretch">
                  {prep(ID_MM_PRIORITY)}
                  <Tooltip content={prep(ID_MM_PRIORITY_TOOLTIP)}>
                    <span className="ico-info fs12 ms-1"></span>
                  </Tooltip>
                </th>
                )
              : null }
            <th>{prep(ID_MM_LOTS)}</th>
            <th>{strategies[gapStrategy].factorLabel}</th>
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
              <IconButton iconClass="ico-plus buycolor" onClick={validateAndAddPlacement} ariaLabel={prep(ID_MM_ADD_PLACEMENT)} />
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
  )
}

const headerDescription = prep(ID_MM_ADV_PLACEMENTS_DESC)

export const AdvancedPlacements: React.FC = () => {
  const header = <PlacementsPanelHeader description={headerDescription} />

  return (
    <div>
      {header}

      <GapStrategySelector />
      <ProfitSelector />

      <div className="row mb-3">
        <div className="col-24 col-md-12 mb-3 mb-md-0 pe-md-2">
          <Placements isSell={false} />
        </div>
        <div className="col-24 col-md-12 ps-md-2">
          <Placements isSell={true} />
        </div>
      </div>

      <PlacementsChartWrapper />
    </div>
  )
}
