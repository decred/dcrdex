import React, { useReducer, useState, createContext, useContext } from 'react'
import { MMCEXStatus, app } from '../../registry'
import { BotSpecs, specLK } from '../../mmsettings'
import Doc from '../../doc'
import MarketSelector from './MarketSelector'
import BotTypeSelector from './BotTypeSelector'
import ConfigureBot from './ConfigureBot'
import ErrorPopup from './ErrorPopup'
import { LoadingSpinner } from './FormComponents'
import { botConfigStateReducer, initialBotConfigState, BotConfigStateContext, BotConfigDispatchContext, BotConfigState } from '../utils/BotConfig'
import State from '../../state'
import { requiredDexAssets } from '../utils/AllocationUtil'

export interface AvailableMarket {
  host: string
  name: string
  baseID: number
  quoteID: number
  baseSymbol: string
  quoteSymbol: string
  hasArb: boolean
  arbs: string[]
  spot?: any
}

export interface AvailableMarkets {
  markets: AvailableMarket[]
  exchangesRequiringRegistration: string[]
}

// Helper function to render symbols with proper capitalization and token parent logos
export const renderSymbol = (assetID: number, symbol: string): JSX.Element => {
  const asset = app().assets[assetID]
  if (!asset) {
    return <span>{symbol.toUpperCase()}</span>
  }

  const parts = symbol.split('.')
  const isToken = parts.length === 2

  if (!isToken) {
    return <span>{symbol.toUpperCase()}</span>
  }

  const tokenSymbol = parts[0]
  const parentSymbol = parts[1]

  return (
    <span className="d-inline-flex align-items-center">
      <span>{tokenSymbol.toUpperCase()}</span>
      <img
        src={Doc.logoPath(parentSymbol)}
        className="token-parent ms-1"
        alt={`${parentSymbol} network`}
      />
    </span>
  )
}

// cexSupportsArbOnMarket checks whether the CEX supports arbitrage market
// making on the given market. It returns a tuple of:
//
// - whether the CEX supports direct arbitrage on the market
// - the intermediate assets that can be used for multi-hop arbitrage
// - the CEX assetIDs that the base asset can be bridged to
// - the CEX assetIDs that the quote asset can be bridged to
//
// If the CEX does not support direct arb and there are no intermediate assets,
// the CEX does not support arbitrage market making on the market.
// The bridge destination assets will be empty if the CEX supports the same
// asset that is used on the DEX market.
export const cexSupportsArbOnMarket = (
  baseID: number,
  quoteID: number,
  cexStatus: MMCEXStatus,
  bridgePaths: Record<number, Record<number, string[]>>
): [boolean, number[] | null, Record<number, string[]> | null, Record<number, string[]> | null] => {
  const supportedBridgePath = (dexAssetID: number, cexAssetID: number) => {
    if (!bridgePaths[dexAssetID]) return false
    const dests = bridgePaths[dexAssetID]
    return dests[cexAssetID] !== undefined
  }

  const getBridgeNames = (dexAssetID: number, cexAssetID: number): string[] => {
    if (!bridgePaths[dexAssetID]) return []
    const dests = bridgePaths[dexAssetID]
    return dests[cexAssetID] || []
  }

  const supportedMarkets = (dexBaseID: number, dexQuoteID: number, cexBaseID: number, cexQuoteID: number) => {
    if (dexBaseID !== cexBaseID) {
      if (!supportedBridgePath(dexBaseID, cexBaseID)) return false
    }
    if (dexQuoteID !== cexQuoteID) {
      if (!supportedBridgePath(dexQuoteID, cexQuoteID)) return false
    }
    return true
  }

  // baseBridges and quoteBridges are all the assets that the base and quote
  // asset can be bridged to that are supported by the CEX, mapping to available bridge names.
  // If the CEX supports the base or quote assets directly, the bridge map will be empty.
  let baseBridges: Record<number, string[]> | null = {}
  let quoteBridges: Record<number, string[]> | null = {}
  for (const { baseID: cexBaseID, quoteID: cexQuoteID } of Object.values(cexStatus.markets ?? [])) {
    if (cexBaseID === baseID) {
      baseBridges = null
    }
    if (cexQuoteID === quoteID) {
      quoteBridges = null
    }
    if (baseBridges && supportedBridgePath(baseID, cexBaseID)) {
      baseBridges[cexBaseID] = getBridgeNames(baseID, cexBaseID)
      continue
    }
    if (baseBridges && supportedBridgePath(baseID, cexQuoteID)) {
      baseBridges[cexQuoteID] = getBridgeNames(baseID, cexQuoteID)
      continue
    }
    if (quoteBridges && supportedBridgePath(quoteID, cexQuoteID)) {
      quoteBridges[cexQuoteID] = getBridgeNames(quoteID, cexQuoteID)
      continue
    }
    if (quoteBridges && supportedBridgePath(quoteID, cexBaseID)) {
      quoteBridges[cexBaseID] = getBridgeNames(quoteID, cexBaseID)
      continue
    }
  }

  // Find all markets that trade either base or quote assets trade on. If there
  // is an exact match, we can return early.
  const baseMarkets = new Set<number>()
  const quoteMarkets = new Set<number>()
  for (const { baseID: cexBaseID, quoteID: cexQuoteID } of Object.values(cexStatus.markets ?? [])) {
    if (supportedMarkets(cexBaseID, cexQuoteID, baseID, quoteID)) {
      return [true, null, baseBridges, quoteBridges]
    }

    if (cexBaseID === baseID || (baseBridges && baseBridges[cexBaseID])) baseMarkets.add(cexQuoteID)
    if (cexQuoteID === baseID || (baseBridges && baseBridges[cexQuoteID])) baseMarkets.add(cexBaseID)
    if (cexBaseID === quoteID || (quoteBridges && quoteBridges[cexBaseID])) quoteMarkets.add(cexQuoteID)
    if (cexQuoteID === quoteID || (quoteBridges && quoteBridges[cexQuoteID])) quoteMarkets.add(cexBaseID)
  }

  // If there was no exact match, find all the intermediate assets that can
  // be used for a multi-hop arb.
  const intermediateAssets: Record<number, boolean> = {}
  for (const intermediateAsset of baseMarkets) {
    if (quoteMarkets.has(intermediateAsset)) {
      intermediateAssets[intermediateAsset] = true
    }
  }

  return [false, Object.keys(intermediateAssets).map(Number), baseBridges, quoteBridges]
}

// Function to check if a specific CEX supports arbitrage on a market
const createCexMarketSupportChecker = (
  bridgePaths: Record<number, Record<number, string[]>>,
  cexes: Record<string, MMCEXStatus>
) => {
  return (baseID: number, quoteID: number, cexName: string): boolean => {
    const cexStatus = cexes[cexName]
    if (!cexStatus) return false

    const [supportsDirectArb, intermediateAssets] = cexSupportsArbOnMarket(
      baseID,
      quoteID,
      cexStatus,
      bridgePaths
    )

    return supportsDirectArb || (!!intermediateAssets && intermediateAssets.length > 0)
  }
}

export const MMSettingsSetErrorContext = createContext<React.Dispatch<MMSettingsError | null> | undefined>(undefined)

export const MMSettingsSetLoadingContext = createContext<React.Dispatch<boolean> | undefined>(undefined)

export const useMMSettingsSetError = () => {
  const context = useContext(MMSettingsSetErrorContext)
  if (context === undefined) {
    throw new Error('useMMSettingsSetError must be used within a MMSettingsSetErrorProvider')
  }
  return context
}

export const useMMSettingsSetLoading = () => {
  const context = useContext(MMSettingsSetLoadingContext)
  if (context === undefined) {
    throw new Error('useMMSettingsSetLoading must be used within a MMSettingsSetLoadingProvider')
  }
  return context
}

export interface MMSettingsError {
  message: string
  onClose?: () => void
}

const checkFiatRates = (botConfigState: BotConfigState): boolean => {
  const assetIDs = requiredDexAssets(botConfigState)
  console.log(assetIDs);
  return true
}

function initialErrorState(botConfigState: BotConfigState | string | undefined, returnToMM?: boolean): [BotConfigState | null, MMSettingsError | null] {
  if (!botConfigState) {
    return [null, null]
  }

  if (typeof botConfigState === 'string') {
    return [null, {
      message: botConfigState,
      onClose: () => {
        if (returnToMM) app().loadPage('mm')
      }
    }]
  }

  if (!checkFiatRates(botConfigState)) {
    return [null, {
      message: 'Fiat rates are not available for the selected market',
      onClose: () => {
        if (returnToMM) app().loadPage('mm')
      }
    }]
  }

  return [botConfigState, null]
}

interface MMSettingsProps {
  availableMarkets?: AvailableMarkets
  cexes?: Record<string, MMCEXStatus>
  bridgePaths?: Record<number, Record<number, string[]>>
  initialSpecs?: BotSpecs
  // botConfigStateOnLoad may be a string, which means an error should be displayed.
  botConfigStateOnLoad?: BotConfigState | string
}

const MMSettings: React.FC<MMSettingsProps> = ({
  availableMarkets = { markets: [], exchangesRequiringRegistration: [] },
  cexes = {},
  bridgePaths = {},
  botConfigStateOnLoad = undefined
}) => {
  const [initialState, initialError] = initialErrorState(botConfigStateOnLoad)
  const [error, setError] = useState<MMSettingsError | null>(initialError)
  const [botConfigState, dispatch] = useReducer(botConfigStateReducer, initialState)
  const [isLoading, setIsLoading] = useState<boolean>(false)
  const [updatingMarketOrType, setUpdatingMarketOrType] = useState<boolean>(false)
  const [selectedMarket, setSelectedMarket] = useState<{
    host: string;
    baseID: number;
    quoteID: number;
  } | null>(null)

  // Create the CEX market support checker function
  const checkCexMarketSupport = createCexMarketSupportChecker(bridgePaths, cexes)

  const handleBotTypeSelected = async (botType: 'basicMM' | 'arbMM' | 'basicArb', cexName?: string) => {
    if (!selectedMarket) {
      console.error('No market selected')
      return
    }

    // Check if the market and bot type are the same as currently selected
    if (initialState) {
      const currentConfig = initialState.botConfig
      const marketMatches = (
        currentConfig.host === selectedMarket.host &&
        currentConfig.baseID === selectedMarket.baseID &&
        currentConfig.quoteID === selectedMarket.quoteID
      )

      // Determine current bot type from config
      let currentBotType: 'basicMM' | 'arbMM' | 'basicArb'
      if (currentConfig.basicMarketMakingConfig) {
        currentBotType = 'basicMM'
      } else if (currentConfig.arbMarketMakingConfig) {
        currentBotType = 'arbMM'
      } else if (currentConfig.simpleArbConfig) {
        currentBotType = 'basicArb'
      } else {
        throw new Error('Invalid bot type in current config')
      }

      const botTypeMatches = currentBotType === botType
      const cexMatches = currentConfig.cexName === (cexName || '')

      // If everything matches, just set updatingMarketOrType to false
      if (marketMatches && botTypeMatches && cexMatches) {
        setUpdatingMarketOrType(false)
        return
      }
    }

    let baseBridges: Record<number, string[]> | null = null
    let quoteBridges: Record<number, string[]> | null = null
    let intermediateAssets: number[] | null = null
    let cexStatus: MMCEXStatus | null = null

    if (cexName) {
      cexStatus = cexes[cexName] ?? null;
      [, intermediateAssets, baseBridges, quoteBridges] = cexSupportsArbOnMarket(
        selectedMarket.baseID,
        selectedMarket.quoteID,
        cexes[cexName],
        bridgePaths
      )
    }

    // Create the default BotConfig based on the selected market and bot type
    const newBotConfigState = await initialBotConfigState(
      selectedMarket.host,
      selectedMarket.baseID,
      selectedMarket.quoteID,
      botType,
      intermediateAssets,
      baseBridges,
      quoteBridges,
      cexStatus,
      cexName
    )

    const [botConfigState, errorState] = initialErrorState(newBotConfigState)
    if (errorState != null) {
      setError(errorState)
      return 
    }


    const botSpecs : BotSpecs = {
      host: selectedMarket.host,
      baseID: selectedMarket.baseID,
      quoteID: selectedMarket.quoteID,
      botType: botType,
      cexName: cexName
    }
  
    State.storeLocal(specLK, botSpecs)

    dispatch({ type: 'SET_INITIAL_CONFIG', payload: botConfigState })
    setUpdatingMarketOrType(false)
  }

  const handleChangeMarket = () => {
    setSelectedMarket(null)
    setUpdatingMarketOrType(true)
  }

  const handleChangeBotType = () => {
    setUpdatingMarketOrType(true)
  }

  let mainComponent

  if (botConfigState && !updatingMarketOrType) {
    mainComponent = (
      <BotConfigStateContext.Provider value={botConfigState}>
        <BotConfigDispatchContext.Provider value={dispatch}>
            <ConfigureBot
              onChangeMarket={handleChangeMarket}
              onChangeBotType={handleChangeBotType}
            />
        </BotConfigDispatchContext.Provider>
      </BotConfigStateContext.Provider>
    )
  } else if (selectedMarket) {
    mainComponent = (
      <BotTypeSelector
        selectedMarket={selectedMarket}
        cexes={cexes}
        checkCexMarketSupport={checkCexMarketSupport}
        onClose={() => {
          if (updatingMarketOrType) {
            setUpdatingMarketOrType(false)
          } else {
            setSelectedMarket(null)
          }
        }}
        onBotTypeSelected={handleBotTypeSelected}
        onChangeMarket={handleChangeMarket}
      />
    )
  } else {
    mainComponent = (
      <MarketSelector
        markets={availableMarkets.markets}
        exchangesRequiringRegistration={availableMarkets.exchangesRequiringRegistration}
        cexes={cexes}
        onClose={()=> {
          if (botConfigState) {
            setSelectedMarket({
              host: botConfigState.botConfig.host,
              baseID: botConfigState.botConfig.baseID,
              quoteID: botConfigState.botConfig.quoteID,
            })
            setUpdatingMarketOrType(false)
          } else {
            app().loadPage('mm')
          }
        }}
        checkCexMarketSupport={checkCexMarketSupport}
        handleMarketSelected={(host: string, baseID: number, quoteID: number) => {
          setSelectedMarket({ host, baseID, quoteID })}}
      />
    )
  }

  return (
    <div>
      <MMSettingsSetErrorContext.Provider value={setError}>
      <MMSettingsSetLoadingContext.Provider value={setIsLoading}>
      { mainComponent }
      <ErrorPopup error={error} />
      <LoadingSpinner isLoading={isLoading} />
      </MMSettingsSetLoadingContext.Provider>
      </MMSettingsSetErrorContext.Provider>
    </div>
  )
}

export default MMSettings
