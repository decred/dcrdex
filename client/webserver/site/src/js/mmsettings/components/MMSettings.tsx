import React, { useReducer, useState, createContext, useContext, forwardRef, useImperativeHandle } from 'react'
import { MMCEXStatus, app, BalanceNote, CEXBalanceUpdate, SupportedAsset, ApprovalStatus } from '../../registry'
import { MM } from '../../mmutil'
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
    }
    if (baseBridges && supportedBridgePath(baseID, cexQuoteID)) {
      baseBridges[cexQuoteID] = getBridgeNames(baseID, cexQuoteID)
    }
    if (quoteBridges && supportedBridgePath(quoteID, cexQuoteID)) {
      quoteBridges[cexQuoteID] = getBridgeNames(quoteID, cexQuoteID)
    }
    if (quoteBridges && supportedBridgePath(quoteID, cexBaseID)) {
      quoteBridges[cexBaseID] = getBridgeNames(quoteID, cexBaseID)
    }
  }

  // Find all markets that trade either base or quote assets trade on. If there
  // is an exact match, we can return early.
  const baseMarkets = new Set<number>()
  const quoteMarkets = new Set<number>()
  for (const { baseID: cexBaseID, quoteID: cexQuoteID } of Object.values(cexStatus.markets ?? [])) {
    if (supportedMarkets(baseID, quoteID, cexBaseID, cexQuoteID)) {
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

  // Filter out duplicate intermediate assets. If two intermediate assets
  // share the same symbol, only one of them is required. WETH is also
  // ignored.
  const intermediateAssetSymbols : Set<string> = new Set()
  const filteredIntermediateAssets: number[] = []
  for (const intermediateAsset of Object.keys(intermediateAssets).map(Number)) {
    const asset = app().assets[intermediateAsset]
    if (!asset) {
      continue
    }
    const assetSymbol = asset.symbol.split('.')[0]
    if (assetSymbol === 'weth') {
      continue
    }
    if (intermediateAssetSymbols.has(assetSymbol)) {
      continue
    }
    intermediateAssetSymbols.add(assetSymbol)
    filteredIntermediateAssets.push(intermediateAsset)
  }

  return [false, filteredIntermediateAssets, baseBridges, quoteBridges]
}

// Function to check if a specific CEX supports arbitrage on a market
const createCexMarketSupportChecker = (
  bridgePaths: Record<number, Record<number, string[]>>,
  cexes: Record<string, MMCEXStatus>
) => {
  return (baseID: number, quoteID: number, cexName: string, directOnly: boolean): boolean => {
    const cexStatus = cexes[cexName]
    if (!cexStatus) return false

    const [supportsDirectArb, intermediateAssets] = cexSupportsArbOnMarket(
      baseID,
      quoteID,
      cexStatus,
      bridgePaths
    )

    if (directOnly) {
      return supportsDirectArb
    }

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

const checkFiatRates = (): boolean => {
  // TODO: check fiat rates for the required assets
  return true
}

function initialErrorState (botConfigState: BotConfigState | string | undefined, returnToMM?: boolean): [BotConfigState | null, MMSettingsError | null] {
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

  if (!checkFiatRates()) {
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
  initialCexes?: Record<string, MMCEXStatus>
  bridgePaths?: Record<number, Record<number, string[]>>
  // botConfigStateOnLoad may be a string, which means an error should be displayed.
  botConfigStateOnLoad?: BotConfigState | string
}

export interface MMSettingsHandle {
  handleBalanceNote: (note: BalanceNote) => void
  handleCEXBalanceUpdate: (cexName: string, update: CEXBalanceUpdate) => void
}

function tokenAssetApprovalStatuses (host: string, b: SupportedAsset, q: SupportedAsset) {
  let baseApprovalStatus = ApprovalStatus.Approved
  let quoteApprovalStatus = ApprovalStatus.Approved

  if (b?.token) {
    const baseAsset = app().assets[b.id]
    const baseVersion = app().exchanges[host].assets[b.id].version
    if (baseAsset?.wallet?.approved && baseAsset.wallet.approved[baseVersion] !== undefined) {
      baseApprovalStatus = baseAsset.wallet.approved[baseVersion]
    }
  }
  if (q?.token) {
    const quoteAsset = app().assets[q.id]
    const quoteVersion = app().exchanges[host].assets[q.id].version
    if (quoteAsset?.wallet?.approved && quoteAsset.wallet.approved[quoteVersion] !== undefined) {
      quoteApprovalStatus = quoteAsset.wallet.approved[quoteVersion]
    }
  }

  return {
    baseApprovalStatus,
    quoteApprovalStatus
  }
}

const MMSettings = forwardRef<MMSettingsHandle, MMSettingsProps>(({
  availableMarkets = { markets: [], exchangesRequiringRegistration: [] },
  initialCexes = {},
  bridgePaths = {},
  botConfigStateOnLoad = undefined
}, ref) => {
  const [initialState, initialError] = initialErrorState(botConfigStateOnLoad)
  const [error, setError] = useState<MMSettingsError | null>(initialError)
  const [botConfigState, dispatch] = useReducer(botConfigStateReducer, initialState)
  const [isLoading, setIsLoading] = useState<boolean>(false)
  const [updatingMarketOrType, setUpdatingMarketOrType] = useState<boolean>(false)
  const [cexes, setCexes] = useState<Record<string, MMCEXStatus>>(initialCexes)
  const [selectedMarket, setSelectedMarket] = useState<{
    host: string;
    baseID: number;
    quoteID: number;
  } | null>(null)

  const handleCEXesUpdated = async () => {
    try {
      const status = await MM.status()
      setCexes(status.cexes)
    } catch (error) {
      console.error('Failed to update CEX status:', error)
    }
  }

  // Expose handleBalanceNote and handleCEXBalanceUpdate to parent via ref
  useImperativeHandle(ref, () => ({
    handleBalanceNote: async (note: BalanceNote) => {
      // Only update if we have a bot config state
      if (!botConfigState) return

      // Check if the updated asset is required for the bot
      const requiredAssets = requiredDexAssets(botConfigState)
      if (!requiredAssets.includes(note.assetID)) return

      try {
        // Fetch updated available balances
        const { dexBalances, cexBalances } = await MM.availableBalances(
          { host: botConfigState.botConfig.host, baseID: botConfigState.dexMarket.baseID, quoteID: botConfigState.dexMarket.quoteID },
          botConfigState.botConfig.cexBaseID,
          botConfigState.botConfig.cexQuoteID,
          botConfigState.botConfig.cexName
        )

        // Dispatch action to update balances
        dispatch({
          type: 'UPDATE_AVAILABLE_BALANCES',
          payload: { dexBalances, cexBalances }
        })
      } catch (error) {
        console.error('Failed to update available balances:', error)
      }
    },

    handleCEXBalanceUpdate: async (cexName: string, update: CEXBalanceUpdate) => {
      // Only update if we have a bot config state
      if (!botConfigState) return

      // Only update if this is the CEX we're using for this bot
      if (botConfigState.botConfig.cexName !== cexName) return

      // Check if the updated asset is required for the bot (CEX side)
      const { cexBaseID, cexQuoteID } = botConfigState.botConfig
      if (update.assetID !== cexBaseID && update.assetID !== cexQuoteID) return

      try {
        // Fetch updated available balances
        const { dexBalances, cexBalances } = await MM.availableBalances(
          { host: botConfigState.botConfig.host, baseID: botConfigState.dexMarket.baseID, quoteID: botConfigState.dexMarket.quoteID },
          cexBaseID,
          cexQuoteID,
          cexName
        )

        // Dispatch action to update balances
        dispatch({
          type: 'UPDATE_AVAILABLE_BALANCES',
          payload: { dexBalances, cexBalances }
        })
      } catch (error) {
        console.error('Failed to update CEX available balances:', error)
      }
    }
  }), [botConfigState])

  // Create the CEX market support checker function
  const checkCexMarketSupport = createCexMarketSupportChecker(bridgePaths, cexes)

  const handleBotTypeSelected = async (botType: 'basicMM' | 'arbMM' | 'basicArb', cexName?: string) => {
    if (!selectedMarket) {
      console.error('No market selected')
      return
    }

    // Check if the market and bot type are the same as currently selected
    if (botConfigState) {
      const currentConfig = botConfigState.botConfig
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

    const [newState, errorState] = initialErrorState(newBotConfigState)
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

    dispatch({ type: 'SET_INITIAL_CONFIG', payload: newState })
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
        handleCEXesUpdated={handleCEXesUpdated}
      />
    )
  } else {
    mainComponent = (
      <MarketSelector
        markets={availableMarkets.markets}
        exchangesRequiringRegistration={availableMarkets.exchangesRequiringRegistration}
        cexes={cexes}
        onClose={() => {
          if (botConfigState) {
            setSelectedMarket({
              host: botConfigState.botConfig.host,
              baseID: botConfigState.botConfig.baseID,
              quoteID: botConfigState.botConfig.quoteID
            })
            setUpdatingMarketOrType(false)
          } else {
            app().loadPage('mm')
          }
        }}
        checkCexMarketSupport={checkCexMarketSupport}
        handleMarketSelected={(host: string, baseID: number, quoteID: number) => {
          const baseWallet = app().walletMap[baseID]
          const quoteWallet = app().walletMap[quoteID]

          if (!baseWallet) {
            setError({
              message: `You must create a ${app().assets[baseID].symbol} wallet to market make on this market.`
            })
            return
          }

          if (baseWallet.disabled) {
            setError({
              message: `The ${app().assets[baseID].symbol} wallet is disabled. Please enable it to market make on this market.`
            })
            return
          }

          if (!quoteWallet) {
            setError({
              message: `You must create a ${app().assets[quoteID].symbol} wallet to market make on this market.`
            })
            return
          }

          if (quoteWallet.disabled) {
            setError({
              message: `The ${app().assets[quoteID].symbol} wallet is disabled. Please enable it to market make on this market.`
            })
            return
          }

          const { baseApprovalStatus, quoteApprovalStatus } = tokenAssetApprovalStatuses(host, app().assets[baseID], app().assets[quoteID])

          if (baseApprovalStatus === ApprovalStatus.NotApproved) {
            setError({
              message: `You must approve the ${app().assets[baseID].symbol} asset to market make on this market.`
            })
            return
          }

          if (quoteApprovalStatus === ApprovalStatus.NotApproved) {
            setError({
              message: `You must approve the ${app().assets[quoteID].symbol} asset to market make on this market.`
            })
            return
          }

          setSelectedMarket({ host, baseID, quoteID })
        }}
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
})

export default MMSettings
