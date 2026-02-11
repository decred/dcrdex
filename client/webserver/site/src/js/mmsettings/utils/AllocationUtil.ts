import { app, RunStats } from '../../registry'
import { BotConfigState } from './BotConfig'

// Interfaces for allocation calculations
interface PerLotBreakdown {
  totalAmount: number
  tradedAmount: number
  fees: Fees
  slippageBuffer: number
  multiSplitBuffer: number
}

interface PerLot {
  dex: Record<number, PerLotBreakdown>
  cex: Record<number, PerLotBreakdown>
}

interface Fees {
  swap: number
  redeem: number
  refund: number
  funding: number
}

interface CalculationBreakdown {
  buyLot: PerLotBreakdown
  sellLot: PerLotBreakdown

  numBuyLots: number
  numSellLots: number

  feeReserves: {
    buyReserves: Fees
    sellReserves: Fees
  }
  numBuyFeeReserves: number
  numSellFeeReserves: number

  initialBuyFundingFees: number
  initialSellFundingFees: number

  bridgeFeeReserves: number

  bridgeFees: number
  available: number
  totalRequired: number
  runningBotTotal?: number
  runningBotAvailable?: number
  rebalanceAdjustment?: number
}

export interface AllocationDetail {
  amount: number
  status: string
  calculation: CalculationBreakdown
}

export interface AllocationResult {
  dex: Record<number, AllocationDetail>
  cex: Record<number, AllocationDetail>
}

function newPerLotBreakdown () : PerLotBreakdown {
  return {
    totalAmount: 0,
    tradedAmount: 0,
    fees: { swap: 0, redeem: 0, refund: 0, funding: 0 },
    slippageBuffer: 0,
    multiSplitBuffer: 0
  }
}

function newAllocationDetail () : AllocationDetail {
  return {
    amount: 0,
    status: 'sufficient',
    calculation: newCalculationBreakdown()
  }
}

function newCalculationBreakdown () : CalculationBreakdown {
  return {
    buyLot: newPerLotBreakdown(),
    sellLot: newPerLotBreakdown(),
    numBuyLots: 0,
    numSellLots: 0,
    feeReserves: {
      buyReserves: { swap: 0, redeem: 0, refund: 0, funding: 0 },
      sellReserves: { swap: 0, redeem: 0, refund: 0, funding: 0 }
    },
    numBuyFeeReserves: 0,
    numSellFeeReserves: 0,
    initialBuyFundingFees: 0,
    initialSellFundingFees: 0,
    bridgeFeeReserves: 0,
    bridgeFees: 0,
    available: 0,
    totalRequired: 0
  }
}

// perLotRequirements calculates the funding requirements for a single buy and sell lot.
function perLotRequirements (state: BotConfigState) : { perSellLot: PerLot, perBuyLot: PerLot } {
  const perSellLot: PerLot = { cex: {}, dex: {} }
  const perBuyLot: PerLot = { cex: {}, dex: {} }
  const dexAssetIDs = requiredDexAssets(state)
  const cexAssetIDs = [state.botConfig.cexBaseID, state.botConfig.cexQuoteID]

  for (const assetID of dexAssetIDs) {
    perSellLot.dex[assetID] = newPerLotBreakdown()
    perBuyLot.dex[assetID] = newPerLotBreakdown()
  }
  for (const assetID of cexAssetIDs) {
    perSellLot.cex[assetID] = newPerLotBreakdown()
    perBuyLot.cex[assetID] = newPerLotBreakdown()
  }

  const {
    dexMarket: { baseID, lotSize, quoteLot, baseFeeAssetID, quoteID, quoteFeeAssetID, baseIsAccountLocker, quoteIsAccountLocker },
    botConfig: { cexBaseID, cexQuoteID, uiConfig: { quickBalance: { slippageBuffer } } }, marketReport
  } = state

  perSellLot.dex[baseID].tradedAmount = lotSize
  perSellLot.dex[baseFeeAssetID].fees.swap = marketReport.baseFees.max.swap
  perSellLot.cex[cexQuoteID].tradedAmount = quoteLot
  perSellLot.cex[cexQuoteID].slippageBuffer = slippageBuffer
  perSellLot.dex[baseFeeAssetID].fees.funding = 0 // TODO: update this
  if (baseIsAccountLocker) perSellLot.dex[baseFeeAssetID].fees.refund = marketReport.baseFees.max.refund
  if (quoteIsAccountLocker) perSellLot.dex[quoteFeeAssetID].fees.redeem = marketReport.quoteFees.max.redeem

  perBuyLot.dex[quoteID].tradedAmount = quoteLot
  perBuyLot.dex[quoteID].slippageBuffer = slippageBuffer
  perBuyLot.cex[cexBaseID].tradedAmount = lotSize
  perBuyLot.dex[quoteFeeAssetID].fees.swap = marketReport.quoteFees.max.swap
  perBuyLot.dex[quoteFeeAssetID].fees.funding = 0 // TODO: update this
  if (baseIsAccountLocker) perBuyLot.dex[baseFeeAssetID].fees.redeem = marketReport.baseFees.max.redeem
  if (quoteIsAccountLocker) perBuyLot.dex[quoteFeeAssetID].fees.refund = marketReport.quoteFees.max.refund

  const calculateTotalAmount = (perLot: PerLotBreakdown) : number => {
    let total = perLot.tradedAmount
    total *= (1 + perLot.slippageBuffer + perLot.multiSplitBuffer)
    total = Math.floor(total)
    total += perLot.fees.swap + perLot.fees.redeem + perLot.fees.refund + perLot.fees.funding
    return total
  }

  for (const assetID of dexAssetIDs) {
    perSellLot.dex[assetID].totalAmount = calculateTotalAmount(perSellLot.dex[assetID])
    perBuyLot.dex[assetID].totalAmount = calculateTotalAmount(perBuyLot.dex[assetID])
  }
  for (const assetID of cexAssetIDs) {
    perSellLot.cex[assetID].totalAmount = calculateTotalAmount(perSellLot.cex[assetID])
    perBuyLot.cex[assetID].totalAmount = calculateTotalAmount(perBuyLot.cex[assetID])
  }

  return { perSellLot, perBuyLot }
}

function calcNumLots (state: BotConfigState) : { numBuyLots: number, numSellLots: number } {
  if (state.botConfig.basicMarketMakingConfig) {
    const numBuyLots = state.botConfig.basicMarketMakingConfig.buyPlacements.reduce((acc, placement) => acc + placement.lots, 0)
    const numSellLots = state.botConfig.basicMarketMakingConfig.sellPlacements.reduce((acc, placement) => acc + placement.lots, 0)
    return { numBuyLots, numSellLots }
  } else if (state.botConfig.arbMarketMakingConfig) {
    const numBuyLots = state.botConfig.arbMarketMakingConfig.buyPlacements.reduce((acc, placement) => acc + placement.lots, 0)
    const numSellLots = state.botConfig.arbMarketMakingConfig.sellPlacements.reduce((acc, placement) => acc + placement.lots, 0)
    return { numBuyLots, numSellLots }
  } else if (state.botConfig.simpleArbConfig) {
    return { numBuyLots: 1, numSellLots: 1 }
  } else {
    throw new Error('Invalid bot config')
  }
}

// requiredFunds calculates the total funds required for a bot based on the quick allocation settings.
function requiredFunds (state: BotConfigState) : AllocationResult {
  const { numBuyLots, numSellLots } = calcNumLots(state)
  const totalBuyLots = numBuyLots + state.botConfig.uiConfig.quickBalance.buysBuffer
  const totalSellLots = numSellLots + state.botConfig.uiConfig.quickBalance.sellsBuffer

  const { dexMarket: { baseIsAccountLocker, quoteIsAccountLocker }, marketReport } = state

  const dexAssetIDs = requiredDexAssets(state)
  const cexAssetIDs = [state.botConfig.cexBaseID, state.botConfig.cexQuoteID]

  const toAllocate: AllocationResult = { dex: {}, cex: {} }

  for (const assetID of dexAssetIDs) {
    toAllocate.dex[assetID] = newAllocationDetail()
  }
  if (state.botConfig.cexName) {
    for (const assetID of cexAssetIDs) {
      toAllocate.cex[assetID] = newAllocationDetail()
    }
  }

  const { perBuyLot, perSellLot } = perLotRequirements(state)

  if (state.botConfig.cexName) {
    for (const assetID of cexAssetIDs) {
      toAllocate.cex[assetID].calculation.buyLot = perBuyLot.cex[assetID]
      toAllocate.cex[assetID].calculation.sellLot = perSellLot.cex[assetID]
      toAllocate.cex[assetID].calculation.numBuyLots = totalBuyLots
      toAllocate.cex[assetID].calculation.numSellLots = totalSellLots
    }
  }

  for (const assetID of dexAssetIDs) {
    toAllocate.dex[assetID].calculation.buyLot = perBuyLot.dex[assetID]
    toAllocate.dex[assetID].calculation.sellLot = perSellLot.dex[assetID]
    toAllocate.dex[assetID].calculation.numBuyLots = totalBuyLots
    toAllocate.dex[assetID].calculation.numSellLots = totalSellLots

    if (assetID === state.dexMarket.baseFeeAssetID) {
      toAllocate.dex[assetID].calculation.feeReserves.sellReserves.swap = marketReport.baseFees.estimated.swap
      if (baseIsAccountLocker) {
        toAllocate.dex[assetID].calculation.feeReserves.buyReserves.redeem = marketReport.baseFees.estimated.redeem
        toAllocate.dex[assetID].calculation.feeReserves.sellReserves.refund = marketReport.baseFees.estimated.refund
      }
      toAllocate.dex[assetID].calculation.initialSellFundingFees = 0 // TODO: update this
    }

    if (assetID === state.dexMarket.quoteFeeAssetID) {
      toAllocate.dex[assetID].calculation.feeReserves.buyReserves.swap = marketReport.quoteFees.estimated.swap
      if (quoteIsAccountLocker) {
        toAllocate.dex[assetID].calculation.feeReserves.sellReserves.redeem = marketReport.quoteFees.estimated.redeem
        toAllocate.dex[assetID].calculation.feeReserves.buyReserves.refund = marketReport.quoteFees.estimated.refund
      }
      toAllocate.dex[assetID].calculation.initialBuyFundingFees = 0 // TODO: update this
      toAllocate.dex[assetID].calculation.initialSellFundingFees = 0 // TODO: update this
    }

    toAllocate.dex[assetID].calculation.bridgeFeeReserves = state.botConfig.uiConfig.quickBalance.bridgeFeeReserve

    if (state.baseBridgeFeesAndLimits) {
      toAllocate.dex[assetID].calculation.bridgeFees += state.baseBridgeFeesAndLimits.withdrawal?.fees[assetID] ?? 0
      toAllocate.dex[assetID].calculation.bridgeFees += state.baseBridgeFeesAndLimits.deposit?.fees[assetID] ?? 0
    }
    if (state.quoteBridgeFeesAndLimits) {
      toAllocate.dex[assetID].calculation.bridgeFees += state.quoteBridgeFeesAndLimits.withdrawal?.fees[assetID] ?? 0
      toAllocate.dex[assetID].calculation.bridgeFees += state.quoteBridgeFeesAndLimits.deposit?.fees[assetID] ?? 0
    }

    toAllocate.dex[assetID].calculation.numBuyFeeReserves = state.botConfig.uiConfig.quickBalance.buyFeeReserve
    toAllocate.dex[assetID].calculation.numSellFeeReserves = state.botConfig.uiConfig.quickBalance.sellFeeReserve
  }

  const totalFees = (fees: Fees) : number => {
    return fees.swap + fees.redeem + fees.refund + fees.funding
  }

  const calculateTotalRequired = (breakdown: CalculationBreakdown) : number => {
    let total = 0
    total += breakdown.buyLot.totalAmount * breakdown.numBuyLots
    total += breakdown.sellLot.totalAmount * breakdown.numSellLots
    total += totalFees(breakdown.feeReserves.buyReserves) * breakdown.numBuyFeeReserves
    total += totalFees(breakdown.feeReserves.sellReserves) * breakdown.numSellFeeReserves
    total += breakdown.initialBuyFundingFees
    total += breakdown.initialSellFundingFees
    total += breakdown.bridgeFeeReserves * breakdown.bridgeFees
    return total
  }

  for (const assetID of dexAssetIDs) {
    toAllocate.dex[assetID].calculation.totalRequired = calculateTotalRequired(toAllocate.dex[assetID].calculation)
  }

  if (state.botConfig.cexName) {
    for (const assetID of cexAssetIDs) {
      toAllocate.cex[assetID].calculation.totalRequired = calculateTotalRequired(toAllocate.cex[assetID].calculation)
    }
  }

  return toAllocate
}

export function allocationResultAmounts (result: AllocationResult) : { dex: Record<number, number>, cex: Record<number, number> } {
  const amounts: { dex: Record<number, number>, cex: Record<number, number> } = { dex: {}, cex: {} }
  for (const [assetID, allocationDetail] of Object.entries(result.dex)) {
    amounts.dex[Number(assetID)] = allocationDetail.amount
  }
  for (const [assetID, allocationDetail] of Object.entries(result.cex)) {
    amounts.cex[Number(assetID)] = allocationDetail.amount
  }
  return amounts
}

// toAllocate calculates the quick allocations for a bot that is not running.
export function toAllocate (state: BotConfigState) : AllocationResult {
  const availableFunds = { dex: state.availableDEXBalances, cex: state.availableCEXBalances }
  const canRebalance = !!state.botConfig.autoRebalance && !state.botConfig.autoRebalance.internalOnly

  const result = requiredFunds(state)

  const dexAssetIDs = requiredDexAssets(state)
  const { cexBaseID, cexQuoteID } = state.botConfig
  const cexAssetIDs = [cexBaseID, cexQuoteID]

  let dexBaseSurplus = 0
  let dexQuoteSurplus = 0
  let cexBaseSurplus = 0
  let cexQuoteSurplus = 0

  for (const assetID of dexAssetIDs) {
    const allocationDetail = result.dex[assetID]
    allocationDetail.calculation.available = availableFunds.dex[assetID] ?? 0
    const surplus = allocationDetail.calculation.available - allocationDetail.calculation.totalRequired
    if (surplus < 0) {
      allocationDetail.status = 'insufficient'
      allocationDetail.amount = allocationDetail.calculation.available
    } else {
      allocationDetail.amount = allocationDetail.calculation.totalRequired
    }
    if (assetID === state.dexMarket.baseID) dexBaseSurplus = surplus
    if (assetID === state.dexMarket.quoteID) dexQuoteSurplus = surplus
  }

  if (state.botConfig.cexName) {
    for (const assetID of cexAssetIDs) {
      const allocationDetail = result.cex[assetID]
      allocationDetail.calculation.available = availableFunds.cex?.[assetID] ?? 0
      const surplus = allocationDetail.calculation.available - allocationDetail.calculation.totalRequired
      if (surplus < 0) {
        allocationDetail.status = 'insufficient'
        allocationDetail.amount = allocationDetail.calculation.available
      } else {
        allocationDetail.amount = allocationDetail.calculation.totalRequired
      }
      if (assetID === cexBaseID) cexBaseSurplus = surplus
      if (assetID === cexQuoteID) cexQuoteSurplus = surplus
    }
  }

  const rebalance = (dexAssetID: number, cexAssetID: number, dexSurplus: number, cexSurplus: number) => {
    if (canRebalance && dexSurplus < 0 && cexSurplus > 0) {
      const dexDeficit = -dexSurplus
      const additionalCEX = Math.min(dexDeficit, cexSurplus)
      result.cex[cexAssetID].calculation.rebalanceAdjustment = additionalCEX
      result.cex[cexAssetID].amount += additionalCEX
      if (cexSurplus >= dexDeficit) result.dex[dexAssetID].status = 'sufficient-with-rebalance'
    }

    if (canRebalance && cexSurplus < 0 && dexSurplus > 0) {
      const cexDeficit = -cexSurplus
      const additionalDEX = Math.min(cexDeficit, dexSurplus)
      result.dex[dexAssetID].calculation.rebalanceAdjustment = additionalDEX
      result.dex[dexAssetID].amount += additionalDEX
      if (dexSurplus >= cexDeficit) result.cex[cexAssetID].status = 'sufficient-with-rebalance'
    }
  }

  if (state.botConfig.cexName) {
    rebalance(state.dexMarket.baseID, cexBaseID, dexBaseSurplus, cexBaseSurplus)
    rebalance(state.dexMarket.quoteID, cexQuoteID, dexQuoteSurplus, cexQuoteSurplus)
  }

  return result
}

export function requiredDexAssets (botConfigState: BotConfigState) : number[] {
  const { dexMarket: { baseID, quoteID }, botConfig, botConfig: { cexBaseID, cexQuoteID } } = botConfigState
  const cexRebalance = !!botConfig.autoRebalance && !botConfig.autoRebalance.internalOnly

  const assetIDs = [baseID, quoteID]
  const addAssetID = (assetID: number) => {
    if (!assetIDs.includes(assetID)) assetIDs.push(assetID)
  }

  const baseAsset = app().assets[baseID]
  const baseAssetFeeID = baseAsset.token ? baseAsset.token.parentID : baseID
  addAssetID(baseAssetFeeID)

  const quoteAsset = app().assets[quoteID]
  const quoteAssetFeeID = quoteAsset.token ? quoteAsset.token.parentID : quoteID
  addAssetID(quoteAssetFeeID)

  if (baseID !== cexBaseID && cexRebalance) {
    const cexBaseAsset = app().assets[cexBaseID]
    const cexBaseAssetFeeID = cexBaseAsset.token ? cexBaseAsset.token.parentID : cexBaseID
    addAssetID(cexBaseAssetFeeID)
  }

  if (quoteID !== cexQuoteID && cexRebalance) {
    const cexQuoteAsset = app().assets[cexQuoteID]
    const cexQuoteAssetFeeID = cexQuoteAsset.token ? cexQuoteAsset.token.parentID : cexQuoteID
    addAssetID(cexQuoteAssetFeeID)
  }

  return assetIDs
}

// toAllocateRunning calculates the quick allocations for a running bot.
export function toAllocateRunning (botConfigState: BotConfigState, runStats: RunStats) : AllocationResult {
  const result = requiredFunds(botConfigState)

  const dexAssetIDs = requiredDexAssets(botConfigState)
  const cexAssetIDs = [botConfigState.botConfig.cexBaseID, botConfigState.botConfig.cexQuoteID]

  const totalBotBalance = (source: 'cex' | 'dex', assetID: number) => {
    let bals
    if (source === 'dex') {
      bals = runStats.dexBalances[assetID] ?? { available: 0, locked: 0, pending: 0, reserved: 0 }
    } else {
      bals = runStats.cexBalances[assetID] ?? { available: 0, locked: 0, pending: 0, reserved: 0 }
    }
    return bals.available + bals.locked + bals.pending + bals.reserved
  }

  let dexBaseSurplus = 0
  let dexQuoteSurplus = 0
  let cexBaseSurplus = 0
  let cexQuoteSurplus = 0

  for (const assetID of dexAssetIDs) {
    const runningBotTotal = totalBotBalance('dex', assetID)
    const runningBotAvailable = runStats.dexBalances[assetID]?.available ?? 0
    result.dex[assetID].calculation.runningBotTotal = runningBotTotal
    result.dex[assetID].calculation.runningBotAvailable = runningBotAvailable
    result.dex[assetID].calculation.available = botConfigState.availableDEXBalances[assetID] ?? 0

    const dexTotalAvailable = runningBotTotal + result.dex[assetID].calculation.available
    const surplus = dexTotalAvailable - result.dex[assetID].calculation.totalRequired

    if (surplus >= 0) {
      result.dex[assetID].amount = result.dex[assetID].calculation.totalRequired - runningBotTotal
      if (result.dex[assetID].amount < 0) result.dex[assetID].amount = -Math.min(-result.dex[assetID].amount, runningBotAvailable)
    } else {
      result.dex[assetID].status = 'insufficient'
      result.dex[assetID].amount = result.dex[assetID].calculation.available
    }

    if (assetID === botConfigState.dexMarket.baseID) dexBaseSurplus = surplus
    if (assetID === botConfigState.dexMarket.quoteID) dexQuoteSurplus = surplus
  }

  if (botConfigState.botConfig.cexName) {
    for (const assetID of cexAssetIDs) {
      const runningBotTotal = totalBotBalance('cex', assetID)
      const runningBotAvailable = runStats.cexBalances[assetID]?.available ?? 0
      result.cex[assetID].calculation.runningBotTotal = runningBotTotal
      result.cex[assetID].calculation.runningBotAvailable = runningBotAvailable
      result.cex[assetID].calculation.available = botConfigState.availableCEXBalances?.[assetID] ?? 0

      const cexTotalAvailable = runningBotTotal + result.cex[assetID].calculation.available
      const surplus = cexTotalAvailable - result.cex[assetID].calculation.totalRequired

      if (surplus >= 0) {
        result.cex[assetID].amount = result.cex[assetID].calculation.totalRequired - runningBotTotal
        if (result.cex[assetID].amount < 0) result.cex[assetID].amount = -Math.min(-result.cex[assetID].amount, runningBotAvailable)
      } else {
        result.cex[assetID].status = 'insufficient'
        result.cex[assetID].amount = result.cex[assetID].calculation.available
      }

      if (assetID === botConfigState.botConfig.cexBaseID) cexBaseSurplus = surplus
      if (assetID === botConfigState.botConfig.cexQuoteID) cexQuoteSurplus = surplus
    }
  }

  const canRebalance = !!botConfigState.botConfig.autoRebalance && !botConfigState.botConfig.autoRebalance.internalOnly

  const rebalance = (dexAssetID: number, cexAssetID: number, dexSurplus: number, cexSurplus: number) => {
    if (canRebalance && dexSurplus < 0 && cexSurplus > 0) {
      const dexDeficit = -dexSurplus
      const additionalCEX = Math.min(dexDeficit, cexSurplus)
      result.cex[cexAssetID].calculation.rebalanceAdjustment = additionalCEX
      result.cex[cexAssetID].amount += additionalCEX
      if (cexSurplus >= dexDeficit) result.dex[dexAssetID].status = 'sufficient-with-rebalance'
    }

    if (canRebalance && cexSurplus < 0 && dexSurplus > 0) {
      const cexDeficit = -cexSurplus
      const additionalDEX = Math.min(cexDeficit, dexSurplus)
      result.dex[dexAssetID].calculation.rebalanceAdjustment = additionalDEX
      result.dex[dexAssetID].amount += additionalDEX
      if (dexSurplus >= cexDeficit) result.cex[cexAssetID].status = 'sufficient-with-rebalance'
    }
  }

  if (botConfigState.botConfig.cexName) {
    rebalance(botConfigState.dexMarket.baseID, botConfigState.botConfig.cexBaseID, dexBaseSurplus, cexBaseSurplus)
    rebalance(botConfigState.dexMarket.quoteID, botConfigState.botConfig.cexQuoteID, dexQuoteSurplus, cexQuoteSurplus)
  }

  return result
}
