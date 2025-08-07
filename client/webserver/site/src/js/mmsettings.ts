import {
  PageElement,
  BotConfig,
  OrderPlacement,
  app,
  Spot,
  MarketReport,
  OrderOption,
  CEXConfig,
  BasicMarketMakingConfig,
  ArbMarketMakingConfig,
  SimpleArbConfig,
  ArbMarketMakingPlacement,
  ExchangeBalance,
  MarketMakingStatus,
  MMCEXStatus,
  BalanceNote,
  ApprovalStatus,
  SupportedAsset,
  StartConfig,
  MMBotStatus,
  RunStats,
  UIConfig,
  UnitInfo,
  AutoRebalanceConfig,
  BotBalanceAllocation,
  MultiHopCfg
} from './registry'
import Doc, {
  NumberInput,
  MiniSlider,
  IncrementalInput,
  toFourSigFigs,
  toPrecision,
  parseFloatDefault
} from './doc'
import State from './state'
import BasePage from './basepage'
import { setOptionTemplates } from './opts'
import {
  MM,
  CEXDisplayInfos,
  botTypeBasicArb,
  botTypeArbMM,
  botTypeBasicMM,
  runningBotInventory,
  setMarketElements,
  setCexElements,
  calculateQuoteLot,
  PlacementsChart,
  liveBotConfig,
  GapStrategyMultiplier,
  GapStrategyAbsolute,
  GapStrategyAbsolutePlus,
  GapStrategyPercent,
  GapStrategyPercentPlus
} from './mmutil'
import { Forms, bind as bindForm, NewWalletForm, TokenApprovalForm, DepositAddress, CEXConfigurationForm } from './forms'
import * as intl from './locales'
import * as OrderUtil from './orderutil'

const specLK = 'lastMMSpecs'
const lastBotsLK = 'lastBots'
const lastArbExchangeLK = 'lastArbExchange'
const arbMMRowCacheKey = 'arbmm'

const defaultDriftTolerance = {
  value: 0.002,
  minV: 0,
  maxV: 0.02,
  range: 0.02,
  prec: 5
}
const defaultOrderPersistence = {
  value: 20,
  minV: 0,
  maxV: 40, // 10 minutes @ 15 second epochs
  range: 40,
  prec: 0
}
const defaultProfit = {
  prec: 3,
  value: 0.01,
  minV: 0.001,
  maxV: 0.1,
  range: 0.1 - 0.001
}
const defaultLevelSpacing = {
  prec: 3,
  value: 0.005,
  minV: 0.001,
  maxV: 0.02,
  range: 0.02 - 0.0001
}
const defaultMatchBuffer = {
  value: 0,
  prec: 3,
  minV: 0,
  maxV: 1,
  range: 1
}
const defaultLevelsPerSide = {
  prec: 0,
  inc: 1,
  value: 1,
  minV: 1
}
const defaultLotsPerLevel = {
  prec: 0,
  value: 1,
  minV: 1,
  usdIncrement: 100
}
const defaultUSDPerSide = {
  prec: 2
}

function defaultUIConfig (baseMinWithdraw: number, quoteMinWithdraw: number, botType: string) : UIConfig {
  const buffer = botType === botTypeBasicArb ? 1 : 0
  return {
    allocation: {
      dex: {},
      cex: {}
    },
    quickBalance: {
      buysBuffer: buffer,
      sellsBuffer: buffer,
      buyFeeReserve: 0,
      sellFeeReserve: 0,
      slippageBuffer: 5
    },
    usingQuickBalance: true,
    internalTransfers: true,
    baseMinTransfer: baseMinWithdraw,
    quoteMinTransfer: quoteMinWithdraw,
    cexRebalance: false
  }
}

function defaultMultiHopCfg (possibleArbMkts: string | MultiHopArbMarket[] | undefined) : MultiHopCfg | undefined {
  if (!possibleArbMkts) return undefined
  if (typeof possibleArbMkts === 'string') return undefined
  return {
    baseAssetMarket: possibleArbMkts[0].BaseMarket,
    quoteAssetMarket: possibleArbMkts[0].QuoteMarket
  }
}

const defaultMarketMakingConfig: ConfigState = {
  gapStrategy: GapStrategyPercentPlus,
  sellPlacements: [],
  buyPlacements: [],
  driftTolerance: defaultDriftTolerance.value,
  profit: 0.02,
  orderPersistence: defaultOrderPersistence.value
} as any as ConfigState

// cexButton stores parts of a CEX selection button.
interface cexButton {
  name: string
  div: PageElement
  tmpl: Record<string, PageElement>
}

/*
 * ConfigState is an amalgamation of BotConfig, ArbMarketMakingCfg, and
 * BasicMarketMakingCfg. ConfigState tracks the global state of the options
 * presented on the page, with a single field for each option / control element.
 * ConfigState is necessary because there are duplicate fields in the various
 * config structs, and the placement types are not identical.
 */
interface ConfigState {
  gapStrategy: string
  profit: number
  driftTolerance: number
  orderPersistence: number // epochs
  buyPlacements: OrderPlacement[]
  sellPlacements: OrderPlacement[]
  baseOptions: Record<string, string>
  quoteOptions: Record<string, string>
  uiConfig: UIConfig
  multiHop?: MultiHopCfg
}

interface BotSpecs {
  host: string
  baseID: number
  quoteID: number
  botType: string
  cexName?: string
}

interface MarketRow {
  tr: PageElement
  tmpl: Record<string, PageElement>
  host: string
  name: string
  baseID: number
  quoteID: number
  arbs: string[]
  spot: Spot
}

interface UIOpts {
  usingUSDPerSide?: boolean
}

interface MultiHopArbMarket {
  BaseMarket: [number, number]
  QuoteMarket: [number, number]
  BridgeAsset: number
}

export default class MarketMakerSettingsPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  opts: UIOpts
  runningBot: boolean
  newWalletForm: NewWalletForm
  approveTokenForm: TokenApprovalForm
  walletAddrForm: DepositAddress
  cexConfigForm: CEXConfigurationForm
  currentMarket: string
  originalConfig: ConfigState
  updatedConfig: ConfigState
  creatingNewBot: boolean
  marketReport: MarketReport
  qcProfit: NumberInput
  qcProfitSlider: MiniSlider
  qcLevelSpacing: NumberInput
  qcLevelSpacingSlider: MiniSlider
  qcMatchBuffer: NumberInput
  qcMatchBufferSlider: MiniSlider
  qcLevelsPerSide: IncrementalInput
  qcLotsPerLevel: IncrementalInput
  qcUSDPerSide: IncrementalInput
  cexBaseBalance: ExchangeBalance
  cexQuoteBalance: ExchangeBalance
  specs: BotSpecs
  dexMktID: string
  arbMkt: string | MultiHopArbMarket
  multiHopMarkets: MultiHopArbMarket[]
  formSpecs: BotSpecs
  formCexes: Record<string, cexButton>
  placementsCache: Record<string, [OrderPlacement[], OrderPlacement[]]>
  botTypeSelectors: PageElement[]
  marketRows: MarketRow[]
  lotsPerLevelIncrement: number
  placementsChart: PlacementsChart
  baseSettings: WalletSettings
  quoteSettings: WalletSettings
  driftTolerance: NumberInput
  driftToleranceSlider: MiniSlider
  orderPersistence: NumberInput
  orderPersistenceSlider: MiniSlider
  availableDEXBalances: Record<number, number>
  availableCEXBalances: Record<number, number>
  buyBufferSlider: MiniSlider
  buyBufferInput: NumberInput
  sellBufferSlider: MiniSlider
  sellBufferInput: NumberInput
  slippageBufferSlider: MiniSlider
  slippageBufferInput: NumberInput
  buyFeeReserveSlider: MiniSlider
  buyFeeReserveInput: NumberInput
  sellFeeReserveSlider: MiniSlider
  sellFeeReserveInput: NumberInput
  baseMinTransferSlider: MiniSlider
  baseMinTransferInput: NumberInput
  quoteMinTransferSlider: MiniSlider
  quoteMinTransferInput: NumberInput
  baseDexBalanceSlider: MiniSlider
  baseDexBalanceInput: NumberInput
  quoteDexBalanceSlider: MiniSlider
  quoteDexBalanceInput: NumberInput
  baseFeeBalanceSlider: MiniSlider
  baseFeeBalanceInput: NumberInput
  quoteFeeBalanceSlider: MiniSlider
  quoteFeeBalanceInput: NumberInput
  baseCexBalanceSlider: MiniSlider
  baseCexBalanceInput: NumberInput
  quoteCexBalanceSlider: MiniSlider
  quoteCexBalanceInput: NumberInput
  fundingFeesCache: Record<string, [number, number]>

  constructor (main: HTMLElement, specs: BotSpecs) {
    super()

    this.placementsCache = {}
    this.fundingFeesCache = {}
    this.opts = {}

    const page = this.page = Doc.idDescendants(main)

    this.forms = new Forms(page.forms, {
      closed: () => {
        if (!this.specs?.host || !this.specs?.botType) app().loadPage('mm')
      }
    })

    this.placementsChart = new PlacementsChart(page.placementsChart)
    this.approveTokenForm = new TokenApprovalForm(page.approveTokenForm, () => { this.submitBotType() })
    this.walletAddrForm = new DepositAddress(page.walletAddrForm)
    this.cexConfigForm = new CEXConfigurationForm(page.cexConfigForm, (cexName: string) => this.cexConfigured(cexName))
    page.quoteSettings = page.baseSettings.cloneNode(true) as PageElement
    page.walletSettingsBox.appendChild(page.quoteSettings)
    this.baseSettings = new WalletSettings(this, page.baseSettings, () => { this.updateAllocations() })
    this.quoteSettings = new WalletSettings(this, page.quoteSettings, () => { this.updateAllocations() })

    app().headerSpace.appendChild(page.mmTitle)

    setOptionTemplates(page)
    Doc.cleanTemplates(
      page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl, page.placementRowTmpl,
      page.oracleTmpl, page.cexOptTmpl, page.arbBttnTmpl, page.marketRowTmpl, page.needRegTmpl)
    page.baseSettings.removeAttribute('id') // don't remove from layout

    Doc.bind(page.resetButton, 'click', () => { this.setOriginalValues() })
    Doc.bind(page.updateButton, 'click', () => { this.updateSettings() })
    Doc.bind(page.updateStartButton, 'click', () => { this.saveSettingsAndStart() })
    Doc.bind(page.updateRunningButton, 'click', () => { this.updateSettings() })
    Doc.bind(page.deleteBttn, 'click', () => { this.delete() })
    bindForm(page.botTypeForm, page.botTypeSubmit, () => { this.submitBotType() })
    Doc.bind(page.noMarketBttn, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.botTypeHeader, 'click', () => { this.reshowBotTypeForm() })
    Doc.bind(page.botTypeChangeMarket, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.marketHeader, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.marketFilterInput, 'input', () => { this.sortMarketRows() })
    Doc.bind(page.internalOnlyRadio, 'change', () => { this.internalOnlyChanged() })
    Doc.bind(page.externalTransfersRadio, 'change', () => { this.externalTransfersChanged() })
    Doc.bind(page.switchToAdvanced, 'click', () => { this.showAdvancedConfig() })
    Doc.bind(page.switchToQuickConfig, 'click', () => { this.switchToQuickConfig() })
    Doc.bind(page.qcMatchBuffer, 'change', () => { this.matchBufferChanged() })
    Doc.bind(page.switchToUSDPerSide, 'click', () => { this.changeSideCommitmentDialog() })
    Doc.bind(page.switchToLotsPerLevel, 'click', () => { this.changeSideCommitmentDialog() })
    Doc.bind(page.manuallyAllocateBttn, 'click', () => { this.setAllocationTechnique(false) })
    Doc.bind(page.quickConfigBttn, 'click', () => { this.setAllocationTechnique(true) })
    Doc.bind(page.enableRebalance, 'change', () => { this.autoRebalanceChanged() })

    // Gap Strategy
    Doc.bind(page.gapStrategySelect, 'change', () => {
      if (!page.gapStrategySelect.value) return
      const gapStrategy = page.gapStrategySelect.value
      this.clearPlacements(this.updatedConfig.gapStrategy)
      this.loadCachedPlacements(gapStrategy)
      this.updatedConfig.gapStrategy = gapStrategy
      this.setGapFactorLabels(gapStrategy)
      this.updateModifiedMarkers()
    })
    Doc.bind(page.bridgeAssetSelect, 'change', () => {
      if (!page.bridgeAssetSelect.value) return
      const bridgeAsset = page.bridgeAssetSelect.value
      const bridgeAssetID = parseInt(bridgeAsset)
      const multiHopMkts = this.multiHopMarkets.find(m => m.BridgeAsset === bridgeAssetID)
      if (!multiHopMkts) {
        console.error('Bridge asset not found in multi-hop markets', bridgeAssetID)
        return
      }
      this.updatedConfig.multiHop = {
        baseAssetMarket: multiHopMkts.BaseMarket,
        quoteAssetMarket: multiHopMkts.QuoteMarket
      }
    })

    // Buy/Sell placements
    Doc.bind(page.addBuyPlacementBtn, 'click', () => {
      this.addPlacement(true, null)
      page.addBuyPlacementLots.value = ''
      page.addBuyPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
      this.placementsChart.render()
      this.updateAllocations()
    })
    Doc.bind(page.addSellPlacementBtn, 'click', () => {
      this.addPlacement(false, null)
      page.addSellPlacementLots.value = ''
      page.addSellPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
      this.placementsChart.render()
      this.updateAllocations()
    })

    this.driftTolerance = new NumberInput(page.driftToleranceInput, {
      prec: defaultDriftTolerance.prec - 2, // converting to percent for display
      sigFigs: true,
      min: 0,
      changed: (rawV: number) => {
        const { minV, range, prec } = defaultDriftTolerance
        const [v] = toFourSigFigs(rawV / 100, prec)
        this.driftToleranceSlider.setValue((v - minV) / range)
        this.updatedConfig.driftTolerance = v
      }
    })

    this.driftToleranceSlider = new MiniSlider(page.driftToleranceSlider, (r: number) => {
      const { minV, range, prec } = defaultDriftTolerance
      const [v] = toFourSigFigs(minV + r * range, prec)
      this.updatedConfig.driftTolerance = v
      this.driftTolerance.setValue(v * 100)
    })

    this.orderPersistence = new NumberInput(page.orderPersistence, {
      changed: (v: number) => {
        const { minV, range } = defaultOrderPersistence
        this.updatedConfig.orderPersistence = v
        this.orderPersistenceSlider.setValue((v - minV) / range)
      }
    })

    this.orderPersistenceSlider = new MiniSlider(page.orderPersistenceSlider, (r: number) => {
      const { minV, range, prec } = defaultOrderPersistence
      const rawV = minV + r * range
      const [v] = toPrecision(rawV, prec)
      this.updatedConfig.orderPersistence = v
      this.orderPersistence.setValue(v)
    })

    this.qcProfit = new NumberInput(page.qcProfit, {
      prec: defaultProfit.prec - 2, // converting to percent
      sigFigs: true,
      min: defaultProfit.minV * 100,
      changed: (vPct: number) => {
        const { minV, range } = defaultProfit
        const v = vPct / 100
        this.updatedConfig.profit = v
        page.profitInput.value = this.qcProfit.input.value
        this.qcProfitSlider.setValue((v - minV) / range)
        this.quickConfigUpdated()
      }
    })

    this.qcProfitSlider = new MiniSlider(page.qcProfitSlider, (r: number) => {
      const { minV, range, prec } = defaultProfit
      const [v] = toFourSigFigs((minV + r * range) * 100, prec)
      this.updatedConfig.profit = v / 100
      this.qcProfit.setValue(v)
      page.profitInput.value = this.qcProfit.input.value
      this.quickConfigUpdated()
    })

    this.qcLevelSpacing = new NumberInput(page.qcLevelSpacing, {
      prec: defaultLevelSpacing.prec - 2, // converting to percent
      sigFigs: true,
      min: defaultLevelSpacing.minV * 100,
      changed: (vPct: number) => {
        const { minV, range } = defaultLevelSpacing
        this.qcLevelSpacingSlider.setValue((vPct / 100 - minV) / range)
        this.quickConfigUpdated()
      }
    })

    this.qcLevelSpacingSlider = new MiniSlider(page.qcLevelSpacingSlider, (r: number) => {
      const { minV, range } = defaultLevelSpacing
      this.qcLevelSpacing.setValue(minV + r * range * 100)
      this.quickConfigUpdated()
    })

    this.qcMatchBuffer = new NumberInput(page.qcMatchBuffer, {
      prec: defaultMatchBuffer.prec - 2, // converting to percent
      sigFigs: true,
      min: defaultMatchBuffer.minV * 100,
      changed: (vPct: number) => {
        const { minV, range } = defaultMatchBuffer
        this.qcMatchBufferSlider.setValue((vPct / 100 - minV) / range)
        this.quickConfigUpdated()
      }
    })

    this.qcMatchBufferSlider = new MiniSlider(page.qcMatchBufferSlider, (r: number) => {
      const { minV, range } = defaultMatchBuffer
      this.qcMatchBuffer.setValue(minV + r * range * 100)
      this.quickConfigUpdated()
    })

    this.qcLevelsPerSide = new IncrementalInput(page.qcLevelsPerSide, {
      prec: defaultLevelsPerSide.prec,
      min: defaultLevelsPerSide.minV,
      inc: defaultLevelsPerSide.inc,
      changed: (v: number) => {
        this.qcUSDPerSide.setValue(this.lotSizeUSD() * v * this.qcLotsPerLevel.value())
        this.quickConfigUpdated()
      }
    })

    this.qcLotsPerLevel = new IncrementalInput(page.qcLotsPerLevel, {
      prec: defaultLotsPerLevel.prec,
      min: defaultLotsPerLevel.minV,
      inc: 1, // set showQuickConfig
      changed: (v: number) => {
        this.qcUSDPerSide.setValue(this.lotSizeUSD() * v * this.qcLevelsPerSide.value())
        page.qcUSDPerSideEcho.textContent = this.qcUSDPerSide.input.value as string
        this.quickConfigUpdated()
      },
      set: (v: number) => {
        const [, s] = toFourSigFigs(v * this.qcLevelsPerSide.value() * this.lotSizeUSD(), 2)
        page.qcUSDPerSideEcho.textContent = s
        page.qcLotsPerLevelEcho.textContent = s
      }
    })

    this.qcUSDPerSide = new IncrementalInput(page.qcUSDPerSide, {
      prec: defaultUSDPerSide.prec,
      min: 1, // changed by showQuickConfig
      inc: 1, // changed by showQuickConfig
      changed: (v: number) => {
        this.qcLotsPerLevel.setValue(v / this.qcLevelsPerSide.value() / this.lotSizeUSD())
        page.qcLotsPerLevelEcho.textContent = this.qcLotsPerLevel.input.value as string
        this.quickConfigUpdated()
      },
      set: (v: number, s: string) => {
        page.qcUSDPerSideEcho.textContent = s
        page.qcLotsPerLevelEcho.textContent = String(Math.round(v / this.lotSizeUSD()))
      }
    })

    this.buyBufferSlider = new MiniSlider(page.buyBufferSlider, (amt: number) => this.quickBalanceSliderChanged(amt, 'buyBuffer'))
    this.buyBufferInput = new NumberInput(page.buyBuffer, { prec: 0, min: 0, changed: (amt: number) => this.quickBalanceInputChanged(amt, 'buyBuffer') })
    this.sellBufferSlider = new MiniSlider(page.sellBufferSlider, (amt: number) => this.quickBalanceSliderChanged(amt, 'sellBuffer'))
    this.sellBufferInput = new NumberInput(page.sellBuffer, { prec: 0, min: 0, changed: (amt: number) => this.quickBalanceInputChanged(amt, 'sellBuffer') })
    this.slippageBufferSlider = new MiniSlider(page.slippageBufferSlider, (amt: number) => this.quickBalanceSliderChanged(amt, 'slippageBuffer'))
    this.slippageBufferInput = new NumberInput(page.slippageBuffer, { prec: 3, min: 0, changed: (amt: number) => this.quickBalanceInputChanged(amt, 'slippageBuffer') })
    this.buyFeeReserveSlider = new MiniSlider(page.buyFeeReserveSlider, (amt: number) => this.quickBalanceSliderChanged(amt, 'buyFeeReserve'))
    this.buyFeeReserveInput = new NumberInput(page.buyFeeReserve, { prec: 0, min: 0, changed: (amt: number) => this.quickBalanceInputChanged(amt, 'buyFeeReserve') })
    this.sellFeeReserveSlider = new MiniSlider(page.sellFeeReserveSlider, (amt: number) => this.quickBalanceSliderChanged(amt, 'sellFeeReserve'))
    this.sellFeeReserveInput = new NumberInput(page.sellFeeReserve, { prec: 0, min: 0, changed: (amt: number) => this.quickBalanceInputChanged(amt, 'sellFeeReserve') })
    this.baseMinTransferSlider = new MiniSlider(page.baseMinTransferSlider, (amt: number) => this.minTransferSliderChanged(amt, 'base'))
    this.baseMinTransferInput = new NumberInput(page.baseMinTransfer, { prec: 0, min: 0, changed: (amt: number) => this.minTransferInputChanged(amt, 'base') })
    this.quoteMinTransferSlider = new MiniSlider(page.quoteMinTransferSlider, (amt: number) => this.minTransferSliderChanged(amt, 'quote'))
    this.quoteMinTransferInput = new NumberInput(page.quoteMinTransfer, { prec: 0, min: 0, changed: (amt: number) => this.minTransferInputChanged(amt, 'quote') })
    this.baseDexBalanceSlider = new MiniSlider(page.baseDexBalanceSlider, (amt: number) => this.balanceSliderChanged(amt, 'base', 'dex'))
    this.baseDexBalanceInput = new NumberInput(page.baseDexBalance, { prec: 0, min: 0, changed: (amt: number) => this.balanceInputChanged(amt, 'base', 'dex') })
    this.quoteDexBalanceSlider = new MiniSlider(page.quoteDexBalanceSlider, (amt: number) => this.balanceSliderChanged(amt, 'quote', 'dex'))
    this.quoteDexBalanceInput = new NumberInput(page.quoteDexBalance, { prec: 0, min: 0, changed: (amt: number) => this.balanceInputChanged(amt, 'quote', 'dex') })
    this.baseFeeBalanceSlider = new MiniSlider(page.baseFeeBalanceSlider, (amt: number) => this.balanceSliderChanged(amt, 'baseFee', 'dex'))
    this.baseFeeBalanceInput = new NumberInput(page.baseFeeBalance, { prec: 0, min: 0, changed: (amt: number) => this.balanceInputChanged(amt, 'baseFee', 'dex') })
    this.quoteFeeBalanceSlider = new MiniSlider(page.quoteFeeBalanceSlider, (amt: number) => this.balanceSliderChanged(amt, 'quoteFee', 'dex'))
    this.quoteFeeBalanceInput = new NumberInput(page.quoteFeeBalance, { prec: 0, min: 0, changed: (amt: number) => this.balanceInputChanged(amt, 'quoteFee', 'dex') })
    this.baseCexBalanceSlider = new MiniSlider(page.baseCexBalanceSlider, (amt: number) => this.balanceSliderChanged(amt, 'base', 'cex'))
    this.baseCexBalanceInput = new NumberInput(page.baseCexBalance, { prec: 0, min: 0, changed: (amt: number) => this.balanceInputChanged(amt, 'base', 'cex') })
    this.quoteCexBalanceSlider = new MiniSlider(page.quoteCexBalanceSlider, (amt: number) => this.balanceSliderChanged(amt, 'quote', 'cex'))
    this.quoteCexBalanceInput = new NumberInput(page.quoteCexBalance, { prec: 0, min: 0, changed: (amt: number) => this.balanceInputChanged(amt, 'quote', 'cex') })

    const maybeSubmitBuyRow = (e: KeyboardEvent) => {
      if (e.key !== 'Enter') return
      if (
        !isNaN(parseFloat(page.addBuyPlacementGapFactor.value || '')) &&
        !isNaN(parseFloat(page.addBuyPlacementLots.value || ''))
      ) {
        page.addBuyPlacementBtn.click()
      }
    }
    Doc.bind(page.addBuyPlacementGapFactor, 'keyup', (e: KeyboardEvent) => { maybeSubmitBuyRow(e) })
    Doc.bind(page.addBuyPlacementLots, 'keyup', (e: KeyboardEvent) => { maybeSubmitBuyRow(e) })

    const maybeSubmitSellRow = (e: KeyboardEvent) => {
      if (e.key !== 'Enter') return
      if (
        !isNaN(parseFloat(page.addSellPlacementGapFactor.value || '')) &&
        !isNaN(parseFloat(page.addSellPlacementLots.value || ''))
      ) {
        page.addSellPlacementBtn.click()
      }
    }
    Doc.bind(page.addSellPlacementGapFactor, 'keyup', (e: KeyboardEvent) => { maybeSubmitSellRow(e) })
    Doc.bind(page.addSellPlacementLots, 'keyup', (e: KeyboardEvent) => { maybeSubmitSellRow(e) })

    Doc.bind(page.profitInput, 'change', () => {
      Doc.hide(page.profitInputErr)
      const showError = (errID: string) => {
        Doc.show(page.profitInputErr)
        page.profitInputErr.textContent = intl.prep(errID)
      }
      const profit = parseFloat(page.profitInput.value || '') / 100
      if (isNaN(profit)) return showError(intl.ID_INVALID_VALUE)
      if (profit === 0) return showError(intl.ID_NO_ZERO)
      this.updatedConfig.profit = profit
      this.updateModifiedMarkers()
    })

    this.botTypeSelectors = Doc.applySelector(page.botTypeForm, '[data-bot-type]')
    for (const div of this.botTypeSelectors) {
      Doc.bind(div, 'click', () => {
        if (div.classList.contains('disabled')) return
        Doc.hide(page.botTypeErr)
        page.cexSelection.classList.toggle('disabled', div.dataset.botType === botTypeBasicMM)
        this.setBotTypeSelected(div.dataset.botType as string)
      })
    }

    this.newWalletForm = new NewWalletForm(
      page.newWalletForm,
      async () => {
        await app().fetchUser()
        this.submitBotType()
      }
    )

    app().registerNoteFeeder({
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) }
    })

    this.initialize(specs)
  }

  unload () {
    this.forms.exit()
  }

  async initialize (specs?: BotSpecs) {
    this.setupCEXes()
    this.initializeMarketRows()

    const isRefresh = specs && Object.keys(specs).length === 0
    if (isRefresh) specs = State.fetchLocal(specLK)
    if (!specs || !app().walletMap[specs.baseID] || !app().walletMap[specs.quoteID]) {
      this.showMarketSelectForm()
      return
    }

    // If we have specs specifying only a market, make sure the cex name and
    // bot type are set.
    if (specs && !specs.botType) {
      const botCfg = liveBotConfig(specs.host, specs.baseID, specs.quoteID)
      specs.cexName = botCfg?.cexName ?? ''
      specs.botType = botTypeBasicMM
      if (botCfg?.arbMarketMakingConfig) specs.botType = botTypeArbMM
      else if (botCfg?.simpleArbConfig) specs.botType = botTypeBasicArb
    }

    // Must be a reconfig.
    this.specs = specs
    await this.fetchCEXBalances(specs)
    this.configureUI()
  }

  // clampOriginalAllocations sets the allocations to be within the valid range
  // based on the available balances.
  clampOriginalAllocations (uiConfig: UIConfig) {
    const { baseID, quoteID, baseFeeAssetID, quoteFeeAssetID } = this.walletStuff()
    const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeAssetID, quoteFeeAssetID]))
    for (const assetID of assetIDs) {
      const [dexMin, dexMax] = this.validManualBalanceRange(assetID, 'dex', false)
      uiConfig.allocation.dex[assetID] = Math.min(Math.max(uiConfig.allocation.dex[assetID], dexMin), dexMax)
      if (this.specs.cexName) {
        const [cexMin, cexMax] = this.validManualBalanceRange(assetID, 'cex', false)
        uiConfig.allocation.cex[assetID] = Math.min(Math.max(uiConfig.allocation.cex[assetID], cexMin), cexMax)
      }
    }
  }

  async setAvailableBalances () {
    const { specs } = this
    const availableBalances = await MM.availableBalances({ host: specs.host, baseID: specs.baseID, quoteID: specs.quoteID }, specs.cexName)
    this.availableDEXBalances = availableBalances.dexBalances
    this.availableCEXBalances = availableBalances.cexBalances
  }

  minWithdrawals (arbMkt: string | MultiHopArbMarket | undefined, baseID: number, quoteID: number, cexName: string | undefined): { minBaseWithdraw: number, minQuoteWithdraw: number } {
    if (!cexName || !arbMkt) {
      return { minBaseWithdraw: 0, minQuoteWithdraw: 0 }
    }

    const cex = app().mmStatus.cexes[cexName]
    if (typeof arbMkt === 'string') {
      const mkt = cex.markets[arbMkt]
      return { minBaseWithdraw: mkt.baseMinWithdraw, minQuoteWithdraw: mkt.quoteMinWithdraw }
    }

    const mktToSymbol = ([baseID, quoteID]: [number, number]) : string => {
      const assetToSymbol = (assetID: number) : string => app().assets[assetID].symbol
      return `${assetToSymbol(baseID)}_${assetToSymbol(quoteID)}`
    }
    const baseMkt = cex.markets[mktToSymbol(arbMkt.BaseMarket)]
    const quoteMkt = cex.markets[mktToSymbol(arbMkt.QuoteMarket)]
    return {
      minBaseWithdraw: arbMkt.BaseMarket[0] === baseID ? baseMkt.baseMinWithdraw : baseMkt.quoteMinWithdraw,
      minQuoteWithdraw: arbMkt.QuoteMarket[0] === quoteID ? quoteMkt.baseMinWithdraw : quoteMkt.quoteMinWithdraw
    }
  }

  possibleArbMarkets (cexName: string | undefined, mmStatus: MarketMakingStatus, baseID: number, quoteID: number, baseSymbol: string, quoteSymbol: string, isArbMM: boolean) : string | MultiHopArbMarket[] | undefined {
    if (!cexName) {
      return undefined
    }

    const cexStatus = mmStatus.cexes[cexName]
    if (!cexStatus) {
      throw new Error(`CEX name not found in MM status: ${cexName}`)
    }

    const [supportsDirectArb, multiHopMarkets] = this.cexSupportsArbOnMarket(baseID, quoteID, cexStatus)
    if (!supportsDirectArb && multiHopMarkets.length === 0) {
      throw new Error(`CEX does not support arb on market: ${cexName} ${baseID} ${quoteID}`)
    }

    if (supportsDirectArb) {
      return `${baseSymbol}_${quoteSymbol}`
    }

    if (!isArbMM) {
      throw new Error(`Only arbMM bots can use multi-hop arb: ${cexName} ${baseID} ${quoteID}`)
    }

    return multiHopMarkets
  }

  originalMultiHopCfg (savedMultiHopCfg: MultiHopCfg | undefined, possibleArbMkts: string | MultiHopArbMarket[] | undefined) : MultiHopCfg | undefined {
    if (!possibleArbMkts || typeof possibleArbMkts === 'string') return undefined
    if (!savedMultiHopCfg) return { baseAssetMarket: possibleArbMkts[0].BaseMarket, quoteAssetMarket: possibleArbMkts[0].QuoteMarket }
    let foundSavedMarket = false
    const mktsEqual = (mkt1: [number, number], mkt2: [number, number]) => {
      return mkt1[0] === mkt2[0] && mkt1[1] === mkt2[1]
    }
    for (const mkt of possibleArbMkts) {
      if (mktsEqual(mkt.BaseMarket, savedMultiHopCfg.baseAssetMarket) && mktsEqual(mkt.QuoteMarket, savedMultiHopCfg.quoteAssetMarket)) {
        foundSavedMarket = true
        break
      }
    }
    if (!foundSavedMarket) return { baseAssetMarket: possibleArbMkts[0].BaseMarket, quoteAssetMarket: possibleArbMkts[0].QuoteMarket }
    return savedMultiHopCfg
  }

  // setOriginalConfigValues sets the initial values of the page's original config
  // based on the savedBotCfg. This should be called after the originalConfig
  // has been initialized with the default values.
  setOriginalConfigValues (savedBotCfg: BotConfig | undefined, possibleArbMkts: string | MultiHopArbMarket[] | undefined) {
    if (!savedBotCfg) return
    const { basicMarketMakingConfig: mmCfg, arbMarketMakingConfig: arbMMCfg, simpleArbConfig: arbCfg } = savedBotCfg
    const oldCfg = this.originalConfig

    // This is kinda sloppy, but we'll copy any relevant issues from the
    // old config into the originalConfig.
    const idx = savedBotCfg as { [k: string]: any } // typescript
    for (const [k, v] of Object.entries(savedBotCfg)) if (idx[k] !== undefined) idx[k] = v

    oldCfg.baseOptions = savedBotCfg.baseWalletOptions || {}
    oldCfg.quoteOptions = savedBotCfg.quoteWalletOptions || {}
    if (savedBotCfg.uiConfig) oldCfg.uiConfig = savedBotCfg.uiConfig
    if (this.runningBot && !savedBotCfg.uiConfig.usingQuickBalance) {
      // If the bot is running and we are allocating manually, initialize
      // the allocations to 0.
      oldCfg.uiConfig.allocation = { dex: {}, cex: {} }
    }
    this.clampOriginalAllocations(oldCfg.uiConfig)

    if (mmCfg) {
      oldCfg.buyPlacements = mmCfg.buyPlacements
      oldCfg.sellPlacements = mmCfg.sellPlacements
      oldCfg.driftTolerance = mmCfg.driftTolerance
      oldCfg.gapStrategy = mmCfg.gapStrategy
    } else if (arbMMCfg) {
      const { buyPlacements, sellPlacements } = arbMMCfg
      oldCfg.buyPlacements = Array.from(buyPlacements, (p: ArbMarketMakingPlacement) => { return { lots: p.lots, gapFactor: p.multiplier } })
      oldCfg.sellPlacements = Array.from(sellPlacements, (p: ArbMarketMakingPlacement) => { return { lots: p.lots, gapFactor: p.multiplier } })
      oldCfg.profit = arbMMCfg.profit
      oldCfg.driftTolerance = arbMMCfg.driftTolerance
      oldCfg.orderPersistence = arbMMCfg.orderPersistence
      oldCfg.multiHop = this.originalMultiHopCfg(arbMMCfg.multiHop, possibleArbMkts)
    } else if (arbCfg) {
      // TODO: expose maxActiveArbs
      oldCfg.profit = arbCfg.profitTrigger
      oldCfg.orderPersistence = arbCfg.numEpochsLeaveOpen
    }
  }

  async configureUI () {
    const { page, specs } = this
    const { host, baseID, quoteID, cexName, botType } = specs

    // Set the visibility of fee asset sections.
    this.fundingFeesCache = {}
    const { baseFeeAssetID, quoteFeeAssetID, bui, qui, baseFeeUI, quoteFeeUI } = this.walletStuff()
    const baseFeeNotTraded = baseFeeAssetID !== baseID && baseFeeAssetID !== quoteID
    const quoteFeeNotTraded = quoteFeeAssetID !== baseID && quoteFeeAssetID !== quoteID
    Doc.setVis(baseFeeNotTraded || quoteFeeNotTraded, page.buyFeeReserveSection, page.sellFeeReserveSection)
    Doc.setVis(baseFeeNotTraded, page.baseDexFeeBalanceSection)
    Doc.setVis(quoteFeeNotTraded && baseFeeAssetID !== quoteFeeAssetID, page.quoteDexFeeBalanceSection)

    // Get all assets, and hide page if any fiat rates are missing.
    const [{ symbol: baseSymbol, token: baseToken }, { symbol: quoteSymbol, token: quoteToken }] = [app().assets[baseID], app().assets[quoteID]]
    this.dexMktID = `${baseSymbol}_${quoteSymbol}`
    Doc.hide(page.botSettingsContainer, page.marketBox, page.resetButton, page.noMarket, page.missingFiatRates, page.bridgeAssetBox)
    if ([baseID, quoteID, baseToken?.parentID ?? baseID, quoteToken?.parentID ?? quoteID].some((assetID: number) => !app().fiatRatesMap[assetID])) {
      Doc.show(page.missingFiatRates)
      return
    }

    await this.setAvailableBalances()
    Doc.show(page.marketLoading)
    State.storeLocal(specLK, specs)

    // Allow deletion of bot if it is not running or we are not switching bot types.
    const mmStatus = app().mmStatus
    this.runningBot = botIsRunning(specs, mmStatus)
    Doc.setVis(this.runningBot, page.runningBotAllocationNote)
    let savedBotCfg = liveBotConfig(host, baseID, quoteID)
    if (savedBotCfg) {
      const oldBotType = savedBotCfg.arbMarketMakingConfig ? botTypeArbMM : savedBotCfg.basicMarketMakingConfig ? botTypeBasicMM : botTypeBasicArb
      if (oldBotType !== botType) savedBotCfg = undefined
    }
    Doc.setVis(savedBotCfg && !this.runningBot, page.deleteBttnBox)

    // Only allow changing the bot type if the bot is not running.
    page.marketHeader.classList.remove('hoverbg', 'pointer')
    page.botTypeHeader.classList.remove('hoverbg', 'pointer')
    if (!this.runningBot) {
      page.botTypeHeader.classList.add('hoverbg', 'pointer')
      page.marketHeader.classList.add('hoverbg', 'pointer')
    }

    // Check is this bot is able to arb on an exactly matching CEX market, needs
    // to use multi-hop, or is not able to arb at all.
    let possibleArbMkts: string | MultiHopArbMarket[] | undefined
    try {
      possibleArbMkts = this.possibleArbMarkets(cexName, mmStatus, baseID, quoteID, baseSymbol, quoteSymbol, botType === botTypeArbMM)
    } catch (e) {
      console.error(e)
      Doc.show(page.missingFiatRates)
      return
    }

    // Set default original config values
    if (typeof possibleArbMkts === 'string') this.arbMkt = possibleArbMkts
    else if (possibleArbMkts !== undefined) this.arbMkt = possibleArbMkts[0]
    const { minBaseWithdraw, minQuoteWithdraw } = this.minWithdrawals(this.arbMkt, baseID, quoteID, cexName)
    const oldCfg = this.originalConfig = Object.assign({}, defaultMarketMakingConfig, {
      baseOptions: this.defaultWalletOptions(baseID),
      quoteOptions: this.defaultWalletOptions(quoteID),
      buyPlacements: [],
      sellPlacements: [],
      multiHop: defaultMultiHopCfg(possibleArbMkts),
      uiConfig: defaultUIConfig(minBaseWithdraw, minQuoteWithdraw, botType)
    }) as ConfigState

    // Update original config values based on saved bot config
    this.creatingNewBot = !savedBotCfg
    this.setOriginalConfigValues(savedBotCfg, possibleArbMkts)

    // Setup the multi-hop market selection UI
    if (possibleArbMkts !== undefined && typeof possibleArbMkts !== 'string') { // MultiHopArbMarket[]
      this.multiHopMarkets = possibleArbMkts
      Doc.empty(page.bridgeAssetSelect)
      for (const market of possibleArbMkts) {
        const bridgeSymbol = app().assets[market.BridgeAsset].symbol.toUpperCase()
        const opt = document.createElement('option')
        opt.value = String(market.BridgeAsset)
        opt.textContent = bridgeSymbol
        if (!oldCfg.multiHop) throw new Error('should have a multi hop config')
        const mktsEqual = (mkt1: [number, number], mkt2: [number, number]) => {
          return mkt1[0] === mkt2[0] && mkt1[1] === mkt2[1]
        }
        if (mktsEqual(oldCfg.multiHop.baseAssetMarket, market.BaseMarket) && mktsEqual(oldCfg.multiHop.quoteAssetMarket, market.QuoteMarket)) {
          this.arbMkt = market
          opt.selected = true
        }
        page.bridgeAssetSelect.appendChild(opt)
      }
      Doc.show(page.bridgeAssetBox)
    }

    Doc.setVis(this.runningBot, page.updateRunningButton)
    Doc.setVis(!this.runningBot, page.updateStartButton, page.updateButton)

    // Now that we've updated the originalConfig, we'll copy it.
    this.updatedConfig = JSON.parse(JSON.stringify(oldCfg))

    switch (botType) {
      case botTypeBasicMM:
        page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
        break
      case botTypeArbMM:
        page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
        break
      case botTypeBasicArb:
        page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
    }

    setMarketElements(document.body, baseID, quoteID, host)
    Doc.setVis(botType === botTypeBasicArb, page.numBuysLabel, page.numSellsLabel)
    Doc.setVis(botType !== botTypeBasicArb, page.driftToleranceBox, page.switchToAdvanced, page.qcTitle,
      page.buyBufferLabel, page.sellBufferLabel)
    Doc.setVis(Boolean(cexName), ...Doc.applySelector(document.body, '[data-cex-show]'))

    Doc.setVis(this.runningBot, page.botRunningMsg)

    await this.fetchMarketReport()
    await this.updateAllocations()

    if (cexName) {
      setCexElements(document.body, cexName)
      const { bui, qui } = this.walletStuff()
      this.baseMinTransferInput.min = minBaseWithdraw / bui.conventional.conversionFactor
      this.quoteMinTransferInput.min = minQuoteWithdraw / qui.conventional.conversionFactor
      this.baseMinTransferInput.prec = Math.log10(bui.conventional.conversionFactor)
      this.quoteMinTransferInput.prec = Math.log10(qui.conventional.conversionFactor)
    }
    Doc.setVis(cexName, page.rebalanceSection, page.adjustManuallyCexBalances)

    this.baseDexBalanceInput.prec = Math.log10(bui.conventional.conversionFactor)
    const [baseDexMin] = this.validManualBalanceRange(baseID, 'dex', false)
    this.baseDexBalanceInput.min = baseDexMin / bui.conventional.conversionFactor
    this.quoteDexBalanceInput.prec = Math.log10(qui.conventional.conversionFactor)
    const [quoteDexMin] = this.validManualBalanceRange(quoteID, 'dex', false)
    this.quoteDexBalanceInput.min = quoteDexMin / qui.conventional.conversionFactor
    this.baseCexBalanceInput.prec = Math.log10(bui.conventional.conversionFactor)
    const [baseCexMin] = this.validManualBalanceRange(baseID, 'cex', false)
    this.baseCexBalanceInput.min = baseCexMin / bui.conventional.conversionFactor
    this.quoteCexBalanceInput.prec = Math.log10(qui.conventional.conversionFactor)
    const [quoteCexMin] = this.validManualBalanceRange(quoteID, 'cex', false)
    this.quoteCexBalanceInput.min = quoteCexMin / qui.conventional.conversionFactor
    this.baseFeeBalanceInput.prec = Math.log10(baseFeeUI.conventional.conversionFactor)
    const [baseFeeMin] = this.validManualBalanceRange(baseFeeAssetID, 'dex', false)
    this.baseFeeBalanceInput.min = baseFeeMin / baseFeeUI.conventional.conversionFactor
    this.quoteFeeBalanceInput.prec = Math.log10(quoteFeeUI.conventional.conversionFactor)
    const [quoteFeeMin] = this.validManualBalanceRange(quoteFeeAssetID, 'dex', false)
    this.quoteFeeBalanceInput.min = quoteFeeMin / quoteFeeUI.conventional.conversionFactor

    const lotSizeUSD = this.lotSizeUSD()
    this.lotsPerLevelIncrement = Math.round(Math.max(1, defaultLotsPerLevel.usdIncrement / lotSizeUSD))
    this.qcLotsPerLevel.inc = this.lotsPerLevelIncrement
    this.qcUSDPerSide.inc = this.lotsPerLevelIncrement * lotSizeUSD
    this.qcUSDPerSide.min = lotSizeUSD

    const { marketReport: { baseFiatRate } } = this
    this.placementsChart.setMarket({ cexName: cexName as string, botType, baseFiatRate, dict: this.updatedConfig })

    // If this is a new bot, show the quick config form.
    const isQuickPlacements = !savedBotCfg || this.isQuickPlacements(this.updatedConfig.buyPlacements, this.updatedConfig.sellPlacements)
    const gapStrategy = savedBotCfg?.basicMarketMakingConfig?.gapStrategy ?? GapStrategyPercentPlus
    page.gapStrategySelect.value = gapStrategy
    if (botType === botTypeBasicArb || (isQuickPlacements && gapStrategy === GapStrategyPercentPlus)) this.showQuickConfig()
    else this.showAdvancedConfig()

    this.setOriginalValues()

    Doc.hide(page.marketLoading)
    Doc.show(page.botSettingsContainer, page.marketBox)
  }

  initializeMarketRows () {
    this.marketRows = []
    Doc.empty(this.page.marketSelect)
    for (const { host, markets, assets, auth: { effectiveTier, pendingStrength } } of Object.values(app().exchanges)) {
      if (effectiveTier + pendingStrength === 0) {
        const { needRegTmpl, needRegBox } = this.page
        const bttn = needRegTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(bttn)
        Doc.bind(bttn, 'click', () => { app().loadPage('register', { host, backTo: 'mmsettings' }) })
        tmpl.host.textContent = host
        needRegBox.appendChild(bttn)
        Doc.show(needRegBox)
        continue
      }
      for (const { name, baseid: baseID, quoteid: quoteID, spot, basesymbol: baseSymbol, quotesymbol: quoteSymbol } of Object.values(markets)) {
        if (!app().assets[baseID] || !app().assets[quoteID]) continue
        const tr = this.page.marketRowTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(tr)
        const mr = { tr, tmpl, host: host, name, baseID, quoteID, spot: spot, arbs: [] } as MarketRow
        this.marketRows.push(mr)
        this.page.marketSelect.appendChild(tr)
        tmpl.baseIcon.src = Doc.logoPath(baseSymbol)
        tmpl.quoteIcon.src = Doc.logoPath(quoteSymbol)
        tmpl.baseSymbol.appendChild(Doc.symbolize(assets[baseID], true))
        tmpl.quoteSymbol.appendChild(Doc.symbolize(assets[quoteID], true))
        tmpl.host.textContent = host
        const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
        for (const [cexName, dinfo] of Object.entries(CEXDisplayInfos)) {
          if (cexHasMarket(cexName)) {
            const img = this.page.arbBttnTmpl.cloneNode(true) as PageElement
            img.src = dinfo.logo
            tmpl.arbs.appendChild(img)
            mr.arbs.push(cexName)
          }
        }
        Doc.bind(tr, 'click', () => { this.showBotTypeForm(host, baseID, quoteID) })
      }
    }
    if (this.marketRows.length === 0) {
      const { marketSelectionTable, marketFilterBox, noMarkets } = this.page
      Doc.hide(marketSelectionTable, marketFilterBox)
      Doc.show(noMarkets)
    } else Doc.hide(this.page.noMarkets)
    const fiatRates = app().fiatRatesMap
    this.marketRows.sort((a: MarketRow, b: MarketRow) => {
      let [volA, volB] = [a.spot?.vol24 ?? 0, b.spot?.vol24 ?? 0]
      if (fiatRates[a.baseID] && fiatRates[b.baseID]) {
        volA *= fiatRates[a.baseID]
        volB *= fiatRates[b.baseID]
      }
      return volB - volA
    })
  }

  runningBotInventory (assetID: number) {
    return runningBotInventory(assetID)
  }

  setAllocationTechnique (quick: boolean) {
    const { page, updatedConfig } = this
    updatedConfig.uiConfig.usingQuickBalance = quick
    this.updateAllocations()
    Doc.setVis(quick, page.quickAllocateSection)
    Doc.setVis(!quick, page.manuallyAllocateSection)
  }

  quickBalanceMin (config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') : number {
    const { botType } = this.marketStuff()
    switch (config) {
      case 'buyBuffer': return botType === botTypeBasicArb ? 1 : 0
      case 'sellBuffer': return botType === botTypeBasicArb ? 1 : 0
      case 'slippageBuffer': return 0
      case 'buyFeeReserve': return botType === botTypeBasicArb ? 1 : 0
      case 'sellFeeReserve': return botType === botTypeBasicArb ? 1 : 0
    }
  }

  quickBalanceMax (config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') : number {
    const { buyLots, sellLots, botType } = this.marketStuff()
    switch (config) {
      case 'buyBuffer': return botType === botTypeBasicArb ? 20 : 3 * buyLots
      case 'sellBuffer': return botType === botTypeBasicArb ? 20 : 3 * sellLots
      case 'slippageBuffer': return 100
      case 'buyFeeReserve': return 1000
      case 'sellFeeReserve': return 1000
    }
  }

  quickBalanceInput (config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') : NumberInput {
    switch (config) {
      case 'buyBuffer': return this.buyBufferInput
      case 'sellBuffer': return this.sellBufferInput
      case 'slippageBuffer': return this.slippageBufferInput
      case 'buyFeeReserve': return this.buyFeeReserveInput
      case 'sellFeeReserve': return this.sellFeeReserveInput
    }
  }

  quickBalanceSlider (config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') : MiniSlider {
    switch (config) {
      case 'buyBuffer': return this.buyBufferSlider
      case 'sellBuffer': return this.sellBufferSlider
      case 'slippageBuffer': return this.slippageBufferSlider
      case 'buyFeeReserve': return this.buyFeeReserveSlider
      case 'sellFeeReserve': return this.sellFeeReserveSlider
    }
  }

  // fundingFees fetches the funding fees (fees for split transactions) required
  // for a given number of buys and sells. To avoid excessive calls, the results
  // are cached.
  async fundingFees (numBuys: number, numSells: number) : Promise<[number, number]> {
    const { updatedConfig: { baseOptions, quoteOptions }, fundingFeesCache, specs: { host, baseID, quoteID } } = this
    const cacheKey = `${numBuys}-${numSells}-${JSON.stringify(baseOptions)}-${JSON.stringify(quoteOptions)}`
    if (fundingFeesCache[cacheKey] !== undefined) return fundingFeesCache[cacheKey]
    const res = await MM.maxFundingFees({ host, baseID, quoteID }, numBuys, numSells, baseOptions, quoteOptions)
    fundingFeesCache[cacheKey] = [res.buyFees, res.sellFees]
    return [res.buyFees, res.sellFees]
  }

  // updateAllocates updates the required allocations if quick balance config is
  // being used.
  async updateAllocations () {
    const { page, specs, updatedConfig } = this

    Doc.setVis(this.specs.cexName && updatedConfig.uiConfig.cexRebalance, page.baseMinTransferSection, page.quoteMinTransferSection)

    if (!updatedConfig.uiConfig.usingQuickBalance) return

    const {
      sellLots, buyLots, baseID, quoteID, baseFeeAssetID, quoteFeeAssetID,
      baseIsAccountLocker, quoteIsAccountLocker, bui, qui, baseFeeUI, quoteFeeUI,
      numBuys, numSells, lotSize, quoteLot
    } = this.marketStuff()

    const {
      slippageBuffer, buysBuffer, sellsBuffer, buyFeeReserve, sellFeeReserve
    } = this.updatedConfig.uiConfig.quickBalance
    const totalBuyLots = buysBuffer + buyLots
    const totalSellLots = sellsBuffer + sellLots

    const availableFunds = { dex: this.availableDEXBalances, cex: this.availableCEXBalances }
    const [oneTradeBuyFundingFees, oneTradeSellFundingFees] = await this.fundingFees(1, 1)
    const [buyFundingFees, sellFundingFees] = await this.fundingFees(numBuys, numSells)

    const canRebalance = !!specs.cexName && updatedConfig.uiConfig.cexRebalance

    let toAlloc : AllocationResult
    if (this.runningBot) {
      const { runStats } = this.status()
      if (!runStats) {
        console.error('cannot find run stats for running bot')
        return
      }
      toAlloc = toAllocateRunning(totalBuyLots, totalSellLots, lotSize, quoteLot, slippageBuffer, this.quoteMultiSplitBuffer(), buyFeeReserve, sellFeeReserve,
        this.marketReport, availableFunds, canRebalance, baseID, quoteID, baseFeeAssetID, quoteFeeAssetID,
        baseIsAccountLocker, quoteIsAccountLocker, runStats, buyFundingFees, sellFundingFees, oneTradeBuyFundingFees, oneTradeSellFundingFees)
    } else {
      toAlloc = toAllocate(totalBuyLots, totalSellLots, lotSize, quoteLot, slippageBuffer, this.quoteMultiSplitBuffer(), buyFeeReserve, sellFeeReserve,
        this.marketReport, availableFunds, canRebalance, baseID, quoteID, baseFeeAssetID, quoteFeeAssetID,
        baseIsAccountLocker, quoteIsAccountLocker, buyFundingFees, sellFundingFees, oneTradeBuyFundingFees, oneTradeSellFundingFees)
    }

    populateAllocationTable(page.minAllocationTable, baseID, quoteID, baseFeeAssetID, quoteFeeAssetID, this.specs.cexName || '',
      toAlloc, bui, qui, baseFeeUI, quoteFeeUI, this.specs.host)

    const assets = Array.from(new Set([baseID, baseFeeAssetID, quoteID, quoteFeeAssetID]))
    for (const assetID of assets) {
      const dexAlloc = toAlloc.dex[assetID] ? toAlloc.dex[assetID].amount : 0
      const cexAlloc = toAlloc.cex[assetID] ? toAlloc.cex[assetID].amount : 0
      if (assetID === this.specs.baseID) {
        this.baseDexBalanceInput.setValue(dexAlloc / bui.conventional.conversionFactor)
        this.setManualBalanceSliderValue(dexAlloc, 'base', 'dex')
        this.setConfigAllocation(dexAlloc, 'base', 'dex')

        this.baseCexBalanceInput.setValue(cexAlloc / bui.conventional.conversionFactor)
        this.setManualBalanceSliderValue(cexAlloc, 'base', 'cex')
        this.setConfigAllocation(cexAlloc, 'base', 'cex')
      }
      if (assetID === quoteID) {
        this.quoteDexBalanceInput.setValue(dexAlloc / qui.conventional.conversionFactor)
        this.setManualBalanceSliderValue(dexAlloc, 'quote', 'dex')
        this.setConfigAllocation(dexAlloc, 'quote', 'dex')

        this.quoteCexBalanceInput.setValue(cexAlloc / qui.conventional.conversionFactor)
        this.setManualBalanceSliderValue(cexAlloc, 'quote', 'cex')
        this.setConfigAllocation(cexAlloc, 'quote', 'cex')
      }
      if (assetID === baseFeeAssetID && baseFeeAssetID !== baseID && baseFeeAssetID !== quoteID) {
        this.baseFeeBalanceInput.setValue(dexAlloc / baseFeeUI.conventional.conversionFactor)
        this.setManualBalanceSliderValue(dexAlloc, 'baseFee', 'dex')
        this.setConfigAllocation(dexAlloc, 'baseFee', 'dex')
      }
      if (assetID === quoteFeeAssetID && quoteFeeAssetID !== quoteID && quoteFeeAssetID !== baseID) {
        this.quoteFeeBalanceInput.setValue(dexAlloc / quoteFeeUI.conventional.conversionFactor)
        this.setManualBalanceSliderValue(dexAlloc, 'quoteFee', 'dex')
        this.setConfigAllocation(dexAlloc, 'quoteFee', 'dex')
      }
    }

    if (this.specs.cexName) {
      this.minTransferInputChanged(updatedConfig.uiConfig.baseMinTransfer / bui.conventional.conversionFactor, 'base')
      this.minTransferInputChanged(updatedConfig.uiConfig.quoteMinTransfer / qui.conventional.conversionFactor, 'quote')
    }
  }

  setQuickBalanceConfig (config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve', amt: number) {
    switch (config) {
      case 'buyBuffer': this.updatedConfig.uiConfig.quickBalance.buysBuffer = amt; break
      case 'sellBuffer': this.updatedConfig.uiConfig.quickBalance.sellsBuffer = amt; break
      case 'slippageBuffer': this.updatedConfig.uiConfig.quickBalance.slippageBuffer = amt; break
      case 'buyFeeReserve': this.updatedConfig.uiConfig.quickBalance.buyFeeReserve = amt; break
      case 'sellFeeReserve': this.updatedConfig.uiConfig.quickBalance.sellFeeReserve = amt; break
    }
  }

  quickBalanceSliderChanged (amt: number, config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') {
    const [min, max] = [this.quickBalanceMin(config), this.quickBalanceMax(config)]
    const input = this.quickBalanceInput(config)
    const val = Math.floor((max - min) * amt + min)
    input.setValue(val)
    this.setQuickBalanceConfig(config, val)
    this.updateAllocations()
  }

  setQuickBalanceSliderValue (amt: number, config: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') {
    const slider = this.quickBalanceSlider(config)
    const [min, max] = [this.quickBalanceMin(config), this.quickBalanceMax(config)]
    const val = (max - min) === 0 ? 0 : (amt - min) / (max - min)
    slider.setValue(val)
  }

  quickBalanceInputChanged (amt: number, sliderName: 'buyBuffer' | 'sellBuffer' | 'slippageBuffer' | 'buyFeeReserve' | 'sellFeeReserve') {
    this.setQuickBalanceSliderValue(amt, sliderName)
    this.setQuickBalanceConfig(sliderName, amt)
    this.updateAllocations()
  }

  // runningBotAllocations returns the total amount allocated to a running bot.
  runningBotAllocations () : BotBalanceAllocation | undefined {
    const { baseID, quoteID, baseFeeAssetID, quoteFeeAssetID } = this.walletStuff()
    const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeAssetID, quoteFeeAssetID]))

    const botStatus = app().mmStatus.bots.find((s: MMBotStatus) =>
      s.config.baseID === this.specs.baseID && s.config.quoteID === this.specs.quoteID
    )
    if (!botStatus || !botStatus.runStats) {
      console.error('cannot find run stats for running bot')
      return undefined
    }

    const result : BotBalanceAllocation = { dex: {}, cex: {} }

    for (const assetID of assetIDs) {
      const { dexBalances, cexBalances } = botStatus.runStats
      let totalDEX = 0
      totalDEX += dexBalances[assetID]?.available ?? 0
      totalDEX += dexBalances[assetID]?.locked ?? 0
      totalDEX += dexBalances[assetID]?.pending ?? 0
      totalDEX += dexBalances[assetID]?.reserved ?? 0
      result.dex[assetID] = totalDEX

      if (cexBalances) {
        let totalCEX = 0
        totalCEX += cexBalances[assetID]?.available ?? 0
        totalCEX += cexBalances[assetID]?.locked ?? 0
        totalCEX += cexBalances[assetID]?.pending ?? 0
        totalCEX += cexBalances[assetID]?.reserved ?? 0
        result.cex[assetID] = totalCEX
      }
    }

    return result
  }

  minTransferValidRange (asset: 'base' | 'quote') : [number, number] {
    const totalAlloc : number = (() => {
      const { bui, qui } = this.walletStuff()
      const ui = asset === 'base' ? bui : qui
      const assetID = asset === 'base' ? this.specs.baseID : this.specs.quoteID
      const { dex, cex } = this.updatedConfig.uiConfig.allocation
      let total = dex[assetID] + cex[assetID]

      if (!this.runningBot) return total / ui.conventional.conversionFactor

      const botAlloc = this.runningBotAllocations()
      if (botAlloc) {
        total += botAlloc.dex[assetID] ?? 0
        total += botAlloc.cex[assetID] ?? 0
      }

      return total / ui.conventional.conversionFactor
    })()

    const min = asset === 'base' ? this.baseMinTransferInput.min : this.quoteMinTransferInput.min
    const max = Math.max(min * 2, totalAlloc)
    return [min, max]
  }

  setMinTransferCfg (asset: 'base' | 'quote', amt: number) {
    const { updatedConfig: cfg } = this
    const { bui, qui } = this.walletStuff()
    const ui = asset === 'base' ? bui : qui
    const msgAmt = Math.floor(amt * ui.conventional.conversionFactor)
    if (asset === 'base') cfg.uiConfig.baseMinTransfer = msgAmt
    else cfg.uiConfig.quoteMinTransfer = msgAmt
  }

  minTransferSliderChanged (r: number, asset: 'base' | 'quote') {
    const input = asset === 'base' ? this.baseMinTransferInput : this.quoteMinTransferInput
    const [min, max] = this.minTransferValidRange(asset)
    const amt = min + (max - min) * r
    input.setValue(amt)
    this.setMinTransferCfg(asset, amt)
  }

  minTransferInputChanged (amt: number, asset: 'base' | 'quote') {
    const [min, max] = this.minTransferValidRange(asset)
    amt = Math.min(Math.max(amt, min), max) // clamp
    const slider = asset === 'base' ? this.baseMinTransferSlider : this.quoteMinTransferSlider
    const input = asset === 'base' ? this.baseMinTransferInput : this.quoteMinTransferInput
    slider.setValue((amt - min) / (max - min))
    input.setValue(amt)
    this.setMinTransferCfg(asset, amt)
  }

  manualBalanceSlider (asset: 'base' | 'quote' | 'baseFee' | 'quoteFee', location: 'dex' | 'cex') : MiniSlider | undefined {
    switch (asset) {
      case 'base': return location === 'dex' ? this.baseDexBalanceSlider : this.baseCexBalanceSlider
      case 'quote': return location === 'dex' ? this.quoteDexBalanceSlider : this.quoteCexBalanceSlider
      case 'baseFee': return location === 'dex' ? this.baseFeeBalanceSlider : undefined
      case 'quoteFee': return location === 'dex' ? this.quoteFeeBalanceSlider : undefined
    }
  }

  manualBalanceInput (asset: 'base' | 'quote' | 'baseFee' | 'quoteFee', location: 'dex' | 'cex') : NumberInput | undefined {
    switch (asset) {
      case 'base': return location === 'dex' ? this.baseDexBalanceInput : this.baseCexBalanceInput
      case 'quote': return location === 'dex' ? this.quoteDexBalanceInput : this.quoteCexBalanceInput
      case 'baseFee': return location === 'dex' ? this.baseFeeBalanceInput : undefined
      case 'quoteFee': return location === 'dex' ? this.quoteFeeBalanceInput : undefined
    }
  }

  assetID (asset: 'base' | 'quote' | 'baseFee' | 'quoteFee') : number {
    const { baseFeeAssetID, quoteFeeAssetID } = this.walletStuff()
    switch (asset) {
      case 'base': return this.specs.baseID
      case 'quote': return this.specs.quoteID
      case 'baseFee': return baseFeeAssetID
      case 'quoteFee': return quoteFeeAssetID
    }
  }

  // validManualBalanceRange returns the valid range for a manual balance slider.
  // For running bots, this ranges from the negative the bot's unused balance to
  // the available balance, and for non-running bots, it ranges from 0 to the
  // available balance.
  validManualBalanceRange (assetID: number, location: 'dex' | 'cex', conventional: boolean) : [number, number] {
    const conventionalRange = (min: number, max: number): [number, number] => {
      if (!conventional) return [min, max]
      const ui = app().assets[assetID].unitInfo
      return [min / ui.conventional.conversionFactor, max / ui.conventional.conversionFactor]
    }

    const max = location === 'cex'
      ? this.availableCEXBalances[assetID] ?? 0
      : this.availableDEXBalances[assetID] ?? 0

    if (!this.runningBot) return conventionalRange(0, max)

    const botStatus = app().mmStatus.bots.find((s: MMBotStatus) =>
      s.config.baseID === this.specs.baseID && s.config.quoteID === this.specs.quoteID
    )

    if (!botStatus?.runStats) return conventionalRange(0, max)

    const min = location === 'cex'
      ? -botStatus.runStats.cexBalances?.[assetID]?.available ?? 0
      : -botStatus.runStats.dexBalances?.[assetID]?.available ?? 0

    return conventionalRange(min, max)
  }

  setConfigAllocation (amt: number, asset: 'base' | 'quote' | 'baseFee' | 'quoteFee', location: 'dex' | 'cex') {
    const { updatedConfig: cfg } = this
    const assetID = this.assetID(asset)
    if (location === 'dex') {
      cfg.uiConfig.allocation.dex[assetID] = amt
    } else {
      cfg.uiConfig.allocation.cex[assetID] = amt
    }
  }

  balanceSliderChanged (amt: number, asset: 'base' | 'quote' | 'baseFee' | 'quoteFee', location: 'dex' | 'cex') {
    const assetID = this.assetID(asset)
    const [min, max] = this.validManualBalanceRange(assetID, location, false)
    const input = this.manualBalanceInput(asset, location)
    const ui = app().assets[assetID].unitInfo
    if (input) {
      amt = (max - min) * amt + min
      if (amt < 0) amt = Math.ceil(amt)
      else amt = Math.floor(amt)
      input.setValue(amt / ui.conventional.conversionFactor)
    }
    this.setConfigAllocation(amt, asset, location)
  }

  setManualBalanceSliderValue (amt: number, asset: 'base' | 'quote' | 'baseFee' | 'quoteFee', location: 'dex' | 'cex') {
    const assetID = this.assetID(asset)
    const [min, max] = this.validManualBalanceRange(assetID, location, false)
    const slider = this.manualBalanceSlider(asset, location)
    if (slider) slider.setValue((amt - min) / (max - min))
  }

  balanceInputChanged (amt: number, asset: 'base' | 'quote' | 'baseFee' | 'quoteFee', location: 'dex' | 'cex') {
    const assetID = this.assetID(asset)
    const [min, max] = this.validManualBalanceRange(assetID, location, false)
    const ui = app().assets[assetID].unitInfo
    amt = amt * ui.conventional.conversionFactor
    if (amt > max || amt < min) {
      if (amt > max) amt = max
      else amt = min
      const input = this.manualBalanceInput(asset, location)
      if (input) input.setValue(amt / ui.conventional.conversionFactor)
    }
    this.setManualBalanceSliderValue(amt, asset, location)
    this.setConfigAllocation(amt, asset, location)
  }

  status () {
    const { specs: { baseID, quoteID } } = this
    const botStatus = app().mmStatus.bots.find((s: MMBotStatus) => s.config.baseID === baseID && s.config.quoteID === quoteID)
    if (!botStatus) return { botCfg: {} as BotConfig, running: false, runStats: {} as RunStats }
    const { config: botCfg, running, runStats, latestEpoch, cexProblems } = botStatus
    return { botCfg, running, runStats, latestEpoch, cexProblems }
  }

  lotSizeUSD () {
    const { specs: { host, baseID }, dexMktID, marketReport: { baseFiatRate } } = this
    const xc = app().exchanges[host]
    const market = xc.markets[dexMktID]
    const { lotsize: lotSize } = market
    const { unitInfo: ui } = app().assets[baseID]
    return lotSize / ui.conventional.conversionFactor * baseFiatRate
  }

  quoteMultiSplitBuffer () : number {
    if (!this.updatedConfig.quoteOptions) return 0
    if (this.updatedConfig.quoteOptions.multisplit !== 'true') return 0
    return Number(this.updatedConfig.quoteOptions.multisplitbuffer || '0')
  }

  /*
    * marketStuff is just a bunch of useful properties for the current specs
    * gathered in one place and with preferable names.
    */
  marketStuff () {
    const {
      page, specs: { host, baseID, quoteID, cexName, botType },
      marketReport: { baseFiatRate, quoteFiatRate, baseFees, quoteFees },
      lotsPerLevelIncrement, updatedConfig: cfg, originalConfig: oldCfg, dexMktID
    } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const xc = app().exchanges[host]
    const market = xc.markets[dexMktID]
    const { lotsize: lotSize, spot } = market
    const lotSizeUSD = lotSize / bui.conventional.conversionFactor * baseFiatRate
    const atomicRate = 1 / bui.conventional.conversionFactor * baseFiatRate / quoteFiatRate * qui.conventional.conversionFactor
    const xcRate = {
      conv: quoteFiatRate / baseFiatRate,
      atomic: atomicRate,
      msg: Math.round(atomicRate * OrderUtil.RateEncodingFactor), // unadjusted
      spot
    }

    let [sellLots, buyLots, numBuys, numSells] = [0, 0, 0, 0]
    if (botType !== botTypeBasicArb) {
      sellLots = this.updatedConfig.sellPlacements.reduce((lots: number, p: OrderPlacement) => lots + p.lots, 0)
      buyLots = this.updatedConfig.buyPlacements.reduce((lots: number, p: OrderPlacement) => lots + p.lots, 0)
      numBuys = this.updatedConfig.buyPlacements.length
      numSells = this.updatedConfig.sellPlacements.length
    }
    const quoteLot = calculateQuoteLot(lotSize, baseID, quoteID, spot)

    return {
      page, cfg, oldCfg, host, xc, botType, cexName, baseFiatRate, quoteFiatRate,
      xcRate, baseSymbol, quoteSymbol, dexMktID, lotSize, lotSizeUSD, lotsPerLevelIncrement,
      quoteLot, baseFees, quoteFees, sellLots, buyLots, numBuys, numSells, ...this.walletStuff()
    }
  }

  walletStuff () {
    const { specs: { baseID, quoteID } } = this
    const [baseWallet, quoteWallet] = [app().walletMap[baseID], app().walletMap[quoteID]]
    const [{ token: baseToken, unitInfo: bui }, { token: quoteToken, unitInfo: qui }] = [app().assets[baseID], app().assets[quoteID]]
    const baseFeeAssetID = baseToken ? baseToken.parentID : baseID
    const quoteFeeAssetID = quoteToken ? quoteToken.parentID : quoteID
    const [baseFeeUI, quoteFeeUI] = [app().assets[baseFeeAssetID].unitInfo, app().assets[quoteFeeAssetID].unitInfo]
    const traitAccountLocker = 1 << 14
    const baseIsAccountLocker = (baseWallet.traits & traitAccountLocker) > 0
    const quoteIsAccountLocker = (quoteWallet.traits & traitAccountLocker) > 0
    return {
      baseWallet, quoteWallet, baseFeeUI, quoteFeeUI, baseToken, quoteToken,
      bui, qui, baseFeeAssetID, quoteFeeAssetID, baseIsAccountLocker, quoteIsAccountLocker,
      baseID, quoteID
    }
  }

  showAdvancedConfig () {
    const { page } = this
    Doc.show(page.advancedConfig)
    Doc.hide(page.quickConfig)
    this.placementsChart.render()
  }

  isQuickPlacements (buyPlacements: OrderPlacement[], sellPlacements: OrderPlacement[]) {
    if (buyPlacements.length === 0 || buyPlacements.length !== sellPlacements.length) return false
    for (let i = 0; i < buyPlacements.length; i++) {
      if (buyPlacements[i].gapFactor !== sellPlacements[i].gapFactor) return false
      if (buyPlacements[i].lots !== sellPlacements[i].lots) return false
    }
    return true
  }

  switchToQuickConfig () {
    const { cfg, botType, lotSizeUSD } = this.marketStuff()
    const { buyPlacements: buys, sellPlacements: sells } = cfg
    // If we have both buys and sells, get the best approximation quick config
    // approximation.
    if (buys.length > 0 && sells.length > 0) {
      const bestBuy = buys.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
      const bestSell = sells.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
      const placementCount = buys.length + sells.length
      const levelsPerSide = Math.max(1, Math.floor((placementCount) / 2))
      if (botType === botTypeBasicMM) {
        cfg.profit = (bestBuy.gapFactor + bestSell.gapFactor) / 2
        const worstBuy = buys.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor > prev.gapFactor ? curr : prev)
        const worstSell = sells.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor > prev.gapFactor ? curr : prev)
        const range = ((worstBuy.gapFactor - bestBuy.gapFactor) + (worstSell.gapFactor - bestSell.gapFactor)) / 2
        const inc = range / (levelsPerSide - 1)
        this.qcProfit.setValue(cfg.profit * 100)
        this.qcProfitSlider.setValue((cfg.profit - defaultProfit.minV) / defaultProfit.range)
        this.qcLevelSpacing.setValue(inc * 100)
        this.qcLevelSpacingSlider.setValue((inc - defaultLevelSpacing.minV) / defaultLevelSpacing.range)
      } else if (botType === botTypeArbMM) {
        const multSum = buys.reduce((v: number, p: OrderPlacement) => v + p.gapFactor, 0) + sells.reduce((v: number, p: OrderPlacement) => v + p.gapFactor, 0)
        const buffer = ((multSum / placementCount) - 1) || defaultMatchBuffer.value
        this.qcMatchBuffer.setValue(buffer * 100)
        this.qcMatchBufferSlider.setValue((buffer - defaultMatchBuffer.minV) / defaultMatchBuffer.range)
      }
      const lots = buys.reduce((v: number, p: OrderPlacement) => v + p.lots, 0) + sells.reduce((v: number, p: OrderPlacement) => v + p.lots, 0)
      const lotsPerLevel = Math.max(1, Math.round(lots / 2 / levelsPerSide))
      this.qcLotsPerLevel.setValue(lotsPerLevel)
      this.qcUSDPerSide.setValue(lotsPerLevel * levelsPerSide * lotSizeUSD)
      this.qcLevelsPerSide.setValue(levelsPerSide)
    } else if (botType === botTypeBasicArb) {
      this.qcLotsPerLevel.setValue(1)
    }
    this.showQuickConfig()
    this.quickConfigUpdated()
  }

  showQuickConfig () {
    const { page, lotSizeUSD, botType, lotsPerLevelIncrement } = this.marketStuff()

    if (!this.qcLevelsPerSide.input.value) {
      this.qcLevelsPerSide.setValue(defaultLevelsPerSide.value)
      this.qcUSDPerSide.setValue(defaultLevelsPerSide.value * (this.qcLotsPerLevel.value() || lotsPerLevelIncrement) * lotSizeUSD)
    }
    if (!this.qcLotsPerLevel.input.value) {
      this.qcLotsPerLevel.setValue(lotsPerLevelIncrement)
      this.qcUSDPerSide.setValue(lotSizeUSD * lotsPerLevelIncrement * this.qcLevelsPerSide.value())
    }
    if (!page.qcLevelSpacing.value) {
      this.qcLevelSpacing.setValue(defaultLevelSpacing.value * 100)
      this.qcLevelSpacingSlider.setValue((defaultLevelSpacing.value - defaultLevelSpacing.minV) / defaultLevelSpacing.range)
    }
    if (!page.qcMatchBuffer.value) page.qcMatchBuffer.value = String(defaultMatchBuffer.value * 100)

    Doc.hide(page.advancedConfig)
    Doc.show(page.quickConfig)

    this.showInputsForBot(botType)
  }

  showInputsForBot (botType: string) {
    const { page, opts: { usingUSDPerSide } } = this
    Doc.hide(
      page.matchMultiplierBox, page.placementsChartBox, page.placementChartLegend,
      page.lotsPerLevelLabel, page.levelSpacingBox, page.arbLotsLabel, page.qcLevelPerSideBox,
      page.qcUSDPerSideBox, page.qcLotsBox
    )
    switch (botType) {
      case botTypeArbMM:
        Doc.show(
          page.qcLevelPerSideBox, page.matchMultiplierBox, page.placementsChartBox,
          page.placementChartLegend, page.lotsPerLevelLabel
        )
        Doc.setVis(usingUSDPerSide, page.qcUSDPerSideBox)
        Doc.setVis(!usingUSDPerSide, page.qcLotsBox)
        break
      case botTypeBasicMM:
        Doc.show(
          page.qcLevelPerSideBox, page.levelSpacingBox, page.placementsChartBox,
          page.lotsPerLevelLabel
        )
        Doc.setVis(usingUSDPerSide, page.qcUSDPerSideBox)
        Doc.setVis(!usingUSDPerSide, page.qcLotsBox)
        break
    }
  }

  async quickConfigUpdated () {
    const { page, cfg, botType, cexName } = this.marketStuff()

    Doc.hide(page.qcError)
    const setError = (msg: string) => {
      page.qcError.textContent = msg
      Doc.show(page.qcError)
    }

    const levelsPerSide = botType === botTypeBasicArb ? 1 : this.qcLevelsPerSide.value()
    if (isNaN(levelsPerSide)) {
      setError('invalid value for levels per side')
    }

    const lotsPerLevel = this.qcLotsPerLevel.value()
    if (isNaN(lotsPerLevel)) {
      setError('invalid value for lots per level')
    }

    const profit = parseFloat(page.qcProfit.value ?? '') / 100
    if (isNaN(profit)) {
      setError('invalid value for profit')
    }

    const levelSpacing = botType === botTypeBasicMM ? parseFloat(page.qcLevelSpacing.value ?? '') / 100 : 0
    if (isNaN(levelSpacing)) {
      setError('invalid value for level spacing')
    }

    const matchBuffer = botType === botTypeArbMM ? parseFloat(page.qcMatchBuffer.value ?? '') / 100 : 0
    if (isNaN(matchBuffer)) {
      setError('invalid value for match buffer')
    }
    const multiplier = matchBuffer + 1

    const levelSpacingDisabled = levelsPerSide === 1
    page.levelSpacingBox.classList.toggle('disabled', levelSpacingDisabled)
    page.qcLevelSpacing.disabled = levelSpacingDisabled

    if (botType !== botTypeBasicArb) {
      this.clearPlacements(cexName ? arbMMRowCacheKey : cfg.gapStrategy)
      for (let levelN = 0; levelN < levelsPerSide; levelN++) {
        const placement = { lots: lotsPerLevel } as OrderPlacement
        placement.gapFactor = botType === botTypeBasicMM ? profit + levelSpacing * levelN : multiplier
        cfg.buyPlacements.push(placement)
        cfg.sellPlacements.push(placement)
        // Add rows in the advanced config table.
        this.addPlacement(true, placement)
        this.addPlacement(false, placement)
      }

      this.placementsChart.render()
    }

    await this.updateAllocations()
  }

  matchBufferChanged () {
    const { page } = this
    page.qcMatchBuffer.value = Math.max(0, parseFloat(page.qcMatchBuffer.value ?? '') || defaultMatchBuffer.value * 100).toFixed(2)
    this.quickConfigUpdated()
  }

  showAddress (assetID: number) {
    this.walletAddrForm.setAsset(assetID)
    this.forms.show(this.page.walletAddrForm)
  }

  changeSideCommitmentDialog () {
    const { page, opts } = this
    opts.usingUSDPerSide = !opts.usingUSDPerSide
    Doc.setVis(opts.usingUSDPerSide, page.qcUSDPerSideBox)
    Doc.setVis(!opts.usingUSDPerSide, page.qcLotsBox)
  }

  async showBotTypeForm (host: string, baseID: number, quoteID: number, botType?: string, configuredCEX?: string) {
    const { page } = this
    this.formSpecs = { host, baseID, quoteID, botType: '' }
    const botRunning = botIsRunning(this.formSpecs, app().mmStatus)
    if (botRunning) {
      const botCfg = liveBotConfig(host, baseID, quoteID)
      const specs = this.specs = this.formSpecs
      switch (true) {
        case Boolean(botCfg?.simpleArbConfig):
          specs.botType = botTypeBasicArb
          break
        case Boolean(botCfg?.arbMarketMakingConfig):
          specs.botType = botTypeArbMM
          break
        default:
          specs.botType = botTypeBasicMM
      }
      specs.cexName = botCfg?.cexName
      await this.fetchCEXBalances(this.formSpecs)
      await this.configureUI()
      this.forms.close()
      return
    }
    setMarketElements(page.botTypeForm, baseID, quoteID, host)
    Doc.empty(page.botTypeBaseSymbol, page.botTypeQuoteSymbol)
    const [b, q] = [app().assets[baseID], app().assets[quoteID]]
    page.botTypeBaseSymbol.appendChild(Doc.symbolize(b, true))
    page.botTypeQuoteSymbol.appendChild(Doc.symbolize(q, true))
    for (const div of this.botTypeSelectors) div.classList.remove('selected')
    for (const { div } of Object.values(this.formCexes)) div.classList.remove('selected')
    this.setCEXAvailability(baseID, quoteID)
    Doc.hide(page.noCexesConfigured, page.noCexMarket, page.noCexMarketConfigureMore, page.botTypeErr)
    const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
    const supportingCexes: Record<string, CEXConfig> = {}
    for (const cex of Object.values(app().mmStatus.cexes)) {
      if (cexHasMarket(cex.config.name)) supportingCexes[cex.config.name] = cex.config
    }
    const nCexes = Object.keys(supportingCexes).length
    const arbEnabled = nCexes > 0
    for (const div of this.botTypeSelectors) div.classList.toggle('disabled', div.dataset.botType !== botTypeBasicMM && !arbEnabled)
    if (Object.keys(app().mmStatus.cexes).length === 0) {
      Doc.show(page.noCexesConfigured)
      this.setBotTypeSelected(botTypeBasicMM)
    } else {
      const lastBots = (State.fetchLocal(lastBotsLK) || {}) as Record<string, BotSpecs>
      const lastBot = lastBots[`${baseID}_${quoteID}_${host}`]
      let cex: CEXConfig | undefined
      botType = botType ?? (lastBot ? lastBot.botType : botTypeArbMM)
      if (botType !== botTypeBasicMM) {
        // Four ways to auto-select a cex.
        // 1. Coming back from the cex configuration form.
        if (configuredCEX) cex = supportingCexes[configuredCEX]
        // 2. We have a saved configuration.
        if (!cex && lastBot) cex = supportingCexes[lastBot.cexName ?? '']
        // 3. The last exchange that the user selected.
        if (!cex) {
          const lastCEX = State.fetchLocal(lastArbExchangeLK)
          if (lastCEX) cex = supportingCexes[lastCEX]
        }
        // 4. Any supporting cex.
        if (!cex && nCexes > 0) cex = Object.values(supportingCexes)[0]
      }
      if (cex) {
        page.cexSelection.classList.remove('disabled')
        this.setBotTypeSelected(botType ?? (lastBot ? lastBot.botType : botTypeArbMM))
        this.selectFormCEX(cex.name)
      } else {
        page.cexSelection.classList.add('disabled')
        Doc.show(page.noCexMarket)
        this.setBotTypeSelected(botTypeBasicMM)
        // If there are unconfigured cexes, show configureMore message.
        const unconfigured = Object.keys(CEXDisplayInfos).filter((cexName: string) => !app().mmStatus.cexes[cexName])
        const allConfigured = unconfigured.length === 0 || (unconfigured.length === 1 && (unconfigured[0] === 'Binance' || unconfigured[0] === 'BinanceUS'))
        if (!allConfigured) Doc.show(page.noCexMarketConfigureMore)
      }
    }

    Doc.show(page.cexSelection)
    // Check if we have any cexes configured.
    this.forms.show(page.botTypeForm)
  }

  reshowBotTypeForm () {
    if (this.runningBot) return
    const { baseID, quoteID, host, cexName, botType } = this.specs
    this.showBotTypeForm(host, baseID, quoteID, botType, cexName)
  }

  setBotTypeSelected (selectedType: string) {
    const { formSpecs: { baseID, quoteID, host }, botTypeSelectors, formCexes } = this
    for (const { classList, dataset: { botType } } of botTypeSelectors) classList.toggle('selected', botType === selectedType)
    // If we don't have a cex selected, attempt to select one
    if (selectedType === botTypeBasicMM) return
    const mmStatus = app().mmStatus
    if (Object.keys(mmStatus.cexes).length === 0) return
    const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
    // If there is one currently selected and it supports this market, leave it.
    const selecteds = Object.values(formCexes).filter((cex: cexButton) => cex.div.classList.contains('selected'))
    if (selecteds.length && cexHasMarket(selecteds[0].name)) return
    // See if we have a saved configuration.
    const lastBots = (State.fetchLocal(lastBotsLK) || {}) as Record<string, BotSpecs>
    const lastBot = lastBots[`${baseID}_${quoteID}_${host}`]
    if (lastBot) {
      const cex = mmStatus.cexes[lastBot.cexName ?? '']
      if (cex && cexHasMarket(cex.config.name)) {
        this.selectFormCEX(cex.config.name)
        return
      }
    }
    // 2. The last exchange that the user selected.
    const lastCEX = State.fetchLocal(lastArbExchangeLK)
    if (lastCEX) {
      const cex = mmStatus.cexes[lastCEX]
      if (cex && cexHasMarket(cex.config.name)) {
        this.selectFormCEX(cex.config.name)
        return
      }
    }
    // 3. Any supporting cex.
    const cexes = Object.values(mmStatus.cexes).filter((cex: MMCEXStatus) => cexHasMarket(cex.config.name))
    if (cexes.length) this.selectFormCEX(cexes[0].config.name)
  }

  showMarketSelectForm () {
    if (this.runningBot) return
    this.page.marketFilterInput.value = ''
    this.sortMarketRows()
    this.forms.show(this.page.marketSelectForm)
  }

  sortMarketRows () {
    const page = this.page
    const filter = page.marketFilterInput.value?.toLowerCase()
    Doc.empty(page.marketSelect)
    for (const mr of this.marketRows) {
      mr.tr.classList.remove('selected')
      if (filter && !mr.name.includes(filter)) continue
      page.marketSelect.appendChild(mr.tr)
    }
  }

  async handleBalanceNote (note: BalanceNote) {
    if (!this.marketReport) return
    const { assetID } = note
    const { baseID, quoteID, baseFeeAssetID, quoteFeeAssetID } = this.walletStuff()
    if ([baseID, quoteID, baseFeeAssetID, quoteFeeAssetID].indexOf(assetID) >= 0) {
      await this.setAvailableBalances()
      this.updateAllocations()
    }
  }

  internalOnlyChanged () {
    const checked = Boolean(this.page.internalOnlyRadio.checked)
    this.page.externalTransfersRadio.checked = !checked
    this.updatedConfig.uiConfig.cexRebalance = !checked
    this.updatedConfig.uiConfig.internalTransfers = checked
    this.updateAllocations()
  }

  externalTransfersChanged () {
    const checked = Boolean(this.page.externalTransfersRadio.checked)
    this.page.internalOnlyRadio.checked = !checked
    this.updatedConfig.uiConfig.cexRebalance = checked
    this.updatedConfig.uiConfig.internalTransfers = !checked
    this.updateAllocations()
  }

  autoRebalanceChanged () {
    const { page, updatedConfig: cfg } = this
    const checked = page.enableRebalance.checked
    Doc.setVis(checked, page.internalOnlySettings, page.externalTransfersSettings)
    if (checked && !cfg.uiConfig.cexRebalance && !cfg.uiConfig.internalTransfers) {
      // default to external transfers
      cfg.uiConfig.cexRebalance = true
      page.externalTransfersRadio.checked = true
      page.internalOnlyRadio.checked = false
    } else if (!checked) {
      cfg.uiConfig.cexRebalance = false
      cfg.uiConfig.internalTransfers = false
      page.externalTransfersRadio.checked = false
      page.internalOnlyRadio.checked = false
    } else if (cfg.uiConfig.cexRebalance && cfg.uiConfig.internalTransfers) {
      // should not happen.. set to default
      cfg.uiConfig.internalTransfers = false
      page.externalTransfersRadio.checked = true
      page.internalOnlyRadio.checked = false
    } else {
      // set to current values. This case should only be called when the form
      // is loaded.
      page.externalTransfersRadio.checked = cfg.uiConfig.cexRebalance
      page.internalOnlyRadio.checked = cfg.uiConfig.internalTransfers
    }

    this.updateAllocations()
  }

  async submitBotType () {
    const loaded = app().loading(this.page.botTypeForm)
    try {
      await this.submitBotWithValidation()
    } finally {
      loaded()
    }
  }

  async submitBotWithValidation () {
    // check for wallets
    const { page, forms, formSpecs: { baseID, quoteID, host } } = this

    if (!app().walletMap[baseID]) {
      this.newWalletForm.setAsset(baseID)
      forms.show(this.page.newWalletForm)
      return
    }
    if (!app().walletMap[quoteID]) {
      this.newWalletForm.setAsset(quoteID)
      forms.show(this.page.newWalletForm)
      return
    }
    // Are tokens approved?
    const [bApproval, qApproval] = tokenAssetApprovalStatuses(host, app().assets[baseID], app().assets[quoteID])
    if (bApproval === ApprovalStatus.NotApproved) {
      this.approveTokenForm.setAsset(baseID, host)
      forms.show(page.approveTokenForm)
      return
    }
    if (qApproval === ApprovalStatus.NotApproved) {
      this.approveTokenForm.setAsset(quoteID, host)
      forms.show(page.approveTokenForm)
      return
    }

    const { botTypeSelectors } = this
    const selecteds = botTypeSelectors.filter((div: PageElement) => div.classList.contains('selected'))
    if (selecteds.length < 1) {
      page.botTypeErr.textContent = intl.prep(intl.ID_NO_BOTTYPE)
      Doc.show(page.botTypeErr)
      return
    }
    const botType = this.formSpecs.botType = selecteds[0].dataset.botType ?? ''
    if (botType !== botTypeBasicMM) {
      const selecteds = Object.values(this.formCexes).filter((cex: cexButton) => cex.div.classList.contains('selected'))
      if (selecteds.length < 1) {
        page.botTypeErr.textContent = intl.prep(intl.ID_NO_CEX)
        Doc.show(page.botTypeErr)
        return
      }
      const cexName = selecteds[0].name
      this.formSpecs.cexName = cexName
      await this.fetchCEXBalances(this.formSpecs)
    }

    this.specs = this.formSpecs

    this.configureUI()
    this.forms.close()
  }

  async fetchCEXBalances (specs: BotSpecs) {
    const { page } = this
    const { baseID, quoteID, cexName, botType } = specs
    if (botType === botTypeBasicMM || !cexName) return

    try {
      // This won't work if we implement live reconfiguration, because locked
      // funds would need to be considered.
      this.cexBaseBalance = await MM.cexBalance(cexName, baseID)
    } catch (e) {
      page.botTypeErr.textContent = intl.prep(intl.ID_CEXBALANCE_ERR, { cexName, assetID: String(baseID), err: String(e) })
      Doc.show(page.botTypeErr)
      throw e
    }

    try {
      this.cexQuoteBalance = await MM.cexBalance(cexName, quoteID)
    } catch (e) {
      page.botTypeErr.textContent = intl.prep(intl.ID_CEXBALANCE_ERR, { cexName, assetID: String(quoteID), err: String(e) })
      Doc.show(page.botTypeErr)
      throw e
    }
  }

  defaultWalletOptions (assetID: number): Record<string, string> {
    const walletDef = app().currentWalletDefinition(assetID)
    if (!walletDef.multifundingopts) {
      return {}
    }
    const options: Record<string, string> = {}
    for (const opt of walletDef.multifundingopts) {
      if (opt.quoteAssetOnly && assetID !== this.specs.quoteID) {
        continue
      }
      options[opt.key] = `${opt.default}`
    }
    return options
  }

  /*
   * updateModifiedMarkers checks each of the input elements on the page and
   * if the current value does not match the original value (since the last
   * save), then the input will have a colored border.
   */
  updateModifiedMarkers () {
    if (this.creatingNewBot) return
    const { page, originalConfig: oldCfg, updatedConfig: newCfg } = this

    // Gap strategy input
    const gapStrategyModified = oldCfg.gapStrategy !== newCfg.gapStrategy
    page.gapStrategySelect.classList.toggle('modified', gapStrategyModified)

    const profitModified = oldCfg.profit !== newCfg.profit
    page.profitInput.classList.toggle('modified', profitModified)

    // Buy placements Input
    let buyPlacementsModified = false
    if (oldCfg.buyPlacements.length !== newCfg.buyPlacements.length) {
      buyPlacementsModified = true
    } else {
      for (let i = 0; i < oldCfg.buyPlacements.length; i++) {
        if (oldCfg.buyPlacements[i].lots !== newCfg.buyPlacements[i].lots ||
          oldCfg.buyPlacements[i].gapFactor !== newCfg.buyPlacements[i].gapFactor) {
          buyPlacementsModified = true
          break
        }
      }
    }
    page.buyPlacementsTableWrapper.classList.toggle('modified', buyPlacementsModified)

    // Sell placements input
    let sellPlacementsModified = false
    if (oldCfg.sellPlacements.length !== newCfg.sellPlacements.length) {
      sellPlacementsModified = true
    } else {
      for (let i = 0; i < oldCfg.sellPlacements.length; i++) {
        if (oldCfg.sellPlacements[i].lots !== newCfg.sellPlacements[i].lots ||
          oldCfg.sellPlacements[i].gapFactor !== newCfg.sellPlacements[i].gapFactor) {
          sellPlacementsModified = true
          break
        }
      }
    }
    page.sellPlacementsTableWrapper.classList.toggle('modified', sellPlacementsModified)
  }

  /*
   * gapFactorHeaderUnit returns the header on the placements table and the
   * units in the gap factor rows needed for each gap strategy.
   */
  gapFactorHeaderUnit (gapStrategy: string): [string, string] {
    switch (gapStrategy) {
      case GapStrategyMultiplier:
        return ['Multiplier', 'x']
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus: {
        const rateUnit = `${app().assets[this.specs.quoteID].symbol}/${app().assets[this.specs.baseID].symbol}`
        return ['Rate', rateUnit]
      }
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        return ['Percent', '%']
      default:
        throw new Error(`Unknown gap strategy ${gapStrategy}`)
    }
  }

  /*
   * checkGapFactorRange returns an error string if the value input for a
   * gap factor is valid for the currently selected gap strategy.
   */
  checkGapFactorRange (gapFactor: string, value: number): (string | null) {
    switch (gapFactor) {
      case GapStrategyMultiplier:
        if (value < 1 || value > 100) {
          return 'Multiplier must be between 1 and 100'
        }
        return null
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus:
        if (value <= 0) {
          return 'Rate must be greater than 0'
        }
        return null
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        if (value <= 0 || value > 10) {
          return 'Percent must be between 0 and 10'
        }
        return null
      default: {
        throw new Error(`Unknown gap factor ${gapFactor}`)
      }
    }
  }

  /*
   * convertGapFactor converts between the displayed gap factor in the
   * placement tables and the number that is passed to the market maker.
   * For gap strategies that involve a percentage it converts between the
   * decimal value required by the backend and a percentage displayed to
   * the user.
   */
  convertGapFactor (gapFactor: number, gapStrategy: string, toDisplay: boolean): number {
    switch (gapStrategy) {
      case GapStrategyMultiplier:
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus:
        return gapFactor
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        if (toDisplay) {
          return gapFactor * 100
        }
        return gapFactor / 100
      default:
        throw new Error(`Unknown gap factor ${gapStrategy}`)
    }
  }

  /*
   * addPlacement adds a row to a placement table. This is called both when
   * the page is initially loaded, and when the "add" button is pressed on
   * the placement table. initialLoadPlacement is non-nil if this is being
   * called on the initial load.
   */
  addPlacement (isBuy: boolean, initialLoadPlacement: OrderPlacement | null, gapStrategy?: string) {
    const { page, updatedConfig: cfg } = this

    let tableBody: PageElement = page.sellPlacementsTableBody
    let addPlacementRow: PageElement = page.addSellPlacementRow
    let lotsElement: PageElement = page.addSellPlacementLots
    let gapFactorElement: PageElement = page.addSellPlacementGapFactor
    let errElement: PageElement = page.sellPlacementsErr
    if (isBuy) {
      tableBody = page.buyPlacementsTableBody
      addPlacementRow = page.addBuyPlacementRow
      lotsElement = page.addBuyPlacementLots
      gapFactorElement = page.addBuyPlacementGapFactor
      errElement = page.buyPlacementsErr
    }

    Doc.hide(errElement)

    // updateArrowVis updates the visibility of the move up/down arrows in
    // each row of the placement table. The up arrow is not shown on the
    // top row, and the down arrow is not shown on the bottom row. They
    // are all hidden if market making is running.
    const updateArrowVis = () => {
      for (let i = 0; i < tableBody.children.length - 1; i++) {
        const row = Doc.parseTemplate(tableBody.children[i] as HTMLElement)
        Doc.setVis(i !== 0, row.upBtn)
        Doc.setVis(i !== tableBody.children.length - 2, row.downBtn)
      }
    }

    Doc.hide(errElement)
    const setErr = (err: string) => {
      errElement.textContent = err
      Doc.show(errElement)
    }

    let lots: number
    let actualGapFactor: number
    let displayedGapFactor: number
    if (!gapStrategy) gapStrategy = this.specs.cexName ? GapStrategyMultiplier : cfg.gapStrategy
    const placements = isBuy ? cfg.buyPlacements : cfg.sellPlacements
    const unit = this.gapFactorHeaderUnit(gapStrategy)[1]
    if (initialLoadPlacement) {
      lots = initialLoadPlacement.lots
      actualGapFactor = initialLoadPlacement.gapFactor
      displayedGapFactor = this.convertGapFactor(actualGapFactor, gapStrategy, true)
    } else {
      lots = parseInt(lotsElement.value || '0')
      displayedGapFactor = parseFloat(gapFactorElement.value || '0')
      actualGapFactor = this.convertGapFactor(displayedGapFactor, gapStrategy, false)
      if (lots === 0) {
        setErr('Lots must be greater than 0')
        return
      }

      const gapFactorErr = this.checkGapFactorRange(gapStrategy, displayedGapFactor)
      if (gapFactorErr) {
        setErr(gapFactorErr)
        return
      }

      if (placements.find((placement) => placement.gapFactor === actualGapFactor)
      ) {
        setErr('Duplicate placement')
        return
      }

      placements.push({ lots, gapFactor: actualGapFactor })
    }

    const newRow = page.placementRowTmpl.cloneNode(true) as PageElement
    const newRowTmpl = Doc.parseTemplate(newRow)
    newRowTmpl.priority.textContent = `${tableBody.children.length}`
    newRowTmpl.lots.textContent = `${lots}`
    newRowTmpl.gapFactor.textContent = `${displayedGapFactor} ${unit}`
    Doc.bind(newRowTmpl.removeBtn, 'click', () => {
      const index = placements.findIndex((placement) => {
        return placement.lots === lots && placement.gapFactor === actualGapFactor
      })
      if (index === -1) return
      placements.splice(index, 1)
      newRow.remove()
      updateArrowVis()
      this.updateModifiedMarkers()
      this.placementsChart.render()
      this.updateAllocations()
    })

    Doc.bind(newRowTmpl.upBtn, 'click', () => {
      const index = placements.findIndex((p: OrderPlacement) => p.lots === lots && p.gapFactor === actualGapFactor)
      if (index === 0) return
      const prevPlacement = placements[index - 1]
      placements[index - 1] = placements[index]
      placements[index] = prevPlacement
      newRowTmpl.priority.textContent = `${index}`
      newRow.remove()
      tableBody.insertBefore(newRow, tableBody.children[index - 1])
      const movedDownTmpl = Doc.parseTemplate(
        tableBody.children[index] as HTMLElement
      )
      movedDownTmpl.priority.textContent = `${index + 1}`
      updateArrowVis()
      this.updateModifiedMarkers()
    })

    Doc.bind(newRowTmpl.downBtn, 'click', () => {
      const index = placements.findIndex((p) => p.lots === lots && p.gapFactor === actualGapFactor)
      if (index === placements.length - 1) return
      const nextPlacement = placements[index + 1]
      placements[index + 1] = placements[index]
      placements[index] = nextPlacement
      newRowTmpl.priority.textContent = `${index + 2}`
      newRow.remove()
      tableBody.insertBefore(newRow, tableBody.children[index + 1])
      const movedUpTmpl = Doc.parseTemplate(
        tableBody.children[index] as HTMLElement
      )
      movedUpTmpl.priority.textContent = `${index + 1}`
      updateArrowVis()
      this.updateModifiedMarkers()
    })

    tableBody.insertBefore(newRow, addPlacementRow)
    updateArrowVis()
  }

  setArbMMLabels () {
    this.page.buyGapFactorHdr.textContent = intl.prep(intl.ID_MATCH_BUFFER)
    this.page.sellGapFactorHdr.textContent = intl.prep(intl.ID_MATCH_BUFFER)
  }

  /*
   * setGapFactorLabels sets the headers on the gap factor column of each
   * placement table.
   */
  setGapFactorLabels (gapStrategy: string) {
    const page = this.page
    const header = this.gapFactorHeaderUnit(gapStrategy)[0]
    page.buyGapFactorHdr.textContent = header
    page.sellGapFactorHdr.textContent = header
    Doc.hide(page.percentPlusInfo, page.percentInfo, page.absolutePlusInfo, page.absoluteInfo, page.multiplierInfo)
    switch (gapStrategy) {
      case 'percent-plus':
        return Doc.show(page.percentPlusInfo)
      case 'percent':
        return Doc.show(page.percentInfo)
      case 'absolute-plus':
        return Doc.show(page.absolutePlusInfo)
      case 'absolute':
        return Doc.show(page.absoluteInfo)
      case 'multiplier':
        return Doc.show(page.multiplierInfo)
    }
  }

  clearPlacements (cacheKey: string) {
    const { page, updatedConfig: cfg } = this
    while (page.buyPlacementsTableBody.children.length > 1) {
      page.buyPlacementsTableBody.children[0].remove()
    }
    while (page.sellPlacementsTableBody.children.length > 1) {
      page.sellPlacementsTableBody.children[0].remove()
    }
    this.placementsCache[cacheKey] = [cfg.buyPlacements, cfg.sellPlacements]
    cfg.buyPlacements.splice(0, cfg.buyPlacements.length)
    cfg.sellPlacements.splice(0, cfg.sellPlacements.length)
  }

  loadCachedPlacements (cacheKey: string) {
    const c = this.placementsCache[cacheKey]
    if (!c) return
    const { updatedConfig: cfg } = this
    cfg.buyPlacements.splice(0, cfg.buyPlacements.length)
    cfg.sellPlacements.splice(0, cfg.sellPlacements.length)
    cfg.buyPlacements.push(...c[0])
    cfg.sellPlacements.push(...c[1])
    const gapStrategy = cacheKey === arbMMRowCacheKey ? GapStrategyMultiplier : cacheKey
    for (const p of cfg.buyPlacements) this.addPlacement(true, p, gapStrategy)
    for (const p of cfg.sellPlacements) this.addPlacement(false, p, gapStrategy)
  }

  /*
   * setOriginalValues sets the updatedConfig field to be equal to the
   * and sets the values displayed buy each field input to be equal
   * to the values since the last save.
   */
  setOriginalValues () {
    const {
      page, originalConfig: oldCfg, updatedConfig: cfg, specs: { cexName, botType }
    } = this

    this.clearPlacements(cexName ? arbMMRowCacheKey : cfg.gapStrategy)

    const assign = (to: any, from: any) => { // not recursive
      for (const [k, v] of Object.entries(from)) {
        if (Array.isArray(v)) {
          to[k].splice(0, to[k].length)
          for (const i of v) to[k].push(i)
        } else if (typeof v === 'object') Object.assign(to[k], v)
        else to[k] = from[k]
      }
    }
    assign(cfg, JSON.parse(JSON.stringify(oldCfg)))

    const tol = cfg.driftTolerance ?? defaultDriftTolerance.value
    this.driftTolerance.setValue(tol * 100)
    this.driftToleranceSlider.setValue(tol / defaultDriftTolerance.maxV)

    const persist = cfg.orderPersistence ?? defaultOrderPersistence.value
    this.orderPersistence.setValue(persist)
    this.orderPersistenceSlider.setValue(persist / defaultOrderPersistence.maxV)

    const profit = cfg.profit ?? defaultProfit.value
    page.profitInput.value = String(profit * 100)
    this.qcProfit.setValue(profit * 100)
    this.qcProfitSlider.setValue((profit - defaultProfit.minV) / defaultProfit.range)

    if (cexName) {
      page.enableRebalance.checked = cfg.uiConfig.cexRebalance || cfg.uiConfig.internalTransfers
      page.internalOnlyRadio.checked = cfg.uiConfig.internalTransfers
      page.externalTransfersRadio.checked = cfg.uiConfig.cexRebalance
      this.autoRebalanceChanged()
    }

    // Gap strategy
    if (!page.gapStrategySelect.options) return
    Array.from(page.gapStrategySelect.options).forEach((opt: HTMLOptionElement) => { opt.selected = opt.value === cfg.gapStrategy })
    this.setGapFactorLabels(cfg.gapStrategy)

    if (botType === botTypeBasicMM) {
      Doc.show(page.gapStrategyBox)
      Doc.hide(page.profitSelectorBox, page.orderPersistenceBox)
      this.setGapFactorLabels(page.gapStrategySelect.value || '')
    } else if (cexName && app().mmStatus.cexes[cexName]) {
      Doc.hide(page.gapStrategyBox)
      Doc.show(page.profitSelectorBox, page.orderPersistenceBox)
      this.setArbMMLabels()
    }

    // Buy/Sell placements
    oldCfg.buyPlacements.forEach((p) => { this.addPlacement(true, p) })
    oldCfg.sellPlacements.forEach((p) => { this.addPlacement(false, p) })

    // Quick balance
    this.buyBufferInput.setValue(cfg.uiConfig.quickBalance.buysBuffer)
    this.sellBufferInput.setValue(cfg.uiConfig.quickBalance.sellsBuffer)
    this.buyFeeReserveInput.setValue(cfg.uiConfig.quickBalance.buyFeeReserve)
    this.sellFeeReserveInput.setValue(cfg.uiConfig.quickBalance.sellFeeReserve)
    this.slippageBufferInput.setValue(cfg.uiConfig.quickBalance.slippageBuffer)
    this.setQuickBalanceSliderValue(cfg.uiConfig.quickBalance.buysBuffer, 'buyBuffer')
    this.setQuickBalanceSliderValue(cfg.uiConfig.quickBalance.sellsBuffer, 'sellBuffer')
    this.setQuickBalanceSliderValue(cfg.uiConfig.quickBalance.buyFeeReserve, 'buyFeeReserve')
    this.setQuickBalanceSliderValue(cfg.uiConfig.quickBalance.sellFeeReserve, 'sellFeeReserve')
    this.setQuickBalanceSliderValue(cfg.uiConfig.quickBalance.slippageBuffer, 'slippageBuffer')

    // Manual balance
    const { bui, qui, baseFeeUI, quoteFeeUI } = this.walletStuff()
    const { baseID, quoteID, baseFeeAssetID, quoteFeeAssetID } = this.marketStuff()
    this.baseDexBalanceInput.setValue(cfg.uiConfig.allocation.dex[baseID] / bui.conventional.conversionFactor)
    this.quoteDexBalanceInput.setValue(cfg.uiConfig.allocation.dex[quoteID] / qui.conventional.conversionFactor)
    this.baseCexBalanceInput.setValue(cfg.uiConfig.allocation.cex[baseID] / bui.conventional.conversionFactor)
    this.quoteCexBalanceInput.setValue(cfg.uiConfig.allocation.cex[quoteID] / qui.conventional.conversionFactor)
    this.baseFeeBalanceInput.setValue(cfg.uiConfig.allocation.dex[baseFeeAssetID] / baseFeeUI.conventional.conversionFactor)
    this.quoteFeeBalanceInput.setValue(cfg.uiConfig.allocation.dex[quoteFeeAssetID] / quoteFeeUI.conventional.conversionFactor)
    this.setManualBalanceSliderValue(cfg.uiConfig.allocation.dex[quoteID], 'quote', 'dex')
    this.setManualBalanceSliderValue(cfg.uiConfig.allocation.cex[baseID], 'base', 'cex')
    this.setManualBalanceSliderValue(cfg.uiConfig.allocation.cex[quoteID], 'quote', 'cex')
    this.setManualBalanceSliderValue(cfg.uiConfig.allocation.dex[baseFeeAssetID], 'base', 'dex')
    this.setManualBalanceSliderValue(cfg.uiConfig.allocation.dex[quoteFeeAssetID], 'quote', 'dex')

    this.setAllocationTechnique(cfg.uiConfig.usingQuickBalance)

    if (cfg.uiConfig.cexRebalance) {
      this.minTransferInputChanged(cfg.uiConfig.baseMinTransfer / bui.conventional.conversionFactor, 'base')
      this.minTransferInputChanged(cfg.uiConfig.quoteMinTransfer / qui.conventional.conversionFactor, 'quote')
    }

    this.baseSettings.clear()
    this.quoteSettings.clear()
    this.baseSettings.init(cfg.baseOptions, this.specs.baseID, false)
    this.quoteSettings.init(cfg.quoteOptions, this.specs.quoteID, true)

    this.updateModifiedMarkers()
    if (Doc.isDisplayed(page.quickConfig)) this.switchToQuickConfig()
  }

  /*
   * validateFields validates configuration values and optionally shows error
   * messages.
   */
  validateFields (showErrors: boolean): boolean {
    let ok = true
    const {
      page, specs: { botType },
      updatedConfig: { sellPlacements, buyPlacements, profit }
    } = this
    const setError = (errEl: PageElement, errID: string) => {
      ok = false
      if (!showErrors) return
      errEl.textContent = intl.prep(errID)
      Doc.show(errEl)
    }
    if (showErrors) {
      Doc.hide(
        page.buyPlacementsErr, page.sellPlacementsErr, page.profitInputErr
      )
    }
    if (botType !== botTypeBasicArb && buyPlacements.length + sellPlacements.length === 0) {
      setError(page.buyPlacementsErr, intl.ID_NO_PLACEMENTS)
      setError(page.sellPlacementsErr, intl.ID_NO_PLACEMENTS)
    }
    if (botType !== botTypeBasicMM) {
      if (isNaN(profit)) setError(page.profitInputErr, intl.ID_INVALID_VALUE)
      else if (profit === 0) setError(page.profitInputErr, intl.ID_NO_ZERO)
    }
    return ok
  }

  autoRebalanceSettings () : AutoRebalanceConfig | undefined {
    const { updatedConfig: cfg } = this
    if (!cfg.uiConfig.cexRebalance && !cfg.uiConfig.internalTransfers) return
    return {
      minBaseTransfer: cfg.uiConfig.baseMinTransfer,
      minQuoteTransfer: cfg.uiConfig.quoteMinTransfer,
      internalOnly: !cfg.uiConfig.cexRebalance
    }
  }

  async doSave () {
    // Make a copy and delete either the basic mm config or the arb-mm config,
    // depending on whether a cex is selected.
    if (!this.validateFields(true)) return
    const { cfg, baseID, quoteID, host, botType, cexName } = this.marketStuff()

    const botCfg: BotConfig = {
      host: host,
      baseID: baseID,
      quoteID: quoteID,
      cexName: cexName ?? '',
      uiConfig: cfg.uiConfig,
      baseWalletOptions: cfg.baseOptions,
      quoteWalletOptions: cfg.quoteOptions
    }
    switch (botType) {
      case botTypeBasicMM:
        botCfg.basicMarketMakingConfig = this.basicMMConfig()
        break
      case botTypeArbMM:
        botCfg.arbMarketMakingConfig = this.arbMMConfig()
        break
      case botTypeBasicArb:
        botCfg.simpleArbConfig = this.basicArbConfig()
    }

    app().log('mm', 'saving bot config', botCfg)

    // When loading a running bot with balances configured manually, we set
    // all the diffs initially to 0. However, we save the UI with the total
    // allocations for each asset, so that if the bot is stopped and then the
    // settings are reloaded, the total allocations will be shown.
    const updatedAllocation = cfg.uiConfig.allocation
    if (!botCfg.uiConfig.usingQuickBalance && this.runningBot) {
      const botAlloc = this.runningBotAllocations()
      if (botAlloc) {
        botCfg.uiConfig.allocation = combineBotAllocations(botAlloc, updatedAllocation)
      }
    }

    if (this.runningBot) await MM.updateRunningBot(botCfg, updatedAllocation, this.autoRebalanceSettings())
    else await MM.updateBotConfig(botCfg)

    await app().fetchMMStatus()
    this.originalConfig = JSON.parse(JSON.stringify(cfg))
    this.updateModifiedMarkers()
    const lastBots = State.fetchLocal(lastBotsLK) || {}
    lastBots[`${baseID}_${quoteID}_${host}`] = this.specs
    State.storeLocal(lastBotsLK, lastBots)
    if (cexName) State.storeLocal(lastArbExchangeLK, cexName)
  }

  async updateSettings () {
    await this.doSave()
    app().loadPage('mm')
  }

  async saveSettingsAndStart () {
    const { specs: { host, baseID, quoteID }, updatedConfig: cfg } = this
    await this.doSave()

    const startConfig: StartConfig = {
      baseID: baseID,
      quoteID: quoteID,
      host: host,
      alloc: cfg.uiConfig.allocation,
      autoRebalance: this.autoRebalanceSettings()
    }

    await MM.startBot(startConfig)
    app().loadPage('mm')
  }

  async delete () {
    const { page, specs: { host, baseID, quoteID } } = this
    Doc.hide(page.deleteErr)
    const loaded = app().loading(page.botSettingsContainer)
    const resp = await MM.removeBotConfig(host, baseID, quoteID)
    loaded()
    if (!app().checkResponse(resp)) {
      page.deleteErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: resp.msg })
      Doc.show(page.deleteErr)
      return
    }
    await app().fetchMMStatus()
    app().loadPage('mm')
  }

  /*
   * arbMMConfig parses the configuration for the arb-mm bot. Only one of
   * arbMMConfig or basicMMConfig should be used when updating the bot
   * configuration. Which is used depends on if the user has configured and
   * selected a CEX or not.
   */
  arbMMConfig (): ArbMarketMakingConfig {
    const { updatedConfig: cfg } = this
    const arbCfg: ArbMarketMakingConfig = {
      buyPlacements: [],
      sellPlacements: [],
      profit: cfg.profit,
      driftTolerance: cfg.driftTolerance,
      orderPersistence: cfg.orderPersistence,
      multiHop: cfg.multiHop
    }
    for (const p of cfg.buyPlacements) arbCfg.buyPlacements.push({ lots: p.lots, multiplier: p.gapFactor })
    for (const p of cfg.sellPlacements) arbCfg.sellPlacements.push({ lots: p.lots, multiplier: p.gapFactor })
    return arbCfg
  }

  basicArbConfig (): SimpleArbConfig {
    const { updatedConfig: cfg } = this
    const arbCfg: SimpleArbConfig = {
      profitTrigger: cfg.profit,
      maxActiveArbs: 100, // TODO
      numEpochsLeaveOpen: cfg.orderPersistence
    }
    return arbCfg
  }

  /*
   * basicMMConfig parses the configuration for the basic marketmaker. Only of
   * of basidMMConfig or arbMMConfig should be used when updating the bot
   * configuration.
   */
  basicMMConfig (): BasicMarketMakingConfig {
    const { updatedConfig: cfg } = this
    const mmCfg: BasicMarketMakingConfig = {
      gapStrategy: cfg.gapStrategy,
      sellPlacements: cfg.sellPlacements,
      buyPlacements: cfg.buyPlacements,
      driftTolerance: cfg.driftTolerance
    }
    return mmCfg
  }

  /*
   * fetchOracles fetches the current oracle rates and fiat rates, and displays
   * them on the screen.
   */
  async fetchMarketReport (): Promise<void> {
    const { page, specs: { host, baseID, quoteID } } = this

    const res = await MM.report(host, baseID, quoteID)
    Doc.hide(page.oraclesLoading, page.oraclesTable, page.noOracles)

    if (!app().checkResponse(res)) {
      page.oraclesErrMsg.textContent = res.msg
      Doc.show(page.oraclesErr)
      return
    }

    const r = this.marketReport = res.report as MarketReport
    if (!r.oracles || r.oracles.length === 0) {
      Doc.show(page.noOracles)
    } else {
      Doc.hide(page.noOracles)
      Doc.empty(page.oracles)
      for (const o of r.oracles ?? []) {
        const tr = page.oracleTmpl.cloneNode(true) as PageElement
        page.oracles.appendChild(tr)
        const tmpl = Doc.parseTemplate(tr)
        tmpl.logo.src = 'img/' + o.host + '.png'
        tmpl.host.textContent = ExchangeNames[o.host]
        tmpl.volume.textContent = Doc.formatFourSigFigs(o.usdVol)
        tmpl.price.textContent = Doc.formatFourSigFigs((o.bestBuy + o.bestSell) / 2)
      }
      page.avgPrice.textContent = r.price ? Doc.formatFourSigFigs(r.price) : '0'
      Doc.show(page.oraclesTable)
    }

    if (r.baseFiatRate > 0) {
      page.baseFiatRate.textContent = Doc.formatFourSigFigs(r.baseFiatRate)
    } else {
      page.baseFiatRate.textContent = 'N/A'
    }

    if (r.quoteFiatRate > 0) {
      page.quoteFiatRate.textContent = Doc.formatFourSigFigs(r.quoteFiatRate)
    } else {
      page.quoteFiatRate.textContent = 'N/A'
    }
    Doc.show(page.fiatRates)
  }

  /*
   * handleCEXSubmit handles clicks on the CEX configuration submission button.
   */
  async cexConfigured (cexName: string) {
    const { page, formSpecs: { host, baseID, quoteID } } = this
    const dinfo = CEXDisplayInfos[cexName]
    for (const { baseID, quoteID, tmpl, arbs } of this.marketRows) {
      if (arbs.indexOf(cexName) !== -1) continue
      const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
      if (cexHasMarket(cexName)) {
        const img = page.arbBttnTmpl.cloneNode(true) as PageElement
        img.src = dinfo.logo
        tmpl.arbs.appendChild(img)
        arbs.push(cexName)
      }
    }
    this.setCEXAvailability(baseID, quoteID, cexName)
    this.showBotTypeForm(host, baseID, quoteID, botTypeArbMM, cexName)
  }

  /*
   * setupCEXes should be called during initialization.
   */
  setupCEXes () {
    this.formCexes = {}
    for (const name of Object.keys(CEXDisplayInfos)) this.addCEX(name)
  }

  /*
   * setCEXAvailability sets the coloring and messaging of the CEX selection
   * buttons.
   */
  setCEXAvailability (baseID: number, quoteID: number, selectedCEX?: string) {
    const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
    for (const { name, div, tmpl } of Object.values(this.formCexes)) {
      const has = cexHasMarket(name)
      const cexStatus = app().mmStatus.cexes[name]
      Doc.hide(tmpl.unavailable, tmpl.needsconfig, tmpl.disconnected)
      Doc.setVis(Boolean(cexStatus), tmpl.reconfig)
      tmpl.logo.classList.remove('greyscal')
      div.classList.toggle('configured', Boolean(cexStatus) && !cexStatus.connectErr)
      if (!cexStatus) {
        Doc.show(tmpl.needsconfig)
      } else if (cexStatus.connectErr) {
        Doc.show(tmpl.disconnected)
      } else if (!has) {
        Doc.show(tmpl.unavailable)
        tmpl.logo.classList.add('greyscal')
      } else if (name === selectedCEX) this.selectFormCEX(name)
    }
  }

  selectFormCEX (cexName: string) {
    for (const { name, div } of Object.values(this.formCexes)) {
      div.classList.toggle('selected', name === cexName)
    }
  }

  addCEX (cexName: string) {
    const dinfo = CEXDisplayInfos[cexName]
    const div = this.page.cexOptTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    tmpl.name.textContent = dinfo.name
    tmpl.logo.src = dinfo.logo
    this.page.cexSelection.appendChild(div)
    this.formCexes[cexName] = { name: cexName, div, tmpl }
    Doc.bind(div, 'click', () => {
      const cexStatus = app().mmStatus.cexes[cexName]
      if (!cexStatus || cexStatus.connectErr) {
        this.showCEXConfigForm(cexName)
        return
      }
      const cex = this.formCexes[cexName]
      if (cex.div.classList.contains('selected')) { // unselect
        for (const cex of Object.values(this.formCexes)) cex.div.classList.remove('selected')
        const { baseID, quoteID } = this.formSpecs
        this.setCEXAvailability(baseID, quoteID)
        return
      }
      for (const cex of Object.values(this.formCexes)) cex.div.classList.toggle('selected', cex.name === cexName)
    })
    Doc.bind(tmpl.reconfig, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      this.showCEXConfigForm(cexName)
    })
  }

  showCEXConfigForm (cexName: string) {
    const page = this.page
    this.cexConfigForm.setCEX(cexName)
    this.forms.show(page.cexConfigForm)
  }

  cexSupportsArbOnMarket (baseID: number, quoteID: number, cexStatus: MMCEXStatus): [boolean, MultiHopArbMarket[]] {
    let supportsDirectArb = false

    const baseMarkets = new Set<number>()
    const quoteMarkets = new Set<number>()

    // Find all markets that trade either base or quote asset
    for (const { baseID: b, quoteID: q } of Object.values(cexStatus.markets ?? [])) {
      if (b === baseID && q === quoteID) {
        supportsDirectArb = true
        break
      }

      if (b === baseID) {
        baseMarkets.add(q)
      } else if (q === baseID) {
        baseMarkets.add(b)
      }
      if (b === quoteID) {
        quoteMarkets.add(q)
      } else if (q === quoteID) {
        quoteMarkets.add(b)
      }
    }

    // Find all bridge assets that connect base and quote
    const multiHopMarkets: MultiHopArbMarket[] = []
    for (const bridgeAsset of baseMarkets) {
      if (quoteMarkets.has(bridgeAsset)) {
        // Check if bridge asset exists as base or quote in both markets
        const markets = Object.values(cexStatus.markets ?? {})
        let baseMarket = null
        let quoteMarket = null
        for (const market of markets) {
          if ((market.baseID === baseID && market.quoteID === bridgeAsset) ||
              (market.baseID === bridgeAsset && market.quoteID === baseID)) {
            baseMarket = market
          }
          if ((market.baseID === quoteID && market.quoteID === bridgeAsset) ||
              (market.baseID === bridgeAsset && market.quoteID === quoteID)) {
            quoteMarket = market
          }
        }

        if (baseMarket && quoteMarket) {
          multiHopMarkets.push({
            BaseMarket: [baseMarket.baseID, baseMarket.quoteID],
            QuoteMarket: [quoteMarket.baseID, quoteMarket.quoteID],
            BridgeAsset: bridgeAsset
          })
        }
      }
    }

    return [supportsDirectArb, multiHopMarkets]
  }

  /*
   * cexMarketSupportFilter returns a lookup CEXes that have a matching market
   * for the currently selected base and quote assets.
   */
  cexMarketSupportFilter (baseID: number, quoteID: number) {
    const cexes: Record<string, boolean> = {}
    for (const [cexName, cexStatus] of Object.entries(app().mmStatus.cexes)) {
      const [supportsDirectArb, bridgeAssets] = this.cexSupportsArbOnMarket(baseID, quoteID, cexStatus)
      if (supportsDirectArb || bridgeAssets.length > 0) {
        cexes[cexName] = true
      }
    }
    return (cexName: string) => Boolean(cexes[cexName])
  }
}

function botIsRunning (specs: BotSpecs, mmStatus: MarketMakingStatus): boolean {
  const botStatus = mmStatus.bots.find(({ config: cfg }) => cfg.host === specs.host && cfg.baseID === specs.baseID && cfg.quoteID === specs.quoteID)
  return Boolean(botStatus?.running)
}

const ExchangeNames: Record<string, string> = {
  'binance.com': 'Binance',
  'coinbase.com': 'Coinbase',
  'bittrex.com': 'Bittrex',
  'hitbtc.com': 'HitBTC',
  'exmo.com': 'EXMO'
}

function tokenAssetApprovalStatuses (host: string, b: SupportedAsset, q: SupportedAsset) {
  let baseAssetApprovalStatus = ApprovalStatus.Approved
  let quoteAssetApprovalStatus = ApprovalStatus.Approved

  if (b?.token) {
    const baseAsset = app().assets[b.id]
    const baseVersion = app().exchanges[host].assets[b.id].version
    if (baseAsset?.wallet?.approved && baseAsset.wallet.approved[baseVersion] !== undefined) {
      baseAssetApprovalStatus = baseAsset.wallet.approved[baseVersion]
    }
  }
  if (q?.token) {
    const quoteAsset = app().assets[q.id]
    const quoteVersion = app().exchanges[host].assets[q.id].version
    if (quoteAsset?.wallet?.approved && quoteAsset.wallet.approved[quoteVersion] !== undefined) {
      quoteAssetApprovalStatus = quoteAsset.wallet.approved[quoteVersion]
    }
  }

  return [
    baseAssetApprovalStatus,
    quoteAssetApprovalStatus
  ]
}

class WalletSettings {
  pg: MarketMakerSettingsPage
  div: PageElement
  page: Record<string, PageElement>
  updated: () => void
  optElements: Record<string, PageElement | NumberInput>

  constructor (pg: MarketMakerSettingsPage, div: PageElement, updated: () => void) {
    this.pg = pg
    this.div = div
    this.page = Doc.parseTemplate(div)
    this.updated = updated
  }

  clear () {
    Doc.empty(this.page.walletSettings)
  }

  init (walletConfig: Record<string, string>, assetID: number, isQuote: boolean) {
    const { page } = this
    const walletSettings = app().currentWalletDefinition(assetID)
    Doc.empty(page.walletSettings)
    Doc.setVis(!walletSettings.multifundingopts, page.walletSettingsNone)
    const { symbol } = app().assets[assetID]
    page.ticker.textContent = symbol.toUpperCase()
    page.logo.src = Doc.logoPath(symbol)
    if (!walletSettings.multifundingopts) return
    const optToDiv: Record<string, PageElement> = {}
    const dependentOpts: Record<string, string[]> = {}
    const addDependentOpt = (optKey: string, optSetting: PageElement, dependentOn: string) => {
      if (!dependentOpts[dependentOn]) dependentOpts[dependentOn] = []
      dependentOpts[dependentOn].push(optKey)
      optToDiv[optKey] = optSetting
    }
    const setDependentOptsVis = (parentOptKey: string, vis: boolean) => {
      const optKeys = dependentOpts[parentOptKey]
      if (!optKeys) return
      for (const optKey of optKeys) Doc.setVis(vis, optToDiv[optKey])
    }
    this.optElements = {}
    const addOpt = (opt: OrderOption) => {
      if (opt.quoteAssetOnly && !isQuote) return
      const currVal = walletConfig[opt.key]
      let div: PageElement | undefined
      if (opt.isboolean) {
        div = page.boolSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(div)
        tmpl.name.textContent = opt.displayname
        tmpl.input.checked = currVal === 'true'
        Doc.bind(tmpl.input, 'change', () => {
          walletConfig[opt.key] = tmpl.input.checked ? 'true' : 'false'
          setDependentOptsVis(opt.key, Boolean(tmpl.input.checked))
          this.updated()
        })
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        this.optElements[opt.key] = tmpl.input
      } else if (opt.xyRange) {
        const { start, end, xUnit } = opt.xyRange
        const range = end.x - start.x
        div = page.rangeSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(div)
        tmpl.name.textContent = opt.displayname
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        if (xUnit) tmpl.unit.textContent = xUnit
        else Doc.hide(tmpl.unit)

        const input = new NumberInput(tmpl.value, {
          prec: 1,
          changed: (rawV: number) => {
            const [v, s] = toFourSigFigs(rawV, 1)
            walletConfig[opt.key] = s
            slider.setValue((v - start.x) / range)
            this.updated()
          }
        })
        const slider = new MiniSlider(tmpl.slider, (r: number) => {
          const rawV = start.x + r * range
          const [v, s] = toFourSigFigs(rawV, 1)
          walletConfig[opt.key] = s
          input.setValue(v)
          this.updated()
        })
        // TODO: default value should be smaller or none for base asset.
        const [v, s] = toFourSigFigs(parseFloatDefault(currVal, start.x), 3)
        walletConfig[opt.key] = s
        slider.setValue((v - start.x) / range)
        input.setValue(v)
        tmpl.value.textContent = s
        this.optElements[opt.key] = input
      }
      if (!div) return console.error("don't know how to handle opt", opt)
      page.walletSettings.appendChild(div)
      if (opt.dependsOn) {
        addDependentOpt(opt.key, div, opt.dependsOn)
        const parentOptVal = walletConfig[opt.dependsOn]
        Doc.setVis(parentOptVal === 'true', div)
      }
    }
    if (walletSettings.multifundingopts && walletSettings.multifundingopts.length > 0) {
      for (const opt of walletSettings.multifundingopts) addOpt(opt)
    }
    app().bindTooltips(page.walletSettings)
  }
}

function populateAllocationTable (
  div: PageElement, baseID: number, quoteID: number, baseFeeID: number,
  quoteFeeID: number, cexName: string, allocationResult: AllocationResult,
  baseUI: UnitInfo, quoteUI: UnitInfo, baseFeeUI: UnitInfo,
  quoteFeeUI: UnitInfo, host: string) {
  const dexBalances: Record<number, number> = {}
  const cexBalances: Record<number, number> = {}
  const page = Doc.parseTemplate(div)

  const setColor = (el: PageElement, status: AllocationStatus) => {
    el.classList.remove('text-buycolor', 'text-danger', 'text-warning')
    switch (status) {
      case 'sufficient': el.classList.add('text-buycolor'); break
      case 'insufficient': el.classList.add('text-danger'); break
      case 'sufficient-with-rebalance': el.classList.add('text-warning'); break
    }
  }

  for (const [key, value] of Object.entries(allocationResult.dex)) {
    const assetID = Number(key)
    if (assetID === baseID) setColor(page.dexBaseAlloc, value.status)
    if (assetID === quoteID) setColor(page.dexQuoteAlloc, value.status)
    if (assetID === baseFeeID) setColor(page.dexBaseFeeAlloc, value.status)
    if (assetID === quoteFeeID) setColor(page.dexQuoteFeeAlloc, value.status)
    dexBalances[assetID] = value.amount
  }
  for (const [key, value] of Object.entries(allocationResult.cex)) {
    const assetID = Number(key)
    if (assetID === baseID) setColor(page.cexBaseAlloc, value.status)
    if (assetID === quoteID) setColor(page.cexQuoteAlloc, value.status)
    cexBalances[assetID] = value.amount
  }

  const baseFeeNotTraded = baseFeeID !== baseID && baseFeeID !== quoteID
  const quoteFeeNotTraded = quoteFeeID !== quoteID && quoteFeeID !== baseID

  Doc.setVis(baseFeeNotTraded, page.baseFeeHeader, page.dexBaseFeeAlloc)
  Doc.setVis(quoteFeeNotTraded && baseFeeID !== quoteFeeID, page.quoteFeeHeader, page.dexQuoteFeeAlloc)
  Doc.setVis(cexName, page.cexRow)

  const format = (v: number, unitInfo: UnitInfo) => v ? Doc.formatCoinValue(v, unitInfo) : '0'

  page.dexBaseAlloc.textContent = format(dexBalances[baseID], baseUI)
  page.dexQuoteAlloc.textContent = format(dexBalances[quoteID], quoteUI)
  if (baseFeeNotTraded) page.dexBaseFeeAlloc.textContent = format(dexBalances[baseFeeID], baseFeeUI)
  if (quoteFeeNotTraded) page.dexQuoteFeeAlloc.textContent = format(dexBalances[quoteFeeID], quoteFeeUI)

  if (cexBalances && cexName) {
    page.cexBaseAlloc.textContent = format(cexBalances[baseID], baseUI)
    page.cexQuoteAlloc.textContent = format(cexBalances[quoteID], quoteUI)
    setCexElements(div, cexName)
  }

  setMarketElements(div, baseID, quoteID, host)
}

type Fees = {
  swap: number
  redeem: number
  refund: number
  funding: number
}

interface PerLotBreakdown {
  totalAmount: number
  tradedAmount: number
  fees: Fees
  slippageBuffer: number
  multiSplitBuffer: number
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

interface PerLot {
  cex: Record<number, PerLotBreakdown>
  dex: Record<number, PerLotBreakdown>
}

interface FeeReserveBreakdown {
  buyReserves: Fees
  sellReserves: Fees
}

type AllocationStatus = 'sufficient' | 'insufficient' | 'sufficient-with-rebalance'

interface CalculationBreakdown {
  totalRequired: number

  feeReserves: FeeReserveBreakdown
  numBuyFeeReserves: number
  numSellFeeReserves: number

  numBuyLots: number
  buyLot: PerLotBreakdown
  numSellLots: number
  sellLot: PerLotBreakdown

  // initialFundingFees are the fees to initially place
  // every buy and sell lot.
  initialBuyFundingFees: number
  initialSellFundingFees: number

  available: number
  allocated: number
  rebalanceAdjustment: number

  // For running bots only
  runningBotAvailable: number
  runningBotTotal: number
}

function newCalculationBreakdown () : CalculationBreakdown {
  return {
    buyLot: newPerLotBreakdown(),
    sellLot: newPerLotBreakdown(),
    feeReserves: {
      buyReserves: { swap: 0, redeem: 0, refund: 0, funding: 0 },
      sellReserves: { swap: 0, redeem: 0, refund: 0, funding: 0 }
    },
    numBuyFeeReserves: 0,
    numSellFeeReserves: 0,
    numBuyLots: 0,
    numSellLots: 0,
    initialBuyFundingFees: 0,
    initialSellFundingFees: 0,
    totalRequired: 0,
    available: 0,
    allocated: 0,
    rebalanceAdjustment: 0,
    runningBotAvailable: 0,
    runningBotTotal: 0
  }
}

interface AllocationDetail {
  amount: number
  status: AllocationStatus
  calculation: CalculationBreakdown
}

function newAllocationDetail () : AllocationDetail {
  return {
    amount: 0,
    status: 'sufficient',
    calculation: newCalculationBreakdown()
  }
}

type AllocationResult = {
  dex: Record<number, AllocationDetail>
  cex: Record<number, AllocationDetail>
}

export type AvailableFunds = {
  dex: Record<number, number>
  cex?: Record<number, number>
}

// perLotRequirements calculates the funding requirements for a single buy and sell lot.
function perLotRequirements (
  baseID: number,
  quoteID: number,
  baseFeeID: number,
  quoteFeeID: number,
  lotSize: number,
  quoteLot: number,
  marketReport: MarketReport,
  slippageBuffer: number,
  multiSplitBuffer: number,
  oneTradeBuyFundingFees: number,
  oneTradeSellFundingFees: number,
  baseIsAccountLocker: boolean,
  quoteIsAccountLocker: boolean): { perSellLot: PerLot, perBuyLot: PerLot } {
  const perSellLot: PerLot = { cex: {}, dex: {} }
  const perBuyLot: PerLot = { cex: {}, dex: {} }
  const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeID, quoteFeeID]))
  for (const assetID of assetIDs) {
    perSellLot.dex[assetID] = newPerLotBreakdown()
    perBuyLot.dex[assetID] = newPerLotBreakdown()
    perSellLot.cex[assetID] = newPerLotBreakdown()
    perBuyLot.cex[assetID] = newPerLotBreakdown()
  }

  perSellLot.dex[baseID].tradedAmount = lotSize
  perSellLot.dex[baseFeeID].fees.swap = marketReport.baseFees.max.swap
  perSellLot.cex[quoteID].tradedAmount = quoteLot
  perSellLot.cex[quoteID].slippageBuffer = slippageBuffer
  perSellLot.dex[baseFeeID].fees.funding = oneTradeSellFundingFees
  if (baseIsAccountLocker) perSellLot.dex[baseFeeID].fees.refund = marketReport.baseFees.max.refund
  if (quoteIsAccountLocker) perSellLot.dex[quoteFeeID].fees.redeem = marketReport.quoteFees.max.redeem

  perBuyLot.dex[quoteID].tradedAmount = quoteLot
  perBuyLot.dex[quoteID].multiSplitBuffer = multiSplitBuffer
  perBuyLot.dex[quoteID].slippageBuffer = slippageBuffer
  perBuyLot.cex[baseID].tradedAmount = lotSize
  perBuyLot.dex[quoteFeeID].fees.swap = marketReport.quoteFees.max.swap
  perBuyLot.dex[quoteFeeID].fees.funding = oneTradeBuyFundingFees
  if (baseIsAccountLocker) perBuyLot.dex[baseFeeID].fees.redeem = marketReport.baseFees.max.redeem
  if (quoteIsAccountLocker) perBuyLot.dex[quoteFeeID].fees.refund = marketReport.quoteFees.max.refund

  const calculateTotalAmount = (perLot: PerLotBreakdown) : number => {
    let total = perLot.tradedAmount
    const slippagePercentage = perLot.slippageBuffer / 100
    const multiSplitPercentage = perLot.multiSplitBuffer / 100
    total *= (1 + slippagePercentage + multiSplitPercentage)
    total = Math.floor(total)
    total += perLot.fees.swap + perLot.fees.redeem + perLot.fees.refund + perLot.fees.funding
    return total
  }

  for (const assetID of assetIDs) {
    perSellLot.dex[assetID].totalAmount = calculateTotalAmount(perSellLot.dex[assetID])
    perBuyLot.dex[assetID].totalAmount = calculateTotalAmount(perBuyLot.dex[assetID])
    perSellLot.cex[assetID].totalAmount = calculateTotalAmount(perSellLot.cex[assetID])
    perBuyLot.cex[assetID].totalAmount = calculateTotalAmount(perBuyLot.cex[assetID])
  }

  return { perSellLot, perBuyLot }
}

// requiredFunds calculates the total funds required for a bot based on the quick
// allocation settings.
function requiredFunds (
  numBuyLots: number,
  numSellLots: number,
  lotSize: number,
  quoteLot: number,
  slippageBuffer: number,
  multiSplitBuffer: number,
  buyFeeBuffer: number,
  sellFeeBuffer: number,
  marketReport: MarketReport,
  baseIsAccountLocker: boolean,
  quoteIsAccountLocker: boolean,
  baseID: number,
  quoteID: number,
  baseFeeID: number,
  quoteFeeID: number,
  buyFundingFees: number,
  sellFundingFees: number,
  oneTradeBuyFundingFees: number,
  oneTradeSellFundingFees: number) : AllocationResult {
  const toAllocate: AllocationResult = { dex: {}, cex: {} }
  const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeID, quoteFeeID]))

  for (const assetID of assetIDs) {
    toAllocate.dex[assetID] = newAllocationDetail()
    toAllocate.cex[assetID] = newAllocationDetail()
  }

  const { perBuyLot, perSellLot } = perLotRequirements(baseID, quoteID, baseFeeID, quoteFeeID,
    lotSize, quoteLot, marketReport, slippageBuffer, multiSplitBuffer, oneTradeBuyFundingFees,
    oneTradeSellFundingFees, baseIsAccountLocker, quoteIsAccountLocker)

  for (const assetID of assetIDs) {
    toAllocate.dex[assetID].calculation.buyLot = perBuyLot.dex[assetID]
    toAllocate.dex[assetID].calculation.sellLot = perSellLot.dex[assetID]
    toAllocate.cex[assetID].calculation.buyLot = perBuyLot.cex[assetID]
    toAllocate.cex[assetID].calculation.sellLot = perSellLot.cex[assetID]
    toAllocate.dex[assetID].calculation.numBuyLots = numBuyLots
    toAllocate.dex[assetID].calculation.numSellLots = numSellLots
    toAllocate.cex[assetID].calculation.numBuyLots = numBuyLots
    toAllocate.cex[assetID].calculation.numSellLots = numSellLots

    if (assetID === baseFeeID) {
      toAllocate.dex[assetID].calculation.feeReserves.sellReserves.swap = marketReport.baseFees.estimated.swap
      if (baseIsAccountLocker) {
        toAllocate.dex[assetID].calculation.feeReserves.buyReserves.redeem = marketReport.baseFees.estimated.redeem
        toAllocate.dex[assetID].calculation.feeReserves.sellReserves.refund = marketReport.baseFees.estimated.refund
      }
      toAllocate.dex[assetID].calculation.initialSellFundingFees = sellFundingFees
    }

    if (assetID === quoteFeeID) {
      toAllocate.dex[assetID].calculation.feeReserves.buyReserves.swap = marketReport.quoteFees.estimated.swap
      if (quoteIsAccountLocker) {
        toAllocate.dex[assetID].calculation.feeReserves.sellReserves.redeem = marketReport.quoteFees.estimated.redeem
        toAllocate.dex[assetID].calculation.feeReserves.buyReserves.refund = marketReport.quoteFees.estimated.refund
      }
      toAllocate.dex[assetID].calculation.initialBuyFundingFees = buyFundingFees
      toAllocate.dex[assetID].calculation.initialSellFundingFees = sellFundingFees
    }
    toAllocate.dex[assetID].calculation.numBuyFeeReserves = buyFeeBuffer
    toAllocate.dex[assetID].calculation.numSellFeeReserves = sellFeeBuffer
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
    return total
  }

  for (const assetID of assetIDs) {
    toAllocate.dex[assetID].calculation.totalRequired = calculateTotalRequired(toAllocate.dex[assetID].calculation)
    toAllocate.cex[assetID].calculation.totalRequired = calculateTotalRequired(toAllocate.cex[assetID].calculation)
  }

  return toAllocate
}

// toAllocation calculates the quick allocations for a bot that is not running.
function toAllocate (
  numBuyLots: number,
  numSellLots: number,
  lotSize: number,
  quoteLot: number,
  slippageBuffer: number,
  multiSplitBuffer: number,
  buyFeeBuffer: number,
  sellFeeBuffer: number,
  marketReport: MarketReport,
  availableFunds: AvailableFunds,
  canRebalance: boolean,
  baseID: number,
  quoteID: number,
  baseFeeID: number,
  quoteFeeID: number,
  baseIsAccountLocker: boolean,
  quoteIsAccountLocker: boolean,
  buyFundingFees: number,
  sellFundingFees: number,
  oneTradeBuyFundingFees: number,
  oneTradeSellFundingFees: number
) : AllocationResult {
  const result = requiredFunds(numBuyLots, numSellLots, lotSize, quoteLot, slippageBuffer, multiSplitBuffer,
    buyFeeBuffer, sellFeeBuffer, marketReport, baseIsAccountLocker, quoteIsAccountLocker, baseID, quoteID,
    baseFeeID, quoteFeeID, buyFundingFees, sellFundingFees, oneTradeBuyFundingFees, oneTradeSellFundingFees)

  const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeID, quoteFeeID]))

  // For each asset, check if allocation is sufficient and set status
  for (const assetID of assetIDs) {
    result.dex[assetID].calculation.available = availableFunds.dex[assetID] ?? 0
    result.cex[assetID].calculation.available = availableFunds.cex?.[assetID] ?? 0

    // dexSurplus / cexSurplus may be negative
    const dexSurplus = result.dex[assetID].calculation.available - result.dex[assetID].calculation.totalRequired
    const cexSurplus = result.cex[assetID].calculation.available - result.cex[assetID].calculation.totalRequired

    if (dexSurplus >= 0) {
      result.dex[assetID].amount = result.dex[assetID].calculation.totalRequired
    } else {
      result.dex[assetID].status = 'insufficient'
      result.dex[assetID].amount = result.dex[assetID].calculation.available
    }

    if (cexSurplus >= 0) {
      result.cex[assetID].amount = result.cex[assetID].calculation.totalRequired
    } else {
      result.cex[assetID].status = 'insufficient'
      result.cex[assetID].amount = result.cex[assetID].calculation.available
    }

    // If dex is insufficient, increase cex allocation
    if (canRebalance && dexSurplus < 0 && cexSurplus > 0) {
      const dexDeficit = -dexSurplus
      const additionalCEX = Math.min(dexDeficit, cexSurplus)
      result.cex[assetID].calculation.rebalanceAdjustment = additionalCEX
      result.cex[assetID].amount += additionalCEX
      if (cexSurplus >= dexDeficit) result.dex[assetID].status = 'sufficient-with-rebalance'
    }

    // If cex is insufficient, increase dex allocation
    if (canRebalance && cexSurplus < 0 && dexSurplus > 0) {
      const cexDeficit = -cexSurplus
      const additionalDEX = Math.min(cexDeficit, dexSurplus)
      result.dex[assetID].calculation.rebalanceAdjustment = additionalDEX
      result.dex[assetID].amount += additionalDEX
      if (dexSurplus >= cexDeficit) result.cex[assetID].status = 'sufficient-with-rebalance'
    }
  }

  return result
}

// toAllocateRunning calculates the quick allocations for a running bot.
function toAllocateRunning (
  numBuyLots: number,
  numSellLots: number,
  lotSize: number,
  quoteLot: number,
  slippageBuffer: number,
  multiSplitBuffer: number,
  buyFeeBuffer: number,
  sellFeeBuffer: number,
  marketReport: MarketReport,
  availableFunds: AvailableFunds,
  canRebalance: boolean,
  baseID: number,
  quoteID: number,
  baseFeeID: number,
  quoteFeeID: number,
  baseIsAccountLocker: boolean,
  quoteIsAccountLocker: boolean,
  runStats: RunStats,
  buyFundingFees: number,
  sellFundingFees: number,
  oneTradeBuyFundingFees: number,
  oneTradeSellFundingFees: number) : AllocationResult {
  const result = requiredFunds(numBuyLots, numSellLots, lotSize, quoteLot, slippageBuffer, multiSplitBuffer, buyFeeBuffer, sellFeeBuffer,
    marketReport, baseIsAccountLocker, quoteIsAccountLocker, baseID, quoteID, baseFeeID, quoteFeeID,
    buyFundingFees, sellFundingFees, oneTradeBuyFundingFees, oneTradeSellFundingFees)

  const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeID, quoteFeeID]))

  const totalBotBalance = (source: 'cex' | 'dex', assetID: number) => {
    let bals
    if (source === 'dex') {
      bals = runStats.dexBalances[assetID] ?? { available: 0, locked: 0, pending: 0, reserved: 0 }
    } else {
      bals = runStats.cexBalances[assetID] ?? { available: 0, locked: 0, pending: 0, reserved: 0 }
    }
    return bals.available + bals.locked + bals.pending + bals.reserved
  }

  for (const assetID of assetIDs) {
    result.dex[assetID].calculation.runningBotTotal = totalBotBalance('dex', assetID)
    result.cex[assetID].calculation.runningBotTotal = totalBotBalance('cex', assetID)
    result.dex[assetID].calculation.runningBotAvailable = runStats.dexBalances[assetID]?.available ?? 0
    result.cex[assetID].calculation.runningBotAvailable = runStats.cexBalances[assetID]?.available ?? 0
    result.dex[assetID].calculation.available = availableFunds.dex[assetID] ?? 0
    result.cex[assetID].calculation.available = availableFunds.cex?.[assetID] ?? 0

    const dexTotalAvailable = result.dex[assetID].calculation.runningBotTotal + result.dex[assetID].calculation.available
    const dexSurplus = dexTotalAvailable - result.dex[assetID].calculation.totalRequired

    const cexTotalAvailable = result.cex[assetID].calculation.runningBotTotal + result.cex[assetID].calculation.available
    const cexSurplus = cexTotalAvailable - result.cex[assetID].calculation.totalRequired

    if (dexSurplus >= 0) {
      result.dex[assetID].amount = result.dex[assetID].calculation.totalRequired - result.dex[assetID].calculation.runningBotTotal
      if (result.dex[assetID].amount < 0) result.dex[assetID].amount = -Math.min(-result.dex[assetID].amount, result.dex[assetID].calculation.runningBotAvailable)
    } else {
      result.dex[assetID].status = 'insufficient'
      result.dex[assetID].amount = result.dex[assetID].calculation.available
    }

    if (cexSurplus >= 0) {
      result.cex[assetID].amount = result.cex[assetID].calculation.totalRequired - result.cex[assetID].calculation.runningBotTotal
      if (result.cex[assetID].amount < 0) result.cex[assetID].amount = -Math.min(-result.cex[assetID].amount, result.cex[assetID].calculation.runningBotAvailable)
    } else {
      result.cex[assetID].status = 'insufficient'
      result.cex[assetID].amount = result.cex[assetID].calculation.available
    }

    // If dex is insufficient, increase cex allocation
    if (canRebalance && dexSurplus < 0 && cexSurplus > 0) {
      const dexDeficit = -dexSurplus
      const additionalCEX = Math.min(dexDeficit, cexSurplus)
      result.cex[assetID].calculation.rebalanceAdjustment = additionalCEX
      result.cex[assetID].amount += additionalCEX
      if (cexSurplus >= dexDeficit) result.dex[assetID].status = 'sufficient-with-rebalance'
    }

    // If cex is insufficient, increase dex allocation
    if (canRebalance && cexSurplus < 0 && dexSurplus > 0) {
      const cexDeficit = -cexSurplus
      const additionalDEX = Math.min(cexDeficit, dexSurplus)
      result.dex[assetID].calculation.rebalanceAdjustment = additionalDEX
      result.dex[assetID].amount += additionalDEX
      if (dexSurplus >= cexDeficit) result.cex[assetID].status = 'sufficient-with-rebalance'
    }
  }

  return result
}

// combineBotAllocations combines two allocations. If the result of an allocation
// is negative, it is set to 0.
function combineBotAllocations (alloc1: BotBalanceAllocation, alloc2: BotBalanceAllocation) : BotBalanceAllocation {
  const result: BotBalanceAllocation = { dex: {}, cex: {} }

  for (const assetIDStr of Object.keys(alloc1.dex)) {
    const assetID = Number(assetIDStr)
    result.dex[assetID] = (alloc1.dex?.[assetID] ?? 0) + (alloc2.dex?.[assetID] ?? 0)
    if (result.dex[assetID] < 0) {
      result.dex[assetID] = 0
    }
  }

  for (const assetIDStr of Object.keys(alloc1.cex)) {
    const assetID = Number(assetIDStr)
    result.cex[assetID] = (alloc1.cex?.[assetID] ?? 0) + (alloc2.cex?.[assetID] ?? 0)
    if (result.cex[assetID] < 0) {
      result.cex[assetID] = 0
    }
  }

  return result
}
