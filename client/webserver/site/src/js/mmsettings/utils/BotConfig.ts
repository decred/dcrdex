import {
  BotConfig,
  MMBotStatus,
  MarketReport,
  SupportedAsset,
  GapStrategy,
  OrderPlacement,
  ArbMarketMakingPlacement,
  QuickBalanceConfig,
  app,
  BridgeFeesAndLimits,
  RunStats,
  OrderOption,
  AutoRebalanceConfig,
  MMCEXStatus,
  MultiHopCfg
} from '../../registry'
import { MM, calculateQuoteLot } from '../../mmutil'
import { toAllocate, toAllocateRunning, AllocationResult, allocationResultAmounts } from './AllocationUtil'
import { createContext, useContext } from 'react'

// Interfaces
interface MarketInfo {
  host: string;
  baseID: number;
  quoteID: number;
  baseFeeAssetID: number;
  quoteFeeAssetID: number;
  baseAsset: SupportedAsset;
  quoteAsset: SupportedAsset;
  lotSize: number;
  quoteLot: number;
  baseIsAccountLocker: boolean;
  quoteIsAccountLocker: boolean;
}

export interface QuickPlacementsConfig {
  priceLevelsPerSide: number;
  lotsPerLevel: number;
  priceIncrement: number;
  profitThreshold: number;
  matchBuffer: number;
}

export interface BotConfigState {
  botConfig: BotConfig;
  dexMarket: MarketInfo;
  availableDEXBalances: Record<number, number>;
  availableCEXBalances: Record<number, number> | null;
  baseBridges: Record<number, string[]> | null;
  quoteBridges: Record<number, string[]> | null;
  baseBridgeFeesAndLimits: RoundTripFeesAndLimits | null;
  quoteBridgeFeesAndLimits: RoundTripFeesAndLimits | null;
  quickPlacements: QuickPlacementsConfig | null;
  allocationResult: AllocationResult | null;
  runStats: RunStats | null;
  marketReport: MarketReport;
  baseMultiFundingOpts: OrderOption[] | null;
  quoteMultiFundingOpts: OrderOption[] | null;
  baseMinWithdraw: number;
  quoteMinWithdraw: number;
  cexStatus: MMCEXStatus | null;
  intermediateAssets: number[] | null;
  intermediateAsset: number | null;
  fiatRatesMap: Record<number, number>;
}

export interface RoundTripFeesAndLimits {
  withdrawal: BridgeFeesAndLimits;
  deposit: BridgeFeesAndLimits;
  cexAsset: number;
  bridgeName: string;
}

// Constants
const DEFAULT_QUICK_PLACEMENTS: QuickPlacementsConfig = {
  priceLevelsPerSide: 1,
  lotsPerLevel: 1,
  priceIncrement: 0.005,
  profitThreshold: 0.02,
  matchBuffer: 0
}

const TRAIT_ACCOUNT_LOCKER = 1 << 14

// Utility Functions
function getWalletMultiFundingOptions (assetID: number): OrderOption[] | null {
  const walletDef = app().currentWalletDefinition(assetID)
  return walletDef.multifundingopts ?? null
}

function orderOptionsToRecord (opts: OrderOption[] | null): Record<string, string> | null {
  if (!opts) return null
  return opts.reduce((acc, opt) => ({
    ...acc,
    [opt.key]: opt.default?.toString() ?? ''
  }), {})
}

async function fetchCEXAssetAndBridgeInfo (
  dexAssetID: number,
  savedCEXAssetID: number | null,
  savedCEXBridge: string,
  bridges: Record<number, string[]> | null
): Promise<{ cexAssetID: number; cexBridge: string; feesAndLimits: RoundTripFeesAndLimits | null }> {
  if (!bridges || !Object.keys(bridges).length) {
    return { cexAssetID: dexAssetID, cexBridge: '', feesAndLimits: null }
  }

  let cexAssetID = parseInt(Object.keys(bridges)[0], 10)
  let cexBridge = bridges[cexAssetID][0]

  if (savedCEXAssetID != null && savedCEXBridge) {
    for (const [assetIDStr, bridgeNames] of Object.entries(bridges)) {
      const assetID = parseInt(assetIDStr, 10)
      if (assetID === savedCEXAssetID && bridgeNames.includes(savedCEXBridge)) {
        cexAssetID = assetID
        cexBridge = savedCEXBridge
        break
      }
    }
  }

  const feesAndLimits = await fetchRoundTripFeesAndLimits(dexAssetID, cexAssetID, cexBridge)

  return { cexAssetID, cexBridge, feesAndLimits }
}

export async function fetchRoundTripFeesAndLimits (dexAssetID: number, cexAssetID: number, bridgeName: string): Promise<RoundTripFeesAndLimits> {
  const [withdrawal, deposit] = await Promise.all([
    app().bridgeFeesAndLimits(dexAssetID, cexAssetID, bridgeName),
    app().bridgeFeesAndLimits(cexAssetID, dexAssetID, bridgeName)
  ])

  if (!withdrawal || !deposit) {
    throw new Error(`Failed to fetch round trip fees and limits for ${bridgeName}`)
  }

  return { withdrawal, deposit, bridgeName, cexAsset: cexAssetID }
}

function getMinimumTransferAmounts (
  cexName: string,
  baseID: number,
  quoteID: number,
  baseFeesAndLimits: RoundTripFeesAndLimits | null,
  quoteFeesAndLimits: RoundTripFeesAndLimits | null
): { baseMinWithdraw: number; quoteMinWithdraw: number } {
  if (!cexName) return { baseMinWithdraw: 0, quoteMinWithdraw: 0 }

  const cex = app().mmStatus.cexes[cexName]
  if (!cex) throw new Error(`CEX ${cexName} not found`)

  let baseMinWithdraw = 0
  let quoteMinWithdraw = 0

  for (const market of Object.values(cex.markets)) {
    if (market.baseID === baseID) baseMinWithdraw = Math.max(baseMinWithdraw, market.baseMinWithdraw)
    else if (market.quoteID === baseID) baseMinWithdraw = Math.max(baseMinWithdraw, market.quoteMinWithdraw)
    if (market.quoteID === quoteID) quoteMinWithdraw = Math.max(quoteMinWithdraw, market.quoteMinWithdraw)
    else if (market.baseID === quoteID) quoteMinWithdraw = Math.max(quoteMinWithdraw, market.baseMinWithdraw)
  }

  if (baseFeesAndLimits) {
    baseMinWithdraw = Math.max(
      baseMinWithdraw,
      baseFeesAndLimits.deposit.hasLimits ? baseFeesAndLimits.deposit.minLimit : 0,
      baseFeesAndLimits.withdrawal.hasLimits ? baseFeesAndLimits.withdrawal.minLimit : 0
    )
  }

  if (quoteFeesAndLimits) {
    quoteMinWithdraw = Math.max(
      quoteMinWithdraw,
      quoteFeesAndLimits.deposit.hasLimits ? quoteFeesAndLimits.deposit.minLimit : 0,
      quoteFeesAndLimits.withdrawal.hasLimits ? quoteFeesAndLimits.withdrawal.minLimit : 0
    )
  }

  return { baseMinWithdraw, quoteMinWithdraw }
}

function getDEXMarketInfo (host: string, baseID: number, quoteID: number): MarketInfo {
  const baseAsset = app().assets[baseID]
  const quoteAsset = app().assets[quoteID]
  const baseFeeAssetID = baseAsset.token ? baseAsset.token.parentID : baseID
  const quoteFeeAssetID = quoteAsset.token ? quoteAsset.token.parentID : quoteID

  const baseWallet = app().walletMap[baseID]
  const quoteWallet = app().walletMap[quoteID]

  const { markets } = app().exchanges[host]
  const { lotsize: lotSize } = markets[`${baseAsset.symbol}_${quoteAsset.symbol}`]
  const quoteLot = calculateQuoteLot(lotSize, baseID, quoteID)

  return {
    host,
    baseID,
    quoteID,
    baseFeeAssetID,
    quoteFeeAssetID,
    baseAsset,
    quoteAsset,
    lotSize,
    quoteLot,
    baseIsAccountLocker: baseWallet ? (baseWallet.traits & TRAIT_ACCOUNT_LOCKER) > 0 : false,
    quoteIsAccountLocker: quoteWallet ? (quoteWallet.traits & TRAIT_ACCOUNT_LOCKER) > 0 : false
  }
}

function initialMultiHopCfg (
  cexStatus: MMCEXStatus,
  intermediateAssets: number[],
  cexBaseID: number,
  cexQuoteID: number,
  savedCfg?: MultiHopCfg
): MultiHopCfg | undefined {
  const intermediateAsset = savedCfg
    ? savedCfg.baseAssetMarket[0] === cexBaseID ? savedCfg.baseAssetMarket[1] : savedCfg.baseAssetMarket[0]
    : intermediateAssets[0]

  const markets = multiHopMarkets(intermediateAsset, cexBaseID, cexQuoteID, cexStatus) ||
    multiHopMarkets(intermediateAssets[0], cexBaseID, cexQuoteID, cexStatus)

  if (!markets) return undefined

  return {
    baseAssetMarket: markets[0],
    quoteAssetMarket: markets[1],
    marketOrders: savedCfg?.marketOrders ?? false,
    limitOrdersBuffer: savedCfg?.limitOrdersBuffer ?? 0.01
  }
}

function setBotSpecificDefaultConfig (
  config: BotConfig,
  botType: 'basicMM' | 'arbMM' | 'basicArb',
  intermediateAssets: number[] | null,
  cexStatus: MMCEXStatus | null
): void {
  switch (botType) {
    case 'basicMM':
      config.basicMarketMakingConfig = {
        gapStrategy: 'percent-plus',
        sellPlacements: [{ lots: 1, gapFactor: 0.01 }],
        buyPlacements: [{ lots: 1, gapFactor: 0.01 }],
        driftTolerance: 0.001
      }
      break
    case 'arbMM': {
      let multiHop : MultiHopCfg | undefined
      if (intermediateAssets && cexStatus) {
        multiHop = initialMultiHopCfg(cexStatus, intermediateAssets, config.cexBaseID, config.cexQuoteID)
        if (!multiHop) throw new Error('Unable to determine initial multi-hop config')
      }
      config.arbMarketMakingConfig = {
        buyPlacements: [{ lots: 1, multiplier: 1 }],
        sellPlacements: [{ lots: 1, multiplier: 1 }],
        profit: 0.01,
        driftTolerance: 0.001,
        orderPersistence: 2,
        multiHop
      }
      break
    }
    case 'basicArb':
      config.simpleArbConfig = {
        profitTrigger: 0.01,
        maxActiveArbs: 5,
        numEpochsLeaveOpen: 2
      }
      break
    default:
      throw new Error(`Unknown bot type: ${botType}`)
  }
}

// Main Functions
export async function initialBotConfigState (
  host: string,
  baseID: number,
  quoteID: number,
  botType: 'basicMM' | 'arbMM' | 'basicArb',
  intermediateAssets: number[] | null,
  baseBridges: Record<number, string[]> | null,
  quoteBridges: Record<number, string[]> | null,
  cexStatus: MMCEXStatus | null,
  cexName?: string
): Promise<BotConfigState | string> {
  const baseMultiFundingOpts = getWalletMultiFundingOptions(baseID)
  const quoteMultiFundingOpts = getWalletMultiFundingOptions(quoteID)

  let cexBaseID = 0
  let cexQuoteID = 0
  let baseBridgeName = ''
  let quoteBridgeName = ''
  let baseBridgeFeesAndLimits = null
  let quoteBridgeFeesAndLimits = null
  try {
    ({ cexAssetID: cexBaseID, cexBridge: baseBridgeName, feesAndLimits: baseBridgeFeesAndLimits } =
      await fetchCEXAssetAndBridgeInfo(baseID, null, '', baseBridges));
    ({ cexAssetID: cexQuoteID, cexBridge: quoteBridgeName, feesAndLimits: quoteBridgeFeesAndLimits } =
      await fetchCEXAssetAndBridgeInfo(quoteID, null, '', quoteBridges))
  } catch (error) {
    return error instanceof Error ? error.message : String(error)
  }

  const { baseMinWithdraw, quoteMinWithdraw } = getMinimumTransferAmounts(
    cexName || '',
    cexBaseID,
    cexQuoteID,
    baseBridgeFeesAndLimits,
    quoteBridgeFeesAndLimits
  )

  const config: BotConfig = {
    host,
    baseID,
    quoteID,
    cexBaseID,
    cexQuoteID,
    baseBridgeName,
    quoteBridgeName,
    baseWalletOptions: orderOptionsToRecord(baseMultiFundingOpts),
    quoteWalletOptions: orderOptionsToRecord(quoteMultiFundingOpts),
    cexName: cexName || '',
    uiConfig: {
      quickBalance: {
        buysBuffer: 1,
        sellsBuffer: 1,
        buyFeeReserve: 0,
        sellFeeReserve: 0,
        bridgeFeeReserve: 0,
        slippageBuffer: 0.05
      },
      usingQuickBalance: true
    },
    alloc: { dex: {}, cex: {} },
    autoRebalance: cexName ? { minBaseTransfer: baseMinWithdraw, minQuoteTransfer: quoteMinWithdraw, internalOnly: true } : undefined
  }

  setBotSpecificDefaultConfig(config, botType, intermediateAssets, cexStatus)

  const { dexBalances, cexBalances } = await MM.availableBalances(
    { host, baseID, quoteID },
    config.cexBaseID,
    config.cexQuoteID,
    config.cexName
  )

  const marketReportRes = await MM.report(host, baseID, quoteID)
  if (!app().checkResponse(marketReportRes)) {
    return `Failed to get market report: ${marketReportRes.msg}`
  }

  const botConfigState: BotConfigState = {
    botConfig: config,
    dexMarket: getDEXMarketInfo(host, baseID, quoteID),
    availableDEXBalances: dexBalances,
    availableCEXBalances: cexBalances,
    baseBridges,
    quoteBridges,
    baseBridgeFeesAndLimits,
    quoteBridgeFeesAndLimits,
    quickPlacements: DEFAULT_QUICK_PLACEMENTS,
    allocationResult: null,
    runStats: null,
    marketReport: marketReportRes.report as MarketReport,
    baseMultiFundingOpts,
    quoteMultiFundingOpts,
    baseMinWithdraw,
    quoteMinWithdraw,
    intermediateAssets,
    intermediateAsset: intermediateAssets?.[0] ?? null,
    cexStatus,
    fiatRatesMap: app().fiatRatesMap
  }

  return updateAllocationsBasedOnQuickConfig(botConfigState)
}

export async function botConfigStateFromSavedConfig (
  savedBotConfig: BotConfig,
  cexStatus: MMCEXStatus | null,
  intermediateAssets: number[] | null,
  baseBridges: Record<number, string[]> | null,
  quoteBridges: Record<number, string[]> | null
): Promise<BotConfigState | string> {
  let cexBaseID = 0
  let cexQuoteID = 0
  let baseBridgeName = ''
  let quoteBridgeName = ''
  let baseBridgeFeesAndLimits: RoundTripFeesAndLimits | null = null
  let quoteBridgeFeesAndLimits: RoundTripFeesAndLimits | null = null
  try {
    ({ cexAssetID: cexBaseID, cexBridge: baseBridgeName, feesAndLimits: baseBridgeFeesAndLimits } =
      await fetchCEXAssetAndBridgeInfo(savedBotConfig.baseID, savedBotConfig.cexBaseID, savedBotConfig.baseBridgeName, baseBridges));
    ({ cexAssetID: cexQuoteID, cexBridge: quoteBridgeName, feesAndLimits: quoteBridgeFeesAndLimits } =
      await fetchCEXAssetAndBridgeInfo(savedBotConfig.quoteID, savedBotConfig.cexQuoteID, savedBotConfig.quoteBridgeName, quoteBridges))
  } catch (error) {
    return error instanceof Error ? error.message : String(error)
  }

  const { baseMinWithdraw, quoteMinWithdraw } = getMinimumTransferAmounts(
    savedBotConfig.cexName,
    cexBaseID,
    cexQuoteID,
    baseBridgeFeesAndLimits,
    quoteBridgeFeesAndLimits
  )

  const config: BotConfig = {
    ...savedBotConfig,
    cexBaseID,
    cexQuoteID,
    baseBridgeName,
    quoteBridgeName,
    autoRebalance: savedBotConfig.autoRebalance
      ? {
          ...savedBotConfig.autoRebalance,
          minBaseTransfer: Math.max(savedBotConfig.autoRebalance.minBaseTransfer, baseMinWithdraw),
          minQuoteTransfer: Math.max(savedBotConfig.autoRebalance.minQuoteTransfer, quoteMinWithdraw)
        }
      : undefined
  }

  let intermediateAsset: number | null = null
  if (config.arbMarketMakingConfig?.multiHop && cexStatus && intermediateAssets) {
    config.arbMarketMakingConfig.multiHop = initialMultiHopCfg(cexStatus, intermediateAssets, config.cexBaseID, config.cexQuoteID, config.arbMarketMakingConfig.multiHop)
    if (!config.arbMarketMakingConfig.multiHop) {
      return 'Unable to determine initial multi-hop config'
    }
    const baseAssetMarket = config.arbMarketMakingConfig.multiHop.baseAssetMarket
    intermediateAsset = baseAssetMarket[0] === config.cexBaseID ? baseAssetMarket[1] : baseAssetMarket[0]
  }

  const { dexBalances, cexBalances } = await MM.availableBalances(
    { host: savedBotConfig.host, baseID: savedBotConfig.baseID, quoteID: savedBotConfig.quoteID },
    config.cexBaseID,
    config.cexQuoteID,
    config.cexName
  )

  const marketReportRes = await MM.report(savedBotConfig.host, savedBotConfig.baseID, savedBotConfig.quoteID)
  if (!app().checkResponse(marketReportRes)) {
    return `Failed to get market report: ${marketReportRes.msg}`
  }

  const status = await MM.status()
  const botStatus = status.bots.find((b: MMBotStatus) =>
    b.config.baseID === savedBotConfig.baseID &&
    b.config.quoteID === savedBotConfig.quoteID &&
    b.config.host === savedBotConfig.host
  )

  const botConfigState: BotConfigState = {
    botConfig: config,
    dexMarket: getDEXMarketInfo(savedBotConfig.host, savedBotConfig.baseID, savedBotConfig.quoteID),
    availableDEXBalances: dexBalances,
    availableCEXBalances: cexBalances,
    baseBridges,
    quoteBridges,
    baseBridgeFeesAndLimits,
    quoteBridgeFeesAndLimits,
    quickPlacements: null,
    allocationResult: null,
    runStats: botStatus?.runStats ?? null,
    marketReport: marketReportRes.report as MarketReport,
    baseMultiFundingOpts: getWalletMultiFundingOptions(savedBotConfig.baseID),
    quoteMultiFundingOpts: getWalletMultiFundingOptions(savedBotConfig.quoteID),
    baseMinWithdraw,
    quoteMinWithdraw,
    intermediateAssets,
    intermediateAsset,
    cexStatus,
    fiatRatesMap: app().fiatRatesMap
  }

  return config.uiConfig.usingQuickBalance
    ? updateAllocationsBasedOnQuickConfig(botConfigState)
    : clampOriginalAllocations(botConfigState, dexBalances, cexBalances)
}

// Reducer and Context
type RebalanceSettingsAction =
  | { type: 'BASE_MIN_TRANSFER'; payload: number }
  | { type: 'QUOTE_MIN_TRANSFER'; payload: number }
  | { type: 'CEX_REBALANCE'; payload: boolean };

type BotConfigAction =
  | { type: 'SET_INITIAL_CONFIG'; payload: BotConfigState | null }
  | { type: 'USE_QUICK_PLACEMENTS'; payload: boolean }
  | { type: 'SET_GAP_STRATEGY'; payload: GapStrategy }
  | { type: 'SET_PROFIT'; payload: number }
  | { type: 'ADD_PLACEMENT'; payload: { sell: boolean; lots: number; gapFactor: number } }
  | { type: 'REMOVE_PLACEMENT'; payload: { sell: boolean; index: number } }
  | { type: 'REORDER_PLACEMENTS'; payload: { sell: boolean; fromIndex: number; toIndex: number } }
  | { type: 'UPDATE_QUICK_CONFIG'; payload: { field: keyof QuickPlacementsConfig; value: number } }
  | { type: 'UPDATE_QUICK_BALANCE'; payload: { field: keyof QuickBalanceConfig; value: number } }
  | { type: 'TOGGLE_QUICK_BALANCE'; payload: boolean }
  | { type: 'UPDATE_MANUAL_ALLOCATION'; payload: { assetID: number; amount: number; source: 'dex' | 'cex' } }
  | { type: 'UPDATE_DRIFT_TOLERANCE'; payload: number }
  | { type: 'UPDATE_ORDER_PERSISTENCE'; payload: number }
  | { type: 'UPDATE_REBALANCE_SETTINGS'; payload: RebalanceSettingsAction }
  | { type: 'UPDATE_WALLET_SETTING'; payload: { asset: 'base' | 'quote'; key: string; value: string } }
  | { type: 'UPDATE_BRIDGE_SELECTION'; payload: { asset: 'base' | 'quote'; feesAndLimits: RoundTripFeesAndLimits } }
  | { type: 'UPDATE_INTERMEDIATE_ASSET'; payload: number }
  | { type: 'UPDATE_MULTI_HOP_MARKET_COMPLETION'; payload: boolean }
  | { type: 'UPDATE_MULTI_HOP_LIMIT_BUFFER'; payload: number }
  | { type: 'UPDATE_AVAILABLE_BALANCES'; payload: { dexBalances: Record<number, number>; cexBalances: Record<number, number> } }
  | { type: 'TOGGLE_MM_SNAPSHOTS'; payload: boolean };

function rebalanceSettingsReducer (state: AutoRebalanceConfig, action: RebalanceSettingsAction): AutoRebalanceConfig {
  switch (action.type) {
    case 'BASE_MIN_TRANSFER':
      return { ...state, minBaseTransfer: action.payload }
    case 'QUOTE_MIN_TRANSFER':
      return { ...state, minQuoteTransfer: action.payload }
    case 'CEX_REBALANCE':
      return { ...state, internalOnly: !action.payload }
    default:
      return state
  }
}

function deriveQuickConfigFromPlacements (state: BotConfigState): QuickPlacementsConfig {
  const { botConfig } = state
  const isBasicMM = !!botConfig.basicMarketMakingConfig
  const isArbMM = !!botConfig.arbMarketMakingConfig

  // Get placements - handle different types
  let buys: (OrderPlacement | ArbMarketMakingPlacement)[] = []
  let sells: (OrderPlacement | ArbMarketMakingPlacement)[] = []

  if (isBasicMM && botConfig.basicMarketMakingConfig) {
    buys = botConfig.basicMarketMakingConfig.buyPlacements
    sells = botConfig.basicMarketMakingConfig.sellPlacements
  } else if (isArbMM && botConfig.arbMarketMakingConfig) {
    buys = botConfig.arbMarketMakingConfig.buyPlacements
    sells = botConfig.arbMarketMakingConfig.sellPlacements
  }

  // Default values
  let levelsPerSide = 1
  let lotsPerLevel = 1
  let priceIncrement = 0.005
  let profitThreshold = 0.02
  let matchBuffer = 0

  if (buys.length > 0 && sells.length > 0) {
    const placementCount = buys.length + sells.length
    levelsPerSide = Math.max(1, Math.floor(placementCount / 2))

    if (isBasicMM) {
      // Find best/worst placements by gapFactor
      const basicBuys = buys as OrderPlacement[]
      const basicSells = sells as OrderPlacement[]
      const bestBuy = basicBuys.reduce((prev, curr) => curr.gapFactor < prev.gapFactor ? curr : prev)
      const bestSell = basicSells.reduce((prev, curr) => curr.gapFactor < prev.gapFactor ? curr : prev)
      const worstBuy = basicBuys.reduce((prev, curr) => curr.gapFactor > prev.gapFactor ? curr : prev)
      const worstSell = basicSells.reduce((prev, curr) => curr.gapFactor > prev.gapFactor ? curr : prev)

      // Calculate profit as average of best buy/sell gap factors
      profitThreshold = (bestBuy.gapFactor + bestSell.gapFactor) / 2

      // Calculate price increment from range
      if (levelsPerSide > 1) {
        const range = ((worstBuy.gapFactor - bestBuy.gapFactor) + (worstSell.gapFactor - bestSell.gapFactor)) / 2
        priceIncrement = range / (levelsPerSide - 1)
      }
    } else if (isArbMM) {
      const arbBuys = buys as ArbMarketMakingPlacement[]
      const arbSells = sells as ArbMarketMakingPlacement[]
      const multSum = arbBuys.reduce((v, p) => v + p.multiplier, 0) + arbSells.reduce((v, p) => v + p.multiplier, 0)
      matchBuffer = ((multSum / placementCount) - 1) || 0
    }

    // Calculate lots per level from total placements
    const lots = buys.reduce((v, p) => v + p.lots, 0) + sells.reduce((v, p) => v + p.lots, 0)
    lotsPerLevel = Math.max(1, Math.round(lots / 2 / levelsPerSide))
  }

  return {
    priceLevelsPerSide: levelsPerSide,
    lotsPerLevel,
    priceIncrement,
    profitThreshold,
    matchBuffer
  }
}

function regeneratePlacementsFromQuickConfig (state: BotConfigState): BotConfigState {
  if (!state.quickPlacements || state.botConfig.simpleArbConfig) return state

  const { quickPlacements, botConfig } = state
  const isBasicMM = !!botConfig.basicMarketMakingConfig
  const isArbMM = !!botConfig.arbMarketMakingConfig
  const levelsPerSide = quickPlacements.priceLevelsPerSide
  const { lotsPerLevel, profitThreshold: profit, priceIncrement, matchBuffer } = quickPlacements

  let newState = { ...state }

  if (isBasicMM && botConfig.basicMarketMakingConfig) {
    newState = {
      ...newState,
      botConfig: {
        ...botConfig,
        basicMarketMakingConfig: {
          ...botConfig.basicMarketMakingConfig,
          buyPlacements: [],
          sellPlacements: []
        }
      }
    }

    if (!newState.botConfig.basicMarketMakingConfig) return newState
    for (let levelN = 0; levelN < levelsPerSide; levelN++) {
      const gapFactor = profit + (priceIncrement * levelN)
      newState.botConfig.basicMarketMakingConfig.buyPlacements.push({ lots: lotsPerLevel, gapFactor })
      newState.botConfig.basicMarketMakingConfig.sellPlacements.push({ lots: lotsPerLevel, gapFactor })
    }
  } else if (isArbMM && botConfig.arbMarketMakingConfig) {
    newState = {
      ...newState,
      botConfig: {
        ...botConfig,
        arbMarketMakingConfig: {
          ...botConfig.arbMarketMakingConfig,
          profit,
          buyPlacements: [],
          sellPlacements: []
        }
      }
    }

    if (!newState.botConfig.arbMarketMakingConfig) return newState
    for (let levelN = 0; levelN < levelsPerSide; levelN++) {
      const multiplier = matchBuffer + 1
      newState.botConfig.arbMarketMakingConfig.buyPlacements.push({ lots: lotsPerLevel, multiplier })
      newState.botConfig.arbMarketMakingConfig.sellPlacements.push({ lots: lotsPerLevel, multiplier })
    }
  }

  return newState
}

function clampOriginalAllocations (state: BotConfigState, dexBalances: Record<number, number>, cexBalances: Record<number, number>): BotConfigState {
  const allocation = state.botConfig.alloc || { dex: {}, cex: {} }
  const clampedAllocation = {
    dex: { ...allocation.dex },
    cex: { ...allocation.cex }
  }

  for (const [assetIDStr, allocatedAmount] of Object.entries(allocation.dex)) {
    const assetID = parseInt(assetIDStr, 10)
    clampedAllocation.dex[assetID] = state.runStats ? 0 : Math.min(allocatedAmount, dexBalances[assetID] || 0)
  }

  for (const [assetIDStr, allocatedAmount] of Object.entries(allocation.cex)) {
    const assetID = parseInt(assetIDStr, 10)
    clampedAllocation.cex[assetID] = state.runStats ? 0 : Math.min(allocatedAmount, cexBalances[assetID] || 0)
  }

  return {
    ...state,
    botConfig: {
      ...state.botConfig,
      alloc: clampedAllocation
    }
  }
}

function updateAllocationsBasedOnQuickConfig (state: BotConfigState): BotConfigState {
  if (!state.botConfig.uiConfig.usingQuickBalance) return state

  const allocationResult = state.runStats ? toAllocateRunning(state, state.runStats) : toAllocate(state)

  return {
    ...state,
    botConfig: { ...state.botConfig, alloc: allocationResultAmounts(allocationResult) },
    allocationResult
  }
}

function multiHopMarkets (
  intermediateAsset: number,
  cexBaseID: number,
  cexQuoteID: number,
  cexStatus: MMCEXStatus
): [[number, number], [number, number]] | undefined {
  let baseAssetMarket: [number, number] | undefined
  let quoteAssetMarket: [number, number] | undefined

  for (const mkt of Object.values(cexStatus.markets)) {
    if ((mkt.baseID === cexBaseID && mkt.quoteID === intermediateAsset) ||
        (mkt.baseID === intermediateAsset && mkt.quoteID === cexBaseID)) {
      baseAssetMarket = [mkt.baseID, mkt.quoteID]
    }
    if ((mkt.baseID === cexQuoteID && mkt.quoteID === intermediateAsset) ||
        (mkt.baseID === intermediateAsset && mkt.quoteID === cexQuoteID)) {
      quoteAssetMarket = [mkt.baseID, mkt.quoteID]
    }
    if (baseAssetMarket && quoteAssetMarket) break
  }

  return baseAssetMarket && quoteAssetMarket ? [baseAssetMarket, quoteAssetMarket] : undefined
}

export const botConfigStateReducer = (state: BotConfigState | null, action: BotConfigAction): BotConfigState | null => {
  if (action.type === 'SET_INITIAL_CONFIG') return action.payload
  if (!state) return null

  switch (action.type) {
    case 'USE_QUICK_PLACEMENTS': {
      let newState = { ...state, quickPlacements: action.payload ? deriveQuickConfigFromPlacements(state) : null }
      if (action.payload && newState.botConfig.basicMarketMakingConfig) {
        newState = {
          ...newState,
          botConfig: {
            ...newState.botConfig,
            basicMarketMakingConfig: {
              ...newState.botConfig.basicMarketMakingConfig,
              gapStrategy: 'percent-plus'
            }
          }
        }
      }
      return regeneratePlacementsFromQuickConfig(newState)
    }

    case 'SET_GAP_STRATEGY':
      if (state.botConfig.basicMarketMakingConfig) {
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            basicMarketMakingConfig: {
              ...state.botConfig.basicMarketMakingConfig,
              gapStrategy: action.payload,
              buyPlacements: [],
              sellPlacements: []
            }
          }
        }
      }
      return state

    case 'SET_PROFIT':
      if (state.botConfig.arbMarketMakingConfig) {
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            arbMarketMakingConfig: {
              ...state.botConfig.arbMarketMakingConfig,
              profit: action.payload
            }
          }
        }
      }
      if (state.botConfig.simpleArbConfig) {
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            simpleArbConfig: {
              ...state.botConfig.simpleArbConfig,
              profitTrigger: action.payload
            }
          }
        }
      }
      return state

    case 'ADD_PLACEMENT': {
      const placementType = action.payload.sell ? 'sellPlacements' : 'buyPlacements'
      const isBasicMM = !!state.botConfig.basicMarketMakingConfig

      if (isBasicMM && state.botConfig.basicMarketMakingConfig) {
        const newPlacement: OrderPlacement = { lots: action.payload.lots, gapFactor: action.payload.gapFactor }
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            basicMarketMakingConfig: {
              ...state.botConfig.basicMarketMakingConfig,
              [placementType]: [...state.botConfig.basicMarketMakingConfig[placementType], newPlacement]
            }
          }
        }
      } else if (state.botConfig.arbMarketMakingConfig) {
        const newPlacement: ArbMarketMakingPlacement = { lots: action.payload.lots, multiplier: action.payload.gapFactor }
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            arbMarketMakingConfig: {
              ...state.botConfig.arbMarketMakingConfig,
              [placementType]: [...state.botConfig.arbMarketMakingConfig[placementType], newPlacement]
            }
          }
        }
      }
      return state
    }

    case 'REMOVE_PLACEMENT': {
      const placementType = action.payload.sell ? 'sellPlacements' : 'buyPlacements'
      const config = state.botConfig.basicMarketMakingConfig ?? state.botConfig.arbMarketMakingConfig
      if (!config) return state

      const placements = [...config[placementType]]
      if (action.payload.index < 0 || action.payload.index >= placements.length) return state
      placements.splice(action.payload.index, 1)
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          [state.botConfig.basicMarketMakingConfig ? 'basicMarketMakingConfig' : 'arbMarketMakingConfig']: {
            ...config,
            [placementType]: placements
          }
        }
      }
    }

    case 'REORDER_PLACEMENTS': {
      const placementType = action.payload.sell ? 'sellPlacements' : 'buyPlacements'
      const config = state.botConfig.basicMarketMakingConfig ?? state.botConfig.arbMarketMakingConfig
      if (!config) return state

      const placements = [...config[placementType]]
      const { fromIndex, toIndex } = action.payload
      if (fromIndex < 0 || fromIndex >= placements.length || toIndex < 0 || toIndex >= placements.length) return state;
      [placements[fromIndex], placements[toIndex]] = [placements[toIndex], placements[fromIndex]]
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          [state.botConfig.basicMarketMakingConfig ? 'basicMarketMakingConfig' : 'arbMarketMakingConfig']: {
            ...config,
            [placementType]: placements
          }
        }
      }
    }

    case 'UPDATE_QUICK_CONFIG':
      if (state.quickPlacements) {
        const newState = {
          ...state,
          quickPlacements: { ...state.quickPlacements, [action.payload.field]: action.payload.value }
        }
        return updateAllocationsBasedOnQuickConfig(regeneratePlacementsFromQuickConfig(newState))
      }
      return state

    case 'UPDATE_QUICK_BALANCE':
      if (state.botConfig.uiConfig.quickBalance) {
        const newState = {
          ...state,
          botConfig: {
            ...state.botConfig,
            uiConfig: {
              ...state.botConfig.uiConfig,
              quickBalance: {
                ...state.botConfig.uiConfig.quickBalance,
                [action.payload.field]: action.payload.value
              }
            }
          }
        }
        return updateAllocationsBasedOnQuickConfig(newState)
      }
      return state

    case 'TOGGLE_QUICK_BALANCE': {
      const newState = {
        ...state,
        botConfig: {
          ...state.botConfig,
          uiConfig: { ...state.botConfig.uiConfig, usingQuickBalance: action.payload }
        }
      }
      return action.payload ? updateAllocationsBasedOnQuickConfig(newState) : newState
    }

    case 'UPDATE_MANUAL_ALLOCATION': {
      const currentAlloc = state.botConfig.alloc || { dex: {}, cex: {} }
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          alloc: {
            ...currentAlloc,
            [action.payload.source]: {
              ...currentAlloc[action.payload.source],
              [action.payload.assetID]: action.payload.amount
            }
          }
        }
      }
    }

    case 'UPDATE_DRIFT_TOLERANCE':
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          basicMarketMakingConfig: state.botConfig.basicMarketMakingConfig
            ? { ...state.botConfig.basicMarketMakingConfig, driftTolerance: action.payload }
            : undefined,
          arbMarketMakingConfig: state.botConfig.arbMarketMakingConfig
            ? { ...state.botConfig.arbMarketMakingConfig, driftTolerance: action.payload }
            : undefined
        }
      }

    case 'UPDATE_ORDER_PERSISTENCE':
      if (state.botConfig.arbMarketMakingConfig) {
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            arbMarketMakingConfig: {
              ...state.botConfig.arbMarketMakingConfig,
              orderPersistence: action.payload
            }
          }
        }
      }
      if (state.botConfig.simpleArbConfig) {
        return {
          ...state,
          botConfig: {
            ...state.botConfig,
            simpleArbConfig: {
              ...state.botConfig.simpleArbConfig,
              numEpochsLeaveOpen: action.payload
            }
          }
        }
      }
      return state

    case 'UPDATE_REBALANCE_SETTINGS': {
      if (!state.botConfig.autoRebalance) return state
      const newState = {
        ...state,
        botConfig: {
          ...state.botConfig,
          autoRebalance: rebalanceSettingsReducer(state.botConfig.autoRebalance, action.payload)
        }
      }
      return updateAllocationsBasedOnQuickConfig(newState)
    }

    case 'UPDATE_WALLET_SETTING': {
      const optionsKey = action.payload.asset === 'base' ? 'baseWalletOptions' : 'quoteWalletOptions'
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          [optionsKey]: {
            ...state.botConfig[optionsKey],
            [action.payload.key]: action.payload.value
          }
        }
      }
    }

    case 'UPDATE_BRIDGE_SELECTION': {
      const optionsKey = action.payload.asset === 'base' ? 'baseBridgeFeesAndLimits' : 'quoteBridgeFeesAndLimits'
      const cexAssetIDKey = action.payload.asset === 'base' ? 'cexBaseID' : 'cexQuoteID'
      const bridgeNameKey = action.payload.asset === 'base' ? 'baseBridgeName' : 'quoteBridgeName'
      let newState = {
        ...state,
        [optionsKey]: action.payload.feesAndLimits,
        botConfig: {
          ...state.botConfig,
          [cexAssetIDKey]: action.payload.feesAndLimits.cexAsset,
          [bridgeNameKey]: action.payload.feesAndLimits.bridgeName
        }
      }

      const { baseMinWithdraw, quoteMinWithdraw } = getMinimumTransferAmounts(
        newState.botConfig.cexName,
        newState.botConfig.cexBaseID,
        newState.botConfig.cexQuoteID,
        newState.baseBridgeFeesAndLimits,
        newState.quoteBridgeFeesAndLimits
      )

      newState = {
        ...newState,
        botConfig: {
          ...newState.botConfig,
          autoRebalance: newState.botConfig.autoRebalance
            ? {
                ...newState.botConfig.autoRebalance,
                minBaseTransfer: Math.max(newState.botConfig.autoRebalance.minBaseTransfer, baseMinWithdraw),
                minQuoteTransfer: Math.max(newState.botConfig.autoRebalance.minQuoteTransfer, quoteMinWithdraw)
              }
            : undefined
        },
        baseMinWithdraw,
        quoteMinWithdraw
      }

      return updateAllocationsBasedOnQuickConfig(newState)
    }

    case 'UPDATE_INTERMEDIATE_ASSET': {
      if (!state.botConfig.arbMarketMakingConfig?.multiHop || !state.cexStatus) {
        console.error(`Unable to update intermediate asset to ${action.payload}`)
        return state
      }

      const mkts = multiHopMarkets(action.payload, state.botConfig.cexBaseID, state.botConfig.cexQuoteID, state.cexStatus)
      if (!mkts) {
        console.error(`Unable to update intermediate asset to ${action.payload}`)
        return state
      }

      return {
        ...state,
        intermediateAsset: action.payload,
        botConfig: {
          ...state.botConfig,
          arbMarketMakingConfig: {
            ...state.botConfig.arbMarketMakingConfig,
            multiHop: {
              ...state.botConfig.arbMarketMakingConfig.multiHop,
              baseAssetMarket: mkts[0],
              quoteAssetMarket: mkts[1]
            }
          }
        }
      }
    }

    case 'UPDATE_MULTI_HOP_MARKET_COMPLETION':
      if (!state.botConfig.arbMarketMakingConfig?.multiHop) return state
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          arbMarketMakingConfig: {
            ...state.botConfig.arbMarketMakingConfig,
            multiHop: {
              ...state.botConfig.arbMarketMakingConfig.multiHop,
              marketOrders: action.payload
            }
          }
        }
      }

    case 'UPDATE_MULTI_HOP_LIMIT_BUFFER':
      if (!state.botConfig.arbMarketMakingConfig?.multiHop) return state
      return {
        ...state,
        botConfig: {
          ...state.botConfig,
          arbMarketMakingConfig: {
            ...state.botConfig.arbMarketMakingConfig,
            multiHop: {
              ...state.botConfig.arbMarketMakingConfig.multiHop,
              limitOrdersBuffer: action.payload
            }
          }
        }
      }

    case 'UPDATE_AVAILABLE_BALANCES': {
      const { dexBalances, cexBalances } = action.payload
      const updatedState: BotConfigState = {
        ...state,
        availableDEXBalances: dexBalances,
        availableCEXBalances: cexBalances
      }

      return updatedState.botConfig.uiConfig.usingQuickBalance
        ? updateAllocationsBasedOnQuickConfig(updatedState)
        : clampOriginalAllocations(updatedState, dexBalances, cexBalances)
    }

    case 'TOGGLE_MM_SNAPSHOTS':
      return { ...state, botConfig: { ...state.botConfig, mmSnapshots: action.payload } }

    default:
      return state
  }
}

// Context and Hooks
export const BotConfigStateContext = createContext<BotConfigState | undefined>(undefined)
export const BotConfigDispatchContext = createContext<React.Dispatch<BotConfigAction> | undefined>(undefined)

export const useBotConfigState = () => {
  const context = useContext(BotConfigStateContext)
  if (context === undefined) throw new Error('useBotConfigState must be used within a BotConfigProvider')
  return context
}

export const useBotConfigDispatch = () => {
  const context = useContext(BotConfigDispatchContext)
  if (context === undefined) throw new Error('useBotConfigDispatch must be used within a BotConfigProvider')
  return context
}
