import React from 'react'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import Tooltip from './Tooltip'
import { app } from '../../registry'
import Doc from '../../doc'
import { requiredDexAssets, AllocationDetail } from '../utils/AllocationUtil'
import { PanelHeader, NumberInput } from './FormComponents'
import State from '../../state'
import { CEXDisplayInfos } from '../../mmutil'

interface QuickConfigInputProps {
  label: string
  tooltip: string
  showBorder?: boolean
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
  showBorder = true,
  suffix,
  min,
  max,
  precision,
  value,
  onChange
}) => (
  <div className={`d-flex align-items-center pt-2 mt-2 pb-2${showBorder ? ' border-bottom' : ''}`}>
    <div className="fs16 me-3" style={{ minWidth: '140px' }}>
      {label}
      <Tooltip content={tooltip}>
        <span className="ico-info fs13 ms-1"></span>
      </Tooltip>
    </div>

    <div className="flex-grow-1">
      <div className="d-flex align-items-center">
        <NumberInput
          min={min}
          max={max}
          precision={precision}
          value={value}
          onChange={onChange}
          withSlider={true}
        />
        {suffix && <span className="fs14 ms-1">{suffix}</span>}
      </div>
    </div>
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

interface PerLotBreakdownProps {
  type: 'buy' | 'sell'
  perLotBreakdown: any
  numLots: number
  assetID: number
}

const PerLotBreakdown: React.FC<PerLotBreakdownProps> = ({
  type,
  perLotBreakdown,
  numLots,
  assetID
}) => {
  if (numLots === 0 || perLotBreakdown.totalAmount === 0) {
    return null
  }

  const title = `Per ${type === 'buy' ? 'Buy' : 'Sell'} Lot Total`

  return (
    <div className="mb-3">
      <CalculationBreakdownRow
        text={title}
        value={perLotBreakdown.totalAmount}
        assetID={assetID}
        level={1}
        displayZero={true}
      />
      <div className="ms-3">
        <CalculationBreakdownRow
          text="Traded Amount"
          value={perLotBreakdown.tradedAmount}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Swap Fees"
          value={perLotBreakdown.fees.swap}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Redeem Fees"
          value={perLotBreakdown.fees.redeem}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Refund Fees"
          value={perLotBreakdown.fees.refund}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Funding Fees"
          value={perLotBreakdown.fees.funding}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Slippage Buffer"
          value={perLotBreakdown.slippageBuffer}
          assetID={assetID}
          level={2}
          isPercentage={true}
        />
        <CalculationBreakdownRow
          text="Multi-Split Buffer"
          value={perLotBreakdown.multiSplitBuffer}
          assetID={assetID}
          level={2}
          isPercentage={true}
        />
      </div>
    </div>
  )
}

interface FeeReservesBreakdownProps {
  type: 'buy' | 'sell'
  feeReserves: any
  numFeeReserves: number
  assetID: number
}

const FeeReservesBreakdown: React.FC<FeeReservesBreakdownProps> = ({
  type,
  feeReserves,
  numFeeReserves,
  assetID
}) => {
  if (numFeeReserves === 0) {
    return null
  }

  const title = `${type === 'buy' ? 'Buy' : 'Sell'} Fee Reserves`
  const totalFees = feeReserves.swap + feeReserves.redeem + feeReserves.refund + feeReserves.funding

  return (
    <div className="mb-3">
      <CalculationBreakdownRow
        text={title}
        value={totalFees}
        assetID={assetID}
        level={1}
      />
      <div className="ms-3">
        <CalculationBreakdownRow
          text="Swap Fees"
          value={feeReserves.swap}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Redeem Fees"
          value={feeReserves.redeem}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Refund Fees"
          value={feeReserves.refund}
          assetID={assetID}
          level={2}
        />
        <CalculationBreakdownRow
          text="Funding Fees"
          value={feeReserves.funding}
          assetID={assetID}
          level={2}
        />
      </div>
    </div>
  )
}

interface AllocationBreakdownProps {
  allocationDetail: AllocationDetail | undefined
  assetID: number
}

const AllocationBreakdown: React.FC<AllocationBreakdownProps> = ({
  allocationDetail,
  assetID
}) => {
  if (!allocationDetail) {
    return null
  }

  return (
    <div
      className="border rounded-bottom p-3"
      style={{
        backgroundColor: State.isDark() ? '#2a2d31' : '#f8f9fa',
        // width: '300px',
        maxHeight: '400px',
        overflowY: 'auto'
      }}
    >
      {/* Per Lot Section */}
      {(allocationDetail.calculation.numBuyLots > 0 || allocationDetail.calculation.numSellLots > 0) && (
        <div className="mb-3">
          <PerLotBreakdown
            type="buy"
            perLotBreakdown={allocationDetail.calculation.buyLot}
            numLots={allocationDetail.calculation.numBuyLots}
            assetID={assetID}
          />
          <PerLotBreakdown
            type="sell"
            perLotBreakdown={allocationDetail.calculation.sellLot}
            numLots={allocationDetail.calculation.numSellLots}
            assetID={assetID}
          />
        </div>
      )}

      {/* Fee Reserves Section */}
      <div className="mb-3">
        <FeeReservesBreakdown
          type="buy"
          feeReserves={allocationDetail.calculation.feeReserves.buyReserves}
          numFeeReserves={allocationDetail.calculation.numBuyFeeReserves}
          assetID={assetID}
        />
        <FeeReservesBreakdown
          type="sell"
          feeReserves={allocationDetail.calculation.feeReserves.sellReserves}
          numFeeReserves={allocationDetail.calculation.numSellFeeReserves}
          assetID={assetID}
        />

        <CalculationBreakdownRow
          text="Initial Buy Funding Fees"
          value={allocationDetail.calculation.initialBuyFundingFees}
          assetID={assetID}
          level={1}
        />
        <CalculationBreakdownRow
          text="Initial Sell Funding Fees"
          value={allocationDetail.calculation.initialSellFundingFees}
          assetID={assetID}
          level={1}
        />
        <CalculationBreakdownRow
          text="Bridge Fee Reserves"
          value={allocationDetail.calculation.bridgeFeeReserves}
          assetID={assetID}
          level={1}
        />
      </div>

      <hr className="my-3" />

      {/* Aggregation Section */}
      <div className="mb-3">
        {allocationDetail.calculation.numBuyLots > 0 && (
          <CalculationBreakdownRow
            text={`${allocationDetail.calculation.numBuyLots} Buy Lot${allocationDetail.calculation.numBuyLots === 1 ? '' : 's'}`}
            value={allocationDetail.calculation.buyLot.totalAmount * allocationDetail.calculation.numBuyLots}
            assetID={assetID}
            level={1}
          />
        )}
        {allocationDetail.calculation.numSellLots > 0 && (
          <CalculationBreakdownRow
            text={`${allocationDetail.calculation.numSellLots} Sell Lot${allocationDetail.calculation.numSellLots === 1 ? '' : 's'}`}
            value={allocationDetail.calculation.sellLot.totalAmount * allocationDetail.calculation.numSellLots}
            assetID={assetID}
            level={1}
          />
        )}
      </div>

      <hr className="my-3" />

      {/* Totals Section */}
      <div className="mb-3">
        <CalculationBreakdownRow
          text="Total Required"
          value={allocationDetail.calculation.totalRequired}
          assetID={assetID}
          level={1}
          displayZero={true}
        />

        <CalculationBreakdownRow
          text="Already Allocated"
          value={allocationDetail.calculation.runningBotTotal ?? 0}
          assetID={assetID}
          level={1}
        />

        {allocationDetail.amount < 0 && <CalculationBreakdownRow
          text="Available To Unallocate"
          value={allocationDetail.calculation.runningBotAvailable ?? 0}
          assetID={assetID}
          level={1}
        />}

        {allocationDetail.amount >= 0 && <CalculationBreakdownRow
          text="Total Available"
          value={allocationDetail.calculation.available}
          assetID={assetID}
          level={1}
          displayZero={true}
        />}
      </div>

      <hr className="my-3" />

      {/* Allocated Amount */}
      <div>
        <CalculationBreakdownRow
          text="Amount Allocated"
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
        // style={{ maxWidth: '300px' }}
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
            className={`ico-arrowright fs14 transition-transform ${isExpanded ? 'rotate-90' : ''}`}
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
        <div className="col-12 mb-3">
          {/* DEX Header */}
          <div className="d-flex align-items-center mb-3">
            <img className="logo-square mini-icon me-3" src={Doc.logoPath('DEX')} alt="DEX" />
            <span className="fs20 demi">Bison Wallet Balances</span>
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
          <div className="col-12 mb-3">
            {/* CEX Header */}
            <div className="d-flex align-items-center mb-3">
              <img className="mini-icon me-3" src={cexLogoPath} alt={cexDisplayName} />
              <span className="fs20 demi">{cexDisplayName} Balances</span>
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
  return <div className="d-flex align-items-center justify-content-around mb-3">
  <div className="flex-shrink-0 mx-1">
    <span className="text-buycolor" style={{ whiteSpace: 'nowrap' }}>Sufficient Funds</span>
  </div>
  <div className="flex-shrink-0 mx-1">
    <span className="text-danger" style={{ whiteSpace: 'nowrap' }}>Insufficient Funds</span>
  </div>
  <div className="flex-shrink-0 mx-1">
    <span className="text-warning" style={{ whiteSpace: 'nowrap' }}>
      Sufficient With Rebalance
      <Tooltip content="Rebalance is enabled and additional funds were allocated on either the CEX or DEX">
        <span className="ico-info fs13 ms-1"></span>
      </Tooltip>
    </span>
  </div>
</div>
}

interface AllocationPanelHeaderProps {
  description: string
}

export const AllocationPanelHeader: React.FC<AllocationPanelHeaderProps> = ({
  description
}) => {
  const { botConfig } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  const isUsingQuickAllocation = !!botConfig.uiConfig.usingQuickBalance

  const title = isUsingQuickAllocation ? 'Quick Allocation' : 'Manual Allocation'
  const buttonText = isUsingQuickAllocation ? 'Configure manually' : 'Quick config'

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
  const { botConfig, baseBridgeFeesAndLimits, quoteBridgeFeesAndLimits, dexMarket } = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  if (!botConfig.uiConfig.quickBalance) return null

  const quickBalance = botConfig.uiConfig.quickBalance

  const marketHasAToken = botConfig.baseID !== dexMarket.baseFeeAssetID || botConfig.quoteID !== dexMarket.quoteFeeAssetID

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
      <AllocationPanelHeader description="Configure fund allocation settings for automatic balance management across DEX and CEX platforms" />

      {/* Two Column Layout */}
      <div className="row">
        {/* Left Column - Configuration Inputs */}
        <div className="col-9">
            {/* Buy Buffer */}
            <QuickConfigInput
              label="Buy Buffer"
              tooltip="Additional lots to allocate beyond the minimum required for buy orders"
              showBorder={true}
              min={0}
              max={3 * numPlacementLots(false)}
              precision={0}
              value={quickBalance.buysBuffer}
              onChange={(value) => handleQuickConfigChange('buysBuffer', value)}
            />

            {/* Sell Buffer */}
            <QuickConfigInput
              label="Sell Buffer"
              tooltip="Additional lots to allocate beyond the minimum required for sell orders"
              showBorder={true}
              min={0}
              max={3 * numPlacementLots(true)}
              precision={0}
              value={quickBalance.sellsBuffer}
              onChange={(value) => handleQuickConfigChange('sellsBuffer', value)}
            />

            {/* Slippage Buffer */}
            <QuickConfigInput
              label="Slippage Buffer"
              tooltip="Additional funds to allocate to account for price slippage"
              showBorder={true}
              suffix="%"
              min={0}
              max={100}
              precision={2}
              value={quickBalance.slippageBuffer * 100}
              onChange={(value) => handleQuickConfigChange('slippageBuffer', value / 100)}
            />

            {/* Buy Fee Reserve */}
            { marketHasAToken && <QuickConfigInput
              label="Buy Fee Reserve"
              tooltip="Additional funds to reserve for transaction fees on buy orders"
              showBorder={true}
              min={0}
              max={1000}
              precision={0}
              value={quickBalance.buyFeeReserve}
              onChange={(value) => handleQuickConfigChange('buyFeeReserve', value)}
            />}

            {/* Sell Fee Reserve */}
            { marketHasAToken && <QuickConfigInput
              label="Sell Fee Reserve"
              tooltip="Additional funds to reserve for transaction fees on sell orders"
              showBorder={true}
              min={0}
              max={1000}
              precision={0}
              value={quickBalance.sellFeeReserve}
              onChange={(value) => handleQuickConfigChange('sellFeeReserve', value)}
            />}

            {/* Bridge Fee Reserve */}
            { (baseBridgeFeesAndLimits || quoteBridgeFeesAndLimits) && <QuickConfigInput
              label="Bridge Fee Reserve"
              tooltip="Additional funds to reserve for bridge transaction fees"
              showBorder={true}
              min={0}
              max={1000}
              precision={0}
              value={quickBalance.bridgeFeeReserve}
              onChange={(value) => handleQuickConfigChange('bridgeFeeReserve', value)}
            />}
        </div>

        {/* Right Column - Balances */}
        <div className="col-15">
          <StatusLabels />
          <AllocationTable />
        </div>
      </div>
    </div>
  )
}

export default QuickAllocationView
