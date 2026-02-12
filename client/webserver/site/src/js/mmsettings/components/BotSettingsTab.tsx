import React from 'react'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import Tooltip from './Tooltip'
import { app, OrderOption } from '../../registry'
import Doc from '../../doc'
import { PanelHeader, NumberInput } from './FormComponents'

export const SettingsPanelHeader: React.FC = () => {
  return (
    <PanelHeader
      title="Settings"
      description="Configure advanced bot settings including wallet options, rebalancing parameters, and trading preferences"
    />
  )
}

const MultiHopSettings: React.FC = () => {
  const botConfigState = useBotConfigState()
  const dispatch = useBotConfigDispatch()

  const { intermediateAssets, intermediateAsset, botConfig } = botConfigState
  if (!intermediateAssets || intermediateAssets.length === 0) {
    return null
  }

  // Get the current multi-hop config
  const multiHopConfig = botConfig.arbMarketMakingConfig?.multiHop
  const marketOrders = multiHopConfig?.marketOrders ?? false
  const limitOrdersBuffer = multiHopConfig?.limitOrdersBuffer ?? 0.01

  const handleIntermediateAssetChange = (assetID: number) => {
    dispatch({
      type: 'UPDATE_INTERMEDIATE_ASSET',
      payload: assetID
    })
  }

  const handleOrderTypeChange = (isMarketOrder: boolean) => {
    dispatch({
      type: 'UPDATE_MULTI_HOP_MARKET_COMPLETION',
      payload: isMarketOrder
    })
  }

  const handleLimitBufferChange = (value: number) => {
    dispatch({
      type: 'UPDATE_MULTI_HOP_LIMIT_BUFFER',
      payload: value / 100 // Convert from percentage to decimal
    })
  }

  return (
    <div>
      {/* Intermediate Asset */}
      <div className="d-flex align-items-center justify-content-between mb-2">
        <span className="fs16">Intermediate Asset</span>
        <Tooltip content="Asset to use for multi-hop arbitrage">
          <span className="ico-info fs12"></span>
        </Tooltip>
      </div>
      <div className="mb-3">
        <select
          className="form-select"
          value={intermediateAsset || ''}
          onChange={(e) => handleIntermediateAssetChange(parseInt(e.target.value) || 0)}
        >
          {intermediateAssets.map(assetID => (
            <option key={assetID} value={assetID}>
              {Doc.shortSymbol(app().assets[assetID]?.symbol) || `Asset ${assetID}`}
            </option>
          ))}
        </select>
      </div>

      {/* Completion Radio Buttons */}
      <div className="mb-3">
        <div className="d-flex align-items-center justify-content-between mb-2">
          <span className="fs16">Completion Order Type</span>
          <Tooltip content="This specifies the type of order to execute on the second leg of a multi-hop arb. Market orders will always be filled, ensuring that the bot never has any funds stuck in the intermediate asset, but may result in losses if the price suddenly moves against the bot.">
            <span className="ico-info fs12"></span>
          </Tooltip>
        </div>

        <div className="d-flex gap-3">
          <div className="form-check pe-2">
            <input
              className="form-check-input"
              type="radio"
              name="orderType"
              id="marketOrder"
              checked={marketOrders}
              onChange={() => handleOrderTypeChange(true)}
            />
            <label className="form-check-label" htmlFor="marketOrder">
              Market Order
            </label>
          </div>

          <div className="form-check">
            <input
              className="form-check-input"
              type="radio"
              name="orderType"
              id="limitOrder"
              checked={!marketOrders}
              onChange={() => handleOrderTypeChange(false)}
            />
            <label className="form-check-label" htmlFor="limitOrder">
              Limit Order
            </label>
          </div>
        </div>
      </div>

      {/* Limit Orders Buffer Slider - Only show when limit orders are selected */}
      {!marketOrders && (
        <div className="mb-3">
          <div className="d-flex align-items-center justify-content-between mb-2">
            <span className="fs16">Limit Buffer</span>
            <Tooltip content="This specifies the buffer to apply to the limit order rate for the second leg of a multi-hop arb. The buffer will make the rate 'worse' (lower for sell orders, higher for buy orders) resulting in a higher probability of the trade being filled in order to avoid having funds stuck in the intermediate asset.">
              <span className="ico-info fs12"></span>
            </Tooltip>
          </div>

          <div className="d-flex align-items-center">
            <div className="flex-grow-1 me-2">
              <NumberInput
                min={0.1}
                max={2.0}
                precision={2}
                value={limitOrdersBuffer * 100} // Convert to percentage for display
                onChange={handleLimitBufferChange}
                withSlider={true}
              />
            </div>
            <span className="fs14 grey">%</span>
          </div>
        </div>
      )}
    </div>
  )
}

interface IndividualWalletSettingsProps {
  asset: 'base' | 'quote'
  options: OrderOption[] | null
  optionsState: Record<string, string> | null
  onSettingChange: (asset: 'base' | 'quote', key: string, value: string) => void
}

const IndividualWalletSettings: React.FC<IndividualWalletSettingsProps> = ({
  asset,
  options,
  optionsState,
  onSettingChange
}) => {
  if (!options || options.length === 0) {
    return (
      <div className="text-center text-muted p-3">
        No settings available for {asset} wallet
      </div>
    )
  }

  return (
    <div className="d-flex flex-column gap-2">
      {options.map((opt: OrderOption) => {
        // Skip options that are quote-only for base asset
        if (opt.quoteAssetOnly && asset === 'base') return null

        if (opt.dependsOn && ((optionsState?.[opt.dependsOn] || 'false') !== 'true')) return null

        const currentValue = optionsState?.[opt.key] || opt.default?.toString() || ''

        if (opt.isboolean) {
          return (
            <div key={opt.key} className="d-flex align-items-center pt-2 mt-2 border-top">
              <div className="form-check">
                <input
                  className="form-check-input me-2"
                  type="checkbox"
                  id={`${asset}-${opt.key}`}
                  checked={currentValue === 'true'}
                  onChange={(e) => onSettingChange(asset, opt.key, e.target.checked ? 'true' : 'false')}
                />
                <label className="form-check-label" htmlFor={`${asset}-${opt.key}`}>
                  {opt.displayname}
                </label>
                {opt.description && (
                  <Tooltip content={opt.description}>
                    <span className="ico-info fs12 ms-1"></span>
                  </Tooltip>
                )}
              </div>
            </div>
          )
        }

        if (opt.xyRange) {
          const { start, end, xUnit } = opt.xyRange
          const numericValue = parseFloat(currentValue) || start.x

          return (
            <div key={opt.key} className="pt-2 mt-2 border-top">
              <div className="d-flex align-items-center justify-content-between">
                <span>{opt.displayname}</span>
                {opt.description && (
                  <Tooltip content={opt.description}>
                    <span className="ico-info fs12"></span>
                  </Tooltip>
                )}
              </div>
              <div className="d-flex align-items-center">
                <div className="flex-grow-1 me-2">
                  <NumberInput
                    min={start.x}
                    max={end.x}
                    precision={0}
                    value={numericValue}
                    onChange={(value) => onSettingChange(asset, opt.key, value.toString())}
                    withSlider={true}
                  />
                </div>
                {xUnit && <span className="fs14 grey ms-1">{xUnit}</span>}
              </div>
            </div>
          )
        }

        return null
      })}
    </div>
  )
}

const WalletSettings: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { botConfig, dexMarket, baseMultiFundingOpts, quoteMultiFundingOpts } = botConfigState
  const dispatch = useBotConfigDispatch()

  const handleWalletSettingChange = (asset: 'base' | 'quote', key: string, value: string) => {
    dispatch({
      type: 'UPDATE_WALLET_SETTING',
      payload: { asset, key, value }
    })
  }

  const baseAsset = app().assets[dexMarket.baseID]
  const quoteAsset = app().assets[dexMarket.quoteID]

  return (
    <div className="mb-4">
      <div className="d-flex align-items-center mb-3">
        <span className="ico-wallet fs20 pe-2"></span>
        <span className="fs22">Wallet Settings</span>
      </div>

      <div className="row">
        {/* Base Wallet Settings */}
        <div className="col-12 mb-3">
          <div className="border rounded p-3">
            <div className="d-flex align-items-center mb-3">
              <img
                className="mini-icon me-2"
                src={Doc.logoPath(baseAsset.symbol)}
                alt={baseAsset.symbol}
              />
              <span className="fs18">Base Wallet ({baseAsset.unitInfo.conventional.unit})</span>
            </div>

            <IndividualWalletSettings
              asset="base"
              options={baseMultiFundingOpts}
              optionsState={botConfig.baseWalletOptions ?? null}
              onSettingChange={handleWalletSettingChange}
            />
          </div>
        </div>

        {/* Quote Wallet Settings */}
        <div className="col-12 mb-3">
          <div className="border rounded p-3">
            <div className="d-flex align-items-center mb-3">
              <img
                className="mini-icon me-2"
                src={Doc.logoPath(quoteAsset.symbol)}
                alt={quoteAsset.symbol}
              />
              <span className="fs18">Quote Wallet ({quoteAsset.unitInfo.conventional.unit})</span>
            </div>

            <IndividualWalletSettings
              asset="quote"
              options={quoteMultiFundingOpts}
              optionsState={botConfig.quoteWalletOptions ?? null}
              onSettingChange={handleWalletSettingChange}
            />
          </div>
        </div>
      </div>
    </div>
  )
}

const Knobs: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { botConfig } = botConfigState
  const dispatch = useBotConfigDispatch()

  const driftTolerance = botConfig.basicMarketMakingConfig?.driftTolerance ??
                         botConfig.arbMarketMakingConfig?.driftTolerance ?? 0.001

  const orderPersistence = botConfig.arbMarketMakingConfig?.orderPersistence ??
    botConfig.simpleArbConfig?.numEpochsLeaveOpen ?? 2

  return (
    <div className="mb-4">
      <div className="d-flex align-items-center mb-3">
        <span className="ico-lever fs20 pe-2"></span>
        <span className="fs22">Knobs</span>
      </div>

      <div className="border rounded p-3">
        {/* Drift Tolerance (ArbMM or BasicMM) */}
        {(botConfig.arbMarketMakingConfig || botConfig.basicMarketMakingConfig) && (
          <div className="mb-3">
            <div className="d-flex align-items-center justify-content-between mb-2">
              <span className="fs16">Drift Tolerance</span>
              <Tooltip content="Maximum allowed price deviation before repositioning orders">
                <span className="ico-info fs12"></span>
              </Tooltip>
            </div>
            <div className="d-flex align-items-center">
              <div className="flex-grow-1 me-2">
                <NumberInput
                  min={0.01}
                  max={1}
                  precision={2}
                  value={driftTolerance * 100}
                  onChange={(value) => dispatch({
                    type: 'UPDATE_DRIFT_TOLERANCE',
                    payload: value / 100
                  })}
                  withSlider={true}
                />
              </div>
              <span className="fs14 grey">%</span>
            </div>
          </div>
        )}

        {/* Order Persistence (ArbMM or BasicArb) */}
        {(botConfig.arbMarketMakingConfig || botConfig.simpleArbConfig) && (
          <div className="mb-3">
            <div className="d-flex align-items-center justify-content-between mb-2">
              <span className="fs16">Order Persistence</span>
              <Tooltip content="Number of epochs to keep unfilled orders active">
                <span className="ico-info fs12"></span>
              </Tooltip>
            </div>
            <div className="d-flex align-items-center">
              <div className="flex-grow-1 me-2">
                <NumberInput
                  min={2}
                  max={40}
                  precision={0}
                  value={orderPersistence}
                  onChange={(value) => dispatch({
                    type: 'UPDATE_ORDER_PERSISTENCE',
                    payload: value
                  })}
                  withSlider={true}
                />
              </div>
              <span className="fs14 grey">epochs</span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

const MultiHopSection: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { intermediateAssets } = botConfigState
  if (!intermediateAssets || intermediateAssets.length === 0) {
    return null
  }

  return (
    <div className="mb-4">
      <div className="d-flex align-items-center mb-3">
        <span className="ico-shuffle fs20 pe-2"></span>
        <span className="fs22">Multi-Hop Arbitrage</span>
      </div>

      <div className="border rounded p-3">
        <MultiHopSettings />
      </div>
    </div>
  )
}

const BotSettingsTab: React.FC = () => {
  return (
    <div>
      <SettingsPanelHeader />

      <div className="row">
        <div className="col-12">
          <WalletSettings />
        </div>
        <div className="col-12">
          <Knobs />
        </div>
        <div className="col-12">
          <MultiHopSection />
        </div>
      </div>
    </div>
  )
}

export default BotSettingsTab
