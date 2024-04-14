import {
  PageElement,
  BotConfig,
  XYRange,
  OrderPlacement,
  BalanceType,
  app,
  Spot,
  MarketReport,
  OrderOption,
  CEXConfig,
  BasicMarketMakingConfig,
  ArbMarketMakingConfig,
  SimpleArbConfig,
  ArbMarketMakingPlacement,
  BotCEXCfg,
  AutoRebalanceConfig,
  ExchangeBalance,
  MarketMakingStatus,
  MMBotStatus,
  MMCEXStatus,
  BalanceNote,
  UnitInfo,
  Token,
  ApprovalStatus,
  SupportedAsset
} from './registry'
import Doc from './doc'
import State from './state'
import BasePage from './basepage'
import { setOptionTemplates, XYRangeHandler } from './opts'
import { MM, CEXDisplayInfos, botTypeBasicArb, botTypeArbMM, botTypeBasicMM } from './mm'
import { Forms, bind as bindForm, NewWalletForm, TokenApprovalForm, DepositAddress } from './forms'
import * as intl from './locales'
import * as OrderUtil from './orderutil'
import { Chart, Region, Extents, Translator, clamp } from './charts'

const GapStrategyMultiplier = 'multiplier'
const GapStrategyAbsolute = 'absolute'
const GapStrategyAbsolutePlus = 'absolute-plus'
const GapStrategyPercent = 'percent'
const GapStrategyPercentPlus = 'percent-plus'
const arbMMRowCacheKey = 'arbmm'

const specLK = 'lastMMSpecs'
const lastBotsLK = 'lastBots'
const lastArbExchangeLK = 'lastArbExchange'
const tokenFeeMultiplier = 50

const qcDefaultProfitThreshold = 0.02
const qcDefaultLevelSpacing = 0.005
const qcDefaultMatchBuffer = 0
const qcDefaultLotsPerLevelIncrement = 100 // USD
const qcDefaultLevelsPerSide = 1
const qcDefaultProfitMax = 0.1
const qcMinimumProfit = 0.001

const driftToleranceRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '1%',
    x: 0.01,
    y: 1
  },
  xUnit: '',
  yUnit: '%'
}

const orderPersistenceRange: XYRange = {
  start: {
    label: '0',
    x: 0,
    y: 0
  },
  end: {
    label: 'max',
    x: 21,
    y: 21
  },
  xUnit: '',
  yUnit: 'epochs'
}

const oracleBiasRange: XYRange = {
  start: {
    label: '-1%',
    x: -0.01,
    y: -1
  },
  end: {
    label: '1%',
    x: 0.01,
    y: 1
  },
  xUnit: '',
  yUnit: '%'
}

const oracleWeightRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '100%',
    x: 1,
    y: 100
  },
  xUnit: '',
  yUnit: '%'
}

const profitSliderRange: XYRange = {
  start: {
    label: '0.1%',
    x: qcMinimumProfit,
    y: 0.1
  },
  end: {
    label: '10%',
    x: qcDefaultProfitMax,
    y: 10
  },
  xUnit: '',
  yUnit: '%'
}

const levelSpacingRange: XYRange = {
  start: {
    label: '0.01%',
    x: 0.0001,
    y: 0.01
  },
  end: {
    label: '2%',
    x: 0.02,
    y: 2
  },
  xUnit: '',
  yUnit: '%'
}

const defaultMarketMakingConfig : ConfigState = {
  gapStrategy: GapStrategyPercentPlus,
  sellPlacements: [],
  buyPlacements: [],
  driftTolerance: 0.001,
  oracleWeighting: 0.1,
  oracleBias: 0,
  emptyMarketRate: 0,
  profit: 3,
  orderPersistence: 20,
  baseBalanceType: BalanceType.Percentage,
  quoteBalanceType: BalanceType.Percentage,
  baseBalance: 0,
  quoteBalance: 0,
  baseTokenFees: 0,
  quoteTokenFees: 0,
  cexBaseBalanceType: BalanceType.Percentage,
  cexBaseBalance: 0,
  cexBaseMinBalance: 0,
  cexBaseMinTransfer: 0,
  cexQuoteBalance: 0,
  cexQuoteBalanceType: BalanceType.Percentage,
  cexQuoteMinBalance: 0,
  cexQuoteMinTransfer: 0,
  cexRebalance: true
} as any as ConfigState

// walletSettingControl is used by the modified highlighting and
// reset values functionalities to manage the wallet settings
// defined in walletDefinition.multifundingopts
interface walletSettingControl {
  toHighlight: PageElement
  setValue: (value: string) => void
}

// cexButton stores parts of a CEX selection button.
interface cexButton {
  name: string
  div: PageElement
  tmpl: Record<string, PageElement>
}

interface tokenFeeBox {
  fees: FeeEstimates
  fiatRate: number // parent asset
  ui: UnitInfo // parent asset
  conversionFactor:number
  booking: PageElement
  bookingFiat: PageElement
  swapCount: PageElement
  reserves: PageElement
  reservesFiat: PageElement
  input: PageElement
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
  useOracles: boolean
  profit: number
  useEmptyMarketRate: boolean
  emptyMarketRate: number
  driftTolerance: number
  orderPersistence: number // epochs
  oracleWeighting: number
  oracleBias: number
  baseBalanceType: BalanceType
  baseBalance: number
  quoteBalanceType: BalanceType
  quoteBalance: number
  baseTokenFees: number
  quoteTokenFees: number
  cexBaseBalanceType: BalanceType
  cexBaseBalance: number
  cexBaseMinBalance: number
  cexBaseMinTransfer: number
  cexQuoteBalanceType: BalanceType
  cexQuoteBalance: number
  cexQuoteMinBalance: number
  cexQuoteMinTransfer: number
  cexRebalance: boolean
  disabled: boolean
  buyPlacements: OrderPlacement[]
  sellPlacements: OrderPlacement[]
  baseOptions: Record<string, any>
  quoteOptions: Record<string, any>
  autoRebalance: AutoRebalanceConfig
}

interface BotSpecs {
  host: string
  baseID: number
  quoteID: number
  botType: string
  cexName?: string
  startTime?: number
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

interface FeeEstimates {
  bookingFees: number
  reservesPerSwap: number
}
interface FeeOutlook {
  base: FeeEstimates
  quote: FeeEstimates
}

export default class MarketMakerSettingsPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  newWalletForm: NewWalletForm
  approveTokenForm: TokenApprovalForm
  walletAddrForm: DepositAddress
  currentMarket: string
  originalConfig: ConfigState
  updatedConfig: ConfigState
  creatingNewBot: boolean
  marketReport: MarketReport
  mmStatus: MarketMakingStatus
  cexConfigs: CEXConfig[]
  botConfigs: BotConfig[]
  qcProfitSlider: XYRangeHandler
  qcLevelSpacingSlider: XYRangeHandler
  qcMatchBufferSlider: XYRangeHandler
  oracleBias: XYRangeHandler
  oracleWeighting: XYRangeHandler
  driftTolerance: XYRangeHandler
  orderPersistence: XYRangeHandler
  baseBalance: XYRangeHandler
  quoteBalance: XYRangeHandler
  cexBaseBalanceRange: XYRangeHandler
  cexBaseMinBalanceRange: XYRangeHandler
  cexBaseBalance: ExchangeBalance
  cexBaseAvail: number
  cexQuoteBalanceRange: XYRangeHandler
  cexQuoteMinBalanceRange: XYRangeHandler
  cexQuoteBalance: ExchangeBalance
  cexQuoteAvail: number
  baseWalletSettingControl: Record<string, walletSettingControl> = {}
  quoteWalletSettingControl: Record<string, walletSettingControl> = {}
  specs: BotSpecs
  formSpecs: BotSpecs
  formCexes: Record<string, cexButton>
  placementsCache: Record<string, [OrderPlacement[], OrderPlacement[]]>
  botTypeSelectors: PageElement[]
  marketRows: MarketRow[]
  lotsPerLevelIncrement: number
  placementsChart: PlacementsChart
  dexBaseAvail: number
  dexQuoteAvail: number

  constructor (main: HTMLElement, specs: BotSpecs) {
    super()

    this.placementsCache = {}

    const page = this.page = Doc.idDescendants(main)

    this.forms = new Forms(page.forms)

    this.placementsChart = new PlacementsChart(page.placementsChart, this)
    this.approveTokenForm = new TokenApprovalForm(page.approveTokenForm, () => { this.submitBotType() })
    this.walletAddrForm = new DepositAddress(page.walletAddrForm)

    app().headerSpace.appendChild(page.mmTitle)

    setOptionTemplates(page)
    Doc.cleanTemplates(
      page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl, page.placementRowTmpl,
      page.oracleTmpl, page.boolSettingTmpl, page.rangeSettingTmpl, page.cexOptTmpl,
      page.arbBttnTmpl, page.marketRowTmpl, page.needRegTmpl
    )

    Doc.bind(page.resetButton, 'click', () => { this.setOriginalValues(false) })
    Doc.bind(page.updateButton, 'click', () => { this.saveSettings() })
    Doc.bind(page.createButton, 'click', async () => { this.saveSettings() })
    Doc.bind(page.backButton, 'click', () => {
      const backPage = this.specs.startTime ? 'mmarchives' : 'mm'
      app().loadPage(backPage)
    })
    Doc.bind(page.cexSubmit, 'click', () => { this.handleCEXSubmit() })
    bindForm(page.botTypeForm, page.botTypeSubmit, () => { this.submitBotType() })
    Doc.bind(page.noMarketBttn, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.headerReconfig, 'click', () => { this.reshowBotTypeForm() })
    Doc.bind(page.botTypeChangeMarket, 'click', () => { this.showMarketSelectForm() })
    Doc.bind(page.marketFilterInput, 'input', () => { this.sortMarketRows() })
    Doc.bind(page.cexRebalanceCheckbox, 'change', () => { this.autoRebalanceChanged() })
    Doc.bind(page.cexBaseTransferUp, 'click', () => this.incrementTransfer(true, true))
    Doc.bind(page.cexBaseTransferDown, 'click', () => this.incrementTransfer(true, false))
    Doc.bind(page.cexBaseTransferInput, 'change', () => { this.baseTransferChanged() })
    Doc.bind(page.cexQuoteTransferUp, 'click', () => this.incrementTransfer(false, true))
    Doc.bind(page.cexQuoteTransferDown, 'click', () => this.incrementTransfer(false, false))
    Doc.bind(page.cexQuoteTransferInput, 'change', () => { this.quoteTransferChanged() })
    Doc.bind(page.switchToAdvanced, 'click', () => { this.showAdvancedConfig(false) })
    Doc.bind(page.switchToQuickConfig, 'click', () => { this.switchToQuickConfig() })
    Doc.bind(page.levelsPerSide, 'change', () => { this.levelsPerSideChanged() })
    Doc.bind(page.levelsPerSideUp, 'click', () => { this.incrementLevelsPerSide(true) })
    Doc.bind(page.levelsPerSideDown, 'click', () => { this.incrementLevelsPerSide(false) })
    Doc.bind(page.lotsPerLevel, 'change', () => { this.lotsPerLevelChanged() })
    Doc.bind(page.lotsPerLevelUp, 'click', () => { this.incrementLotsPerLevel(true) })
    Doc.bind(page.lotsPerLevelDown, 'click', () => { this.incrementLotsPerLevel(false) })
    Doc.bind(page.quoteTokenFeesUp, 'click', () => { this.incrementTokenFees(true, false) })
    Doc.bind(page.quoteTokenFeesDown, 'click', () => { this.incrementTokenFees(false, false) })
    Doc.bind(page.quoteTokenFees, 'change', () => { this.tokenFeesChanged(false) })
    Doc.bind(page.baseTokenFeesUp, 'click', () => { this.incrementTokenFees(true, true) })
    Doc.bind(page.baseTokenFeesDown, 'click', () => { this.incrementTokenFees(false, true) })
    Doc.bind(page.baseTokenFees, 'change', () => { this.tokenFeesChanged(true) })
    Doc.bind(page.qcProfit, 'change', () => { this.qcProfitChanged() })
    Doc.bind(page.qcLevelSpacing, 'change', () => { this.levelSpacingChanged() })
    Doc.bind(page.qcMatchBuffer, 'change', () => { this.matchBufferChanged() })
    Doc.bind(page.showBaseAddress, 'click', () => { this.showDeposit(this.specs.baseID) })
    Doc.bind(page.showQuoteAddress, 'click', () => { this.showDeposit(this.specs.quoteID) })

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

  async refreshStatus () {
    this.mmStatus = await MM.status()
    this.botConfigs = this.mmStatus.bots.map((s: MMBotStatus) => s.config)
    this.cexConfigs = Object.values(this.mmStatus.cexes).map((s: MMCEXStatus) => s.config)
  }

  async initialize (specs?: BotSpecs) {
    await this.refreshStatus()

    this.setupCEXes()

    this.marketRows = []
    for (const { host, markets, assets, auth } of Object.values(app().exchanges)) {
      if (auth.targetTier === 0) {
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
            img.src = '/img/' + dinfo.logo
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
    }
    const fiatRates = app().fiatRatesMap
    this.marketRows.sort((a: MarketRow, b: MarketRow) => {
      let [volA, volB] = [a.spot.vol24, b.spot.vol24]
      if (fiatRates[a.baseID] && fiatRates[b.baseID]) {
        volA *= fiatRates[a.baseID]
        volB *= fiatRates[b.baseID]
      }
      return volB - volA
    })

    const isRefresh = specs && Object.keys(specs).length === 0
    if (isRefresh) specs = State.fetchLocal(specLK)
    if (!specs || !app().walletMap[specs.baseID] || !app().walletMap[specs.quoteID]) {
      this.showMarketSelectForm()
      return
    }

    // Must be a reconfig.
    this.specs = specs
    await this.fetchCEXBalances(specs)
    this.configureUI()
  }

  async configureUI () {
    const { page, specs: { host, baseID, quoteID, botType, cexName } } = this

    Doc.show(page.marketLoading)
    Doc.hide(page.botSettingsContainer, page.marketHeader)

    State.storeLocal(specLK, this.specs)

    const viewOnly = isViewOnly(this.specs, this.mmStatus)
    const botCfg = await botConfig(this.specs, this.mmStatus)
    const dmm = defaultMarketMakingConfig

    const oldCfg = this.originalConfig = Object.assign({}, defaultMarketMakingConfig, {
      useOracles: dmm.oracleWeighting > 0,
      useEmptyMarketRate: dmm.emptyMarketRate > 0,
      disabled: viewOnly,
      baseOptions: this.defaultWalletOptions(baseID),
      quoteOptions: this.defaultWalletOptions(quoteID),
      buyPlacements: [],
      sellPlacements: []
    }) as ConfigState

    Doc.hide(page.updateButton, page.resetButton, page.createButton, page.cexNameDisplayBox, page.noMarket)
    Doc.setVis(botType === botTypeBasicMM, page.emptyMarketRateBox)

    Doc.empty(page.baseHeader, page.quoteHeader)
    page.baseHeader.appendChild(Doc.symbolize(app().assets[baseID], true))
    page.quoteHeader.appendChild(Doc.symbolize(app().assets[quoteID], true))
    page.hostHeader.textContent = host

    if (botCfg) {
      const { basicMarketMakingConfig: mmCfg, arbMarketMakingConfig: arbMMCfg, simpleArbConfig: arbCfg, cexCfg } = botCfg
      this.creatingNewBot = false
      // This is kinda sloppy, but we'll copy any relevant issues from the
      // old config into the originalConfig.
      const idx = oldCfg as {[k: string]: any} // typescript
      for (const [k, v] of Object.entries(botCfg)) if (idx[k] !== undefined) idx[k] = v
      let rebalanceCfg
      oldCfg.baseTokenFees = botCfg.baseFeeAssetBalance ?? 0
      oldCfg.quoteTokenFees = botCfg.quoteFeeAssetBalance ?? 0
      if (mmCfg) {
        oldCfg.buyPlacements = mmCfg.buyPlacements
        oldCfg.sellPlacements = mmCfg.sellPlacements
        oldCfg.driftTolerance = mmCfg.driftTolerance
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
      }
      if (cexCfg) {
        oldCfg.cexBaseBalance = cexCfg.baseBalance
        oldCfg.cexBaseBalanceType = cexCfg.baseBalanceType
        oldCfg.cexQuoteBalance = cexCfg.quoteBalance
        oldCfg.cexQuoteBalanceType = cexCfg.quoteBalanceType
        rebalanceCfg = cexCfg.autoRebalance
      }

      if (rebalanceCfg) {
        oldCfg.cexRebalance = true
        oldCfg.cexBaseMinBalance = rebalanceCfg.minBaseAmt
        oldCfg.cexBaseMinTransfer = rebalanceCfg.minBaseTransfer
        oldCfg.cexQuoteMinBalance = rebalanceCfg.minQuoteAmt
        oldCfg.cexQuoteMinTransfer = rebalanceCfg.minQuoteTransfer
      }

      Doc.setVis(!viewOnly, page.updateButton, page.resetButton)
    } else {
      this.creatingNewBot = true
      Doc.setVis(!viewOnly, page.createButton)
    }

    switch (botType) {
      case botTypeBasicMM:
        page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
        break
      case botTypeArbMM:
        page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
        Doc.show(page.cexNameDisplayBox)
        page.cexNameDisplay.textContent = CEXDisplayInfos[cexName ?? ''].name
        break
      case botTypeBasicArb:
        page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
        Doc.show(page.cexNameDisplayBox)
        page.cexNameDisplay.textContent = CEXDisplayInfos[cexName ?? ''].name
    }

    Doc.setVis(!viewOnly, page.profitPrompt)
    Doc.setVis(viewOnly && !this.specs.startTime, page.viewOnlyRunning)
    Doc.setVis(viewOnly && this.specs.startTime, page.viewOnlyArchived)
    if (this.specs.startTime) {
      const startTimeStr = (new Date(this.specs.startTime * 1000)).toLocaleString()
      page.viewOnlyArchivedMsg.textContent = intl.prep(intl.ID_ARCHIVED_SETTINGS, { startTime: startTimeStr })
    }
    page.profitInput.disabled = viewOnly

    // Now that we've updated the originalConfig, we'll copy it.
    this.updatedConfig = JSON.parse(JSON.stringify(oldCfg))

    await this.fetchOracles()

    this.setupMMConfigSettings(viewOnly)
    this.setupBalanceSelectors(viewOnly)
    this.setupWalletSettings(viewOnly)
    this.setupCEXSelectors(viewOnly)
    this.setOriginalValues(viewOnly)

    this.setBaseQuote(document.body, baseID, quoteID)

    const hasFiatRates = this.marketReport && this.marketReport.baseFiatRate > 0 && this.marketReport.quoteFiatRate > 0
    Doc.setVis(hasFiatRates, page.switchToQuickConfig)

    // If this is a new bot, show the quick config form.
    if (!botCfg && hasFiatRates) this.showQuickConfig()
    else this.showAdvancedConfig(viewOnly)

    Doc.hide(page.marketLoading)
    Doc.show(page.botSettingsContainer, page.marketHeader)
  }

  setBaseQuote (ancestor: PageElement, baseID: number, quoteID: number) {
    const { unitInfo: bui, name: baseName, symbol: baseSymbol } = app().assets[baseID]
    for (const el of Doc.applySelector(ancestor, '[data-base-name')) el.textContent = baseName
    for (const img of Doc.applySelector(ancestor, '[data-base-logo]')) img.src = Doc.logoPath(baseSymbol)
    for (const el of Doc.applySelector(ancestor, '[data-base-ticker]')) el.textContent = bui.conventional.unit
    const { unitInfo: qui, name: quoteName, symbol: quoteSymbol } = app().assets[quoteID]
    for (const el of Doc.applySelector(ancestor, '[data-quote-name')) el.textContent = quoteName
    for (const img of Doc.applySelector(ancestor, '[data-quote-logo]')) img.src = Doc.logoPath(quoteSymbol)
    for (const el of Doc.applySelector(ancestor, '[data-quote-ticker]')) el.textContent = qui.conventional.unit
  }

  /*
    * marketStuff is just a bunch of useful properties for the current specs
    * gathered in one place and with preferable names.
    */
  marketStuff () {
    const {
      page, specs: { host, baseID, quoteID, cexName, botType }, marketReport: { baseFiatRate, quoteFiatRate },
      lotsPerLevelIncrement, updatedConfig: cfg
    } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const { markets } = app().exchanges[host]
    const mktID = `${baseSymbol}_${quoteSymbol}`
    const { lotsize: lotSize } = markets[mktID]
    const lotSizeUSD = lotSize / bui.conventional.conversionFactor * baseFiatRate

    return {
      page, cfg, host, baseID, quoteID, botType, cexName, baseFiatRate, quoteFiatRate,
      bui, qui, baseSymbol, quoteSymbol, mktID, lotSize, lotSizeUSD, lotsPerLevelIncrement
    }
  }

  walletStuff () {
    const {
      specs: { baseID, quoteID }, cexBaseBalance, cexQuoteBalance,
      cexBaseAvail, cexQuoteAvail, dexBaseAvail, dexQuoteAvail
    } = this
    const [baseWallet, quoteWallet] = [app().walletMap[baseID], app().walletMap[quoteID]]
    const [baseToken, quoteToken] = [app().assets[baseID].token, app().assets[quoteID].token]
    const cexTotalBaseBalance = cexBaseBalance ? cexBaseBalance.available + cexBaseBalance.locked : 0
    const cexTotalQuoteBalance = cexQuoteBalance ? cexQuoteBalance.available + cexQuoteBalance.locked : 0

    return {
      baseWallet, quoteWallet, cexTotalBaseBalance, cexTotalQuoteBalance,
      cexBaseAvail, cexQuoteAvail, dexBaseAvail, dexQuoteAvail, baseToken, quoteToken
    }
  }

  showAdvancedConfig (viewOnly: boolean) {
    const { page } = this
    Doc.show(page.advancedConfig)
    Doc.hide(page.quickConfig, page.balanceChecks)
    page.gapStrategyBox.after(page.buttonsBox)

    this.baseBalance.setDisabled(viewOnly)
    this.quoteBalance.setDisabled(viewOnly)
    this.cexBaseBalanceRange?.setDisabled(viewOnly)
    this.cexQuoteBalanceRange?.setDisabled(viewOnly)
    this.cexBaseMinBalanceRange?.setDisabled(viewOnly)
    this.cexQuoteMinBalanceRange?.setDisabled(viewOnly)
    this.placementsChart.render()
  }

  switchToQuickConfig () {
    // const isQuickConfig = (buyPlacements: OrderPlacement[], sellPlacements: OrderPlacement[]) => {
    //   if (buyPlacements.length === 0 || buyPlacements.length !== sellPlacements.length) return false
    //   for (let i = 0; i < buyPlacements.length; i++) {
    //     if (buyPlacements[i].gapFactor !== sellPlacements[i].gapFactor) return false
    //     if (buyPlacements[i].lots !== sellPlacements[i].lots) return false
    //   }
    //   return true
    // }
    const { page, updatedConfig: cfg, specs: { botType } } = this
    const { buyPlacements: buys, sellPlacements: sells } = cfg
    // If we have both buys and sells, get the best approximation quick config
    // approximation.
    if (buys.length > 0 && sells.length > 0) {
      const bestBuy = buys.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
      const bestSell = sells.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
      const placementCount = buys.length + sells.length
      const levelsPerSide = Math.max(1, Math.floor((placementCount) / 2))
      page.levelsPerSide.value = String(levelsPerSide)
      if (botType === botTypeBasicMM) {
        cfg.profit = (bestBuy.gapFactor + bestSell.gapFactor) / 2
        page.qcProfit.value = String(cfg.profit * 100)
        const worstBuy = buys.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor > prev.gapFactor ? curr : prev)
        const worstSell = sells.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor > prev.gapFactor ? curr : prev)
        const range = ((worstBuy.gapFactor - bestBuy.gapFactor) + (worstSell.gapFactor - bestSell.gapFactor)) / 2
        const inc = range / (levelsPerSide - 1)
        page.qcLevelSpacing.value = String(inc * 100)
      } else if (botType === botTypeArbMM) {
        page.qcProfit.value = page.profitInput.value
        this.qcProfitChanged()
        const multSum = buys.reduce((v: number, p: OrderPlacement) => v + p.gapFactor, 0) + sells.reduce((v: number, p: OrderPlacement) => v + p.gapFactor, 0)
        page.qcMatchBuffer.value = String(((multSum / placementCount) - 1) * 100 || String(qcDefaultMatchBuffer * 100))
      }
      const lots = buys.reduce((v: number, p: OrderPlacement) => v + p.lots, 0) + sells.reduce((v: number, p: OrderPlacement) => v + p.lots, 0)
      page.lotsPerLevel.value = String(Math.max(1, Math.round(lots / 2 / levelsPerSide)))
    } else { // no buys and/or no sells
      // These will be set by showQuickConfig.
      page.lotsPerLevel.value = undefined
      page.levelsPerSide.value = undefined
      page.qcProfit.value = undefined
      page.qcLevelSpacing.value = undefined
      page.qcMatchBuffer.value = undefined
    }
    this.showQuickConfig()
  }

  showQuickConfig () {
    const { page, lotSize, bui, baseFiatRate, botType } = this.marketStuff()

    page.qcLotSizeDisplay.textContent = Doc.formatCoinValue(lotSize, bui)
    page.qcLotSizeUnit.textContent = bui.conventional.unit
    const lotSizeUSD = lotSize / bui.conventional.conversionFactor * baseFiatRate
    page.qcLotSizeUSDDisplay.textContent = Doc.formatFourSigFigs(lotSizeUSD)

    this.lotsPerLevelIncrement = Math.round(Math.max(1, qcDefaultLotsPerLevelIncrement / lotSizeUSD))
    if (!page.lotsPerLevel.value) page.lotsPerLevel.value = String(this.lotsPerLevelIncrement)
    if (!page.levelsPerSide.value) page.levelsPerSide.value = String(qcDefaultLevelsPerSide)
    if (!page.qcProfit.value) page.qcProfit.value = String(qcDefaultProfitThreshold * 100)
    if (!page.qcLevelSpacing.value) page.qcLevelSpacing.value = String(qcDefaultLevelSpacing * 100)
    if (!page.qcMatchBuffer.value) page.qcMatchBuffer.value = String(qcDefaultMatchBuffer * 100)

    Doc.hide(
      page.advancedConfig, page.levelSpacingBox, page.matchMultiplierBox, page.qcLevelSettingsBox,
      page.placementsChartBox, page.placementChartLegend, page.lotsPerLevelLabel, page.arbLotsLabel
    )
    Doc.show(page.quickConfig, page.balanceChecks)
    page.quickConfig.after(page.buttonsBox)

    this.baseBalance.setDisabled(true)
    this.quoteBalance.setDisabled(true)
    this.cexBaseBalanceRange?.setDisabled(true)
    this.cexQuoteBalanceRange?.setDisabled(true)
    this.cexBaseMinBalanceRange?.setDisabled(true)
    this.cexQuoteMinBalanceRange?.setDisabled(true)

    switch (botType) {
      case botTypeArbMM:
        Doc.show(page.qcLevelSettingsBox, page.matchMultiplierBox, page.placementsChartBox, page.placementChartLegend, page.lotsPerLevelLabel)
        page.qcLevelSettingsBox.append(page.qcLotsBox)
        page.qcLevelSettingsBox.after(page.qcCommitDisplay)
        break
      case botTypeBasicMM:
        Doc.show(page.qcLevelSettingsBox, page.levelSpacingBox, page.placementsChartBox, page.lotsPerLevelLabel)
        page.qcLevelSettingsBox.append(page.qcLotsBox)
        page.qcLevelSettingsBox.after(page.qcCommitDisplay)
        page.gapStrategySelect.value = GapStrategyPercentPlus
        break
      case botTypeBasicArb:
        Doc.show(page.arbLotsLabel)
        page.qcProfitBox.append(page.qcLotsBox)
        page.qcProfitBox.after(page.qcCommitDisplay)
    }

    this.quickConfigUpdated()
  }

  quickConfigUpdated () {
    const { page, cfg, lotSize, lotSizeUSD, cexName, quoteFiatRate, bui, qui, botType } = this.marketStuff()
    const {
      cexBaseAvail, cexQuoteAvail, dexBaseAvail, dexQuoteAvail, baseWallet, quoteWallet,
      baseToken, quoteToken, cexTotalBaseBalance, cexTotalQuoteBalance
    } = this.walletStuff()

    Doc.hide(page.qcError)
    const setError = (msg: string) => {
      page.qcError.textContent = msg
      Doc.show(page.qcError)
    }

    const levelsPerSide = botType === botTypeBasicArb ? 1 : parseInt(page.levelsPerSide.value ?? '')
    if (isNaN(levelsPerSide)) {
      setError('invalid value for levels per side')
    }

    const lotsPerLevel = parseInt(page.lotsPerLevel.value ?? '')
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
    if (isNaN(lotsPerLevel)) {
      setError('invalid value for match buffer')
    }
    const multiplier = matchBuffer + 1

    const levelSpacingDisabled = levelsPerSide === 1
    page.levelSpacingBox.classList.toggle('disabled', levelSpacingDisabled)
    this.qcLevelSpacingSlider.setDisabled(levelSpacingDisabled)
    page.qcLevelSpacing.disabled = levelSpacingDisabled

    const lotsPerSide = levelsPerSide * lotsPerLevel
    const totalLots = lotsPerSide * 2
    page.qcLotsDisplay.textContent = String(totalLots)
    page.qcCommitUSDDisplay.textContent = (lotSizeUSD * totalLots).toFixed(2)

    if (botType !== botTypeBasicArb) {
      this.clearPlacements(cexName ? arbMMRowCacheKey : cfg.gapStrategy)
      for (let levelN = 0; levelN < levelsPerSide; levelN++) {
        const placement = { lots: lotsPerLevel } as OrderPlacement
        placement.gapFactor = botType === botTypeBasicMM ? profit + levelSpacing * levelN : multiplier
        cfg.buyPlacements.push(placement)
        cfg.sellPlacements.push(placement)
        // Add rows in the advanced config table.
        this.addPlacement(true, placement, false)
        this.addPlacement(false, placement, false)
      }

      this.placementsChart.render()
    }

    const fees = this.feeOutlook()

    // For a recommended balance allocation, we'll use 3x the lot commitment.
    // 2x for standing orders and cex balance on hand, then some buffer to
    // accomodate transfers and ensure operations while waiting for
    // withdraws and deposits.
    const baseAllocation = lotsPerSide * lotSize * 3
    const driftFactor = 1.5
    const quoteLot = Math.round(lotSizeUSD / quoteFiatRate * qui.conventional.conversionFactor)
    const quoteAllocation = Math.round(lotsPerSide * quoteLot * 3 * driftFactor)

    const allocations = (totalAlloc: number, dexAvail: number, cexAvail: number, bookingFees: number) => {
      let [dexAlloc, cexAlloc, deficit] = [totalAlloc / 2 + bookingFees, totalAlloc / 2, 0]
      if (dexAlloc > dexAvail) {
        cexAlloc += dexAlloc - dexAvail
        dexAlloc = dexAvail
      }
      if (cexAlloc > cexAvail) {
        deficit = cexAlloc - cexAvail
        cexAlloc = cexAvail
        const dexRemain = dexAvail - dexAlloc
        if (dexRemain > 0) {
          const dexAdd = Math.min(dexRemain, deficit)
          deficit -= dexAdd
          dexAlloc += dexAdd
        }
      }
      return [dexAlloc, cexAlloc, deficit]
    }

    const [cexBAvail, cexQAvail] = botType === botTypeBasicMM ? [0, 0] : [cexBaseAvail, cexQuoteAvail]
    const [dexBaseAlloc, cexBaseAlloc, baseDeficit] = allocations(baseAllocation, dexBaseAvail, cexBAvail, (baseToken ? 0 : fees.base.bookingFees))
    const [dexQuoteAlloc, cexQuoteAlloc, quoteDeficit] = allocations(quoteAllocation, dexQuoteAvail, cexQAvail, (quoteToken ? 0 : fees.quote.bookingFees))

    this.baseBalance.setValue(dexBaseAlloc / baseWallet.balance.available * 100, true)
    this.quoteBalance.setValue(dexQuoteAlloc / quoteWallet.balance.available * 100, true)
    this.cexBaseBalanceRange?.setValue(cexBaseAlloc / cexTotalBaseBalance * 100, true)
    this.cexQuoteBalanceRange?.setValue(cexQuoteAlloc / cexTotalQuoteBalance * 100, true)

    if (baseToken) {
      const feeReserves = fees.base.bookingFees + tokenFeeMultiplier * fees.base.reservesPerSwap
      cfg.baseTokenFees = this.setTokenFees(feeReserves, this.baseTokenFeesBox())
    }
    if (quoteToken) {
      const feeReserves = fees.quote.bookingFees + tokenFeeMultiplier * fees.quote.reservesPerSwap
      cfg.quoteTokenFees = this.setTokenFees(feeReserves, this.quoteTokenFeesBox())
    }

    Doc.hide(page.baseBalNotOK, page.baseBalNotOKBals, page.baseBalOK, page.baseBalOKBals)
    Doc.setVis(Boolean(cexName), ...Doc.applySelector(page.balanceChecks, '[data-cex-show]'))
    if (baseDeficit > 0) {
      Doc.show(page.baseBalNotOK, page.baseBalNotOKBals)
      page.baseBalanceAvail.textContent = Doc.formatCoinValue(dexBaseAvail + cexBAvail, bui)
      page.baseBalanceCEXAvail.textContent = Doc.formatCoinValue(cexBAvail, bui)
      page.baseBalanceDEXAvail.textContent = Doc.formatCoinValue(dexBaseAvail, bui)
      page.baseBalanceRequired.textContent = Doc.formatCoinValue(baseAllocation, bui)
    } else {
      Doc.show(page.baseBalOK, page.baseBalOKBals)
      page.baseBalanceCommit.textContent = Doc.formatCoinValue(cexBaseAlloc + dexBaseAlloc, bui)
      page.baseBalanceCEXCommit.textContent = Doc.formatCoinValue(cexBaseAlloc, bui)
      page.baseBalanceDEXCommit.textContent = Doc.formatCoinValue(dexBaseAlloc, bui)
    }

    Doc.hide(page.quoteBalNotOK, page.quoteBalNotOKBals, page.quoteBalOK, page.quoteBalOKBals)
    if (quoteDeficit > 0) {
      Doc.show(page.quoteBalNotOK, page.quoteBalNotOKBals)
      page.quoteBalanceAvail.textContent = Doc.formatCoinValue(dexQuoteAvail + cexQAvail, qui)
      page.quoteBalanceCEXAvail.textContent = Doc.formatCoinValue(cexQAvail, qui)
      page.quoteBalanceDEXAvail.textContent = Doc.formatCoinValue(dexQuoteAvail, qui)
      page.quoteBalanceRequired.textContent = Doc.formatCoinValue(quoteAllocation, qui)
    } else {
      Doc.show(page.quoteBalOK, page.quoteBalOKBals)
      page.quoteBalanceCommit.textContent = Doc.formatCoinValue(cexQuoteAlloc + dexQuoteAlloc, qui)
      page.quoteBalanceCEXCommit.textContent = Doc.formatCoinValue(cexQuoteAlloc, qui)
      page.quoteBalanceDEXCommit.textContent = Doc.formatCoinValue(dexQuoteAlloc, qui)
    }

    if (!page.cexRebalanceCheckbox.checked || botType === botTypeBasicMM) return
    this.cexBaseMinBalanceRange?.setValue(lotsPerSide * lotSize / cexBAvail * 100, true)
    this.cexQuoteMinBalanceRange?.setValue(lotsPerSide * quoteLot * driftFactor / cexQAvail * 100, true)
  }

  levelsPerSideChanged () {
    const { page } = this
    page.levelsPerSide.value = String(Math.round(Math.max(1, parseInt(page.levelsPerSide.value ?? '') || 1)))
    this.quickConfigUpdated()
  }

  incrementLevelsPerSide (up: boolean) {
    const { page } = this
    const delta = up ? 1 : -1
    const levelsPerSide = Math.round(Math.max(1, (parseInt(page.levelsPerSide.value ?? '') || 1) + delta))
    page.levelsPerSide.value = String(levelsPerSide)
    this.quickConfigUpdated()
  }

  lotsPerLevelChanged () {
    const { page } = this
    page.lotsPerLevel.value = String(Math.round(Math.max(1, parseInt(page.lotsPerLevel.value ?? '') || 1)))
    this.quickConfigUpdated()
  }

  incrementLotsPerLevel (up: boolean) {
    const { page, lotsPerLevelIncrement } = this
    const delta = (up ? 1 : -1) * lotsPerLevelIncrement
    const lotsPerLevel = Math.round(Math.max(1, (parseInt(page.lotsPerLevel.value ?? '') || 1) + delta))
    page.lotsPerLevel.value = String(lotsPerLevel)
    this.quickConfigUpdated()
  }

  baseTokenFeesBox (): tokenFeeBox {
    const { specs: { baseID }, page: { baseTokenBooking, baseTokenBookingFiat, baseTokenSwapCount, baseTokenReserves, baseTokenReservesFiat, baseTokenFees } } = this
    const fees = this.feeOutlook()
    const feeAssetID = (app().assets[baseID].token as Token).parentID
    const ui = app().assets[feeAssetID].unitInfo
    return {
      fees: fees.base,
      fiatRate: app().fiatRatesMap[feeAssetID],
      ui,
      conversionFactor: ui.conventional.conversionFactor,
      booking: baseTokenBooking,
      bookingFiat: baseTokenBookingFiat,
      swapCount: baseTokenSwapCount,
      reserves: baseTokenReserves,
      reservesFiat: baseTokenReservesFiat,
      input: baseTokenFees
    }
  }

  quoteTokenFeesBox (): tokenFeeBox {
    const { specs: { quoteID }, page: { quoteTokenBooking, quoteTokenBookingFiat, quoteTokenSwapCount, quoteTokenReserves, quoteTokenReservesFiat, quoteTokenFees } } = this
    const fees = this.feeOutlook()
    const feeAssetID = (app().assets[quoteID].token as Token).parentID
    const ui = app().assets[feeAssetID].unitInfo
    return {
      fees: fees.quote,
      fiatRate: app().fiatRatesMap[feeAssetID],
      ui,
      conversionFactor: ui.conventional.conversionFactor,
      booking: quoteTokenBooking,
      bookingFiat: quoteTokenBookingFiat,
      swapCount: quoteTokenSwapCount,
      reserves: quoteTokenReserves,
      reservesFiat: quoteTokenReservesFiat,
      input: quoteTokenFees
    }
  }

  incrementTokenFees (up: boolean, isBase: boolean) {
    const box = isBase ? this.baseTokenFeesBox() : this.quoteTokenFeesBox()
    const incrementCount = 10
    const adj = incrementCount * box.fees.reservesPerSwap * (up ? 1 : -1)
    const inputValue = parseFloat(box.input.value ?? '') * box.conversionFactor
    const v = Math.max(0, (inputValue + adj) || (box.fees.bookingFees + tokenFeeMultiplier * box.fees.reservesPerSwap))
    if (isBase) this.updatedConfig.baseTokenFees = this.setTokenFees(v, box)
    else this.updatedConfig.quoteTokenFees = this.setTokenFees(v, box)
  }

  setTokenFees (v: number, box: tokenFeeBox) {
    const { fees, fiatRate, ui, booking, bookingFiat, swapCount, reserves, reservesFiat, input, conversionFactor } = box
    booking.textContent = Doc.formatCoinValue(fees.bookingFees, ui)
    bookingFiat.textContent = Doc.formatFourSigFigs(fees.bookingFees / conversionFactor * fiatRate)
    const r = v - fees.bookingFees
    const displayReserves = Math.max(0, r)
    swapCount.textContent = String(Math.floor(displayReserves / fees.reservesPerSwap))
    reserves.textContent = Doc.formatCoinValue(displayReserves, ui)
    reservesFiat.textContent = Doc.formatFourSigFigs(displayReserves / conversionFactor * fiatRate)
    swapCount.classList.toggle('text-danger', r < 0)
    input.classList.toggle('text-danger', r < 0)
    input.value = Doc.formatCoinValue(v, ui)
    return Math.round(v)
  }

  tokenFeesChanged (isBase: boolean) {
    const box = isBase ? this.baseTokenFeesBox() : this.quoteTokenFeesBox()
    const v = parseFloat(box.input.value ?? '') ?? 0
    if (isBase) this.updatedConfig.baseTokenFees = this.setTokenFees(v, box)
    else this.updatedConfig.quoteTokenFees = this.setTokenFees(v, box)
  }

  qcProfitChanged () {
    const { page, updatedConfig: cfg } = this
    const v = Math.max(qcMinimumProfit, parseFloat(page.qcProfit.value ?? '') || qcDefaultProfitThreshold)
    cfg.profit = v
    page.qcProfit.value = v.toFixed(2)
    this.qcProfitSlider.setValue(v / 100, true)
    this.quickConfigUpdated()
  }

  levelSpacingChanged () {
    const { page } = this
    const v = Math.max(1, parseFloat(page.qcLevelSpacing.value ?? '') || qcDefaultLevelSpacing * 100)
    page.qcLevelSpacing.value = v.toFixed(2)
    this.qcLevelSpacingSlider.setValue(v / 100, true)
    this.quickConfigUpdated()
  }

  matchBufferChanged () {
    const { page } = this
    page.qcMatchBuffer.value = Math.max(0, parseFloat(page.qcMatchBuffer.value ?? '') || qcDefaultMatchBuffer * 100).toFixed(2)
    this.quickConfigUpdated()
  }

  showDeposit (assetID: number) {
    this.walletAddrForm.setAsset(assetID)
    this.forms.show(this.page.walletAddrForm)
  }

  async showBotTypeForm (host: string, baseID: number, quoteID: number, botType?: string, configuredCEX?: string) {
    const { page, cexConfigs } = this
    this.formSpecs = { host, baseID, quoteID, botType: '' }
    const viewOnly = isViewOnly(this.formSpecs, this.mmStatus)
    if (viewOnly) {
      const botCfg = await botConfig(this.formSpecs, this.mmStatus)
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
      specs.cexName = botCfg?.cexCfg?.name
      await this.fetchCEXBalances(this.formSpecs)
      await this.configureUI()
      this.forms.close()
      return
    }
    this.setBaseQuote(page.botTypeForm, baseID, quoteID)
    Doc.empty(page.botTypeBaseSymbol, page.botTypeQuoteSymbol)
    const [b, q] = [app().assets[baseID], app().assets[quoteID]]
    page.botTypeBaseSymbol.appendChild(Doc.symbolize(b, true))
    page.botTypeQuoteSymbol.appendChild(Doc.symbolize(q, true))

    for (const div of this.botTypeSelectors) div.classList.remove('selected')
    for (const { div } of Object.values(this.formCexes)) div.classList.remove('selected')
    this.setCEXAvailability(baseID, quoteID)
    Doc.hide(page.noCexesConfigured, page.noCexMarket, page.noCexMarketConfigureMore, page.botTypeErr)
    const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
    const supportingCexes = cexConfigs.filter((cex: CEXConfig) => cexHasMarket(cex.name))
    const arbEnabled = supportingCexes.length > 0
    for (const div of this.botTypeSelectors) div.classList.toggle('disabled', div.dataset.botType !== botTypeBasicMM && !arbEnabled)
    if (cexConfigs.length === 0) Doc.show(page.noCexesConfigured)
    else {
      const lastBots = (State.fetchLocal(lastBotsLK) || {}) as Record<string, BotSpecs>
      const lastBot = lastBots[`${baseID}_${quoteID}_${host}`]
      let cex: CEXConfig | undefined
      botType = botType ?? (lastBot ? lastBot.botType : botTypeArbMM)
      if (botType !== botTypeBasicMM) {
        // Four ways to auto-select a cex.
        // 1. Coming back from the cex configuration form.
        if (configuredCEX) {
          const cexes = supportingCexes.filter((cexCfg: CEXConfig) => cexCfg.name === configuredCEX)
          if (cexes.length > 0 && cexHasMarket(cexes[0].name)) {
            cex = cexes[0]
          }
        }
        // 2. We have a saved configuration.
        if (!cex && lastBot) {
          const cexes = supportingCexes.filter((cexCfg: CEXConfig) => cexCfg.name === lastBot.cexName)
          if (cexes.length > 0) if (cexHasMarket(cexes[0].name)) cex = cexes[0]
        }
        // 3. The last exchange that the user selected.
        if (!cex) {
          const lastCEX = State.fetchLocal(lastArbExchangeLK)
          if (lastCEX) {
            const supporting = supportingCexes.filter((cexCfg: CEXConfig) => cexCfg.name === lastCEX)
            if (supporting.length && cexHasMarket(supporting[0].name)) cex = supporting[0]
          }
        }
        // 4. Any supporting cex.
        if (!cex && supportingCexes.length) cex = supportingCexes[0]
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
        const unconfigured = Object.keys(CEXDisplayInfos).filter((cexName: string) => {
          return cexConfigs.some((cexCfg: CEXConfig) => cexCfg.name === cexName)
        })
        const allConfigured = unconfigured.length === 0 || (unconfigured.length === 1 && (unconfigured[0] === 'Binance' || unconfigured[0] === 'BinanceUS'))
        if (!allConfigured) Doc.show(page.noCexMarketConfigureMore)
      }
    }

    Doc.show(page.cexSelection)
    // Check if we have any cexes configured.
    this.forms.show(page.botTypeForm)
  }

  reshowBotTypeForm () {
    if (isViewOnly(this.specs, this.mmStatus)) this.showMarketSelectForm()
    const { baseID, quoteID, host, cexName, botType } = this.specs
    this.showBotTypeForm(host, baseID, quoteID, botType, cexName)
  }

  setBotTypeSelected (selectedType: string) {
    const { formSpecs: { baseID, quoteID, host }, botTypeSelectors, formCexes, cexConfigs } = this
    for (const { classList, dataset: { botType } } of botTypeSelectors) classList.toggle('selected', botType === selectedType)
    // If we don't have a cex selected, attempt to select one
    if (selectedType === botTypeBasicMM) return
    if (cexConfigs.length === 0) return
    const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
    // If there is one currently selected and it supports this market, leave it.
    const selecteds = Object.values(formCexes).filter((cex: cexButton) => cex.div.classList.contains('selected'))
    if (selecteds.length && cexHasMarket(selecteds[0].name)) return
    // See if we have a saved configuration.
    const lastBots = (State.fetchLocal(lastBotsLK) || {}) as Record<string, BotSpecs>
    const lastBot = lastBots[`${baseID}_${quoteID}_${host}`]
    if (lastBot) {
      const cexes = cexConfigs.filter((cexCfg: CEXConfig) => cexCfg.name === lastBot.cexName)
      if (cexes.length && cexHasMarket(cexes[0].name)) {
        this.selectFormCEX(cexes[0].name)
        return
      }
    }
    // 2. The last exchange that the user selected.
    const lastCEX = State.fetchLocal(lastArbExchangeLK)
    if (lastCEX) {
      const cexes = cexConfigs.filter((cexCfg: CEXConfig) => cexCfg.name === lastCEX)
      if (cexes.length && cexHasMarket(cexes[0].name)) {
        this.selectFormCEX(cexes[0].name)
        return
      }
    }
    // 3. Any supporting cex.
    const cexes = cexConfigs.filter((cexCfg: CEXConfig) => cexHasMarket(cexCfg.name))
    if (cexes.length) this.selectFormCEX(cexes[0].name)
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
    const { page, baseID, quoteID, bui, qui } = this.marketStuff()
    if (n.assetID === baseID) {
      if (n.balance.available > 0 && Doc.isHidden(page.baseBalanceContainer)) this.setupBalanceSelectors(isViewOnly(this.specs, this.mmStatus))
      page.dexBaseAvail.textContent = Doc.formatFourSigFigs(n.balance.available / bui.conventional.conversionFactor)
    } else if (n.assetID === quoteID) {
      if (n.balance.available > 0 && Doc.isHidden(page.quoteBalanceContainer)) this.setupBalanceSelectors(isViewOnly(this.specs, this.mmStatus))
      page.dexQuoteAvail.textContent = Doc.formatFourSigFigs(n.balance.available / qui.conventional.conversionFactor)
    }
    const [{ token: bToken }, { token: qToken }] = [app().assets[baseID], app().assets[quoteID]]
    if (bToken && bToken.parentID === n.assetID) this.tokenFeesChanged(true)
    if (qToken && qToken.parentID === n.assetID) this.tokenFeesChanged(false)
  }

  autoRebalanceChanged () {
    const { page, updatedConfig: cfg } = this
    cfg.cexRebalance = page.cexRebalanceCheckbox?.checked ?? false
    Doc.setVis(cfg.cexRebalance && Doc.isHidden(page.noBaseCEXBalance), page.cexBaseRebalanceOpts)
    Doc.setVis(cfg.cexRebalance && Doc.isHidden(page.noQuoteCEXBalance), page.cexQuoteRebalanceOpts)
  }

  quoteTransferChanged () {
    const { page, updatedConfig: cfg, specs: { quoteID } } = this
    const { unitInfo: ui } = app().assets[quoteID]
    const v = parseFloat(page.cexQuoteTransferInput.value || '') * ui.conventional.conversionFactor
    if (isNaN(v)) return
    cfg.cexQuoteMinTransfer = Math.round(v)
    page.cexQuoteTransferInput.value = Doc.formatCoinValue(v, ui)
    this.updateModifiedMarkers()
  }

  baseTransferChanged () {
    const { page, updatedConfig: cfg, specs: { baseID } } = this
    const { unitInfo: ui } = app().assets[baseID]
    const v = parseFloat(page.cexBaseTransferInput.value || '') * ui.conventional.conversionFactor
    if (isNaN(v)) return
    cfg.cexBaseMinTransfer = Math.round(v)
    page.cexBaseTransferInput.value = Doc.formatCoinValue(v, ui)
    this.updateModifiedMarkers()
  }

  incrementTransfer (isBase: boolean, up: boolean) {
    const { page, updatedConfig: cfg, specs: { host, quoteID, baseID } } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const { markets } = app().exchanges[host]
    const { lotsize: baseLotSize, spot } = markets[`${baseSymbol}_${quoteSymbol}`]
    const lotSize = isBase ? baseLotSize : calculateQuoteLot(baseLotSize, baseID, quoteID, spot)
    const ui = isBase ? bui : qui
    const input = isBase ? page.cexBaseTransferInput : page.cexQuoteTransferInput
    const v = parseFloat(input.value || '') * ui.conventional.conversionFactor || lotSize
    const minIncrement = Math.max(lotSize, Math.round(v * 0.01)) // Minimum increment is 1%
    const newV = Math.round(up ? v + minIncrement : Math.max(lotSize, v - minIncrement))
    input.value = Doc.formatCoinValue(newV, ui)
    if (isBase) cfg.cexBaseMinTransfer = newV
    else cfg.cexQuoteMinTransfer = newV
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
    const { host, baseID, quoteID, cexName, botType } = specs
    if (botType === botTypeBasicMM || !cexName) return

    const reserved = (assetID: number, totalBalance: number) => {
      let v = 0
      for (const { cexCfg, baseID: bID, quoteID: qID, host: hostess } of this.botConfigs) {
        if (!cexCfg) continue
        if (baseID === bID && quoteID === qID && host === hostess) continue
        if (baseID === assetID) v += calcBalanceReserve(totalBalance, cexCfg.baseBalanceType, cexCfg.baseBalance)[1]
        else if (quoteID === assetID) v += calcBalanceReserve(totalBalance, cexCfg.quoteBalanceType, cexCfg.quoteBalance)[1]
      }
      return v
    }

    try {
      // This won't work if we implement live reconfiguration, because locked
      // funds would need to be considered.
      const { available } = this.cexBaseBalance = await MM.cexBalance(cexName, baseID)
      this.cexBaseAvail = Math.max(0, available - reserved(baseID, available))
    } catch (e) {
      page.botTypeErr.textContent = intl.prep(intl.ID_CEXBALANCE_ERR, { cexName, assetID: String(baseID), err: String(e) })
      Doc.show(page.botTypeErr)
      throw e
    }

    try {
      const { available } = this.cexQuoteBalance = await MM.cexBalance(cexName, quoteID)
      this.cexQuoteAvail = Math.max(0, available - reserved(quoteID, available))
    } catch (e) {
      page.botTypeErr.textContent = intl.prep(intl.ID_CEXBALANCE_ERR, { cexName, assetID: String(quoteID), err: String(e) })
      Doc.show(page.botTypeErr)
      throw e
    }
  }

  defaultWalletOptions (assetID: number) : Record<string, string> {
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
    const { page, originalConfig: oldCfg, updatedConfig: newCfg, specs: { baseID, quoteID, botType } } = this

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
    page.driftToleranceContainer.classList.toggle('modified', this.driftTolerance.modified())
    page.oracleBiasContainer.classList.toggle('modified', this.oracleBias.modified())
    page.useOracleCheckbox.classList.toggle('modified', oldCfg.useOracles !== newCfg.useOracles)
    page.oracleWeightingContainer.classList.toggle('modified', this.oracleWeighting.modified())
    page.emptyMarketRateInput.classList.toggle('modified', oldCfg.emptyMarketRate !== newCfg.emptyMarketRate)
    page.emptyMarketRateCheckbox.classList.toggle('modified', oldCfg.useEmptyMarketRate !== newCfg.useEmptyMarketRate)
    page.baseBalanceContainer.classList.toggle('modified', this.baseBalance.modified())
    page.quoteBalanceContainer.classList.toggle('modified', this.quoteBalance.modified())
    page.orderPersistenceContainer.classList.toggle('modified', this.orderPersistence.modified())

    if (botType !== botTypeBasicMM) {
      page.cexBaseBalanceContainer.classList.toggle('modified', this.cexBaseBalanceRange.modified())
      page.cexBaseMinBalanceContainer.classList.toggle('modified', this.cexBaseMinBalanceRange.modified())
      page.cexQuoteBalanceContainer.classList.toggle('modified', this.cexQuoteBalanceRange.modified())
      page.cexQuoteMinBalanceContainer.classList.toggle('modified', this.cexQuoteMinBalanceRange.modified())
      const { unitInfo: bui } = app().assets[baseID]
      const { unitInfo: qui } = app().assets[quoteID]
      page.cexBaseTransferInput.classList.toggle('modified', page.cexBaseTransferInput.value !== Doc.formatCoinValue(oldCfg.cexBaseMinTransfer, bui))
      page.cexQuoteTransferInput.classList.toggle('modified', page.cexQuoteTransferInput.value !== Doc.formatCoinValue(oldCfg.cexQuoteMinTransfer, qui))
    }

    // Base wallet settings
    for (const opt of Object.keys(this.baseWalletSettingControl)) {
      this.baseWalletSettingControl[opt].toHighlight.classList.toggle('modified', oldCfg.baseOptions[opt] !== newCfg.baseOptions[opt])
    }

    // Quote wallet settings
    for (const opt of Object.keys(this.quoteWalletSettingControl)) {
      this.quoteWalletSettingControl[opt].toHighlight.classList.toggle('modified', oldCfg.quoteOptions[opt] !== newCfg.quoteOptions[opt])
    }
  }

  /*
   * gapFactorHeaderUnit returns the header on the placements table and the
   * units in the gap factor rows needed for each gap strategy.
   */
  gapFactorHeaderUnit (gapStrategy: string) : [string, string] {
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
  checkGapFactorRange (gapFactor: string, value: number) : (string | null) {
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
  addPlacement (isBuy: boolean, initialLoadPlacement: OrderPlacement | null, running: boolean, gapStrategy?: string) {
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
        if (running) {
          Doc.hide(row.upBtn, row.downBtn)
        } else {
          Doc.setVis(i !== 0, row.upBtn)
          Doc.setVis(i !== tableBody.children.length - 2, row.downBtn)
        }
      }
    }

    Doc.hide(errElement)
    const setErr = (err: string) => {
      errElement.textContent = err
      Doc.show(errElement)
    }

    let lots : number
    let actualGapFactor : number
    let displayedGapFactor : number
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
    })
    if (running) {
      Doc.hide(newRowTmpl.removeBtn)
    }

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

  /*
   * setupMMConfigSettings sets up the controls for the settings defined in
   * the market making config.
   */
  setupMMConfigSettings (viewOnly: boolean) {
    const { page, updatedConfig: cfg } = this

    // Gap Strategy
    Doc.bind(page.gapStrategySelect, 'change', () => {
      if (!page.gapStrategySelect.value) return
      const gapStrategy = page.gapStrategySelect.value
      this.clearPlacements(cfg.gapStrategy)
      this.loadCachedPlacements(gapStrategy)
      cfg.gapStrategy = gapStrategy
      this.setGapFactorLabels(gapStrategy)
      this.updateModifiedMarkers()
    })
    if (viewOnly) {
      page.gapStrategySelect.setAttribute('disabled', 'true')
    }

    // Buy/Sell placements
    Doc.bind(page.addBuyPlacementBtn, 'click', () => {
      this.addPlacement(true, null, false)
      page.addBuyPlacementLots.value = ''
      page.addBuyPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
      this.placementsChart.render()
    })
    Doc.bind(page.addSellPlacementBtn, 'click', () => {
      this.addPlacement(false, null, false)
      page.addSellPlacementLots.value = ''
      page.addSellPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
      this.placementsChart.render()
    })
    Doc.setVis(!viewOnly, page.addBuyPlacementRow, page.addSellPlacementRow)

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

    const handleChanged = () => { this.updateModifiedMarkers() }

    // Profit
    page.profitInput.value = String(cfg.profit)
    Doc.bind(page.profitInput, 'change', () => {
      Doc.hide(page.profitInputErr)
      const showError = (errID: string) => {
        Doc.show(page.profitInputErr)
        page.profitInputErr.textContent = intl.prep(errID)
      }
      cfg.profit = parseFloat(page.profitInput.value || '')
      if (isNaN(cfg.profit)) return showError(intl.ID_INVALID_VALUE)
      if (cfg.profit === 0) return showError(intl.ID_NO_ZERO)
      this.updateModifiedMarkers()
    })

    Doc.empty(
      page.driftToleranceContainer, page.orderPersistenceContainer, page.oracleBiasContainer,
      page.oracleWeightingContainer, page.qcLevelSpacingContainer, page.qcProfitSliderContainer,
      page.qcLevelSpacingContainer, page.qcMatchBufferContainer
    )

    // Drift tolerance
    this.driftTolerance = new XYRangeHandler(driftToleranceRange, cfg.driftTolerance, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'driftTolerance',
      changed: handleChanged
    })
    page.driftToleranceContainer.appendChild(this.driftTolerance.control)

    // CEX order persistence
    this.orderPersistence = new XYRangeHandler(orderPersistenceRange, cfg.orderPersistence, {
      roundX: true, roundY: true, disabled: viewOnly, settingsDict: cfg, settingsKey: 'orderPersistence',
      changed: handleChanged,
      updated: (x: number) => {
        this.orderPersistence.setXLabel('')
        x = Math.round(x)
        this.orderPersistence.setYLabel(x === 21 ? '∞' : String(x))
        // return x
      }
    })
    page.orderPersistenceContainer.appendChild(this.orderPersistence.control)

    // Use oracle
    Doc.bind(page.useOracleCheckbox, 'change', () => {
      this.useOraclesChanged()
      this.updateModifiedMarkers()
    })
    if (viewOnly) {
      page.useOracleCheckbox.setAttribute('disabled', 'true')
    }

    // Oracle Bias
    this.oracleBias = new XYRangeHandler(oracleBiasRange, cfg.oracleBias, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'oracleBias', changed: handleChanged
    })
    page.oracleBiasContainer.appendChild(this.oracleBias.control)

    // Oracle Weighting
    this.oracleWeighting = new XYRangeHandler(oracleWeightRange, cfg.oracleWeighting, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'oracleWeighting', changed: handleChanged
    })
    page.oracleWeightingContainer.appendChild(this.oracleWeighting.control)

    // Empty Market Rate
    Doc.bind(page.emptyMarketRateCheckbox, 'change', () => {
      this.useEmptyMarketRateChanged()
      this.updateModifiedMarkers()
    })
    Doc.bind(page.emptyMarketRateInput, 'change', () => {
      Doc.hide(page.emptyMarketRateErr)
      cfg.emptyMarketRate = parseFloat(page.emptyMarketRateInput.value || '0')
      this.updateModifiedMarkers()
      if (cfg.emptyMarketRate === 0) {
        Doc.show(page.emptyMarketRateErr)
        page.emptyMarketRateErr.textContent = intl.prep(intl.ID_NO_ZERO)
      }
    })
    if (viewOnly) {
      page.emptyMarketRateCheckbox.setAttribute('disabled', 'true')
      page.emptyMarketRateInput.setAttribute('disabled', 'true')
    }

    this.placementsChart.setMarket()

    // Quick Config
    this.qcProfitSlider = new XYRangeHandler(profitSliderRange, qcDefaultProfitThreshold, {
      updated: (x: number /* , y: number */) => {
        cfg.profit = x * 100
        page.qcProfit.value = page.profitInput.value = cfg.profit.toFixed(2)
        this.quickConfigUpdated()
      },
      changed: () => { this.quickConfigUpdated() },
      disabled: viewOnly
    })
    page.qcProfitSliderContainer.appendChild(this.qcProfitSlider.control)

    this.qcLevelSpacingSlider = new XYRangeHandler(levelSpacingRange, qcDefaultLevelSpacing, {
      updated: (x: number /* , y: number */) => {
        page.qcLevelSpacing.value = (x * 100).toFixed(2)
        this.quickConfigUpdated()
      },
      changed: () => { this.quickConfigUpdated() },
      disabled: viewOnly
    })
    page.qcLevelSpacingContainer.appendChild(this.qcLevelSpacingSlider.control)

    const matchBufferRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: '100%',
        x: 1,
        y: 100
      },
      xUnit: '',
      yUnit: '%'
    }

    this.qcMatchBufferSlider = new XYRangeHandler(matchBufferRange, qcDefaultMatchBuffer, {
      updated: (x: number /* , y: number */) => {
        page.qcMatchBuffer.value = (x * 100).toFixed(2)
        this.quickConfigUpdated()
      },
      changed: () => { this.quickConfigUpdated() },
      disabled: viewOnly
    })
    page.qcMatchBufferContainer.appendChild(this.qcMatchBufferSlider.control)
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
    cfg.buyPlacements = []
    cfg.sellPlacements = []
  }

  loadCachedPlacements (cacheKey: string) {
    const c = this.placementsCache[cacheKey]
    if (!c) return
    const { updatedConfig: cfg } = this
    cfg.buyPlacements = c[0]
    cfg.sellPlacements = c[1]
    const gapStrategy = cacheKey === arbMMRowCacheKey ? GapStrategyMultiplier : cacheKey
    for (const p of cfg.buyPlacements) this.addPlacement(true, p, false, gapStrategy)
    for (const p of cfg.sellPlacements) this.addPlacement(false, p, false, gapStrategy)
  }

  useOraclesChanged () {
    const { page, updatedConfig: cfg } = this
    if (page.useOracleCheckbox.checked) {
      Doc.show(page.oracleBiasSection, page.oracleWeightingSection)
      cfg.useOracles = true
      this.oracleWeighting.setValue(cfg.oracleWeighting || defaultMarketMakingConfig.oracleWeighting)
      this.oracleBias.setValue(cfg.oracleBias || defaultMarketMakingConfig.oracleBias)
    } else {
      Doc.hide(page.oracleBiasSection, page.oracleWeightingSection)
      cfg.useOracles = false
    }
  }

  useEmptyMarketRateChanged () {
    const { page, updatedConfig: cfg } = this
    if (page.emptyMarketRateCheckbox.checked) {
      cfg.useEmptyMarketRate = true
      const r = cfg.emptyMarketRate ?? this.originalConfig.emptyMarketRate ?? 0
      page.emptyMarketRateInput.value = String(r)
      cfg.emptyMarketRate = r
      Doc.show(page.emptyMarketRateInputBox)
      this.updateModifiedMarkers()
    } else {
      cfg.useEmptyMarketRate = false
      Doc.hide(page.emptyMarketRateInputBox)
    }
  }

  /*
   * setOriginalValues sets the updatedConfig field to be equal to the
   * and sets the values displayed buy each field input to be equal
   * to the values since the last save.
   */
  setOriginalValues (viewOnly: boolean) {
    const {
      page, originalConfig: oldCfg, updatedConfig: cfg, cexConfigs,
      oracleBias, oracleWeighting, driftTolerance, orderPersistence,
      cexBaseBalanceRange, cexBaseMinBalanceRange, cexQuoteBalanceRange, cexQuoteMinBalanceRange,
      baseBalance, quoteBalance, specs: { baseID, quoteID, cexName, botType }
    } = this

    this.clearPlacements(cexName ? arbMMRowCacheKey : cfg.gapStrategy)

    // The RangeOptions maintain references to the options object, so we'll
    // need to preserve those.
    const [bOpts, qOpts] = [cfg.baseOptions, cfg.quoteOptions]

    Object.assign(cfg, JSON.parse(JSON.stringify(oldCfg)))

    // Re-assing the wallet options.
    for (const k of Object.keys(bOpts)) delete bOpts[k]
    for (const k of Object.keys(qOpts)) delete qOpts[k]
    Object.assign(bOpts, oldCfg.baseOptions)
    Object.assign(qOpts, oldCfg.quoteOptions)
    cfg.baseOptions = bOpts
    cfg.quoteOptions = qOpts

    oracleBias.reset()
    oracleWeighting.reset()
    driftTolerance.reset()
    orderPersistence.reset()
    if (baseBalance) baseBalance.reset()
    if (quoteBalance) quoteBalance.reset()
    page.profitInput.value = String(cfg.profit)
    page.useOracleCheckbox.checked = cfg.useOracles && oldCfg.oracleWeighting > 0
    this.useOraclesChanged()
    page.emptyMarketRateCheckbox.checked = cfg.useEmptyMarketRate && cfg.emptyMarketRate > 0
    this.useEmptyMarketRateChanged()

    const [{ token: baseToken }, { token: quoteToken }] = [app().assets[baseID], app().assets[quoteID]]
    if (baseToken) {
      const ui = app().assets[baseToken.parentID].unitInfo
      page.baseTokenFees.value = Doc.formatCoinValue(oldCfg.baseTokenFees, ui)
    }
    if (quoteToken) {
      const ui = app().assets[quoteToken.parentID].unitInfo
      page.baseTokenFees.value = Doc.formatCoinValue(oldCfg.quoteTokenFees, ui)
    }

    if (cexName) {
      cexBaseBalanceRange.reset()
      cexBaseMinBalanceRange.reset()
      cexQuoteBalanceRange.reset()
      cexQuoteMinBalanceRange.reset()
      const { unitInfo: bui } = app().assets[baseID]
      const { unitInfo: qui } = app().assets[quoteID]
      page.cexBaseTransferInput.value = Doc.formatCoinValue(cfg.cexBaseMinTransfer, bui)
      page.cexQuoteTransferInput.value = Doc.formatCoinValue(cfg.cexQuoteMinTransfer, qui)
      page.cexRebalanceCheckbox.checked = cfg.cexRebalance
      this.autoRebalanceChanged()
    }

    // Gap strategy
    if (!page.gapStrategySelect.options) return
    Array.from(page.gapStrategySelect.options).forEach((opt: HTMLOptionElement) => { opt.selected = opt.value === cfg.gapStrategy })
    this.setGapFactorLabels(cfg.gapStrategy)

    if (botType !== botTypeBasicMM) {
      for (const cexCfg of cexConfigs) {
        if (cexCfg.name !== cexName) continue
        Doc.hide(page.gapStrategyBox, page.oraclesSettingBox)
        Doc.show(page.profitSelectorBox, page.orderPersistenceBox)
        this.setArbMMLabels()
      }
    } else {
      Doc.show(page.gapStrategyBox, page.oraclesSettingBox)
      Doc.hide(page.profitSelectorBox, page.orderPersistenceBox)
      this.setGapFactorLabels(page.gapStrategySelect.value || '')
    }

    // Buy/Sell placements
    oldCfg.buyPlacements.forEach((p) => { this.addPlacement(true, p, viewOnly) })
    oldCfg.sellPlacements.forEach((p) => { this.addPlacement(false, p, viewOnly) })

    for (const opt of Object.keys(cfg.baseOptions)) {
      const value = oldCfg.baseOptions[opt]
      cfg.baseOptions[opt] = value
      if (this.baseWalletSettingControl[opt]) {
        this.baseWalletSettingControl[opt].setValue(value)
      }
    }

    // Quote wallet options
    for (const opt of Object.keys(cfg.quoteOptions)) {
      const value = oldCfg.quoteOptions[opt]
      cfg.quoteOptions[opt] = value
      if (this.quoteWalletSettingControl[opt]) {
        this.quoteWalletSettingControl[opt].setValue(value)
      }
    }

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
      updatedConfig: { sellPlacements, buyPlacements, profit, useEmptyMarketRate, emptyMarketRate }
    } = this
    const setError = (errEl: PageElement, errID: string) => {
      ok = false
      if (!showErrors) return
      errEl.textContent = intl.prep(errID)
      Doc.show(errEl)
    }
    if (showErrors) {
      Doc.hide(
        page.buyPlacementsErr, page.sellPlacementsErr, page.profitInputErr,
        page.emptyMarketRateErr, page.baseBalanceErr, page.quoteBalanceErr
      )
    }
    if (botType !== botTypeBasicArb && buyPlacements.length + sellPlacements.length === 0) {
      setError(page.buyPlacementsErr, intl.ID_NO_PLACEMENTS)
      setError(page.sellPlacementsErr, intl.ID_NO_PLACEMENTS)
    }
    if (botType !== botTypeBasicMM) {
      if (isNaN(profit)) setError(page.profitInputErr, intl.ID_INVALID_VALUE)
      else if (profit === 0) setError(page.profitInputErr, intl.ID_NO_ZERO)
    } else { // basic mm
      // TODO: Should we enforce an empty market rate if there are no
      // oracles?
      if (useEmptyMarketRate && emptyMarketRate === 0) setError(page.emptyMarketRateErr, intl.ID_NO_ZERO)
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
    const { updatedConfig: cfg, specs: { baseID, quoteID, host, botType, cexName } } = this
    const botCfg: BotConfig = {
      host: host,
      baseID: baseID,
      quoteID: quoteID,
      baseBalanceType: cfg.baseBalanceType,
      baseBalance: cfg.baseBalance,
      quoteBalanceType: cfg.quoteBalanceType,
      quoteBalance: cfg.quoteBalance,
      baseFeeAssetBalanceType: BalanceType.Amount,
      baseFeeAssetBalance: cfg.baseTokenFees,
      quoteFeeAssetBalanceType: BalanceType.Amount,
      quoteFeeAssetBalance: cfg.quoteTokenFees,
      disabled: cfg.disabled,
      baseWalletOptions: cfg.baseOptions,
      quoteWalletOptions: cfg.quoteOptions
    }
    switch (botType) {
      case botTypeBasicMM:
        botCfg.basicMarketMakingConfig = this.basicMMConfig()
        break
      case botTypeArbMM:
        botCfg.arbMarketMakingConfig = this.arbMMConfig()
        botCfg.cexCfg = this.cexConfig()
        break
      case botTypeBasicArb:
        botCfg.simpleArbConfig = this.basicArbConfig()
        botCfg.cexCfg = this.cexConfig()
    }

    await MM.updateBotConfig(botCfg)
    this.originalConfig = JSON.parse(JSON.stringify(cfg))
    this.updateModifiedMarkers()
    const lastBots = State.fetchLocal(lastBotsLK) || {}
    lastBots[`${baseID}_${quoteID}_${host}`] = this.specs
    State.storeLocal(lastBotsLK, lastBots)
    if (cexName) State.storeLocal(lastArbExchangeLK, cexName)
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

  autoRebalanceConfig (): AutoRebalanceConfig {
    const { updatedConfig: cfg, specs: { baseID, quoteID }, cexBaseMinBalanceRange, cexQuoteMinBalanceRange } = this
    const { unitInfo: bui } = app().assets[baseID]
    const { unitInfo: qui } = app().assets[quoteID]
    return {
      minBaseAmt: Math.round(cexBaseMinBalanceRange.y * bui.conventional.conversionFactor),
      minBaseTransfer: cfg.cexBaseMinTransfer,
      minQuoteAmt: Math.round(cexQuoteMinBalanceRange.y * qui.conventional.conversionFactor),
      minQuoteTransfer: cfg.cexQuoteMinTransfer
    }
  }

  cexConfig (): BotCEXCfg {
    const { updatedConfig: cfg } = this
    const cexCfg : BotCEXCfg = {
      name: this.specs.cexName || '',
      baseBalanceType: BalanceType.Percentage,
      baseBalance: cfg.cexBaseBalance,
      quoteBalanceType: BalanceType.Percentage,
      quoteBalance: cfg.cexQuoteBalance
    }
    if (this.page.cexRebalanceCheckbox.checked) {
      cexCfg.autoRebalance = this.autoRebalanceConfig()
    }
    return cexCfg
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
      driftTolerance: cfg.driftTolerance,
      oracleWeighting: cfg.useOracles ? cfg.oracleWeighting : 0,
      oracleBias: cfg.useOracles ? cfg.oracleBias : 0,
      emptyMarketRate: cfg.useEmptyMarketRate ? cfg.emptyMarketRate : 0
    }
    return mmCfg
  }

  /*
   * setupBalanceSelectors sets up the balance selection sections. If an asset
   * has no balance available, or of other market makers have claimed the entire
   * balance, a message communicating this is displayed.
   */
  setupBalanceSelectors (viewOnly: boolean) {
    const { page, updatedConfig: cfg, specs: { host, baseID, quoteID }, botConfigs } = this
    const { wallet: { balance: { available: bAvail } }, unitInfo: bui, token: bToken } = app().assets[baseID]
    const { wallet: { balance: { available: qAvail } }, unitInfo: qui, token: qToken } = app().assets[quoteID]

    let baseReservedByOtherBots = 0
    let quoteReservedByOtherBots = 0
    for (const botCfg of botConfigs) {
      if (botCfg.baseID === baseID && botCfg.quoteID === quoteID && botCfg.host === host) {
        continue
      }
      if (botCfg.baseID === baseID) baseReservedByOtherBots += botCfg.baseBalance
      if (botCfg.quoteID === baseID) baseReservedByOtherBots += botCfg.quoteBalance
      if (botCfg.baseID === quoteID) quoteReservedByOtherBots += botCfg.baseBalance
      if (botCfg.quoteID === quoteID) quoteReservedByOtherBots += botCfg.quoteBalance
    }

    const baseMaxPercent = baseReservedByOtherBots < 100 ? 100 - baseReservedByOtherBots : 0
    const quoteMaxPercent = quoteReservedByOtherBots < 100 ? 100 - quoteReservedByOtherBots : 0
    this.dexBaseAvail = Math.round(bAvail * baseMaxPercent / 100)
    const baseMaxAvailable = Doc.conventionalCoinValue(this.dexBaseAvail, bui)
    this.dexQuoteAvail = Math.round(qAvail * quoteMaxPercent / 100)
    const quoteMaxAvailable = Doc.conventionalCoinValue(this.dexQuoteAvail, qui)

    page.dexBaseAvail.textContent = Doc.formatFourSigFigs(baseMaxAvailable)
    page.dexQuoteAvail.textContent = Doc.formatFourSigFigs(quoteMaxAvailable)

    const baseXYRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: `${baseMaxPercent}%`,
        x: baseMaxPercent,
        y: baseMaxAvailable
      },
      xUnit: '%',
      yUnit: bui.conventional.unit
    }

    const quoteXYRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: `${quoteMaxPercent}%`,
        x: quoteMaxPercent,
        y: quoteMaxAvailable
      },
      xUnit: '%',
      yUnit: qui.conventional.unit
    }

    Doc.hide(
      page.noBaseBalance, page.noQuoteBalance, page.baseBalanceContainer,
      page.quoteBalanceContainer, page.baseTokenFeeSettings, page.quoteTokenFeeSettings
    )
    Doc.empty(page.baseBalanceContainer, page.quoteBalanceContainer)

    this.baseBalance = new XYRangeHandler(baseXYRange, cfg.baseBalance, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'baseBalance',
      changed: () => { this.updateModifiedMarkers() }
    })
    page.baseBalanceContainer.appendChild(this.baseBalance.control)
    if (baseMaxAvailable > 0) Doc.show(page.baseBalanceContainer)
    else Doc.show(page.noBaseBalance)

    if (bToken) {
      Doc.show(page.baseTokenFeeSettings)
      const { symbol, unitInfo: { conventional: { unit } } } = app().assets[bToken.parentID]
      page.baseTokenParentLogo.src = Doc.logoPath(symbol)
      for (const el of Doc.applySelector(page.baseTokenFeeSettings, '[data-base-fee-ticker]')) el.textContent = unit
      const box = this.quoteTokenFeesBox()
      if (cfg.baseTokenFees) this.setTokenFees(cfg.baseTokenFees, box)
      else {
        const defaultFees = box.fees.bookingFees + tokenFeeMultiplier * box.fees.reservesPerSwap
        cfg.baseTokenFees = this.originalConfig.baseTokenFees = this.setTokenFees(defaultFees, this.quoteTokenFeesBox())
      }
    }

    this.quoteBalance = new XYRangeHandler(quoteXYRange, cfg.quoteBalance, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'quoteBalance',
      changed: () => { this.updateModifiedMarkers() }
    })
    page.quoteBalanceContainer.appendChild(this.quoteBalance.control)
    if (quoteMaxAvailable > 0) Doc.show(page.quoteBalanceContainer)
    else Doc.show(page.noQuoteBalance)

    if (qToken) {
      Doc.show(page.quoteTokenFeeSettings)
      const { symbol, unitInfo: { conventional: { unit } } } = app().assets[qToken.parentID]
      page.quoteTokenParentLogo.src = Doc.logoPath(symbol)
      for (const el of Doc.applySelector(page.quoteTokenFeeSettings, '[data-quote-fee-ticker]')) el.textContent = unit
      const box = this.quoteTokenFeesBox()
      if (cfg.quoteTokenFees) this.setTokenFees(cfg.quoteTokenFees, box)
      else {
        const defaultFees = box.fees.bookingFees + tokenFeeMultiplier * box.fees.reservesPerSwap
        cfg.quoteTokenFees = this.originalConfig.quoteTokenFees = this.setTokenFees(defaultFees, box)
      }
    }
  }

  feeOutlook (): FeeOutlook {
    const { updatedConfig: cfg, marketReport: { baseFees, quoteFees } } = this
    const buyLots = cfg.buyPlacements.reduce((sum: number, p: OrderPlacement) => sum + p.lots, 0)
    const sellLots = cfg.sellPlacements.reduce((sum: number, p: OrderPlacement) => sum + p.lots, 0)
    const baseBookingFees = sellLots * (baseFees.max.swap + baseFees.max.refund) + buyLots * baseFees.max.redeem
    const baseFeeReserves = baseFees.estimated.swap + Math.max(baseFees.estimated.redeem, baseFees.estimated.refund)
    const quoteBookingFees = buyLots * (quoteFees.max.swap + quoteFees.max.refund) + sellLots * quoteFees.max.redeem
    const quoteFeeReserves = quoteFees.estimated.swap + Math.max(quoteFees.estimated.redeem + quoteFees.estimated.refund)
    return {
      base: {
        bookingFees: baseBookingFees,
        reservesPerSwap: baseFeeReserves
      },
      quote: {
        bookingFees: quoteBookingFees,
        reservesPerSwap: quoteFeeReserves
      }
    }
  }

  setupCEXSelectors (viewOnly: boolean) {
    const {
      page, updatedConfig: cfg, originalConfig: oldCfg, specs: { host, baseID, quoteID, botType, cexName },
      botConfigs
    } = this
    Doc.hide(
      page.cexRebalanceSettings, page.cexBaseBalanceBox, page.cexBaseRebalanceOpts, page.noBaseCEXBalance,
      page.cexQuoteBalanceBox, page.cexQuoteRebalanceOpts, page.noQuoteCEXBalance, page.cexBaseBalanceContainer,
      page.cexQuoteBalanceContainer, page.cexBaseRebalanceOpts, page.cexQuoteRebalanceOpts
    )
    if (botType === botTypeBasicMM) return
    const { cexQuoteBalance: { available: qAvail }, cexBaseBalance: { available: bAvail } } = this
    if (!cexName) throw Error(`no cex name for bot type ${botType}`)
    Doc.show(page.cexBaseBalanceBox, page.cexQuoteBalanceBox, page.cexRebalanceSettings)
    Doc.setVis(botType === botTypeArbMM, page.sellPlacementsBox, page.buyPlacementsBox)

    const { name: bName, symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { name: qName, symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const xc = app().exchanges[host]
    const { lotsize: lotSize, spot } = xc.markets[`${baseSymbol}_${quoteSymbol}`]
    this.setCexLogo(document.body, cexName)
    page.baseRebalanceName.textContent = page.dexBaseBalName.textContent = bName
    page.baseWalletSettingsName.textContent = page.cexBaseBalName.textContent = bName
    page.quoteRebalanceName.textContent = page.quoteWalletSettingsName.textContent = qName
    page.dexQuoteBalName.textContent = page.cexQuoteBalName.textContent = qName

    let baseReservedByOtherBots = 0
    let basePercentReservedByOtherBots = 0
    let quoteReservedByOtherBots = 0
    let quotePercentReservedByOtherBots = 0
    const processAssetBalance = (assetID: number, balanceType: BalanceType, balanceFactor: number) => {
      switch (assetID) {
        case baseID: {
          const [pct, bal] = calcBalanceReserve(bAvail, balanceType, balanceFactor)
          baseReservedByOtherBots += bal
          basePercentReservedByOtherBots += pct
          break
        }
        case quoteID: {
          const [pct, bal] = calcBalanceReserve(qAvail, balanceType, balanceFactor)
          quoteReservedByOtherBots += bal
          quotePercentReservedByOtherBots += pct
        }
      }
    }

    for (const botCfg of botConfigs) {
      if (botCfg.baseID === baseID && botCfg.quoteID === quoteID && botCfg.host === host) {
        continue
      }
      if (!botCfg.cexCfg) continue
      processAssetBalance(botCfg.baseID, botCfg.cexCfg.baseBalanceType, botCfg.cexCfg.baseBalance)
      processAssetBalance(botCfg.quoteID, botCfg.cexCfg.quoteBalanceType, botCfg.cexCfg.quoteBalance)
    }

    const baseMaxPercent = basePercentReservedByOtherBots < 100 ? 100 - basePercentReservedByOtherBots : 0
    const quoteMaxPercent = quotePercentReservedByOtherBots < 100 ? 100 - quotePercentReservedByOtherBots : 0
    const bRemain = baseReservedByOtherBots <= bAvail ? bAvail - baseReservedByOtherBots : 0
    const qRemain = quoteReservedByOtherBots <= qAvail ? qAvail - quoteReservedByOtherBots : 0
    const baseMaxAvailable = Doc.conventionalCoinValue(bRemain, bui)
    const quoteMaxAvailable = Doc.conventionalCoinValue(qRemain, qui)

    page.cexBaseAvail.textContent = Doc.formatFourSigFigs(baseMaxAvailable)
    page.cexQuoteAvail.textContent = Doc.formatFourSigFigs(quoteMaxAvailable)

    const baseXYRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: `${baseMaxPercent}%`,
        x: baseMaxPercent,
        y: baseMaxAvailable
      },
      xUnit: '%',
      yUnit: bui.conventional.unit
    }

    const quoteXYRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: `${quoteMaxPercent}%`,
        x: quoteMaxPercent,
        y: quoteMaxAvailable
      },
      xUnit: '%',
      yUnit: qui.conventional.unit
    }

    Doc.empty(page.cexBaseBalanceContainer, page.cexQuoteBalanceContainer, page.cexBaseMinBalanceContainer, page.cexQuoteMinBalanceContainer)

    const [basePct] = calcBalanceReserve(bAvail, cfg.cexBaseBalanceType, cfg.cexBaseBalance)
    this.cexBaseBalanceRange = new XYRangeHandler(baseXYRange, basePct * 100, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'cexBaseBalance',
      changed: () => { this.updateModifiedMarkers() }
    })
    page.cexBaseBalanceContainer.appendChild(this.cexBaseBalanceRange.control)

    const minBaseBalance = Math.max(cfg.cexBaseMinBalance, lotSize) / bRemain * 100
    if (oldCfg.cexBaseMinBalance === 0) oldCfg.cexBaseMinBalance = minBaseBalance
    this.cexBaseMinBalanceRange = new XYRangeHandler(baseXYRange, minBaseBalance, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'cexBaseMinBalance',
      updated: () => { this.updateModifiedMarkers() }
    })
    page.cexBaseMinBalanceContainer.appendChild(this.cexBaseMinBalanceRange.control)

    cfg.cexBaseMinTransfer = Math.round(Math.max(cfg.cexBaseMinTransfer, lotSize))
    if (oldCfg.cexBaseMinTransfer === 0) oldCfg.cexBaseMinTransfer = cfg.cexBaseMinTransfer
    page.cexBaseTransferInput.value = Doc.formatCoinValue(cfg.cexBaseMinTransfer, bui)

    const [quotePct] = calcBalanceReserve(qAvail, cfg.cexQuoteBalanceType, cfg.cexQuoteBalance)
    this.cexQuoteBalanceRange = new XYRangeHandler(quoteXYRange, quotePct * 100, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'cexQuoteBalance',
      changed: () => { this.updateModifiedMarkers() }
    })
    page.cexQuoteBalanceContainer.appendChild(this.cexQuoteBalanceRange.control)

    const quoteLot = calculateQuoteLot(lotSize, baseID, quoteID, spot)
    const minQuoteBalance = Math.max(cfg.cexQuoteMinBalance, quoteLot) / qRemain * 100
    if (oldCfg.cexQuoteMinBalance === 0) oldCfg.cexQuoteMinBalance = minQuoteBalance
    this.cexQuoteMinBalanceRange = new XYRangeHandler(quoteXYRange, minQuoteBalance, {
      disabled: viewOnly, settingsDict: cfg, settingsKey: 'cexQuoteMinBalance',
      changed: () => { this.updateModifiedMarkers() }
    })
    page.cexQuoteMinBalanceContainer.appendChild(this.cexQuoteMinBalanceRange.control)

    cfg.cexQuoteMinTransfer = Math.round(Math.max(cfg.cexQuoteMinTransfer, quoteLot))
    if (oldCfg.cexQuoteMinTransfer === 0) oldCfg.cexQuoteMinTransfer = cfg.cexQuoteMinTransfer
    page.cexQuoteTransferInput.value = Doc.formatCoinValue(cfg.cexQuoteMinTransfer, qui)

    if (bRemain > 0) {
      Doc.show(page.cexBaseBalanceContainer)
      Doc.setVis(page.cexRebalanceCheckbox.checked, page.cexBaseRebalanceOpts)
    } else {
      Doc.show(page.noBaseCEXBalance)
    }

    if (qRemain > 0) {
      Doc.show(page.cexQuoteBalanceContainer)
      Doc.setVis(page.cexRebalanceCheckbox.checked, page.cexQuoteRebalanceOpts)
    } else {
      Doc.show(page.noQuoteCEXBalance)
    }
  }

  setCexLogo (ancestor: PageElement, cexName: string) {
    if (!cexName) return
    const dinfo = CEXDisplayInfos[cexName]
    const cexLogoSrc = '/img/' + dinfo.logo
    for (const el of Doc.applySelector(ancestor, '[data-cex-logo')) el.src = cexLogoSrc
  }

  /*
    setupWalletSetting sets up the base and quote wallet setting sections.
    These are based on the multi funding settings in the wallet definition.
  */
  setupWalletSettings (viewOnly: boolean) {
    const { page, updatedConfig: { quoteOptions, baseOptions } } = this
    const baseWalletSettings = app().currentWalletDefinition(this.specs.baseID)
    const quoteWalletSettings = app().currentWalletDefinition(this.specs.quoteID)
    Doc.setVis(baseWalletSettings.multifundingopts, page.baseWalletSettings)
    Doc.setVis(quoteWalletSettings.multifundingopts, page.quoteWalletSettings)
    Doc.empty(page.quoteWalletSettingsContainer, page.baseWalletSettingsContainer)
    const baseOptToSetting : Record<string, PageElement> = {}
    const quoteOptToSetting : Record<string, PageElement> = {}
    const baseDependentOpts : Record<string, string[]> = {}
    const quoteDependentOpts : Record<string, string[]> = {}
    const addDependentOpt = (optKey: string, optSetting: PageElement, dependentOn: string, quote: boolean) => {
      let dependentOpts : Record<string, string[]>
      let optToSetting : Record<string, PageElement>
      if (quote) {
        dependentOpts = quoteDependentOpts
        optToSetting = quoteOptToSetting
      } else {
        dependentOpts = baseDependentOpts
        optToSetting = baseOptToSetting
      }
      if (!dependentOpts[dependentOn]) dependentOpts[dependentOn] = []
      dependentOpts[dependentOn].push(optKey)
      optToSetting[optKey] = optSetting
    }
    const setDependentOptsVis = (parentOptKey: string, vis: boolean, quote: boolean) => {
      let dependentOpts : Record<string, string[]>
      let optToSetting : Record<string, PageElement>
      if (quote) {
        dependentOpts = quoteDependentOpts
        optToSetting = quoteOptToSetting
      } else {
        dependentOpts = baseDependentOpts
        optToSetting = baseOptToSetting
      }
      const optKeys = dependentOpts[parentOptKey]
      if (!optKeys) return
      for (const optKey of optKeys) {
        Doc.setVis(vis, optToSetting[optKey])
      }
    }
    const storeWalletSettingControl = (optKey: string, toHighlight: PageElement, setValue: (x:string) => void, quote: boolean) => {
      if (quote) {
        this.quoteWalletSettingControl[optKey] = {
          toHighlight,
          setValue
        }
      } else {
        this.baseWalletSettingControl[optKey] = {
          toHighlight,
          setValue
        }
      }
    }
    const setWalletOption = (quote: boolean, key: string, value: string) => {
      if (quote) quoteOptions[key] = value
      else baseOptions[key] = value
    }
    const getWalletOption = (quote: boolean, key: string) : string | undefined => {
      if (quote) return quoteOptions[key]
      else return baseOptions[key]
    }
    const addOpt = (opt: OrderOption, quote: boolean) => {
      let container
      let optionsDict
      if (quote) {
        container = page.quoteWalletSettingsContainer
        optionsDict = quoteOptions
      } else {
        if (opt.quoteAssetOnly) return
        container = page.baseWalletSettingsContainer
        optionsDict = baseOptions
      }
      const currVal = optionsDict[opt.key]
      let setting : PageElement | undefined
      if (opt.isboolean) {
        setting = page.boolSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(setting)
        tmpl.name.textContent = opt.displayname
        tmpl.input.checked = currVal === 'true'
        if (viewOnly) tmpl.input.setAttribute('disabled', 'true')
        Doc.bind(tmpl.input, 'change', () => {
          setWalletOption(quote, opt.key, tmpl.input.checked ? 'true' : 'false')
          setDependentOptsVis(opt.key, !!tmpl.input.checked, quote)
          this.updateModifiedMarkers()
        })
        const setValue = (x: string) => {
          tmpl.input.checked = x === 'true'
          setDependentOptsVis(opt.key, !!tmpl.input.checked, quote)
        }
        storeWalletSettingControl(opt.key, tmpl.input, setValue, quote)
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        container.appendChild(setting)
      } else if (opt.xyRange) {
        setting = page.rangeSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(setting)
        tmpl.name.textContent = opt.displayname
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        const handler = new XYRangeHandler(opt.xyRange, parseInt(currVal), {
          roundX: opt.xyRange.roundX, roundY: opt.xyRange.roundY, disabled: viewOnly,
          settingsDict: optionsDict, settingsKey: opt.key, dictValueAsString: true,
          changed: () => { this.updateModifiedMarkers() }
        })
        const setValue = (x: string) => { handler.setValue(parseInt(x)) }
        storeWalletSettingControl(opt.key, tmpl.sliderContainer, setValue, quote)
        tmpl.sliderContainer.appendChild(handler.control)
        container.appendChild(setting)
      }
      if (!setting) return
      if (opt.dependsOn) {
        addDependentOpt(opt.key, setting, opt.dependsOn, quote)
        const parentOptVal = getWalletOption(quote, opt.dependsOn)
        Doc.setVis(parentOptVal === 'true', setting)
      }
    }

    if (baseWalletSettings.multifundingopts && baseWalletSettings.multifundingopts.length > 0) {
      for (const opt of baseWalletSettings.multifundingopts) addOpt(opt, false)
    }
    if (quoteWalletSettings.multifundingopts && quoteWalletSettings.multifundingopts.length > 0) {
      for (const opt of quoteWalletSettings.multifundingopts) addOpt(opt, true)
    }
    app().bindTooltips(page.baseWalletSettingsContainer)
    app().bindTooltips(page.quoteWalletSettingsContainer)
  }

  /*
   * fetchOracles fetches the current oracle rates and fiat rates, and displays
   * them on the screen.
   */
  async fetchOracles (): Promise<void> {
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
  async handleCEXSubmit () {
    const { page, formSpecs: { host, baseID, quoteID } } = this
    Doc.hide(page.cexFormErr)
    const cexName = page.cexConfigForm.dataset.cexName || ''
    const apiKey = page.cexApiKeyInput.value
    const apiSecret = page.cexSecretInput.value
    if (!apiKey || !apiSecret) {
      Doc.show(page.cexFormErr)
      page.cexFormErr.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      return
    }
    const loaded = app().loading(page.cexConfigForm)
    try {
      const res = await MM.updateCEXConfig({
        name: cexName,
        apiKey: apiKey,
        apiSecret: apiSecret
      })
      if (!app().checkResponse(res)) throw res
    } catch (e) {
      Doc.show(page.cexFormErr)
      page.cexFormErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: e.msg ?? String(e) })
      return
    } finally {
      loaded()
      await this.refreshStatus()
    }

    const dinfo = CEXDisplayInfos[cexName]
    for (const { baseID, quoteID, tmpl, arbs } of this.marketRows) {
      if (arbs.indexOf(cexName) !== -1) continue
      const cexHasMarket = this.cexMarketSupportFilter(baseID, quoteID)
      if (cexHasMarket(cexName)) {
        const img = page.arbBttnTmpl.cloneNode(true) as PageElement
        img.src = '/img/' + dinfo.logo
        tmpl.arbs.appendChild(img)
        arbs.push(cexName)
      }
    }
    this.setCEXAvailability(baseID, quoteID, cexName)
    this.showBotTypeForm(host, baseID, quoteID, botTypeArbMM, cexName)
    // this.forms.close()
    // this.selectCEX(cexName)
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
      const cexStatus = this.mmStatus.cexes[name]
      Doc.hide(tmpl.unavailable, tmpl.needsconfig, tmpl.disconnected)
      Doc.setVis(Boolean(cexStatus), tmpl.reconfig)
      tmpl.logo.classList.remove('off')
      div.classList.toggle('configured', Boolean(cexStatus) && !cexStatus.connectErr)
      if (!cexStatus) {
        Doc.show(tmpl.needsconfig)
      } else if (cexStatus.connectErr) {
        Doc.show(tmpl.disconnected)
      } else if (!has) {
        Doc.show(tmpl.unavailable)
        tmpl.logo.classList.add('off')
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
    tmpl.logo.src = '/img/' + dinfo.logo
    this.page.cexSelection.appendChild(div)
    this.formCexes[cexName] = { name: cexName, div, tmpl }
    Doc.bind(div, 'click', () => {
      const cexStatus = this.mmStatus.cexes[cexName]
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
    this.setCexLogo(page.cexConfigForm, cexName)
    Doc.hide(page.cexConfigPrompt, page.cexConnectErrBox, page.cexFormErr)
    const dinfo = CEXDisplayInfos[cexName]
    page.cexConfigName.textContent = dinfo.name
    page.cexConfigForm.dataset.cexName = cexName
    page.cexApiKeyInput.value = ''
    page.cexSecretInput.value = ''
    const cexStatus = this.mmStatus.cexes[cexName]
    const connectErr = cexStatus?.connectErr
    if (connectErr) {
      Doc.show(page.cexConnectErrBox)
      page.cexConnectErr.textContent = connectErr
      page.cexApiKeyInput.value = cexStatus.config.apiKey
      page.cexSecretInput.value = cexStatus.config.apiSecret
    } else {
      Doc.show(page.cexConfigPrompt)
    }
    this.forms.show(page.cexConfigForm)
  }

  /*
   * cexMarketSupportFilter returns a lookup CEXes that have a matching market
   * for the currently selected base and quote assets.
   */
  cexMarketSupportFilter (baseID: number, quoteID: number) {
    const cexes: Record<string, boolean> = {}
    for (const [cexName, cexStatus] of Object.entries(this.mmStatus.cexes)) {
      for (const { baseID: b, quoteID: q } of (cexStatus.markets ?? [])) {
        if (b === baseID && q === quoteID) {
          cexes[cexName] = true
          break
        }
      }
    }
    return (cexName: string) => Boolean(cexes[cexName])
  }
}

function calcBalanceReserve (totalBalance: number, balanceType: BalanceType, balanceFactor: number): [number, number] {
  switch (balanceType) {
    case BalanceType.Amount:
      return [balanceFactor / totalBalance, balanceFactor]
    case BalanceType.Percentage:
      return [balanceFactor / 100, Math.round(balanceFactor / 100 * totalBalance)]
    default: // Throw error
  }
  throw Error(`unknown balance type ${balanceType}`)
}

function calculateQuoteLot (lotSize: number, baseID:number, quoteID: number, spot?: Spot) {
  const baseRate = app().fiatRatesMap[baseID]
  const quoteRate = app().fiatRatesMap[quoteID]
  const { unitInfo: { conventional: { conversionFactor: bFactor } } } = app().assets[baseID]
  const { unitInfo: { conventional: { conversionFactor: qFactor } } } = app().assets[quoteID]
  if (baseRate && quoteRate) {
    return lotSize * baseRate / quoteRate * qFactor / bFactor
  } else if (spot) {
    return lotSize * spot.rate / OrderUtil.RateEncodingFactor
  }
  return qFactor
}

function isViewOnly (specs: BotSpecs, mmStatus: MarketMakingStatus) : boolean {
  return (Boolean(botConfig(specs, mmStatus)) && mmStatus.running) || (specs.startTime !== undefined && specs.startTime > 0)
}

async function botConfig (specs: BotSpecs, mmStatus: MarketMakingStatus) : Promise<BotConfig | null> {
  const { baseID, quoteID, host, startTime } = specs
  if (startTime) return (await MM.mmRunOverview(host, baseID, quoteID, startTime)).cfg

  const cfgs = (mmStatus.bots || []).map((s: MMBotStatus) => s.config).filter((cfg: BotConfig) => {
    return cfg.baseID === baseID && cfg.quoteID === quoteID && cfg.host === host
  })
  if (cfgs.length) return cfgs[0]
  return null
}

const ExchangeNames: Record<string, string> = {
  'binance.com': 'Binance',
  'coinbase.com': 'Coinbase',
  'bittrex.com': 'Bittrex',
  'hitbtc.com': 'HitBTC',
  'exmo.com': 'EXMO'
}

class PlacementsChart extends Chart {
  settingsPage: MarketMakerSettingsPage
  loadedCEX: string
  cexLogo: HTMLImageElement

  constructor (parent: PageElement, settingsPage: MarketMakerSettingsPage) {
    super(parent, {
      resize: () => this.resized(),
      click: (/* e: MouseEvent */) => { /* pass */ },
      zoom: (/* bigger: boolean */) => { /* pass */ }
    })

    this.settingsPage = settingsPage
  }

  resized () {
    this.render()
  }

  draw () { /* pass */ }

  setMarket () {
    const { loadedCEX, settingsPage: { specs: { cexName } } } = this
    if (!cexName || cexName === loadedCEX) return
    this.loadedCEX = cexName
    this.cexLogo = new Image()
    Doc.bind(this.cexLogo, 'load', () => { this.render() })
    this.cexLogo.src = '/img/' + CEXDisplayInfos[cexName || ''].logo
  }

  render () {
    const { ctx, canvas, theme, settingsPage } = this
    if (canvas.width === 0) return
    const { page, cfg: { buyPlacements, sellPlacements, profit: profitPercent }, baseFiatRate, botType } = settingsPage.marketStuff()
    if (botType === botTypeBasicArb) return

    this.clear()

    const drawDashedLine = (x0: number, y0: number, x1: number, y1: number, color: string) => {
      ctx.save()
      ctx.setLineDash([3, 5])
      ctx.lineWidth = 1.5
      ctx.strokeStyle = color
      this.line(x0, y0, x1, y1)
      ctx.restore()
    }

    const isBasicMM = botType === botTypeBasicMM
    const cx = canvas.width / 2
    const [cexGapL, cexGapR] = isBasicMM ? [cx, cx] : [0.48 * canvas.width, 0.52 * canvas.width]
    const profit = profitPercent / 100

    const buyLots = buyPlacements.reduce((v: number, p: OrderPlacement) => v + p.lots, 0)
    const sellLots = sellPlacements.reduce((v: number, p: OrderPlacement) => v + p.lots, 0)
    const maxLots = Math.max(buyLots, sellLots)

    let widest = 0
    let fauxSpacer = 0
    if (isBasicMM) {
      const leftmost = buyPlacements.reduce((v: number, p: OrderPlacement) => Math.max(v, p.gapFactor), 0)
      const rightmost = sellPlacements.reduce((v: number, p: OrderPlacement) => Math.max(v, p.gapFactor), 0)
      widest = Math.max(leftmost, rightmost)
    } else {
      // For arb-mm, we don't know how the orders will be spaced because it
      // depends on the vwap. But we're just trying to capture the general sense
      // of how the parameters will affect order placement, so we'll fake it.
      // Higher match buffer values will lead to orders with less favorable
      // rates, e.g. the spacing will be larger.
      fauxSpacer = 0.01 * (1 + (parseFloat(page.qcMatchBuffer.value || '0') / 100))
      widest = Math.min(10, Math.max(buyPlacements.length, sellPlacements.length)) * fauxSpacer // arb-mm
    }

    // Make the range 15% on each side, which will include profit + placements,
    // unless they have orders with larger gap factors.
    const minRange = profit + widest
    const defaultRange = 0.155
    const range = Math.max(minRange * 1.05, defaultRange)

    // Increase data height logarithmically up to 1,000,000 USD.
    const maxCommitUSD = maxLots * baseFiatRate
    const regionHeight = 0.2 + 0.7 * Math.log(clamp(maxCommitUSD, 0, 1e6)) / Math.log(1e6)

    // Draw a region in the middle representing the cex gap.
    const plotRegion = new Region(ctx, new Extents(0, canvas.width, 0, canvas.height))

    if (isBasicMM) {
      drawDashedLine(cx, 0, cx, canvas.height, theme.gapLine)
    } else { // arb-mm
      plotRegion.plot(new Extents(0, 1, 0, 1), (ctx: CanvasRenderingContext2D, tools: Translator) => {
        const [y0, y1] = [tools.y(0), tools.y(1)]
        drawDashedLine(cexGapL, y0, cexGapL, y1, theme.gapLine)
        drawDashedLine(cexGapR, y0, cexGapR, y1, theme.gapLine)
        const y = tools.y(0.95)
        ctx.drawImage(this.cexLogo, cx - 8, y, 16, 16)
        this.applyLabelStyle(18)
        ctx.fillText('δ', cx, y + 29)
      })
    }

    const plotSide = (isBuy: boolean, placements: OrderPlacement[]) => {
      if (!placements?.length) return
      const [xMin, xMax] = isBuy ? [0, cexGapL] : [cexGapR, canvas.width]
      const reg = new Region(ctx, new Extents(xMin, xMax, canvas.height * (1 - regionHeight), canvas.height))
      const [l, r] = isBuy ? [-range, 0] : [0, range]
      reg.plot(new Extents(l, r, 0, maxLots), (ctx: CanvasRenderingContext2D, tools: Translator) => {
        ctx.lineWidth = 2.5
        ctx.strokeStyle = isBuy ? theme.buyLine : theme.sellLine
        ctx.fillStyle = isBuy ? theme.buyFill : theme.sellFill
        ctx.beginPath()
        const sideFactor = isBuy ? -1 : 1
        const firstPt = placements[0]
        const y0 = tools.y(0)
        const firstX = tools.x((isBasicMM ? firstPt.gapFactor : profit) * sideFactor)
        ctx.moveTo(firstX, y0)
        let cumulativeLots = 0
        for (let i = 0; i < placements.length; i++) {
          const p = placements[i]
          // For arb-mm, we don't know exactly
          const rawX = isBasicMM ? p.gapFactor : profit + i * fauxSpacer
          const x = tools.x(rawX * sideFactor)
          ctx.lineTo(x, tools.y(cumulativeLots))
          cumulativeLots += p.lots
          ctx.lineTo(x, tools.y(cumulativeLots))
        }
        const xInfinity = isBuy ? canvas.width * -0.1 : canvas.width * 1.1
        ctx.lineTo(xInfinity, tools.y(cumulativeLots))
        ctx.stroke()
        ctx.lineTo(xInfinity, y0)
        ctx.lineTo(firstX, y0)
        ctx.closePath()
        ctx.globalAlpha = 0.25
        ctx.fill()
      }, true)
    }

    plotSide(false, sellPlacements)
    plotSide(true, buyPlacements)
  }
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
