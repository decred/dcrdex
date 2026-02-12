import { useBotConfigState, useBotConfigDispatch, fetchRoundTripFeesAndLimits } from '../utils/BotConfig'
import { app } from '../../registry'
import Tooltip from './Tooltip'
import Doc from '../../doc'
import { PanelHeader, NumberInput } from './FormComponents'
import React from 'react'
import { useMMSettingsSetError, useMMSettingsSetLoading } from './MMSettings'
import {
  prep,
  ID_MM_MIN_TRANSFER,
  ID_MM_MIN_TRANSFER_TOOLTIP,
  ID_MM_FAILED_FETCH_BRIDGE_FEES,
  ID_MM_BRIDGE_CONFIGURATION,
  ID_MM_BRIDGE_CONFIG_TOOLTIP,
  ID_MM_BRIDGE_TO_ASSET,
  ID_MM_SELECT_CEX_ASSET,
  ID_MM_BRIDGE,
  ID_MM_SELECT_BRIDGE,
  ID_MM_BRIDGE_FEES,
  ID_MM_WITHDRAWAL,
  ID_MM_DEPOSIT,
  ID_MM_REBALANCE_METHOD,
  ID_MM_CEX_REBALANCE,
  ID_MM_CEX_REBALANCE_DESC,
  ID_MM_INTERNAL_TRANSFERS_ONLY,
  ID_MM_INTERNAL_TRANSFERS_DESC,
  ID_MM_REBALANCE_SETTINGS,
  ID_MM_REBALANCE_DESCRIPTION
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
  const tooltip = prep(ID_MM_MIN_TRANSFER_TOOLTIP, { asset: assetSymbol })
  const value = asset === 'base' ? botConfig.autoRebalance?.minBaseTransfer || 0 : botConfig.autoRebalance?.minQuoteTransfer || 0

  // Use min withdrawal from CEX
  const min = asset === 'base' ? botConfigState.baseMinWithdraw : botConfigState.quoteMinWithdraw

  // Get asset ID and calculate precision from conversion factor
  const assetID = asset === 'base' ? dexMarket.baseID : dexMarket.quoteID
  const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
  const precision = Math.round(Math.log10(conversionFactor))

  // Calculate max as total allocated amount on DEX + CEX
  const cexAssetID = asset === 'base' ? botConfig.cexBaseID : botConfig.cexQuoteID
  const alloc = botConfig.alloc || { dex: {}, cex: {} }
  const dexAmount = alloc.dex[assetID] || 0
  const cexAmount = alloc.cex[cexAssetID] || 0
  const max = Math.max(dexAmount + cexAmount, min)

  const onChange = (value: number) => {
    const actionType = asset === 'base' ? 'BASE_MIN_TRANSFER' : 'QUOTE_MIN_TRANSFER'
    dispatch({
      type: 'UPDATE_REBALANCE_SETTINGS',
      payload: { type: actionType, payload: value }
    })
  }

  return (
      <div className="col-md-6">
        <div className="d-flex align-items-center justify-content-between mb-2">
          <div className="d-flex align-items-center fs16">
            <img className="mini-icon me-1" src={Doc.logoPath(assetInfo.symbol)} alt={assetSymbol} />
            <span className="me-1">{assetSymbol}</span>
            <span>{prep(ID_MM_MIN_TRANSFER)}</span>
          </div>
          <Tooltip content={tooltip}>
            <span className="ico-info fs12"></span>
          </Tooltip>
        </div>
        <NumberInput
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

  interface BridgeFeesDisplayProps {
    fees: Record<string, number>
    title: string
    titleColor: string
  }

const BridgeFeesDisplay: React.FC<BridgeFeesDisplayProps> = ({ fees, title, titleColor }) => {
  return (
      <div className="col-12">
        <h6 className={titleColor}>{title}</h6>
        <div className="d-flex flex-wrap gap-2">
          {Object.entries(fees).map(([assetID, fee]) => {
            const asset = app().assets[parseInt(assetID)]
            return (
              <div key={assetID} className="d-flex align-items-center gap-1">
                <span className="me-1">{fee ? Doc.formatCoinValue(fee, asset?.unitInfo) : '0'}</span>
                <span>{asset?.name}</span>
                {asset && <img className="mini-icon" src={Doc.logoPath(asset.symbol)} alt={asset.symbol} />}
              </div>
            )
          })}
        </div>
      </div>
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
  const bridges = asset === 'base' ? botConfigState.baseBridges : botConfigState.quoteBridges
  const currentFeesAndLimits = asset === 'base' ? botConfigState.baseBridgeFeesAndLimits : botConfigState.quoteBridgeFeesAndLimits
  const dexAssetID = asset === 'base' ? dexMarket.baseID : dexMarket.quoteID

  // Only show bridge controls if CEX rebalance is selected
  if (!botConfig.autoRebalance || botConfig.autoRebalance.internalOnly || !bridges || !currentFeesAndLimits) return null

  const handleCexAssetChange = async (cexAssetID: number) => {
    if (!bridges || !bridges[cexAssetID] || bridges[cexAssetID].length === 0) return

    const bridgeName = bridges[cexAssetID][0] // Default to first bridge
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
  const availableCexAssets = Object.keys(bridges).map(id => parseInt(id))
  const currentCexAsset = currentFeesAndLimits.cexAsset
  const availableBridges = bridges[currentCexAsset]
  const currentBridge = currentFeesAndLimits.bridgeName

  return (
      <div className="border-top pt-3 mt-3">
        <div className="fs17 mb-2">
          <span className="ico-bridge me-2"></span>
          {prep(ID_MM_BRIDGE_CONFIGURATION, { asset: assetName })}
          <Tooltip content={prep(ID_MM_BRIDGE_CONFIG_TOOLTIP, { asset: assetName })}>
            <span className="ico-info fs12 ms-2"></span>
          </Tooltip>
        </div>

        <div className="ms-2">
          <div className="mb-2">
            <label className="form-label fs16">{prep(ID_MM_BRIDGE_TO_ASSET)}</label>
            <select
              className="form-select"
              value={currentCexAsset || ''}
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
            <div>
              <label className="form-label fs16">{prep(ID_MM_BRIDGE)}</label>
              <select
                className="form-select"
                value={currentBridge || ''}
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

          {/* Bridge Fees Display */}
          <div className="mt-3 p-3 border rounded">
            <h6 className="mb-3">{prep(ID_MM_BRIDGE_FEES)}</h6>

            <div className="row">
              <BridgeFeesDisplay
                fees={currentFeesAndLimits.withdrawal.fees}
                title={prep(ID_MM_WITHDRAWAL)}
                titleColor="text-primary"
              />

              <div className="col-12">
                <BridgeFeesDisplay
                  fees={currentFeesAndLimits.deposit.fees}
                  title={prep(ID_MM_DEPOSIT)}
                  titleColor="text-success"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
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

  const handleRebalanceTypeChange = (cexRebalance: boolean) => {
    dispatch({
      type: 'UPDATE_REBALANCE_SETTINGS',
      payload: { type: 'CEX_REBALANCE', payload: cexRebalance }
    })
  }

  return (
      <div>
        <RebalanceSettingsPanelHeader />

        <div className="border rounded p-3">
          {/* Rebalance Method Selection */}
          <div className="mb-3">
            <div className="fs16 mb-2">{prep(ID_MM_REBALANCE_METHOD)}</div>

            {/* CEX Rebalance */}
            <div className="form-check mb-2">
              <input
                className="form-check-input"
                type="radio"
                name="rebalanceMethod"
                id="cexRebalance"
                checked={!!botConfig.autoRebalance && !botConfig.autoRebalance.internalOnly}
                onChange={() => handleRebalanceTypeChange(true)}
              />
              <label className="form-check-label fs16" htmlFor="cexRebalance">
                {prep(ID_MM_CEX_REBALANCE)}
              </label>
            </div>
            <div className="fs14 text-muted mb-3 ms-3">
              {prep(ID_MM_CEX_REBALANCE_DESC)}
            </div>

            {/* Internal Transfers Only */}
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

          {/* Minimum Transfer Amounts - Only show if CEX Rebalance is selected */}
          {botConfig.autoRebalance && !botConfig.autoRebalance.internalOnly && (
            <div className="row">
              <MinTransferControl
                asset="base"
              />

              <MinTransferControl
                asset="quote"
              />
            </div>
          )}

          {/* Bridge Configuration */}
          <div className="row">
            {botConfigState.baseBridges && (<div className="col-12">
                  <BridgeControl
                    asset="base"
                  />
              </div>)}
            {botConfigState.quoteBridges && (<div className="col-12">
                  <BridgeControl
                      asset="quote"
                    />
              </div>)}
          </div>
        </div>
      </div>
  )
}

export default RebalanceSettingsTab
