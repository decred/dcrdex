import React from 'react'
import { useBotConfigState, useBotConfigDispatch, editableAmounts } from '../utils/BotConfig'
import { app } from '../../registry'
import Doc from '../../doc'
import { requiredDexAssets } from '../utils/AllocationUtil'
import { CEXDisplayInfos } from '../../mmutil'
import { AllocationModeNote, AllocationPanelHeader } from './QuickAllocation'
import { NumberInput } from './FormComponents'
import {
  prep,
  ID_MM_MANUAL_ALLOC_DESC,
  ID_CEX_BALANCES
} from '../../locales'

interface ManualBalanceEntryProps {
  assetID: number
  location: 'dex' | 'cex'
}

const ManualBalanceEntry: React.FC<ManualBalanceEntryProps> = ({
  assetID,
  location
}) => {
  const botConfigState = useBotConfigState()
  const {
    runStats, availableCEXBalances, availableDEXBalances
  } = botConfigState
  const editableBalanceAmounts = editableAmounts(botConfigState)
  const dispatch = useBotConfigDispatch()

  const asset = app().assets[assetID]
  const availableBalance = location === 'dex'
    ? (availableDEXBalances[assetID] || 0)
    : (availableCEXBalances?.[assetID] || 0)
  let runningBotAvailable = 0
  if (runStats) {
    runningBotAvailable = location === 'dex'
      ? (runStats.dexBalances[assetID]?.available || 0)
      : (runStats.cexBalances[assetID]?.available || 0)
  }
  const currentValue = editableBalanceAmounts[location][assetID] || 0

  return (
    <div className="d-flex align-items-center mb-2 mm-mixer-row">
      <div className="d-flex align-items-center flex-shrink-0 mm-mixer-label">
        <img
          className="micro-icon me-1"
          src={Doc.logoPath(asset.symbol)}
          alt={asset.symbol}
        />
        <span className="fs14">{asset.unitInfo.conventional.unit}</span>
      </div>
      <div className="flex-grow-1">
        <NumberInput
          sliderPosition="inline"
          className="p-1 text-center fs14"
          withSlider={true}
          min={-runningBotAvailable / asset.unitInfo.conventional.conversionFactor}
          max={availableBalance / asset.unitInfo.conventional.conversionFactor}
          precision={Math.round(Math.log10(asset.unitInfo.conventional.conversionFactor))}
          value={currentValue / asset.unitInfo.conventional.conversionFactor}
          onChange={(amount: number) => dispatch({
            type: 'UPDATE_MANUAL_ALLOCATION',
            payload: { assetID, amount: amount * asset.unitInfo.conventional.conversionFactor, source: location }
          })}
        />
      </div>
    </div>
  )
}

const ManualAllocationView: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { botConfig, runStats } = botConfigState

  const dexAssetIDs = requiredDexAssets(botConfigState)

  return (
    <div>
      <AllocationPanelHeader description={prep(ID_MM_MANUAL_ALLOC_DESC)} />
      <AllocationModeNote isRunning={!!runStats} mode="manual" />

      <div className="row">
        {/* DEX Balances */}
        <div className={`col-24 mb-3 ${botConfig.cexName ? 'col-md-12 pe-md-2' : ''}`}>
          <div className="border rounded p-3 h-100">
            <div className="d-flex align-items-center mb-3">
              <img className="logo-square mini-icon me-3" src={Doc.logoPath('DEX')} alt="DEX" />
              <span className="fs16 demi">Bison Wallet Balances</span>
            </div>
            {dexAssetIDs.map(assetID => (
              <ManualBalanceEntry key={assetID} assetID={assetID} location='dex' />
            ))}
          </div>
        </div>

        {/* CEX Balances - only show if CEX is configured */}
        {botConfig.cexName && CEXDisplayInfos[botConfig.cexName] && (
          <div className="col-24 col-md-12 ps-md-2 mb-3">
            <div className="border rounded p-3 h-100">
              <div className="d-flex align-items-center mb-3">
                <img className="mini-icon me-3" src={CEXDisplayInfos[botConfig.cexName].logo} alt="CEX" />
                <span className="fs16 demi">{prep(ID_CEX_BALANCES, { cexName: CEXDisplayInfos[botConfig.cexName].name })}</span>
              </div>
              {[botConfig.cexBaseID, botConfig.cexQuoteID].map(assetID => (
                <ManualBalanceEntry key={assetID} assetID={assetID} location='cex' />
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default ManualAllocationView
