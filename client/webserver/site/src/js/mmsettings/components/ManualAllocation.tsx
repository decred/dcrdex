import React from 'react'
import { useBotConfigState, useBotConfigDispatch } from '../utils/BotConfig'
import { app } from '../../registry'
import Doc from '../../doc'
import { requiredDexAssets } from '../utils/AllocationUtil'
import { CEXDisplayInfos } from '../../mmutil'
import { AllocationPanelHeader } from './QuickAllocation'
import { NumberInput } from './FormComponents'

interface ManualBalanceEntryProps {
  assetID: number
  location: 'dex' | 'cex',
  columnClass: string
}

const ManualBalanceEntry: React.FC<ManualBalanceEntryProps> = ({
  assetID,
  location,
  columnClass
}) => {
  const {
    runStats, availableCEXBalances, availableDEXBalances,
    botConfig
  } = useBotConfigState()
  const alloc = botConfig.alloc || { dex: {}, cex: {} }
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
  const currentValue = alloc[location][assetID] || 0

  return (
    <div className={`${columnClass} mb-3`}>
      <label className="small d-flex align-items-center">
        <img
          className="micro-icon me-1"
          src={Doc.logoPath(asset.symbol)}
          alt={asset.symbol}
        />
        <span>{asset.unitInfo.conventional.unit}</span>
      </label>
      <div className="ms-2 d-flex flex-column align-items-stretch w-100">
        <NumberInput
          min={-runningBotAvailable / asset.unitInfo.conventional.conversionFactor}
          max={availableBalance / asset.unitInfo.conventional.conversionFactor}
          precision={Math.round(Math.log10(asset.unitInfo.conventional.conversionFactor))}
          value={currentValue / asset.unitInfo.conventional.conversionFactor}
          onChange={(amount : number) => dispatch({
            type: 'UPDATE_MANUAL_ALLOCATION',
            payload: { assetID, amount: amount * asset.unitInfo.conventional.conversionFactor, source: location }
          })}
          withSlider={true}
        />
      </div>
    </div>
  )
}

const ManualAllocationView: React.FC = () => {
  const botConfigState = useBotConfigState()
  const { botConfig } = botConfigState

  const dexAssetIDs = requiredDexAssets(botConfigState)

  // Calculate column class based on number of items
  const getColumnClass = (itemCount: number) => {
    return `col-${Math.floor(12 / itemCount)}`
  }

  return (
    <div>
      <AllocationPanelHeader description="Manually configure fund allocations" />

      {/* DEX Balances */}
      <div className="d-flex align-items-center mb-3">
        <img className="logo-square mini-icon me-2" src={Doc.logoPath('DEX')} alt="DEX" />
        <div className="d-flex flex-grow-1">
          <div className="row">
            {dexAssetIDs.map(assetID => {
              return (
                <ManualBalanceEntry
                  key={assetID}
                  assetID={assetID}
                  location='dex'
                  columnClass={getColumnClass(dexAssetIDs.length)}
                />
              )
            })}
          </div>
        </div>
      </div>

      {/* CEX Balances - only show if CEX is configured */}
      {botConfig.cexName && CEXDisplayInfos[botConfig.cexName] && (
        <div className="d-flex align-items-center">
          <img className="mini-icon me-2" src={CEXDisplayInfos[botConfig.cexName].logo} alt="CEX" />
          <div className="d-flex flex-grow-1">
            <div className="row">
              {[botConfig.cexBaseID, botConfig.cexQuoteID].map(assetID => {
                return (
                  <ManualBalanceEntry
                    key={assetID}
                    assetID={assetID}
                    location='cex'
                    columnClass={getColumnClass(2)} // CEX always shows 2 assets
                  />
                )
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default ManualAllocationView
