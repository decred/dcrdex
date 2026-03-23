import React from 'react'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import Tooltip from './Tooltip'
import { app } from '../../registry'
import Doc from '../../doc'
import { requiredDexAssets, AllocationDetail } from '../utils/AllocationUtil'
import { PanelHeader, NumberInput } from './FormComponents'
import State from '../../state'
import { CEXDisplayInfos } from '../../mmutil'
import {
  prep,
  ID_MM_TRADED_AMOUNT, ID_MM_SLIPPAGE_BUFFER, ID_MM_MULTI_SPLIT_BUFFER,
  ID_MM_SWAP_FEES, ID_MM_REDEEM_FEES, ID_MM_REFUND_FEES, ID_MM_FUNDING_FEES,
  ID_MM_INITIAL_BUY_FUNDING_FEES, ID_MM_INITIAL_SELL_FUNDING_FEES,
  ID_MM_BRIDGE_FEE_RESERVES, ID_MM_BRIDGE_FEES, ID_MM_SEND_FEES,
  ID_MM_BUFFERED_AMOUNT,
  ID_MM_TOTAL_REQUIRED, ID_MM_ALREADY_ALLOCATED,
  ID_MM_AVAILABLE_TO_UNALLOCATE, ID_MM_TOTAL_AVAILABLE,
  ID_MM_AMOUNT_ALLOCATED, ID_MM_ALLOC_CHANGE,
  ID_MM_BUY_FEE_RESERVES, ID_MM_SELL_FEE_RESERVES,
  ID_MM_FUNDED, ID_MM_UNDERFUNDED,
  ID_MM_FUNDED_WITH_REBALANCE, ID_MM_FUNDED_WITH_REBALANCE_TOOLTIP,
  ID_MM_QUICK_ALLOCATION, ID_MM_MANUAL_ALLOCATION,
  ID_MM_SWITCH_TO_MANUAL, ID_MM_USE_AUTO_ESTIMATE,
  ID_CEX_BALANCES,
  ID_MM_BUFFERS_AND_RESERVES, ID_MM_ALLOCATION_PREVIEW,
  ID_MM_QUICK_ALLOC_DESC,
  ID_MM_ALLOC_RUNNING_QUICK, ID_MM_ALLOC_NEW_QUICK,
  ID_MM_ALLOC_RUNNING_MANUAL, ID_MM_ALLOC_RUNNING_MANUAL_NOTE, ID_MM_ALLOC_NEW_MANUAL,
  ID_MM_BUY_BUFFER, ID_MM_EXTRA_BUY_LOTS_TOOLTIP,
  ID_MM_SELL_BUFFER, ID_MM_EXTRA_SELL_LOTS_TOOLTIP,
  ID_MM_SLIPPAGE_BUFFER_TOOLTIP,
  ID_MM_BUY_FEE_RESERVE, ID_MM_EXTRA_BUY_FEE_TOOLTIP,
  ID_MM_SELL_FEE_RESERVE, ID_MM_EXTRA_SELL_FEE_TOOLTIP,
  ID_MM_BRIDGE_FEE_RESERVE, ID_MM_EXTRA_REBALANCE_FEE_TOOLTIP
} from '../../locales'

interface QuickConfigInputProps {
  label: string
  tooltip: string
  suffix?: string
  min: number
  max: number
  precision: number
  value: number
  onChange: (value: number) => void
}

const QuickConfigInput: React.FC<QuickConfigInputProps> = ({
  label,
  tooltip,
  suffix,
  min,
  max,
  precision,
  value,
  onChange
}) => (
  <div className="d-flex align-items-center pt-2 mt-2 pb-2 mm-mixer-row">
    <div className="fs16 me-3" style={{ flex: '0 0 140px' }}>
      {label}
      <Tooltip content={tooltip}>
        <span className="ico-info fs13 ms-1"></span>
      </Tooltip>
    </div>

    <NumberInput
      sliderPosition="inline"
      className="p-1 text-center fs14"
      min={min}
      max={max}
      precision={precision}
      value={value}
      onChange={onChange}
      withSlider={true}
      suffix={suffix}
    />
  </div>
)

interface CalculationBreakdownRowProps {
  text: string
  value: number
  assetID: number
  level: 1 | 2
  isPercentage?: boolean
  displayZero?: boolean
}

const CalculationBreakdownRow: React.FC<CalculationBreakdownRowProps> = ({
  text,
  value,
  assetID,
  level,
  isPercentage = false,
  displayZero = false
}) => {
  // If displayZero is not true and value is zero, don't render the row
  if (!displayZero && value === 0) {
    return null
  }

  // Helper function to format values
  const formatValue = (value: number, assetID: number) => {
    const asset = app().assets[assetID]
    if (isPercentage) {
      return `${(value * 100).toFixed(2)}%`
    }
    return value ? Doc.formatCoinValue(value, asset.unitInfo) : '0'
  }

  const textSizeClass = level === 1 ? 'fs14' : 'fs11'
  const textColorClass = State.isDark() ? 'text-white' : 'text-dark'

  return (
    <div className="d-flex justify-content-between align-items-center py-1">
      <span className={`${textSizeClass} ${textColorClass}`}>{text}</span>
      <span className={`${textSizeClass} ${State.isDark() ? 'text-light' : 'text-muted'}`}>
        {formatValue(value, assetID)}
      </span>
    </div>
  )
}

interface AllocationBreakdownProps {
  allocationDetail: AllocationDetail | undefined
  assetID: number
}

interface DetailRow {
  text: string
  value: number
  isPercentage?: boolean
  displayZero?: boolean
}

interface BreakdownRow {
  key: string
  label: string
  unitNote: string
  subtotal: number
  expandable: boolean
  details: DetailRow[]
}

const AllocationBreakdown: React.FC<AllocationBreakdownProps> = ({
  allocationDetail,
  assetID
}) => {
  const [expandedRow, setExpandedRow] = React.useState<string | null>(null)

  if (!allocationDetail) {
    return null
  }

  const calc = allocationDetail.calculation
  const asset = app().assets[assetID]
  const fmt = (value: number) => Doc.formatCoinValue(value, asset.unitInfo)

  const sumFees = (fees: { swap: number; redeem: number; refund: number; funding: number }) =>
    fees.swap + fees.redeem + fees.refund + fees.funding

  const lotBufferedAmount = (lot: typeof calc.buyLot): number => {
    const principal = lot.tradedAmount * (1 + lot.slippageBuffer + lot.multiSplitBuffer)
    return lot.multiSplitBuffer === 0 ? Math.floor(principal) : Math.round(principal)
  }

  const lotEffectiveSwap = (lot: typeof calc.buyLot): number => {
    return lot.multiSplitBuffer === 0
      ? lot.fees.swap
      : Math.round(lot.fees.swap * (1 + lot.multiSplitBuffer))
  }

  const lotDetails = (lot: typeof calc.buyLot): DetailRow[] => {
    const hasBuffers = lot.slippageBuffer > 0 || lot.multiSplitBuffer > 0
    const details: DetailRow[] = [
      { text: prep(ID_MM_TRADED_AMOUNT), value: lot.tradedAmount },
      { text: prep(ID_MM_SLIPPAGE_BUFFER), value: lot.slippageBuffer, isPercentage: true },
      { text: prep(ID_MM_MULTI_SPLIT_BUFFER), value: lot.multiSplitBuffer, isPercentage: true },
    ]
    if (hasBuffers) {
      details.push({ text: prep(ID_MM_BUFFERED_AMOUNT), value: lotBufferedAmount(lot), displayZero: true })
    }
    details.push(
      { text: prep(ID_MM_SWAP_FEES), value: lotEffectiveSwap(lot) },
      { text: prep(ID_MM_REDEEM_FEES), value: lot.fees.redeem },
      { text: prep(ID_MM_REFUND_FEES), value: lot.fees.refund },
      { text: prep(ID_MM_FUNDING_FEES), value: lot.fees.funding }
    )
    return details
  }

  const rows: BreakdownRow[] = []

  if (calc.numBuyLots > 0 && calc.buyLot.totalAmount > 0) {
    const s = calc.numBuyLots === 1 ? '' : 's'
    rows.push({
      key: 'buyLots',
      label: `${calc.numBuyLots} Buy Lot${s}`,
      unitNote: `(~${fmt(calc.buyLot.totalAmount)} each)`,
      subtotal: calc.buyLot.totalAmount * calc.numBuyLots,
      expandable: true,
      details: lotDetails(calc.buyLot),
    })
  }

  if (calc.numSellLots > 0 && calc.sellLot.totalAmount > 0) {
    const s = calc.numSellLots === 1 ? '' : 's'
    rows.push({
      key: 'sellLots',
      label: `${calc.numSellLots} Sell Lot${s}`,
      unitNote: `(~${fmt(calc.sellLot.totalAmount)} each)`,
      subtotal: calc.sellLot.totalAmount * calc.numSellLots,
      expandable: true,
      details: lotDetails(calc.sellLot),
    })
  }

  const totalBuyFees = sumFees(calc.feeReserves.buyReserves)
  if (calc.numBuyFeeReserves > 0 && totalBuyFees > 0) {
    rows.push({
      key: 'buyFeeReserves',
      label: prep(ID_MM_BUY_FEE_RESERVES),
      unitNote: `(~${fmt(totalBuyFees)} \u00d7 ${calc.numBuyFeeReserves} units)`,
      subtotal: totalBuyFees * calc.numBuyFeeReserves,
      expandable: true,
      details: [
        { text: prep(ID_MM_SWAP_FEES), value: calc.feeReserves.buyReserves.swap },
        { text: prep(ID_MM_REDEEM_FEES), value: calc.feeReserves.buyReserves.redeem },
        { text: prep(ID_MM_REFUND_FEES), value: calc.feeReserves.buyReserves.refund },
        { text: prep(ID_MM_FUNDING_FEES), value: calc.feeReserves.buyReserves.funding },
      ],
    })
  }

  const totalSellFees = sumFees(calc.feeReserves.sellReserves)
  if (calc.numSellFeeReserves > 0 && totalSellFees > 0) {
    rows.push({
      key: 'sellFeeReserves',
      label: prep(ID_MM_SELL_FEE_RESERVES),
      unitNote: `(~${fmt(totalSellFees)} \u00d7 ${calc.numSellFeeReserves} units)`,
      subtotal: totalSellFees * calc.numSellFeeReserves,
      expandable: true,
      details: [
        { text: prep(ID_MM_SWAP_FEES), value: calc.feeReserves.sellReserves.swap },
        { text: prep(ID_MM_REDEEM_FEES), value: calc.feeReserves.sellReserves.redeem },
        { text: prep(ID_MM_REFUND_FEES), value: calc.feeReserves.sellReserves.refund },
        { text: prep(ID_MM_FUNDING_FEES), value: calc.feeReserves.sellReserves.funding },
      ],
    })
  }

  if (calc.initialBuyFundingFees > 0) {
    rows.push({
      key: 'initBuyFunding',
      label: prep(ID_MM_INITIAL_BUY_FUNDING_FEES),
      unitNote: '',
      subtotal: calc.initialBuyFundingFees,
      expandable: false,
      details: [],
    })
  }

  if (calc.initialSellFundingFees > 0) {
    rows.push({
      key: 'initSellFunding',
      label: prep(ID_MM_INITIAL_SELL_FUNDING_FEES),
      unitNote: '',
      subtotal: calc.initialSellFundingFees,
      expandable: false,
      details: [],
    })
  }

  const rebalancePerUnit = calc.bridgeFees + calc.sendFees
  if (calc.rebalanceFeeReserves > 0 && rebalancePerUnit > 0) {
    rows.push({
      key: 'rebalance',
      label: prep(ID_MM_BRIDGE_FEE_RESERVES),
      unitNote: `(~${fmt(rebalancePerUnit)} \u00d7 ${calc.rebalanceFeeReserves} units)`,
      subtotal: calc.rebalanceFeeReserves * rebalancePerUnit,
      expandable: true,
      details: [
        { text: prep(ID_MM_BRIDGE_FEES), value: calc.bridgeFees, displayZero: true },
        { text: prep(ID_MM_SEND_FEES), value: calc.sendFees, displayZero: true },
      ],
    })
  }

  const isRunningBot = calc.runningBotTotal !== undefined
  const allocationLabel = isRunningBot ? prep(ID_MM_ALLOC_CHANGE) : prep(ID_MM_AMOUNT_ALLOCATED)

  const textColorClass = State.isDark() ? 'text-white' : 'text-dark'
  const mutedClass = State.isDark() ? 'text-light' : 'text-muted'

  const toggleRow = (key: string) => setExpandedRow(expandedRow === key ? null : key)

  return (
    <div className="border rounded-bottom p-3 mm-allocation-breakdown">
      {rows.map(row => (
        <div key={row.key}>
          <div
            className={`d-flex justify-content-between align-items-center py-1${row.expandable ? ' cursor-pointer' : ''}`}
            onClick={row.expandable ? () => toggleRow(row.key) : undefined}
          >
            <span className={`fs14 ${textColorClass}`}>
              {row.label}
              {row.unitNote && <span className={`ms-1 ${mutedClass} fs11`}>{row.unitNote}</span>}
            </span>
            <div className="d-flex align-items-center">
              <span className={`fs14 ${mutedClass}`}>{fmt(row.subtotal)}</span>
              {row.expandable && (
                <span
                  className="ico-arrowright fs11 ms-1"
                  style={{
                    transform: expandedRow === row.key ? 'rotate(90deg)' : 'rotate(0deg)',
                    transition: 'transform 0.2s ease'
                  }}
                ></span>
              )}
            </div>
          </div>
          {row.expandable && expandedRow === row.key && (
            <div className="ms-3">
              {row.details.map(detail => (
                <CalculationBreakdownRow
                  key={detail.text}
                  text={detail.text}
                  value={detail.value}
                  assetID={assetID}
                  level={2}
                  isPercentage={detail.isPercentage}
                  displayZero={detail.displayZero}
                />
              ))}
            </div>
          )}
        </div>
      ))}

      <hr className="my-3" />

      {/* Totals Section */}
      <div className="mb-3">
        <CalculationBreakdownRow
          text={prep(ID_MM_TOTAL_REQUIRED)}
          value={calc.totalRequired}
          assetID={assetID}
          level={1}
          displayZero={true}
        />

        <CalculationBreakdownRow
          text={prep(ID_MM_ALREADY_ALLOCATED)}
          value={calc.runningBotTotal ?? 0}
          assetID={assetID}
          level={1}
        />

        {allocationDetail.amount < 0 && <CalculationBreakdownRow
          text={prep(ID_MM_AVAILABLE_TO_UNALLOCATE)}
          value={calc.runningBotAvailable ?? 0}
          assetID={assetID}
          level={1}
        />}

        {allocationDetail.amount >= 0 && <CalculationBreakdownRow
          text={prep(ID_MM_TOTAL_AVAILABLE)}
          value={calc.available}
          assetID={assetID}
          level={1}
          displayZero={true}
        />}
      </div>

      <hr className="my-3" />

      {/* Allocation Result */}
      <div>
        <CalculationBreakdownRow
          text={allocationLabel}
          value={allocationDetail.amount}
          assetID={assetID}
          level={1}
          displayZero={true}
        />
      </div>
    </div>
  )
}

const BalanceItem: React.FC<{
  assetID: number;
  amount: number;
  status?: string;
  allocationDetail?: AllocationDetail;
  isExpanded: boolean;
  onToggle: () => void;
}> = ({
  assetID,
  amount,
  status,
  allocationDetail,
  isExpanded,
  onToggle
}) => {
  const asset = app().assets[assetID]

  // Helper function to format values
  const formatValue = (value: number, assetID: number) => {
    const asset = app().assets[assetID]
    return value ? Doc.formatCoinValue(value, asset.unitInfo) : '0'
  }

  // Helper function to get status color class
  const getStatusClass = (status?: string) => {
    switch (status) {
      case 'sufficient': return 'text-buycolor'
      case 'insufficient': return 'text-danger'
      case 'sufficient-with-rebalance': return 'text-warning'
      default: return 'text-danger'
    }
  }

  return (
    <div className="mb-2">
      <div
        className="d-flex align-items-center justify-content-between p-2 border rounded cursor-pointer"
        onClick={onToggle}
      >
        <div className="d-flex align-items-center">
          <img className="mini-icon me-2" src={Doc.logoPath(asset.symbol)} alt={asset.symbol} />
          <span className="fs16">{asset.unitInfo.conventional.unit}</span>
        </div>
        <div className="d-flex align-items-center">
          <span className={`fs16 me-2 ${getStatusClass(status)}`}>
            {formatValue(amount, assetID)}
          </span>
          <span
            className='ico-arrowright fs14'
            style={{
              transform: isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.2s ease'
            }}
          ></span>
        </div>
      </div>

      {isExpanded && (
        <AllocationBreakdown
          allocationDetail={allocationDetail}
          assetID={assetID}
        />
      )}
    </div>
  )
}

const AllocationTable: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { botConfig, allocationResult } = botConfigState
  const [expandedItem, setExpandedItem] = React.useState<string | null>(null)

  if (!allocationResult) return null

  const dexAssetIDs = requiredDexAssets(botConfigState)

  const toggleExpanded = (itemKey: string) => {
    setExpandedItem(expandedItem === itemKey ? null : itemKey)
  }

  const cexDisplayInfo = CEXDisplayInfos[botConfig.cexName]
  const cexLogoPath = cexDisplayInfo?.logo || ''
  const cexDisplayName = cexDisplayInfo?.name || botConfig.cexName || 'CEX'

  return (
    <div className="mb-4">
      {/* Two Column Layout */}
      <div className="row">
        {/* DEX Column */}
        <div className="col-24 col-xl-12 mb-3">
          {/* DEX Header */}
            <div className="d-flex align-items-center mb-3">
              <img className="logo-square mini-icon me-3" src={Doc.logoPath('DEX')} alt="DEX" />
            <span className="fs16 demi">Bison Wallet Balances</span>
            </div>

          {/* DEX Balances */}
          <div className="d-flex flex-column">
            {dexAssetIDs.map(assetID => {
              const allocation = allocationResult.dex[assetID]
              const amount = allocation ? allocation.amount : 0
              const status = allocation?.status
              const itemKey = `dex-${assetID}`

              return (
                <BalanceItem
                  key={itemKey}
                  assetID={assetID}
                  amount={amount}
                  status={status}
                  allocationDetail={allocation}
                  isExpanded={expandedItem === itemKey}
                  onToggle={() => toggleExpanded(itemKey)}
                />
              )
            })}
          </div>
        </div>

        {/* CEX Column (only if CEX is configured) */}
        {botConfig.cexName && (
          <div className="col-24 col-xl-12 mb-3">
            {/* CEX Header */}
            <div className="d-flex align-items-center mb-3">
              <img className="mini-icon me-3" src={cexLogoPath} alt={cexDisplayName} />
              <span className="fs16 demi">{prep(ID_CEX_BALANCES, { cexName: cexDisplayName })}</span>
            </div>

            {/* CEX Balances */}
            <div className="d-flex flex-column">
              {[botConfig.cexBaseID, botConfig.cexQuoteID].map(assetID => {
                const allocation = allocationResult.cex[assetID]
                const amount = allocation ? allocation.amount : 0
                const status = allocation?.status
                const itemKey = `cex-${assetID}`

                return (
                  <BalanceItem
                    key={itemKey}
                    assetID={assetID}
                    amount={amount}
                    status={status}
                    allocationDetail={allocation}
                    isExpanded={expandedItem === itemKey}
                    onToggle={() => toggleExpanded(itemKey)}
                  />
                )
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

const StatusLabels: React.FC = () => {
  const { botConfig } = useBotConfigState()
  const showRebalance = !!botConfig.autoRebalance && !botConfig.autoRebalance.internalOnly

  return (
    <div className="d-flex align-items-center justify-content-center mb-3 flex-wrap">
      <div className="flex-shrink-0 mx-2">
        <span className="text-buycolor" style={{ whiteSpace: 'nowrap' }}>
          <span className="me-1">{'\u25CF'}</span>{prep(ID_MM_FUNDED)}
        </span>
      </div>
      <div className="flex-shrink-0 mx-2">
        <span className="text-danger" style={{ whiteSpace: 'nowrap' }}>
          <span className="me-1">{'\u25CF'}</span>{prep(ID_MM_UNDERFUNDED)}
        </span>
      </div>
      {showRebalance && (
        <div className="flex-shrink-0 mx-2">
          <span className="text-warning" style={{ whiteSpace: 'nowrap' }}>
            <span className="me-1">{'\u25CF'}</span>{prep(ID_MM_FUNDED_WITH_REBALANCE)}
            <Tooltip content={prep(ID_MM_FUNDED_WITH_REBALANCE_TOOLTIP)}>
              <span className="ico-info fs13 ms-1"></span>
            </Tooltip>
          </span>
        </div>
      )}
    </div>
  )
}

interface AllocationPanelHeaderProps {
  description: string
}

interface AllocationModeNoteProps {
  isRunning: boolean
  mode: 'quick' | 'manual'
}

export const AllocationModeNote: React.FC<AllocationModeNoteProps> = ({
  isRunning,
  mode
}) => {
  const baseClass = 'fs14 text-muted'

  if (mode === 'quick') {
    return (
      <div className="mb-3">
        {isRunning
          ? (
            <>
              <div className={baseClass}>
                {prep(ID_MM_ALLOC_RUNNING_QUICK)}
              </div>
            </>
            )
          : (
            <div className={baseClass}>
              {prep(ID_MM_ALLOC_NEW_QUICK)}
            </div>
            )}
      </div>
    )
  }

  return (
    <div className="mb-3">
      {isRunning
        ? (
          <>
            <div className={baseClass}>
              {prep(ID_MM_ALLOC_RUNNING_MANUAL)}
            </div>
            <div className={`${baseClass} mt-1`}>
              {prep(ID_MM_ALLOC_RUNNING_MANUAL_NOTE)}
            </div>
          </>
          )
        : (
          <div className={baseClass}>
            {prep(ID_MM_ALLOC_NEW_MANUAL)}
          </div>
          )}
    </div>
  )
}

export const AllocationPanelHeader: React.FC<AllocationPanelHeaderProps> = ({
  description
}) => {
  const { botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  const isUsingQuickAllocation = !!botConfig.uiConfig.usingQuickBalance

  const title = isUsingQuickAllocation ? prep(ID_MM_QUICK_ALLOCATION) : prep(ID_MM_MANUAL_ALLOCATION)
  const buttonText = isUsingQuickAllocation ? prep(ID_MM_SWITCH_TO_MANUAL) : prep(ID_MM_USE_AUTO_ESTIMATE)

  const handleSwitch = () => {
    dispatch({ type: 'TOGGLE_QUICK_BALANCE', payload: !isUsingQuickAllocation })
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

const QuickAllocationView: React.FC = () => {
  const { botConfig, dexMarket, runStats } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  if (!botConfig.uiConfig.quickBalance) return null

  const quickBalance = botConfig.uiConfig.quickBalance

  const marketHasAToken = botConfig.baseID !== dexMarket.baseFeeAssetID || botConfig.quoteID !== dexMarket.quoteFeeAssetID
  const cexRebalance = !!botConfig.autoRebalance && !botConfig.autoRebalance.internalOnly
  const isRunning = !!runStats

  const handleQuickConfigChange = (field: keyof typeof quickBalance, value: number) => {
    dispatch({
      type: 'UPDATE_QUICK_BALANCE',
      payload: { field, value }
    })
  }

  const numPlacementLots = (sells: boolean) : number => {
    if (botConfig.arbMarketMakingConfig) {
      const cfg = botConfig.arbMarketMakingConfig
      const placements = sells ? cfg.sellPlacements : cfg.buyPlacements
      return placements.reduce((prev, curr) => {
        return prev + curr.lots
      }, 0)
    }

    if (botConfig.basicMarketMakingConfig) {
      const cfg = botConfig.basicMarketMakingConfig
      const placements = sells ? cfg.sellPlacements : cfg.buyPlacements
      return placements.reduce((prev, curr) => {
        return prev + curr.lots
      }, 0)
    }

    return 1
  }

  return (
    <div>
      <AllocationPanelHeader description={prep(ID_MM_QUICK_ALLOC_DESC)} />
      <AllocationModeNote isRunning={isRunning} mode="quick" />

      {/* Two Column Layout - stacks on small screens */}
      <div className="row">
        {/* Left Column - Configuration Inputs */}
        <div className="col-24 col-xl-9 mb-3">
          <div className="border rounded p-3 h-100">
            <div className="fs18 demi mb-2">{prep(ID_MM_BUFFERS_AND_RESERVES)}</div>
            {/* Buy Buffer */}
            <QuickConfigInput
              label={prep(ID_MM_BUY_BUFFER)}
              tooltip={prep(ID_MM_EXTRA_BUY_LOTS_TOOLTIP)}
              min={0}
              max={3 * numPlacementLots(false)}
              precision={0}
              value={quickBalance.buysBuffer}
              onChange={(value) => handleQuickConfigChange('buysBuffer', value)}
            />

            {/* Sell Buffer */}
            <QuickConfigInput
              label={prep(ID_MM_SELL_BUFFER)}
              tooltip={prep(ID_MM_EXTRA_SELL_LOTS_TOOLTIP)}
              min={0}
              max={3 * numPlacementLots(true)}
              precision={0}
              value={quickBalance.sellsBuffer}
              onChange={(value) => handleQuickConfigChange('sellsBuffer', value)}
            />

            {/* Slippage Buffer */}
            <QuickConfigInput
              label={prep(ID_MM_SLIPPAGE_BUFFER)}
              tooltip={prep(ID_MM_SLIPPAGE_BUFFER_TOOLTIP)}
              suffix="%"
              min={0}
              max={100}
              precision={2}
              value={quickBalance.slippageBuffer * 100}
              onChange={(value) => handleQuickConfigChange('slippageBuffer', value / 100)}
            />

            {/* Buy Fee Reserve */}
            { marketHasAToken && <QuickConfigInput
              label={prep(ID_MM_BUY_FEE_RESERVE)}
              tooltip={prep(ID_MM_EXTRA_BUY_FEE_TOOLTIP)}
              min={0}
              max={1000}
              precision={0}
              value={quickBalance.buyFeeReserve}
              onChange={(value) => handleQuickConfigChange('buyFeeReserve', value)}
            />}

            {/* Sell Fee Reserve */}
            { marketHasAToken && <QuickConfigInput
              label={prep(ID_MM_SELL_FEE_RESERVE)}
              tooltip={prep(ID_MM_EXTRA_SELL_FEE_TOOLTIP)}
              min={0}
              max={1000}
              precision={0}
              value={quickBalance.sellFeeReserve}
              onChange={(value) => handleQuickConfigChange('sellFeeReserve', value)}
            />}

            {/* External Rebalance Fee Reserve */}
            { cexRebalance && <QuickConfigInput
              label={prep(ID_MM_BRIDGE_FEE_RESERVE)}
              tooltip={prep(ID_MM_EXTRA_REBALANCE_FEE_TOOLTIP)}
              min={0}
              max={1000}
              precision={0}
              value={quickBalance.rebalanceFeeReserve}
              onChange={(value) => handleQuickConfigChange('rebalanceFeeReserve', value)}
            />}
          </div>
        </div>

        {/* Right Column - Balances */}
        <div className="col-24 col-xl-15 mb-3">
          <div className="border rounded p-3 h-100">
            <div className="fs18 demi mb-2">{prep(ID_MM_ALLOCATION_PREVIEW)}</div>
            <StatusLabels />
            <AllocationTable />
          </div>
        </div>
      </div>
    </div>
  )
}

export default QuickAllocationView
