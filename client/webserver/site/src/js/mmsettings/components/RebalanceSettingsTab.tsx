import { useBotConfigState, useBotConfigDispatch, fetchRoundTripFeesAndLimits, projectedAllocations } from '../utils/BotConfig'
import { app } from '../../registry'
import Tooltip from './Tooltip'
import Doc from '../../doc'
import { PanelHeader, NumberInput } from './FormComponents'
import React from 'react'
import { useMMSettingsSetError, useMMSettingsSetLoading } from './MMSettings'
import {
  prep,
  ID_MM_FAILED_FETCH_BRIDGE_FEES,
  ID_MM_WITHDRAWAL,
  ID_MM_DEPOSIT,
  ID_MM_MIN_TRANSFER_TOOLTIP,
  ID_MM_BRIDGE_CONFIGURATION,
  ID_MM_BRIDGE_CONFIG_TOOLTIP,
  ID_MM_BRIDGE_LOCKED,
  ID_MM_BRIDGE_TO_ASSET,
  ID_MM_SELECT_CEX_ASSET,
  ID_MM_BRIDGE,
  ID_MM_SELECT_BRIDGE,
  ID_MM_BRIDGE_ROUND_TRIP_FEES,
  ID_MM_REBALANCE_SETTINGS,
  ID_MM_REBALANCE_DESCRIPTION,
  ID_MM_HOW_REBALANCING_WORKS,
  ID_MM_CEX_REBALANCE,
  ID_MM_CEX_REBALANCE_DESC,
  ID_MM_INTERNAL_TRANSFERS_ONLY,
  ID_MM_INTERNAL_TRANSFERS_DESC,
  ID_MM_MIN_EXTERNAL_TRANSFER
} from '../../locales'

interface MinTransferControlProps {
  asset: 'base' | 'quote'
}

const MinTransferControl: React.FC<MinTransferControlProps> = ({
  asset
}) => {
  const botConfigState = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  const { botConfig, dexMarket } = botConfigState

  const assetInfo = asset === 'base' ? dexMarket.baseAsset : dexMarket.quoteAsset
  let assetSymbol = assetInfo?.symbol || (asset === 'base' ? 'Base' : 'Quote')
  assetSymbol = assetSymbol.split('.')[0].toUpperCase()
  const tooltip = prep(ID_MM_MIN_TRANSFER_TOOLTIP)
  const value = asset === 'base' ? botConfig.autoRebalance?.minBaseTransfer || 0 : botConfig.autoRebalance?.minQuoteTransfer || 0

  // Use min withdrawal from CEX
  const min = asset === 'base' ? botConfigState.baseMinWithdraw : botConfigState.quoteMinWithdraw

  // Get asset ID and calculate precision from conversion factor
  const assetID = asset === 'base' ? dexMarket.baseID : dexMarket.quoteID
  const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
  const precision = Math.round(Math.log10(conversionFactor))

  // Calculate max as total allocated amount on DEX + CEX
  const cexAssetID = asset === 'base' ? botConfig.cexBaseID : botConfig.cexQuoteID
  const projectedAlloc = projectedAllocations(botConfigState)
  const dexAmount = projectedAlloc.dex[assetID] || 0
  const cexAmount = projectedAlloc.cex[cexAssetID] || 0
  const max = Math.max(dexAmount + cexAmount, min)

  const onChange = (value: number) => {
    const actionType = asset === 'base' ? 'BASE_MIN_TRANSFER' : 'QUOTE_MIN_TRANSFER'
    dispatch({
      type: 'UPDATE_REBALANCE_SETTINGS',
      payload: { type: actionType, payload: value }
    })
  }

  return (
    <div className="d-flex align-items-center mb-3">
      <div className="d-flex align-items-center fs16 me-3 flex-shrink-0">
        <img className="mini-icon me-1" src={Doc.logoPath(assetInfo.symbol)} alt={assetSymbol} />
        <span className="me-1">{assetSymbol}</span>
        <Tooltip content={tooltip}>
          <span className="ico-info fs12 ms-1"></span>
        </Tooltip>
      </div>
      <NumberInput
        sliderPosition="inline"
        className="p-1 text-center fs14"
        min={min / conversionFactor}
        max={max / conversionFactor}
        precision={precision}
        value={value / conversionFactor}
        onChange={(value) => onChange(Math.floor(value * conversionFactor))}
        withSlider={true}
      />
    </div>
  )
}

const capitalize = (str: string) => str.charAt(0).toUpperCase() + str.slice(1)

interface BridgeFeesTableProps {
  withdrawalFees: Record<string, number>
  depositFees: Record<string, number>
}

const BridgeFeesTable: React.FC<BridgeFeesTableProps> = ({ withdrawalFees, depositFees }) => {
  const allAssetIDs = [...new Set([...Object.keys(withdrawalFees), ...Object.keys(depositFees)])]

  return (
    <table className="fs14 bridge-fees-table">
      <thead>
        <tr className="fs13">
          <th></th>
          <th className="text-primary fw-normal">{prep(ID_MM_WITHDRAWAL)}</th>
          <th className="text-success fw-normal">{prep(ID_MM_DEPOSIT)}</th>
        </tr>
      </thead>
      <tbody>
        {allAssetIDs.map(assetID => {
          const asset = app().assets[parseInt(assetID)]
          const wFee = withdrawalFees[assetID]
          const dFee = depositFees[assetID]
          return (
            <tr key={assetID}>
              <td className="d-flex align-items-center gap-1">
                {asset && <img className="mini-icon" src={Doc.logoPath(asset.symbol)} alt={asset.symbol} />}
                <span>{asset?.name}</span>
              </td>
              <td>{wFee != null ? Doc.formatCoinValue(wFee, asset?.unitInfo) : '\u2014'}</td>
              <td>{dFee != null ? Doc.formatCoinValue(dFee, asset?.unitInfo) : '\u2014'}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

interface BridgeControlProps {
  asset: 'base' | 'quote'
}

const BridgeControl: React.FC<BridgeControlProps> = ({
  asset
}) => {
  const botConfigState = useBotConfigState()
  const { dexMarket } = botConfigState
  const dispatch = useBotConfigDispatch()
  const setIsLoading = useMMSettingsSetLoading()
  const setError = useMMSettingsSetError()

  const { botConfig } = botConfigState
  const isRunning = !!botConfigState.runStats
  const bridges = asset === 'base' ? botConfigState.baseBridges : botConfigState.quoteBridges
  const currentFeesAndLimits = asset === 'base' ? botConfigState.baseBridgeFeesAndLimits : botConfigState.quoteBridgeFeesAndLimits
  const dexAssetID = asset === 'base' ? dexMarket.baseID : dexMarket.quoteID

  // Only show bridge controls if CEX rebalance is selected
  if (!botConfig.autoRebalance || botConfig.autoRebalance.internalOnly || !currentFeesAndLimits) return null

  const displayBridges = { ...(bridges || {}) }
  if (currentFeesAndLimits.cexAsset !== dexAssetID && currentFeesAndLimits.bridgeName) {
    const bridgeNames = displayBridges[currentFeesAndLimits.cexAsset] || []
    if (!bridgeNames.includes(currentFeesAndLimits.bridgeName)) {
      displayBridges[currentFeesAndLimits.cexAsset] = [...bridgeNames, currentFeesAndLimits.bridgeName]
    }
  }
  if (!Object.keys(displayBridges).length) return null

  const handleCexAssetChange = async (cexAssetID: number) => {
    if (isRunning) return
    if (!displayBridges[cexAssetID] || displayBridges[cexAssetID].length === 0) return

    const bridgeName = displayBridges[cexAssetID][0] // Default to first bridge
    try {
      setIsLoading(true)
      const feesAndLimits = await fetchRoundTripFeesAndLimits(dexAssetID, cexAssetID, bridgeName)
      dispatch({
        type: 'UPDATE_BRIDGE_SELECTION',
        payload: { asset, feesAndLimits }
      })
    } catch (error) {
      setError({
        message: prep(ID_MM_FAILED_FETCH_BRIDGE_FEES)
      })
    } finally {
      setIsLoading(false)
    }
  }

  const handleBridgeChange = async (bridgeName: string) => {
    if (isRunning) return
    try {
      setIsLoading(true)
      const feesAndLimits = await fetchRoundTripFeesAndLimits(dexAssetID, currentFeesAndLimits.cexAsset, bridgeName)
      dispatch({
        type: 'UPDATE_BRIDGE_SELECTION',
        payload: { asset, feesAndLimits }
      })
    } catch (error) {
      setError({
        message: prep(ID_MM_FAILED_FETCH_BRIDGE_FEES)
      })
    } finally {
      setIsLoading(false)
    }
  }

  const assetName = asset === 'base' ? 'Base' : 'Quote'
  const availableCexAssets = Object.keys(displayBridges).map(id => parseInt(id))
  const currentCexAsset = currentFeesAndLimits.cexAsset
  const availableBridges = displayBridges[currentCexAsset]
  const currentBridge = currentFeesAndLimits.bridgeName

  return (
    <>
      <span className="fs16 demi d-block mb-2">
        {prep(ID_MM_BRIDGE_CONFIGURATION, { asset: assetName })}
        <Tooltip content={prep(ID_MM_BRIDGE_CONFIG_TOOLTIP)}>
          <span className="ico-info fs12 ms-1"></span>
        </Tooltip>
      </span>

      {isRunning && (
        <div className="fs14 text-muted mb-2">
          {prep(ID_MM_BRIDGE_LOCKED)}
        </div>
      )}

      <div className="ps-2">
        <div className="d-flex align-items-center justify-content-between mb-2">
          <span className="fs16">{prep(ID_MM_BRIDGE_TO_ASSET)}</span>
          <select
            className={`form-select ${isRunning ? 'mm-readonly-select' : ''}`}
            style={{ width: 'auto' }}
            value={currentCexAsset || ''}
            disabled={isRunning}
            onChange={(e) => handleCexAssetChange(parseInt(e.target.value))}
          >
            <option value="">{prep(ID_MM_SELECT_CEX_ASSET)}</option>
            {availableCexAssets.map(cexAssetID => (
              <option key={cexAssetID} value={cexAssetID}>
                {app().prettyPrintAssetID(cexAssetID)}
              </option>
            ))}
          </select>
        </div>

        {availableBridges && availableBridges.length > 0 && (
          <div className="d-flex align-items-center justify-content-between mb-2">
            <span className="fs16">{prep(ID_MM_BRIDGE)}</span>
            <select
              className={`form-select ${isRunning ? 'mm-readonly-select' : ''}`}
              style={{ width: 'auto' }}
              value={currentBridge || ''}
              disabled={isRunning}
              onChange={(e) => handleBridgeChange(e.target.value)}
            >
              <option value="">{prep(ID_MM_SELECT_BRIDGE)}</option>
              {availableBridges.map(bridgeName => (
                <option key={bridgeName} value={bridgeName}>
                  {capitalize(bridgeName)}
                </option>
              ))}
            </select>
          </div>
        )}

      </div>

      {/* Bridge Fees Display */}
      <span className="fs16 d-block mt-2 mb-1">{prep(ID_MM_BRIDGE_ROUND_TRIP_FEES)}</span>
      <div className="ps-2 p-2 section-bg rounded">
        <BridgeFeesTable
          withdrawalFees={currentFeesAndLimits.withdrawal.fees}
          depositFees={currentFeesAndLimits.deposit.fees}
        />
      </div>
    </>
  )
}

export const RebalanceSettingsPanelHeader: React.FC = () => {
  return (
    <PanelHeader
      title={prep(ID_MM_REBALANCE_SETTINGS)}
      description={prep(ID_MM_REBALANCE_DESCRIPTION)}
    />
  )
}

const RebalanceSettingsTab: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { botConfig } = botConfigState
  const dispatch = useBotConfigDispatch()

  const hasBridges = botConfigState.baseBridges || botConfigState.baseBridgeFeesAndLimits ||
    botConfigState.quoteBridges || botConfigState.quoteBridgeFeesAndLimits
  const isCexRebalance = !!botConfig.autoRebalance && !botConfig.autoRebalance.internalOnly

  const handleRebalanceTypeChange = (cexRebalance: boolean) => {
    dispatch({
      type: 'UPDATE_REBALANCE_SETTINGS',
      payload: { type: 'CEX_REBALANCE', payload: cexRebalance }
    })
  }

  return (
    <div>
      <RebalanceSettingsPanelHeader />

      <div className="border rounded p-3 mm-mixer-row">
        {/* Rebalance Method */}
        <span className="fs16 demi d-block mb-2">{prep(ID_MM_HOW_REBALANCING_WORKS)}</span>
        <div className="ps-2">
          <div className="form-check mb-2">
            <input
              className="form-check-input"
              type="radio"
              name="rebalanceMethod"
              id="cexRebalance"
              checked={isCexRebalance}
              onChange={() => handleRebalanceTypeChange(true)}
            />
            <label className="form-check-label fs16" htmlFor="cexRebalance">
              {prep(ID_MM_CEX_REBALANCE)}
            </label>
          </div>
          <div className="fs14 text-muted mb-3 ms-3">
            {prep(ID_MM_CEX_REBALANCE_DESC)}
          </div>

          <div className="form-check">
            <input
              className="form-check-input"
              type="radio"
              name="rebalanceMethod"
              id="internalTransfers"
              checked={!!botConfig.autoRebalance && botConfig.autoRebalance.internalOnly}
              onChange={() => handleRebalanceTypeChange(false)}
            />
            <label className="form-check-label fs16" htmlFor="internalTransfers">
              {prep(ID_MM_INTERNAL_TRANSFERS_ONLY)}
            </label>
          </div>
          <div className="fs14 text-muted ms-3">
            {prep(ID_MM_INTERNAL_TRANSFERS_DESC)}
          </div>
        </div>

        {/* Min Transfers - Only show if CEX Rebalance */}
        {isCexRebalance && (
          <>
            <hr className="my-3" />
            <span className="fs16 demi d-block mb-2">{prep(ID_MM_MIN_EXTERNAL_TRANSFER)}</span>
            <div className="ps-2">
              <MinTransferControl asset="base" />
              <MinTransferControl asset="quote" />
            </div>
          </>
        )}

        {/* Bridge Configs - Only show if CEX Rebalance and bridges exist */}
        {isCexRebalance && hasBridges && (
          <>
            {(botConfigState.baseBridges || botConfigState.baseBridgeFeesAndLimits) && (
              <>
                <hr className="my-3" />
                <BridgeControl asset="base" />
              </>
            )}
            {(botConfigState.quoteBridges || botConfigState.quoteBridgeFeesAndLimits) && (
              <>
                <hr className="my-3" />
                <BridgeControl asset="quote" />
              </>
            )}
          </>
        )}
      </div>
    </div>
  )
}

export default RebalanceSettingsTab
