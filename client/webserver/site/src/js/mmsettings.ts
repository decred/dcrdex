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
  MMCEXStatus
} from './registry'
import Doc from './doc'
import State from './state'
import BasePage from './basepage'
import { setOptionTemplates, XYRangeHandler } from './opts'
import { MM, CEXDisplayInfos, botTypeBasicArb, botTypeArbMM, botTypeBasicMM } from './mm'
import { Forms, bind as bindForm, NewWalletForm } from './forms'
import * as intl from './locales'
import * as OrderUtil from './orderutil'

const GapStrategyMultiplier = 'multiplier'
const GapStrategyAbsolute = 'absolute'
const GapStrategyAbsolutePlus = 'absolute-plus'
const GapStrategyPercent = 'percent'
const GapStrategyPercentPlus = 'percent-plus'
const arbMMRowCacheKey = 'arbmm'

const specLK = 'lastMMSpecs'
const lastBotsLK = 'lastBots'
const lastArbExchangeLK = 'lastArbExchange'

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

/*
 * RangeOption adds some functionality to XYRangeHandler. It will keep the
 * ConfigState up to date, recognizes a modified state, and adds default
 * callbacks for the XYRangeHandler. RangeOption is similar in function to an
 * XYRangeOption. The two types may be able to be merged.
 */
class RangeOption {
  // Args
  cfg: XYRange
  initVal: number
  lastVal: number
  settingsDict: {[key: string]: any}
  settingsKey: string
  // Set in constructor
  div: PageElement
  xyRange: XYRangeHandler
  // Set with setters
  update: (x: number, y: number) => any
  changed: () => void
  selected: () => void
  convert: (x: any) => any

  constructor (cfg: XYRange, initVal: number, roundX: boolean, roundY: boolean, disabled: boolean, settingsDict: {[key:string]: any}, settingsKey: string) {
    this.cfg = cfg
    this.initVal = initVal
    this.lastVal = initVal
    this.settingsDict = settingsDict
    this.settingsKey = settingsKey
    this.convert = (x: any) => x

    this.xyRange = new XYRangeHandler(
      cfg,
      initVal,
      (x: number, y: number) => { this.handleUpdate(x, y) },
      () => { this.handleChange() },
      () => { this.handleSelected() },
      roundX,
      roundY,
      disabled
    )
    this.div = this.xyRange.control
  }

  setUpdate (f: (x: number, y: number) => number) {
    this.update = f
  }

  setChanged (f: () => void) {
    this.changed = f
  }

  setSelected (f: () => void) {
    this.selected = f
  }

  stringify () {
    this.convert = (x: any) => String(x)
  }

  handleUpdate (x:number, y:number) {
    this.lastVal = this.update ? this.update(x, y) : x
    this.settingsDict[this.settingsKey] = this.convert(this.lastVal)
  }

  handleChange () {
    if (this.changed) this.changed()
  }

  handleSelected () {
    if (this.selected) this.selected()
  }

  modified (): boolean {
    return this.lastVal !== this.initVal
  }

  setValue (x: number) {
    this.xyRange.setValue(x, false)
  }

  reset () {
    this.xyRange.setValue(this.initVal, true)
    this.lastVal = this.initVal
  }
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

export default class MarketMakerSettingsPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  newWalletForm: NewWalletForm
  currentMarket: string
  originalConfig: ConfigState
  updatedConfig: ConfigState
  creatingNewBot: boolean
  marketReport: MarketReport
  mmStatus: MarketMakingStatus
  cexConfigs: CEXConfig[]
  botConfigs: BotConfig[]
  oracleBias: RangeOption
  oracleWeighting: RangeOption
  driftTolerance: RangeOption
  orderPersistence: RangeOption
  baseBalance?: RangeOption
  quoteBalance?: RangeOption
  cexBaseBalanceRange: RangeOption
  cexBaseMinBalanceRange: RangeOption
  cexBaseBalance: ExchangeBalance
  cexQuoteBalanceRange: RangeOption
  cexQuoteMinBalanceRange: RangeOption
  cexQuoteBalance: ExchangeBalance
  baseWalletSettingControl: Record<string, walletSettingControl> = {}
  quoteWalletSettingControl: Record<string, walletSettingControl> = {}
  specs: BotSpecs
  formSpecs: BotSpecs
  formCexes: Record<string, cexButton>
  placementsCache: Record<string, [OrderPlacement[], OrderPlacement[]]>
  botTypeSelectors: PageElement[]
  marketRows: MarketRow[]

  constructor (main: HTMLElement, specs: BotSpecs) {
    super()

    this.placementsCache = {}

    const page = this.page = Doc.idDescendants(main)

    this.forms = new Forms(page.forms)

    app().headerSpace.appendChild(page.mmTitle)

    setOptionTemplates(page)
    Doc.cleanTemplates(
      page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl, page.placementRowTmpl,
      page.oracleTmpl, page.boolSettingTmpl, page.rangeSettingTmpl, page.cexOptTmpl,
      page.arbBttnTmpl, page.marketRowTmpl
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
      () => { this.submitBotType() }
    )

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
    for (const { host, markets, assets } of Object.values(app().exchanges)) {
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
    const { host, baseID, quoteID, botType, cexName } = this.specs

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

    const { page } = this

    Doc.hide(page.updateButton, page.resetButton, page.createButton, page.noMarket, page.cexNameDisplayBox)

    page.baseHeader.textContent = app().assets[baseID].symbol.toUpperCase()
    page.quoteHeader.textContent = app().assets[quoteID].symbol.toUpperCase()
    page.hostHeader.textContent = host
    page.baseBalanceLogo.src = Doc.logoPathFromID(baseID)
    page.quoteBalanceLogo.src = Doc.logoPathFromID(quoteID)
    page.baseSettingsLogo.src = Doc.logoPathFromID(baseID)
    page.quoteSettingsLogo.src = Doc.logoPathFromID(quoteID)
    page.baseLogo.src = Doc.logoPathFromID(baseID)
    page.quoteLogo.src = Doc.logoPathFromID(quoteID)

    if (botCfg) {
      const { basicMarketMakingConfig: mmCfg, arbMarketMakingConfig: arbMMCfg, simpleArbConfig: arbCfg, cexCfg } = botCfg
      this.creatingNewBot = false
      // This is kinda sloppy, but we'll copy any relevant issues from the
      // old config into the originalConfig.
      const idx = oldCfg as {[k: string]: any} // typescript
      for (const [k, v] of Object.entries(botCfg)) if (idx[k] !== undefined) idx[k] = v
      let rebalanceCfg
      if (mmCfg) {
        oldCfg.buyPlacements = mmCfg.buyPlacements
        oldCfg.sellPlacements = mmCfg.sellPlacements
      } else if (arbMMCfg) {
        const { buyPlacements, sellPlacements } = arbMMCfg
        oldCfg.buyPlacements = Array.from(buyPlacements, (p: ArbMarketMakingPlacement) => { return { lots: p.lots, gapFactor: p.multiplier } })
        oldCfg.sellPlacements = Array.from(sellPlacements, (p: ArbMarketMakingPlacement) => { return { lots: p.lots, gapFactor: p.multiplier } })
        oldCfg.profit = arbMMCfg.profit
      } else if (arbCfg) {
        // DRAFT TODO
        // maxActiveArbs
        oldCfg.profit = arbCfg.profitTrigger
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

    this.setupMMConfigSettings(viewOnly)
    this.setupBalanceSelectors(viewOnly)
    this.setupWalletSettings(viewOnly)
    this.setupCEXSelectors(viewOnly)
    this.setOriginalValues(viewOnly)
    Doc.show(page.botSettingsContainer, page.marketHeader)
    await this.fetchOracles()

    // If this is a new bot, show the quick config form
    if (!botCfg && this.marketReport.baseFiatRate > 0 && this.marketReport.quoteFiatRate > 0) this.showQuickConfig()
    else this.showAdvancedConfig()
  }

  showQuickConfig () {
    const {
      page, updatedConfig: cfg, specs: { host, quoteID, baseID },
      marketReport: { baseFiatRate, quoteFiatRate },
    } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const { markets } = app().exchanges[host]
    const { lotsize: lotSize } = markets[`${baseSymbol}_${quoteSymbol}`]

    page.qcLotSizeDisplay.textContent = Doc.formatCoinValue(lotSize, bui)
    page.qcLotSizeUnit.textContent = bui.conventional.unit
    const lotSizeUSD = lotSize / bui.conventional.conversionFactor * baseFiatRate
    page.qcLotSizeUSDDisplay.textContent = Doc.formatFourSigFigs(lotSizeUSD)

    page.levelsPerSide.value = '1'

    const commitIncrement = Math.round(Math.max(1, 100 / lotSizeUSD))
    page.lotsPerLevel.value = String(commitIncrement)
    
    this.quickConfigUpdated()

    Doc.hide(this.page.advancedConfig)
    Doc.show(this.page.quickConfig)
  }

  quickConfigUpdated () {
    const { page, specs: { host, baseID, quoteID }, marketReport: { baseFiatRate }, } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const { markets } = app().exchanges[host]
    const { lotsize: lotSize } = markets[`${baseSymbol}_${quoteSymbol}`]

    const levelsPerSide = parseInt(page.levelsPerSide.value ?? '')
    if (isNaN(levelsPerSide)) {
      page.qcError.textContent = 'invalid value for levels per side'
    }
    const lotsPerLevel = parseInt(page.lotsPerLevel.value ?? '')
    if (isNaN(lotsPerLevel)) {
      page.qcError.textContent = 'invalid value for lots per level'
    }

    const lotSizeUSD = lotSize / bui.conventional.conversionFactor * baseFiatRate

    const taper = page.qcTaper.checked
    const levels: OrderPlacement[] = []
    if (taper) {

    } else {

    }



  }

  showAdvancedConfig () {
    Doc.show(this.page.advancedConfig)
    Doc.hide(this.page.quickConfig)
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
    Doc.empty(page.botTypeBaseSymbol, page.botTypeQuoteSymbol)
    const [b, q] = [app().assets[baseID], app().assets[quoteID]]
    page.botTypeBaseLogo.src = Doc.logoPath(b.symbol)
    page.botTypeQuoteLogo.src = Doc.logoPath(q.symbol)
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

  autoRebalanceChanged () {
    const { page, updatedConfig: cfg } = this
    cfg.cexRebalance = page.cexRebalanceCheckbox?.checked ?? false
    Doc.setVis(cfg.cexRebalance && Doc.isHidden(page.noBaseCEXBalance), page.cexBaseRebalanceOpts)
    Doc.setVis(cfg.cexRebalance && Doc.isHidden(page.noQuoteCEXBalance), page.cexQuoteRebalanceOpts)
  }

  quoteTransferChanged () {
    const { page, updatedConfig: cfg, specs: { quoteID } } = this
    const { unitInfo: { conventional: { conversionFactor } } } = app().assets[quoteID]
    const v = parseFloat(page.cexQuoteTransferInput.value || '')
    if (isNaN(v)) return
    cfg.cexQuoteMinTransfer = Math.round(v * conversionFactor)
    page.cexQuoteTransferInput.value = Doc.formatFourSigFigs(v)
    this.updateModifiedMarkers()
  }

  baseTransferChanged () {
    const { page, updatedConfig: cfg, specs: { baseID } } = this
    const { unitInfo: { conventional: { conversionFactor } } } = app().assets[baseID]
    const v = parseFloat(page.cexBaseTransferInput.value || '')
    if (isNaN(v)) return
    cfg.cexBaseMinTransfer = Math.round(v * conversionFactor)
    page.cexBaseTransferInput.value = Doc.formatFourSigFigs(v)
    this.updateModifiedMarkers()
  }

  incrementTransfer (isBase: boolean, up: boolean) {
    const { page, updatedConfig: cfg, specs: { host, quoteID, baseID } } = this
    const { symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    const { symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    const { markets } = app().exchanges[host]
    const { lotsize: baseLotSize, spot } = markets[`${baseSymbol}_${quoteSymbol}`]
    const lotSize = isBase ? baseLotSize : calculateQuoteLot(baseLotSize, baseID, quoteID, spot)
    const { conventional: { conversionFactor } } = isBase ? bui : qui
    const input = isBase ? page.cexBaseTransferInput : page.cexQuoteTransferInput
    const v = parseFloat(input.value || '') * conversionFactor || lotSize
    const minIncrement = Math.max(lotSize, Math.round(v * 0.01)) // Minimum increment is 1%
    const newV = Math.round(up ? v + minIncrement : Math.max(lotSize, v - minIncrement))
    input.value = Doc.formatFourSigFigs(newV / conversionFactor)
    if (isBase) cfg.cexBaseMinTransfer = newV
    else cfg.cexQuoteMinTransfer = newV
  }

  async submitBotType () {
    // check for wallets
    const { baseID, quoteID } = this.formSpecs

    if (!app().walletMap[baseID]) {
      this.newWalletForm.setAsset(baseID)
      this.forms.show(this.page.newWalletForm)
      return
    }
    if (!app().walletMap[quoteID]) {
      this.newWalletForm.setAsset(quoteID)
      this.forms.show(this.page.newWalletForm)
      return
    }

    const { page, botTypeSelectors } = this
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

    this.forms.close()
    this.configureUI()
  }

  async fetchCEXBalances (specs: BotSpecs) {
    const { page } = this
    const { baseID, quoteID, cexName, botType } = specs
    if (botType === botTypeBasicMM || !cexName) return
    try {
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
    page.baseBalanceContainer.classList.toggle('modified', this.baseBalance && this.baseBalance.modified())
    page.quoteBalanceContainer.classList.toggle('modified', this.quoteBalance && this.quoteBalance.modified())
    page.orderPersistenceContainer.classList.toggle('modified', this.orderPersistence.modified())

    if (botType !== botTypeBasicMM) {
      page.cexBaseBalanceContainer.classList.toggle('modified', this.cexBaseBalanceRange.modified())
      page.cexBaseMinBalanceContainer.classList.toggle('modified', this.cexBaseMinBalanceRange.modified())
      page.cexQuoteBalanceContainer.classList.toggle('modified', this.cexQuoteBalanceRange.modified())
      page.cexQuoteMinBalanceContainer.classList.toggle('modified', this.cexQuoteMinBalanceRange.modified())
      const { unitInfo: bui } = app().assets[baseID]
      const { unitInfo: qui } = app().assets[quoteID]
      page.cexBaseTransferInput.classList.toggle('modified', page.cexBaseTransferInput.value !== Doc.formatFourSigFigs(oldCfg.cexBaseMinTransfer / bui.conventional.conversionFactor))
      page.cexQuoteTransferInput.classList.toggle('modified', page.cexQuoteTransferInput.value !== Doc.formatFourSigFigs(oldCfg.cexQuoteMinTransfer / qui.conventional.conversionFactor))
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
    this.page.buyGapFactorHdr.textContent = intl.prep(intl.ID_MULTIPLIER)
    this.page.sellGapFactorHdr.textContent = intl.prep(intl.ID_MULTIPLIER)
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
    })
    Doc.bind(page.addSellPlacementBtn, 'click', () => {
      this.addPlacement(false, null, false)
      page.addSellPlacementLots.value = ''
      page.addSellPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
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

    Doc.empty(page.driftToleranceContainer, page.orderPersistenceContainer, page.oracleBiasContainer, page.oracleWeightingContainer)

    // Drift tolerance
    this.driftTolerance = new RangeOption(driftToleranceRange, cfg.driftTolerance, false, false, viewOnly, cfg, 'driftTolerance')
    this.driftTolerance.setChanged(handleChanged)
    page.driftToleranceContainer.appendChild(this.driftTolerance.div)

    // CEX order persistence
    this.orderPersistence = new RangeOption(orderPersistenceRange, cfg.orderPersistence, true, true, viewOnly, cfg, 'orderPersistence')
    this.orderPersistence.setChanged(handleChanged)
    this.orderPersistence.setUpdate((x: number) => {
      this.orderPersistence.xyRange.setXLabel('')
      x = Math.round(x)
      this.orderPersistence.xyRange.setYLabel(x === 21 ? '∞' : String(x))
      return x
    })
    page.orderPersistenceContainer.appendChild(this.orderPersistence.div)

    // Use oracle
    Doc.bind(page.useOracleCheckbox, 'change', () => {
      this.useOraclesChanged()
      this.updateModifiedMarkers()
    })
    if (viewOnly) {
      page.useOracleCheckbox.setAttribute('disabled', 'true')
    }

    // Oracle Bias
    this.oracleBias = new RangeOption(oracleBiasRange, cfg.oracleBias, false, false, viewOnly, cfg, 'oracleBias')
    this.oracleBias.setChanged(handleChanged)
    page.oracleBiasContainer.appendChild(this.oracleBias.div)

    // Oracle Weighting
    this.oracleWeighting = new RangeOption(oracleWeightRange, cfg.oracleWeighting, false, false, viewOnly, cfg, 'oracleWeighting')
    this.oracleWeighting.setChanged(handleChanged)
    page.oracleWeightingContainer.appendChild(this.oracleWeighting.div)

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

    Object.assign(cfg, oldCfg)

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

    if (cexName) {
      cexBaseBalanceRange.reset()
      cexBaseMinBalanceRange.reset()
      cexQuoteBalanceRange.reset()
      cexQuoteMinBalanceRange.reset()
      const { unitInfo: bui } = app().assets[baseID]
      const { unitInfo: qui } = app().assets[quoteID]
      page.cexBaseTransferInput.value = Doc.formatFourSigFigs(cfg.cexBaseMinTransfer / bui.conventional.conversionFactor)
      page.cexQuoteTransferInput.value = Doc.formatFourSigFigs(cfg.cexQuoteMinTransfer / qui.conventional.conversionFactor)
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
      minBaseAmt: Math.round(cexBaseMinBalanceRange.xyRange.y * bui.conventional.conversionFactor),
      minBaseTransfer: cfg.cexBaseMinTransfer,
      minQuoteAmt: Math.round(cexQuoteMinBalanceRange.xyRange.y * qui.conventional.conversionFactor),
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
    const { wallet: { balance: { available: bAvail } }, unitInfo: bui } = app().assets[baseID]
    const { wallet: { balance: { available: qAvail } }, unitInfo: qui } = app().assets[quoteID]

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
    const baseMaxAvailable = Doc.conventionalCoinValue(bAvail * baseMaxPercent / 100, bui)
    const quoteMaxAvailable = Doc.conventionalCoinValue(qAvail * quoteMaxPercent / 100, qui)

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

    Doc.hide(page.noBaseBalance, page.noQuoteBalance, page.baseBalanceContainer, page.quoteBalanceContainer)
    Doc.empty(page.baseBalanceContainer, page.quoteBalanceContainer)

    if (baseMaxAvailable > 0) {
      this.baseBalance = new RangeOption(baseXYRange, cfg.baseBalance, false, true, viewOnly, cfg, 'baseBalance')
      this.baseBalance.setChanged(() => { this.updateModifiedMarkers() })
      page.baseBalanceContainer.appendChild(this.baseBalance.div)
      Doc.show(page.baseBalanceContainer)
    } else {
      this.baseBalance = undefined
      Doc.show(page.noBaseBalance)
    }

    if (quoteMaxAvailable > 0) {
      this.quoteBalance = new RangeOption(quoteXYRange, cfg.quoteBalance, false, true, viewOnly, cfg, 'quoteBalance')
      this.quoteBalance.setChanged(() => { this.updateModifiedMarkers() })
      page.quoteBalanceContainer.appendChild(this.quoteBalance.div)
      Doc.show(page.quoteBalanceContainer)
    } else {
      this.quoteBalance = undefined
      Doc.show(page.noQuoteBalance)
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
    const dinfo = CEXDisplayInfos[cexName]
    page.cexBRebalCEXLogo.src = page.cexQRebalCEXLogo.src = page.cexQuoteCEXLogo.src = page.cexBaseCEXLogo.src = '/img/' + dinfo.logo
    page.cexBRebalAssetLogo.src = page.cexBaseAssetLogo.src = Doc.logoPath(baseSymbol)
    page.cexQRebalAssetLogo.src = page.cexQuoteAssetLogo.src = Doc.logoPath(quoteSymbol)
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
    this.cexBaseBalanceRange = new RangeOption(baseXYRange, basePct * 100, false, true, viewOnly, cfg, 'cexBaseBalance')
    this.cexBaseBalanceRange.setChanged(() => { this.updateModifiedMarkers() })
    page.cexBaseBalanceContainer.appendChild(this.cexBaseBalanceRange.div)

    const minBaseBalance = Math.max(cfg.cexBaseMinBalance, lotSize) / bRemain * 100
    if (oldCfg.cexBaseMinBalance === 0) oldCfg.cexBaseMinBalance = minBaseBalance
    this.cexBaseMinBalanceRange = new RangeOption(baseXYRange, minBaseBalance, false, true, viewOnly, cfg, 'cexBaseMinBalance')
    this.cexBaseMinBalanceRange.setChanged(() => { this.updateModifiedMarkers() })
    page.cexBaseMinBalanceContainer.appendChild(this.cexBaseMinBalanceRange.div)

    cfg.cexBaseMinTransfer = Math.round(Math.max(cfg.cexBaseMinTransfer, lotSize))
    if (oldCfg.cexBaseMinTransfer === 0) oldCfg.cexBaseMinTransfer = cfg.cexBaseMinTransfer
    page.cexBaseTransferInput.value = Doc.formatFourSigFigs(cfg.cexBaseMinTransfer / bui.conventional.conversionFactor)

    const [quotePct] = calcBalanceReserve(qAvail, cfg.cexQuoteBalanceType, cfg.cexQuoteBalance)
    this.cexQuoteBalanceRange = new RangeOption(quoteXYRange, quotePct * 100, false, true, viewOnly, cfg, 'cexQuoteBalance')
    this.cexQuoteBalanceRange.setChanged(() => { this.updateModifiedMarkers() })
    page.cexQuoteBalanceContainer.appendChild(this.cexQuoteBalanceRange.div)

    const quoteLot = calculateQuoteLot(lotSize, baseID, quoteID, spot)
    const minQuoteBalance = Math.max(cfg.cexQuoteMinBalance, quoteLot) / qRemain * 100
    if (oldCfg.cexQuoteMinBalance === 0) oldCfg.cexQuoteMinBalance = minQuoteBalance
    this.cexQuoteMinBalanceRange = new RangeOption(quoteXYRange, minQuoteBalance, false, true, viewOnly, cfg, 'cexQuoteMinBalance')
    this.cexQuoteMinBalanceRange.setChanged(() => { this.updateModifiedMarkers() })
    page.cexQuoteMinBalanceContainer.appendChild(this.cexQuoteMinBalanceRange.div)

    cfg.cexQuoteMinTransfer = Math.round(Math.max(cfg.cexQuoteMinTransfer, quoteLot))
    if (oldCfg.cexQuoteMinTransfer === 0) oldCfg.cexQuoteMinTransfer = cfg.cexQuoteMinTransfer
    page.cexQuoteTransferInput.value = Doc.formatFourSigFigs(cfg.cexQuoteMinTransfer / qui.conventional.conversionFactor)

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
        const handler = new RangeOption(opt.xyRange, parseInt(currVal), Boolean(opt.xyRange.roundX), Boolean(opt.xyRange.roundY), viewOnly, optionsDict, opt.key)
        handler.stringify()
        handler.setChanged(() => { this.updateModifiedMarkers() })
        const setValue = (x: string) => { handler.setValue(parseInt(x)) }
        storeWalletSettingControl(opt.key, tmpl.sliderContainer, setValue, quote)
        tmpl.sliderContainer.appendChild(handler.div)
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
    const { page, specs: { baseID, quoteID } } = this

    const res = await MM.report(baseID, quoteID)
    Doc.hide(page.oraclesLoading)

    if (!app().checkResponse(res)) {
      page.oraclesErrMsg.textContent = res.msg
      Doc.show(page.oraclesErr)
      return
    }

    const r = this.marketReport = res.report as MarketReport
    if (!r.oracles || r.oracles.length === 0) {
      Doc.show(page.noOracles)
    } else {
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

    page.baseFiatRateSymbol.textContent = app().assets[baseID].symbol.toUpperCase()
    page.baseFiatRateLogo.src = Doc.logoPathFromID(baseID)
    if (r.baseFiatRate > 0) {
      page.baseFiatRate.textContent = Doc.formatFourSigFigs(r.baseFiatRate)
    } else {
      page.baseFiatRate.textContent = 'N/A'
    }

    page.quoteFiatRateSymbol.textContent = app().assets[quoteID].symbol.toUpperCase()
    page.quoteFiatRateLogo.src = Doc.logoPathFromID(quoteID)
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
    Doc.hide(page.cexConfigPrompt, page.cexConnectErrBox, page.cexFormErr)
    const dinfo = CEXDisplayInfos[cexName]
    page.cexConfigLogo.src = '/img/' + dinfo.logo
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
