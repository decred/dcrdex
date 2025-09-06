import { useBotConfigState, useBotConfigDispatch, fetchRoundTripFeesAndLimits } from '../utils/BotConfig'
import { app } from '../../registry'
import Tooltip from './Tooltip'
import Doc from '../../doc'
import { PanelHeader, NumberInput } from './FormComponents'
import React from 'react'
import { useMMSettingsSetError, useMMSettingsSetLoading } from './MMSettings'

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
    const label = `Min ${assetSymbol} Transfer`
    const tooltip = `Minimum ${assetSymbol} asset amount for transfers`
    const value = asset === 'base' ? botConfig.uiConfig.baseMinTransfer : botConfig.uiConfig.quoteMinTransfer
  
    // Use min withdrawal from CEX
    const min = asset === 'base' ? botConfigState.baseMinWithdraw : botConfigState.quoteMinWithdraw
  
    // Get asset ID and calculate precision from conversion factor
    const assetID = asset === 'base' ? dexMarket.baseID : dexMarket.quoteID
    const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
    const precision = Math.log10(conversionFactor)
  
    // Calculate max as total allocated amount on DEX + CEX
    const cexAssetID = asset === 'base' ? botConfig.cexBaseID : botConfig.cexQuoteID
    const dexAmount = botConfig.uiConfig.allocation.dex[assetID] || 0
    const cexAmount = botConfig.uiConfig.allocation.cex[cexAssetID] || 0
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
          <span className="fs16">{label}</span>
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
    if (!botConfig.uiConfig.cexRebalance || !bridges || !currentFeesAndLimits) return null
  
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
          message: 'Failed to fetch bridge fees and limits'
        })
      } finally {
        setIsLoading(false)
      }
    }
  
    const handleBridgeChange = async (bridgeName: string) => {
      try {
        const feesAndLimits = await fetchRoundTripFeesAndLimits(dexAssetID, currentFeesAndLimits.cexAsset, bridgeName)
        dispatch({
          type: 'UPDATE_BRIDGE_SELECTION',
          payload: { asset, feesAndLimits }
        })
      } catch (error) {
        console.error('Failed to fetch bridge fees and limits:', error)
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
          {assetName} Bridge Configuration
          <Tooltip content={`The ${assetName} asset cannot be directly transferred between Bison Wallet and the CEX. It must be bridged before deposits and after withdrawals.`}>
            <span className="ico-info fs12 ms-2"></span>
          </Tooltip>
        </div>
  
        <div className="ms-2">
          <div className="mb-2">
            <label className="form-label fs16">Bridge to Asset:</label>
            <select
              className="form-select"
              value={currentCexAsset || ''}
              onChange={(e) => handleCexAssetChange(parseInt(e.target.value))}
            >
              <option value="">Select CEX Asset</option>
              {availableCexAssets.map(cexAssetID => (
                <option key={cexAssetID} value={cexAssetID}>
                  {app().prettyPrintAssetID(cexAssetID)}
                </option>
              ))}
            </select>
          </div>
  
          {availableBridges.length > 0 && (
            <div>
              <label className="form-label fs16">Bridge:</label>
              <select
                className="form-select"
                value={currentBridge || ''}
                onChange={(e) => handleBridgeChange(e.target.value)}
              >
                <option value="">Select Bridge</option>
                {availableBridges.map(bridgeName => (
                  <option key={bridgeName} value={bridgeName}>
                    {capitalize(bridgeName)}
                  </option>
                ))}
              </select>
            </div>
          )}
  
          {/* Bridge Fees Display */}
          <div className="mt-3 p-3 bg-light rounded">
            <h6 className="mb-3">Bridge Fees</h6>

            <div className="row">
              <BridgeFeesDisplay
                fees={currentFeesAndLimits.withdrawal.fees}
                title="Withdrawal"
                titleColor="text-primary"
              />

              <div className="col-12 mt-3">
                <BridgeFeesDisplay
                  fees={currentFeesAndLimits.deposit.fees}
                  title="Deposit"
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
        title="Rebalance Settings"
        description={"Configure settings related to rebalancing between Bison Wallet and a CEX. " +
            "If all of the required placements cannot be made, the bot will automatically transfer funds" + 
            " in order to be able to make the maximum amount of placements."}
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
            <div className="fs16 mb-2">Rebalance Method</div>
  
            {/* CEX Rebalance */}
            <div className="form-check mb-2">
              <input
                className="form-check-input"
                type="radio"
                name="rebalanceMethod"
                id="cexRebalance"
                checked={botConfig.uiConfig.cexRebalance && !botConfig.uiConfig.internalTransfers}
                onChange={() => handleRebalanceTypeChange(true)}
              />
              <label className="form-check-label fs16" htmlFor="cexRebalance">
                CEX Rebalance
              </label>
            </div>
            <div className="fs14 text-muted mb-3 ms-3">
              Automatically rebalance funds between DEX and CEX
            </div>
  
            {/* Internal Transfers Only */}
            <div className="form-check">
              <input
                className="form-check-input"
                type="radio"
                name="rebalanceMethod"
                id="internalTransfers"
                checked={botConfig.uiConfig.internalTransfers && !botConfig.uiConfig.cexRebalance}
                onChange={() => handleRebalanceTypeChange(false)}
              />
              <label className="form-check-label fs16" htmlFor="internalTransfers">
                Internal Transfers Only
              </label>
            </div>
            <div className="fs14 text-muted ms-3">
              Only use internal wallet transfers for rebalancing
            </div>
          </div>
  
          {/* Minimum Transfer Amounts - Only show if CEX Rebalance is selected */}
          {botConfig.uiConfig.cexRebalance && (
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
            <div className="col-12">
              {botConfigState.baseBridges && (
                <BridgeControl
                  asset="base"
                />
              )}
            </div>
  
            <div className="col-12">
              {botConfigState.quoteBridges && (
                <BridgeControl
                    asset="quote"
                  />
                )}
            </div>
          </div>
        </div>
      </div>
    )
  }

export default RebalanceSettingsTab;