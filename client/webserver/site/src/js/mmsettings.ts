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
  BotAssetConfig,
  ApprovalStatus,
  SupportedAsset,
  WalletState,
  UnitInfo,
  ProjectedAlloc,
  AssetBookingFees
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
  GapStrategyPercentPlus,
  feesAndCommit
} from './mmutil'
import { Forms, bind as bindForm, NewWalletForm, TokenApprovalForm, DepositAddress, CEXConfigurationForm } from './forms'
import * as intl from './locales'
import * as OrderUtil from './orderutil'

const specLK = 'lastMMSpecs'
const lastBotsLK = 'lastBots'
const lastArbExchangeLK = 'lastArbExchange'
const arbMMRowCacheKey = 'arbmm'

const defaultSwapReserves = {
  n: 50,
  prec: 0,
  inc: 10,
  minR: 0,
  maxR: 1000,
  range: 1000
}
const defaultOrderReserves = {
  factor: 1.0,
  minR: 0,
  maxR: 3,
  range: 3,
  prec: 3
}
const defaultTransfer = {
  factor: 0.1,
  minR: 0,
  maxR: 1,
  range: 1
}
const defaultSlippage = {
  factor: 0.05,
  minR: 0,
  maxR: 0.3,
  range: 0.3,
  prec: 3
}
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

const defaultMarketMakingConfig: ConfigState = {
  gapStrategy: GapStrategyPercentPlus,
  sellPlacements: [],
  buyPlacements: [],
  driftTolerance: defaultDriftTolerance.value,
  profit: 0.02,
  orderPersistence: defaultOrderPersistence.value,
  cexRebalance: true,
  simpleArbLots: 1
} as any as ConfigState

const defaultBotAssetConfig: BotAssetConfig = {
  swapFeeN: defaultSwapReserves.n,
  orderReservesFactor: defaultOrderReserves.factor,
  slippageBufferFactor: defaultSlippage.factor,
  transferFactor: defaultTransfer.factor
}

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
  cexRebalance: boolean
  disabled: boolean
  buyPlacements: OrderPlacement[]
  sellPlacements: OrderPlacement[]
  baseOptions: Record<string, string>
  quoteOptions: Record<string, string>
  baseConfig: BotAssetConfig
  quoteConfig: BotAssetConfig
  simpleArbLots: number
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

export default class MarketMakerSettingsPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  opts: UIOpts
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
  mktID: string
  formSpecs: BotSpecs
  formCexes: Record<string, cexButton>
  placementsCache: Record<string, [OrderPlacement[], OrderPlacement[]]>
  botTypeSelectors: PageElement[]
  marketRows: MarketRow[]
  lotsPerLevelIncrement: number
  placementsChart: PlacementsChart
  basePane: AssetPane
  quotePane: AssetPane
  driftTolerance: NumberInput
  driftToleranceSlider: MiniSlider
  orderPersistence: NumberInput
  orderPersistenceSlider: MiniSlider

  constructor (main: HTMLElement, specs: BotSpecs) {
    super()

    this.placementsCache = {}
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
    page.quotePane = page.basePane.cloneNode(true) as PageElement
    page.assetPaneBox.appendChild(page.quotePane)
    this.basePane = new AssetPane(this, page.basePane)
    this.quotePane = new AssetPane(this, page.quotePane)

    app().headerSpace.appendChild(page.mmTitle)

    setOptionTemplates(page)
    Doc.cleanTemplates(
      page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl, page.placementRowTmpl,
      page.oracleTmpl, page.cexOptTmpl, page.arbBttnTmpl, page.marketRowTmpl, page.needRegTmpl
    )
    page.basePane.removeAttribute('id') // don't remove from layout

    Doc.bind(page.resetButton, 'click', () => { this.setOriginalValues() })
    Doc.bind(page.updateButton, 'click', () => { this.saveSettings() })
    Doc.bind(page.createButton, 'click', async () => { this.saveSettings() })
    Doc.bind(page.deleteBttn, 'click', () => { this.delete() })
    bindForm(page.botTypeForm, page.botTypeSubmit, () => { this.submitBotType() })
    Doc.bind(page.noMarketBttn, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.botTypeHeader, 'click', () => { this.reshowBotTypeForm() })
    Doc.bind(page.botTypeChangeMarket, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.marketHeader, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.marketFilterInput, 'input', () => { this.sortMarketRows() })
    Doc.bind(page.cexRebalanceCheckbox, 'change', () => { this.autoRebalanceChanged() })
    Doc.bind(page.switchToAdvanced, 'click', () => { this.showAdvancedConfig() })
    Doc.bind(page.switchToQuickConfig, 'click', () => { this.switchToQuickConfig() })
    Doc.bind(page.qcMatchBuffer, 'change', () => { this.matchBufferChanged() })
    Doc.bind(page.switchToUSDPerSide, 'click', () => { this.changeSideCommitmentDialog() })
    Doc.bind(page.switchToLotsPerLevel, 'click', () => { this.changeSideCommitmentDialog() })
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

  async configureUI () {
    const { page, specs } = this
    const { host, baseID, quoteID, cexName, botType } = specs

    const [{ symbol: baseSymbol, token: baseToken }, { symbol: quoteSymbol, token: quoteToken }] = [app().assets[baseID], app().assets[quoteID]]
    this.mktID = `${baseSymbol}_${quoteSymbol}`
    Doc.hide(
      page.botSettingsContainer, page.marketBox, page.updateButton, page.resetButton,
      page.createButton, page.noMarket, page.missingFiatRates
    )

    if ([baseID, quoteID, baseToken?.parentID ?? baseID, quoteToken?.parentID ?? quoteID].some((assetID: number) => !app().fiatRatesMap[assetID])) {
      Doc.show(page.missingFiatRates)
      return
    }

    Doc.show(page.marketLoading)
    State.storeLocal(specLK, specs)

    const mmStatus = app().mmStatus
    const viewOnly = isViewOnly(specs, mmStatus)
    let botCfg = liveBotConfig(host, baseID, quoteID)
    if (botCfg) {
      const oldBotType = botCfg.arbMarketMakingConfig ? botTypeArbMM : botCfg.basicMarketMakingConfig ? botTypeBasicMM : botTypeBasicArb
      if (oldBotType !== botType) botCfg = undefined
    }
    Doc.setVis(botCfg, page.deleteBttnBox)

    const oldCfg = this.originalConfig = Object.assign({}, defaultMarketMakingConfig, {
      disabled: viewOnly,
      baseOptions: this.defaultWalletOptions(baseID),
      quoteOptions: this.defaultWalletOptions(quoteID),
      buyPlacements: [],
      sellPlacements: [],
      baseConfig: Object.assign({}, defaultBotAssetConfig),
      quoteConfig: Object.assign({}, defaultBotAssetConfig)
    }) as ConfigState

    if (botCfg) {
      const { basicMarketMakingConfig: mmCfg, arbMarketMakingConfig: arbMMCfg, simpleArbConfig: arbCfg, uiConfig: { cexRebalance } } = botCfg
      this.creatingNewBot = false
      // This is kinda sloppy, but we'll copy any relevant issues from the
      // old config into the originalConfig.
      const idx = oldCfg as { [k: string]: any } // typescript
      for (const [k, v] of Object.entries(botCfg)) if (idx[k] !== undefined) idx[k] = v

      oldCfg.baseConfig = Object.assign({}, defaultBotAssetConfig, botCfg.uiConfig.baseConfig)
      oldCfg.quoteConfig = Object.assign({}, defaultBotAssetConfig, botCfg.uiConfig.quoteConfig)
      oldCfg.baseOptions = botCfg.baseWalletOptions || {}
      oldCfg.quoteOptions = botCfg.quoteWalletOptions || {}
      oldCfg.cexRebalance = cexRebalance

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
      } else if (arbCfg) {
        // TODO: expose maxActiveArbs
        oldCfg.profit = arbCfg.profitTrigger
        oldCfg.orderPersistence = arbCfg.numEpochsLeaveOpen
        oldCfg.simpleArbLots = botCfg.uiConfig.simpleArbLots ?? 1
      }
      Doc.setVis(!viewOnly, page.updateButton, page.resetButton)
    } else {
      this.creatingNewBot = true
      Doc.setVis(!viewOnly, page.createButton)
    }

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
    Doc.setVis(botType !== botTypeBasicArb, page.driftToleranceBox, page.switchToAdvanced)
    Doc.setVis(Boolean(cexName), ...Doc.applySelector(document.body, '[data-cex-show]'))

    Doc.setVis(viewOnly, page.viewOnlyRunning)
    Doc.setVis(cexName, page.cexRebalanceSettings)
    if (cexName) setCexElements(document.body, cexName)

    await this.fetchMarketReport()

    const lotSizeUSD = this.lotSizeUSD()
    this.lotsPerLevelIncrement = Math.round(Math.max(1, defaultLotsPerLevel.usdIncrement / lotSizeUSD))
    this.qcLotsPerLevel.inc = this.lotsPerLevelIncrement
    this.qcUSDPerSide.inc = this.lotsPerLevelIncrement * lotSizeUSD
    this.qcUSDPerSide.min = lotSizeUSD

    this.basePane.setAsset(baseID, false)
    this.quotePane.setAsset(quoteID, true)
    const { marketReport: { baseFiatRate } } = this
    this.placementsChart.setMarket({ cexName: cexName as string, botType, baseFiatRate, dict: this.updatedConfig })

    // If this is a new bot, show the quick config form.
    const isQuickPlacements = !botCfg || this.isQuickPlacements(this.updatedConfig.buyPlacements, this.updatedConfig.sellPlacements)
    const gapStrategy = botCfg?.basicMarketMakingConfig?.gapStrategy ?? GapStrategyPercentPlus
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

  adjustedBalances (baseWallet: WalletState, quoteWallet: WalletState) {
    const { cexBaseBalance, cexQuoteBalance } = this
    const [bInv, qInv] = [this.runningBotInventory(baseWallet.assetID), this.runningBotInventory(quoteWallet.assetID)]
    const [cexBaseAvail, cexQuoteAvail] = [(cexBaseBalance?.available || 0) - bInv.cex.total, (cexQuoteBalance?.available || 0) - qInv.cex.total]
    const [dexBaseAvail, dexQuoteAvail] = [baseWallet.balance.available - bInv.dex.total, quoteWallet.balance.available - qInv.dex.total]
    const baseAvail = dexBaseAvail + cexBaseAvail
    const quoteAvail = dexQuoteAvail + cexQuoteAvail
    return { baseAvail, quoteAvail, dexBaseAvail, dexQuoteAvail, cexBaseAvail, cexQuoteAvail }
  }

  lotSizeUSD () {
    const { specs: { host, baseID }, mktID, marketReport: { baseFiatRate } } = this
    const xc = app().exchanges[host]
    const market = xc.markets[mktID]
    const { lotsize: lotSize } = market
    const { unitInfo: ui } = app().assets[baseID]
    return lotSize / ui.conventional.conversionFactor * baseFiatRate
  }

  /*
    * marketStuff is just a bunch of useful properties for the current specs
    * gathered in one place and with preferable names.
    */
  marketStuff () {
    const {
      page, specs: { host, baseID, quoteID, cexName, botType }, basePane, quotePane,
      marketReport: { baseFiatRate, quoteFiatRate, baseFees, quoteFees },
      lotsPerLevelIncrement, updatedConfig: cfg, originalConfig: oldCfg, mktID
    } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const xc = app().exchanges[host]
    const market = xc.markets[mktID]
    const { lotsize: lotSize, spot } = market
    const lotSizeUSD = lotSize / bui.conventional.conversionFactor * baseFiatRate
    const atomicRate = 1 / bui.conventional.conversionFactor * baseFiatRate / quoteFiatRate * qui.conventional.conversionFactor
    const xcRate = {
      conv: quoteFiatRate / baseFiatRate,
      atomic: atomicRate,
      msg: Math.round(atomicRate * OrderUtil.RateEncodingFactor), // unadjusted
      spot
    }

    let [dexBaseLots, dexQuoteLots] = [cfg.simpleArbLots, cfg.simpleArbLots]
    if (botType !== botTypeBasicArb) {
      dexBaseLots = this.updatedConfig.sellPlacements.reduce((lots: number, p: OrderPlacement) => lots + p.lots, 0)
      dexQuoteLots = this.updatedConfig.buyPlacements.reduce((lots: number, p: OrderPlacement) => lots + p.lots, 0)
    }
    const quoteLot = calculateQuoteLot(lotSize, baseID, quoteID, spot)
    const walletStuff = this.walletStuff()
    const { baseFeeAssetID, quoteFeeAssetID, baseIsAccountLocker, quoteIsAccountLocker } = walletStuff

    const { commit, fees } = feesAndCommit(
      baseID, quoteID, baseFees, quoteFees, lotSize, dexBaseLots, dexQuoteLots,
      baseFeeAssetID, quoteFeeAssetID, baseIsAccountLocker, quoteIsAccountLocker,
      cfg.baseConfig.orderReservesFactor, cfg.quoteConfig.orderReservesFactor
    )

    return {
      page, cfg, oldCfg, host, xc, baseID, quoteID, botType, cexName, baseFiatRate, quoteFiatRate,
      xcRate, baseSymbol, quoteSymbol, mktID, lotSize, lotSizeUSD, lotsPerLevelIncrement,
      quoteLot, commit, basePane, quotePane, fees, ...walletStuff
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
      ...this.adjustedBalances(baseWallet, quoteWallet)
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
      this.qcLotsPerLevel.setValue(cfg.simpleArbLots)
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
      page.lotsPerLevelLabel, page.levelSpacingBox, page.arbLotsLabel, page.qcLevelPerSideBox
    )
    Doc.setVis(usingUSDPerSide, page.qcUSDPerSideBox)
    Doc.setVis(!usingUSDPerSide, page.qcLotsBox)
    switch (botType) {
      case botTypeArbMM:
        Doc.show(
          page.qcLevelPerSideBox, page.matchMultiplierBox, page.placementsChartBox,
          page.placementChartLegend, page.lotsPerLevelLabel
        )
        break
      case botTypeBasicMM:
        Doc.show(
          page.qcLevelPerSideBox, page.levelSpacingBox, page.placementsChartBox,
          page.lotsPerLevelLabel
        )
        break
      case botTypeBasicArb:
        Doc.show(page.arbLotsLabel)
    }
  }

  quickConfigUpdated () {
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
    cfg.simpleArbLots = lotsPerLevel

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

    this.updateAllocations()
  }

  updateAllocations () {
    this.updateBaseAllocations()
    this.updateQuoteAllocations()
  }

  updateBaseAllocations () {
    const { commit, lotSize, basePane, fees } = this.marketStuff()

    basePane.updateInventory(commit.dex.base.lots, commit.dex.quote.lots, lotSize, commit.dex.base.val, commit.cex.base.val, fees.base)
    basePane.updateCommitTotal()
  }

  updateQuoteAllocations () {
    const { commit, quoteLot: lotSize, quotePane, fees } = this.marketStuff()

    quotePane.updateInventory(commit.dex.quote.lots, commit.dex.base.lots, lotSize, commit.dex.quote.val, commit.cex.quote.val, fees.quote)
    quotePane.updateCommitTotal()
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
    const viewOnly = isViewOnly(this.formSpecs, app().mmStatus)
    if (viewOnly) {
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
    if (isViewOnly(this.specs, app().mmStatus)) this.showMarketSelectForm()
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

  handleBalanceNote (n: BalanceNote) {
    this.approveTokenForm.handleBalanceNote(n)
    if (!this.marketReport) return
    const { baseID, quoteID, quoteToken, baseToken } = this.marketStuff()
    if (n.assetID === baseID || n.assetID === baseToken?.parentID) {
      this.basePane.updateBalances()
    } else if (n.assetID === quoteID || n.assetID === quoteToken?.parentID) {
      this.quotePane.updateBalances()
    }
  }

  autoRebalanceChanged () {
    const { page, updatedConfig: cfg } = this
    cfg.cexRebalance = page.cexRebalanceCheckbox?.checked ?? false
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
      page.cexRebalanceCheckbox.checked = cfg.cexRebalance
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

    this.basePane.setupWalletSettings()
    this.quotePane.setupWalletSettings()

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

  /*
   * saveSettings updates the settings in the backend, and sets the originalConfig
   * to be equal to the updatedConfig.
   */
  async saveSettings () {
    // Make a copy and delete either the basic mm config or the arb-mm config,
    // depending on whether a cex is selected.
    if (!this.validateFields(true)) return
    const { cfg, baseID, quoteID, host, botType, cexName } = this.marketStuff()

    const botCfg: BotConfig = {
      host: host,
      baseID: baseID,
      quoteID: quoteID,
      cexName: cexName ?? '',
      uiConfig: {
        simpleArbLots: cfg.simpleArbLots,
        baseConfig: cfg.baseConfig,
        quoteConfig: cfg.quoteConfig,
        cexRebalance: cfg.cexRebalance
      },
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
    await MM.updateBotConfig(botCfg)
    await app().fetchMMStatus()
    this.originalConfig = JSON.parse(JSON.stringify(cfg))
    this.updateModifiedMarkers()
    const lastBots = State.fetchLocal(lastBotsLK) || {}
    lastBots[`${baseID}_${quoteID}_${host}`] = this.specs
    State.storeLocal(lastBotsLK, lastBots)
    if (cexName) State.storeLocal(lastArbExchangeLK, cexName)
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
      orderPersistence: cfg.orderPersistence
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

  /*
   * cexMarketSupportFilter returns a lookup CEXes that have a matching market
   * for the currently selected base and quote assets.
   */
  cexMarketSupportFilter (baseID: number, quoteID: number) {
    const cexes: Record<string, boolean> = {}
    for (const [cexName, cexStatus] of Object.entries(app().mmStatus.cexes)) {
      for (const { baseID: b, quoteID: q } of Object.values(cexStatus.markets ?? [])) {
        if (b === baseID && q === quoteID) {
          cexes[cexName] = true
          break
        }
      }
    }
    return (cexName: string) => Boolean(cexes[cexName])
  }
}

function isViewOnly (specs: BotSpecs, mmStatus: MarketMakingStatus): boolean {
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

class AssetPane {
  pg: MarketMakerSettingsPage
  div: PageElement
  page: Record<string, PageElement>
  assetID: number
  ui: UnitInfo
  walletConfig: Record<string, string>
  feeAssetID: number
  feeUI: UnitInfo
  isQuote: boolean
  isToken: boolean
  lotSize: number // might be quote converted
  lotSizeConv: number
  cfg: BotAssetConfig
  inv: ProjectedAlloc
  nSwapFees: IncrementalInput
  nSwapFeesSlider: MiniSlider
  orderReserves: NumberInput
  orderReservesSlider: MiniSlider
  slippageBuffer: NumberInput
  slippageBufferSlider: MiniSlider
  minTransfer: NumberInput
  minTransferSlider: MiniSlider

  constructor (pg: MarketMakerSettingsPage, div: PageElement) {
    this.pg = pg
    this.div = div
    const page = this.page = Doc.parseTemplate(div)

    this.nSwapFees = new IncrementalInput(page.nSwapFees, {
      prec: defaultSwapReserves.prec,
      inc: defaultSwapReserves.inc,
      changed: (v: number) => {
        const { minR, range } = defaultSwapReserves
        this.cfg.swapFeeN = v
        this.nSwapFeesSlider.setValue((v - minR) / range)
        this.pg.updateAllocations()
      }
    })

    this.nSwapFeesSlider = new MiniSlider(page.nSwapFeesSlider, (r: number) => {
      const { minR, range, prec } = defaultSwapReserves
      const [v] = toPrecision(minR + r * range, prec)
      this.cfg.swapFeeN = v
      this.nSwapFees.setValue(v)
      this.pg.updateAllocations()
    })
    this.orderReserves = new NumberInput(page.orderReservesFactor, {
      prec: defaultOrderReserves.prec,
      min: 0,
      changed: (v: number) => {
        const { minR, range } = defaultOrderReserves
        this.cfg.orderReservesFactor = v
        this.orderReservesSlider.setValue((v - minR) / range)
        this.pg.updateAllocations()
      }
    })
    this.orderReservesSlider = new MiniSlider(page.orderReservesSlider, (r: number) => {
      const { minR, range, prec } = defaultOrderReserves
      const [v] = toPrecision(minR + r * range, prec)
      this.orderReserves.setValue(v)
      this.cfg.orderReservesFactor = v
      this.pg.updateAllocations()
    })
    this.slippageBuffer = new NumberInput(page.slippageBufferFactor, {
      prec: defaultSlippage.prec,
      min: 0,
      changed: (v: number) => {
        const { minR, range } = defaultSlippage
        this.cfg.slippageBufferFactor = v
        this.slippageBufferSlider.setValue((v - minR) / range)
        this.pg.updateAllocations()
      }
    })
    this.slippageBufferSlider = new MiniSlider(page.slippageBufferSlider, (r: number) => {
      const { minR, range, prec } = defaultSlippage
      const [v] = toPrecision(minR + r * range, prec)
      this.slippageBuffer.setValue(minR + r * range)
      this.cfg.slippageBufferFactor = v
      this.pg.updateAllocations()
    })
    this.minTransfer = new NumberInput(page.minTransfer, {
      sigFigs: true,
      min: 0,
      changed: (v: number) => {
        const { cfg } = this
        const totalInventory = this.commit()
        const [minV, maxV] = [this.minTransfer.min, Math.max(this.minTransfer.min * 2, totalInventory)]
        cfg.transferFactor = (v - minV) / (maxV - minV)
        this.minTransferSlider.setValue(cfg.transferFactor)
      }
    })
    this.minTransferSlider = new MiniSlider(page.minTransferSlider, (r: number) => {
      const { cfg } = this
      const totalInventory = this.commit()
      const [minV, maxV] = [this.minTransfer.min, Math.max(this.minTransfer.min, totalInventory)]
      cfg.transferFactor = r
      this.minTransfer.setValue(minV + r * (maxV - minV))
    })

    Doc.bind(page.showBalance, 'click', () => { pg.showAddress(this.assetID) })
  }

  // lot size can change if this is the quote asset, keep it updated.
  setLotSize (lotSize: number) {
    const { ui } = this
    this.lotSize = lotSize
    this.lotSizeConv = lotSize / ui.conventional.conversionFactor
  }

  setAsset (assetID: number, isQuote: boolean) {
    this.assetID = assetID
    this.isQuote = isQuote
    const cfg = this.cfg = isQuote ? this.pg.updatedConfig.quoteConfig : this.pg.updatedConfig.baseConfig
    const { page, div, pg: { specs: { botType, baseID, cexName }, mktID, updatedConfig: { baseOptions, quoteOptions } } } = this
    const { symbol, name, token, unitInfo: ui } = app().assets[assetID]
    this.ui = ui
    this.walletConfig = assetID === baseID ? baseOptions : quoteOptions
    const { conventional: { unit: ticker } } = ui
    this.feeAssetID = token ? token.parentID : assetID
    const { unitInfo: feeUI, name: feeName, symbol: feeSymbol } = app().assets[this.feeAssetID]
    this.feeUI = feeUI
    this.inv = { book: 0, bookingFees: 0, swapFeeReserves: 0, cex: 0, orderReserves: 0, slippageBuffer: 0 }
    this.isToken = Boolean(token)
    Doc.setVis(this.isToken, page.feeTotalBox, page.feeReservesBox, page.feeBalances)
    Doc.setVis(isQuote, page.slippageBufferBox)
    Doc.setSrc(div, '[data-logo]', Doc.logoPath(symbol))
    Doc.setText(div, '[data-name]', name)
    Doc.setText(div, '[data-ticker]', ticker)
    const { conventional: { unit: feeTicker } } = feeUI
    Doc.setText(div, '[data-fee-ticker]', feeTicker)
    Doc.setText(div, '[data-fee-name]', feeName)
    Doc.setSrc(div, '[data-fee-logo]', Doc.logoPath(feeSymbol))
    Doc.setVis(botType !== botTypeBasicMM, page.cexMinInvBox)
    Doc.setVis(botType !== botTypeBasicArb, page.orderReservesBox)
    this.nSwapFees.setValue(cfg.swapFeeN ?? defaultSwapReserves.n)
    this.nSwapFeesSlider.setValue(cfg.swapFeeN / defaultSwapReserves.maxR)
    if (botType !== botTypeBasicArb) {
      const [v] = toPrecision(cfg.orderReservesFactor ?? defaultOrderReserves.factor, defaultOrderReserves.prec)
      this.orderReserves.setValue(v)
      this.orderReservesSlider.setValue((v - defaultOrderReserves.minR) / defaultOrderReserves.range)
    }
    if (botType !== botTypeBasicMM) {
      this.minTransfer.prec = Math.log10(ui.conventional.conversionFactor)
      const mkt = app().mmStatus.cexes[cexName as string].markets[mktID]
      this.minTransfer.min = ((isQuote ? mkt.quoteMinWithdraw : mkt.baseMinWithdraw) / ui.conventional.conversionFactor)
    }
    this.slippageBuffer.setValue(cfg.slippageBufferFactor)
    const { minR, range } = defaultSlippage
    this.slippageBufferSlider.setValue((cfg.slippageBufferFactor - minR) / range)
    this.setupWalletSettings()
    this.updateBalances()
  }

  commit () {
    const { inv, isToken } = this
    let commit = inv.book + inv.cex + inv.orderReserves + inv.slippageBuffer
    if (!isToken) commit += inv.bookingFees + inv.swapFeeReserves
    return commit
  }

  updateInventory (lots: number, counterLots: number, lotSize: number, dexCommit: number, cexCommit: number, fees: AssetBookingFees) {
    this.setLotSize(lotSize)
    const { page, cfg, lotSizeConv, inv, ui, feeUI, isToken, isQuote, pg: { specs: { cexName, botType } } } = this
    page.bookLots.textContent = String(lots)
    page.bookLotSize.textContent = Doc.formatFourSigFigs(lotSizeConv)
    inv.book = lots * lotSizeConv
    page.bookCommitment.textContent = Doc.formatFourSigFigs(inv.book)
    const feesPerLotConv = fees.bookingFeesPerLot / feeUI.conventional.conversionFactor
    page.bookingFeesPerLot.textContent = Doc.formatFourSigFigs(feesPerLotConv)
    page.swapReservesFactor.textContent = fees.swapReservesFactor.toFixed(2)
    page.bookingFeesLots.textContent = String(lots)
    inv.bookingFees = fees.bookingFees / feeUI.conventional.conversionFactor
    page.bookingFees.textContent = Doc.formatFourSigFigs(inv.bookingFees)
    if (cexName) {
      inv.cex = cexCommit / ui.conventional.conversionFactor
      page.cexMinInv.textContent = Doc.formatFourSigFigs(inv.cex)
    }
    if (botType !== botTypeBasicArb) {
      const totalInventory = Math.max(cexCommit, dexCommit) / ui.conventional.conversionFactor
      page.orderReservesBasis.textContent = Doc.formatFourSigFigs(totalInventory)
      const orderReserves = totalInventory * cfg.orderReservesFactor
      inv.orderReserves = orderReserves
      page.orderReserves.textContent = Doc.formatFourSigFigs(orderReserves)
    }
    if (isToken) {
      const feesPerSwapConv = fees.tokenFeesPerSwap / feeUI.conventional.conversionFactor
      page.feeReservesPerSwap.textContent = Doc.formatFourSigFigs(feesPerSwapConv)
      inv.swapFeeReserves = feesPerSwapConv * cfg.swapFeeN
      page.feeReserves.textContent = Doc.formatFourSigFigs(inv.swapFeeReserves)
    }
    if (isQuote) {
      const basis = inv.book + inv.cex + inv.orderReserves
      page.slippageBufferBasis.textContent = Doc.formatCoinValue(basis * ui.conventional.conversionFactor, ui)
      inv.slippageBuffer = basis * cfg.slippageBufferFactor
      page.slippageBuffer.textContent = Doc.formatCoinValue(inv.slippageBuffer * ui.conventional.conversionFactor, ui)
    }
    Doc.setVis(fees.bookingFeesPerCounterLot > 0, page.redemptionFeesBox)
    if (fees.bookingFeesPerCounterLot > 0) {
      const feesPerLotConv = fees.bookingFeesPerCounterLot / feeUI.conventional.conversionFactor
      page.redemptionFeesPerLot.textContent = Doc.formatFourSigFigs(feesPerLotConv)
      page.redemptionFeesLots.textContent = String(counterLots)
      page.redeemReservesFactor.textContent = fees.redeemReservesFactor.toFixed(2)
    }
    this.updateCommitTotal()
    this.updateTokenFees()
    this.updateRebalance()
  }

  updateCommitTotal () {
    const { page, assetID, ui } = this
    const commit = this.commit()
    page.commitTotal.textContent = Doc.formatCoinValue(Math.round(commit * ui.conventional.conversionFactor), ui)
    page.commitTotalFiat.textContent = Doc.formatFourSigFigs(commit * app().fiatRatesMap[assetID])
  }

  updateTokenFees () {
    const { page, inv, feeAssetID, feeUI, isToken } = this
    if (!isToken) return
    const feeReserves = inv.bookingFees + inv.swapFeeReserves
    page.feeTotal.textContent = Doc.formatCoinValue(feeReserves * feeUI.conventional.conversionFactor, feeUI)
    page.feeTotalFiat.textContent = Doc.formatFourSigFigs(feeReserves * app().fiatRatesMap[feeAssetID])
  }

  updateRebalance () {
    const { page, cfg, pg: { updatedConfig: { cexRebalance }, specs: { cexName } } } = this
    const showRebalance = cexName && cexRebalance
    Doc.setVis(showRebalance, page.rebalanceOpts)
    if (!showRebalance) return
    const totalInventory = this.commit()
    const [minV, maxV] = [this.minTransfer.min, Math.max(this.minTransfer.min * 2, totalInventory)]
    const rangeV = maxV - minV
    this.minTransfer.setValue(minV + cfg.transferFactor * rangeV)
    this.minTransferSlider.setValue((cfg.transferFactor - defaultTransfer.minR) / defaultTransfer.range)
  }

  setupWalletSettings () {
    const { page, assetID, walletConfig } = this
    const walletSettings = app().currentWalletDefinition(assetID)
    Doc.empty(page.walletSettings)
    Doc.setVis(!walletSettings.multifundingopts, page.walletSettingsNone)
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
    const addOpt = (opt: OrderOption) => {
      if (opt.quoteAssetOnly && !this.isQuote) return
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
        })
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
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
          }
        })
        const slider = new MiniSlider(tmpl.slider, (r: number) => {
          const rawV = start.x + r * range
          const [v, s] = toFourSigFigs(rawV, 1)
          walletConfig[opt.key] = s
          input.setValue(v)
        })
        // TODO: default value should be smaller or none for base asset.
        const [v, s] = toFourSigFigs(parseFloatDefault(currVal, start.x), 3)
        walletConfig[opt.key] = s
        slider.setValue((v - start.x) / range)
        input.setValue(v)
        tmpl.value.textContent = s
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

  updateBalances () {
    const { page, assetID, ui, feeAssetID, feeUI, pg: { specs: { cexName, baseID }, cexBaseBalance, cexQuoteBalance } } = this
    const { balance: { available } } = app().walletMap[assetID]
    const botInv = this.pg.runningBotInventory(assetID)
    const dexAvail = available - botInv.dex.total
    let cexAvail = 0
    Doc.setVis(cexName, page.balanceBreakdown)
    if (cexName) {
      page.dexAvail.textContent = Doc.formatFourSigFigs(dexAvail / ui.conventional.conversionFactor)
      const { available: cexRawAvail } = assetID === baseID ? cexBaseBalance : cexQuoteBalance
      cexAvail = cexRawAvail - botInv.cex.total
      page.cexAvail.textContent = Doc.formatFourSigFigs(cexAvail / ui.conventional.conversionFactor)
    }
    page.avail.textContent = Doc.formatFourSigFigs((dexAvail + cexAvail) / ui.conventional.conversionFactor)
    if (assetID === feeAssetID) return
    const { balance: { available: feeAvail } } = app().walletMap[feeAssetID]
    page.feeAvail.textContent = Doc.formatFourSigFigs(feeAvail / feeUI.conventional.conversionFactor)
  }
}
