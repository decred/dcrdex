import {
  app,
  PageElement,
  Market,
  Exchange,
  OrderOption,
  XYRange,
  BotReport,
  BotNote,
  MaxSell,
  MaxBuy,
  MarketReport,
  SupportedAsset,
  MakerProgram,
  GapEngineCfg,
  ArbEngineCfg,
  CEXReport,
  CEXMarket,
  CEXNote
} from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { setOptionTemplates, XYRangeOption } from './opts'
import State from './state'
import { bind as bindForm, NewWalletForm } from './forms'
import { RateEncodingFactor } from './orderutil'

const gapBotType = 'gapBot'
// const arbBotType = 'arbBot'

const CEXes = ['Binance', 'BinanceUS']

const GapStrategyMultiplier = 'multiplier'
const GapStrategyAbsolute = 'absolute'
const GapStrategyAbsolutePlus = 'absolute-plus'
const GapStrategyPercent = 'percent'
const GapStrategyPercentPlus = 'percent-plus'

interface HostedMarket extends Market {
  host: string
}

interface LiveProgram extends BotReport {
  tmpl: Record<string, PageElement>
  div: PageElement
}

interface BaseOption {
  key: string
  displayname: string
  description: string
  default: number
  min: number
  max: number
}

// Oracle Weighting
const oracleWeightBaseOption: BaseOption = {
  key: 'oracleWeighting',
  displayname: 'Oracle Weight',
  description: 'Enable the oracle and set its weight in calculating our target price.',
  default: 0.1,
  max: 1.0,
  min: 0.0
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
const oracleWeightOption: OrderOption = createXYRange(oracleWeightBaseOption, oracleWeightRange)

const oracleBiasBaseOption: BaseOption = {
  key: 'oracleBias',
  displayname: 'Oracle Bias',
  description: 'Apply an adjustment to the oracles rate, up to +/-1%.',
  default: 0.0,
  max: 0.01,
  min: -0.01
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
const oracleBiasOption: OrderOption = createXYRange(oracleBiasBaseOption, oracleBiasRange)

const driftToleranceBaseOption: BaseOption = {
  key: 'driftTolerance',
  displayname: 'Drift Tolerance',
  description: 'How far from the ideal price will we allow orders to drift. Typically a fraction of a percent.',
  default: 0.001,
  max: 0.01,
  min: 0
}
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
const driftToleranceOption: OrderOption = createXYRange(driftToleranceBaseOption, driftToleranceRange)

const gapMultiplierBaseOption: BaseOption = {
  key: 'gapMultiplier',
  displayname: 'Spread Multiplier',
  description: 'Increase the spread for reduced risk and higher potential profits, but with a lower fill rate. ' +
    'The baseline value, 1, is the break-even value, where tx fees and profits are equivalent in an otherwise static market.',
  default: 2,
  max: 10,
  min: 1
}
const gapMultiplierRange: XYRange = {
  start: {
    label: '1x',
    x: 1,
    y: 100
  },
  end: {
    label: '100x',
    x: 100,
    y: 10000
  },
  xUnit: 'X',
  yUnit: '%'
}
const gapMultiplierOption: OrderOption = createXYRange(gapMultiplierBaseOption, gapMultiplierRange)

const gapPercentBaseOption: BaseOption = {
  key: 'gapPercent',
  displayname: 'Percent Spread',
  description: 'The spread is set as a percent of current spot price.',
  default: 0.005,
  max: 1,
  min: 0
}
const gapPercentRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '10%',
    x: 0.1,
    y: 10
  },
  xUnit: 'X',
  yUnit: '%'
}
const gapPercentOption: OrderOption = createXYRange(gapPercentBaseOption, gapPercentRange)

const profitTriggerBaseOption: BaseOption = {
  key: 'profitTrigger',
  displayname: 'Profit Trigger',
  description: 'The amount of arbitrage needed to trigger an arb sequence.',
  default: 0.01,
  max: 0.1,
  min: 0.001
}

const profitTriggerRange: XYRange = {
  start: {
    label: '0.1%',
    x: 0.001,
    y: 0.1
  },
  end: {
    label: '10%',
    x: 0.1,
    y: 10
  },
  xUnit: '',
  yUnit: '%'
}

const arbProfitTriggerOption = createXYRange(profitTriggerBaseOption, profitTriggerRange)

const maxNumArbsBaseOption: BaseOption = {
  key: 'maxActiveArbs',
  displayname: 'Max Active Arbs',
  description: 'The maximum number of arbitrage sequences that will simultaneously be open.',
  default: 5,
  max: 10,
  min: 1
}

const maxNumArbsRange: XYRange = {
  start: {
    label: '1',
    x: 1,
    y: 1
  },
  end: {
    label: '10',
    x: 10,
    y: 10
  },
  xUnit: '',
  yUnit: ''
}

const maxNumArbsOption = createXYRange(maxNumArbsBaseOption, maxNumArbsRange)

const numEpochsLeaveOpenBaseOpt: BaseOption = {
  key: 'numEpochsLeaveOpen',
  displayname: 'Arbitrage Length',
  description: 'The number of epochs before unfilled orders will be cancelled.',
  default: 10,
  max: 20,
  min: 1
}

const numEpochsLeaveOpenRange: XYRange = {
  start: {
    label: '2',
    x: 2,
    y: 2
  },
  end: {
    label: '20',
    x: 20,
    y: 20
  },
  xUnit: '',
  yUnit: ''
}

const numEpochsLeaveOpenOption = createXYRange(numEpochsLeaveOpenBaseOpt, numEpochsLeaveOpenRange)

const animationLength = 300

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  data: any
  createOpts: Record<string, number>
  gapRanges: Record<string, number>
  arbRanges: Record<string, number>
  currentMarket: HostedMarket
  currentForm: PageElement | null
  keyup: (e: KeyboardEvent) => void
  programs: Record<number, LiveProgram>
  editProgram: BotReport | null
  gapMultiplierOpt: XYRangeOption
  gapPercentOpt: XYRangeOption
  driftToleranceOpt: XYRangeOption
  arbProfitTriggerOpt: XYRangeOption
  maxNumArbsOpt: XYRangeOption
  numEpochsLeaveOpenOpt: XYRangeOption
  biasOpt: XYRangeOption
  weightOpt: XYRangeOption
  pwHandler: ((pw: string) => Promise<void>) | null
  newWalletForm: NewWalletForm
  specifiedPrice: number
  currentReport: MarketReport | null
  registeringNewApiKey: boolean

  constructor (main: HTMLElement, data: any) {
    super()
    const page = this.page = Doc.idDescendants(main)
    this.data = data
    this.programs = {}
    this.editProgram = null
    this.pwHandler = null
    this.createOpts = {
      [oracleBiasBaseOption.key]: oracleBiasBaseOption.default,
      [oracleWeightBaseOption.key]: oracleWeightBaseOption.default,
      [driftToleranceOption.key]: driftToleranceOption.default
    }
    this.gapRanges = {
      [gapMultiplierBaseOption.key]: gapMultiplierBaseOption.default,
      [gapPercentBaseOption.key]: gapPercentBaseOption.default
    }
    this.arbRanges = {
      [profitTriggerBaseOption.key]: profitTriggerBaseOption.default,
      [maxNumArbsBaseOption.key]: maxNumArbsBaseOption.default,
      [numEpochsLeaveOpenBaseOpt.key]: numEpochsLeaveOpenBaseOpt.default
    }

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })

    setOptionTemplates(page)

    Doc.cleanTemplates(page.assetRowTmpl, page.booleanOptTmpl, page.rangeOptTmpl,
      page.orderOptTmpl, page.runningProgramTmpl, page.oracleTmpl, page.updateApiKeysBtn,
      page.registerApiKeysBtn, page.balancesBtn, page.marketsBtn, page.cexTableRow,
      page.balancesTableRow, page.marketsTableRow, page.connectCexBtn, page.disconnectCexBtn)

    const selectClicked = (e: MouseEvent, isBase: boolean): void => {
      e.stopPropagation()
      const select = isBase ? page.baseSelect : page.quoteSelect
      const m = Doc.descendentMetrics(main, select)
      page.assetDropdown.style.left = `${m.bodyLeft}px`
      page.assetDropdown.style.top = `${m.bodyTop}px`

      const counterAsset = isBase ? this.currentMarket.quoteid : this.currentMarket.baseid
      const clickedSymbol = isBase ? this.currentMarket.basesymbol : this.currentMarket.quotesymbol

      // Look through markets for other base assets for the counter asset.
      const matches: Set<string> = new Set()
      const otherAssets: Set<string> = new Set()

      for (const mkt of sortedMarkets()) {
        otherAssets.add(mkt.basesymbol)
        otherAssets.add(mkt.quotesymbol)
        const [firstID, secondID] = isBase ? [mkt.quoteid, mkt.baseid] : [mkt.baseid, mkt.quoteid]
        const [firstSymbol, secondSymbol] = isBase ? [mkt.quotesymbol, mkt.basesymbol] : [mkt.basesymbol, mkt.quotesymbol]
        if (firstID === counterAsset) matches.add(secondSymbol)
        else if (secondID === counterAsset) matches.add(firstSymbol)
      }

      const options = Array.from(matches)
      options.sort((a: string, b: string) => a.localeCompare(b))
      for (const symbol of options) otherAssets.delete(symbol)
      const nonOptions = Array.from(otherAssets)
      nonOptions.sort((a: string, b: string) => a.localeCompare(b))

      Doc.empty(page.assetDropdown)
      const addOptions = (symbols: string[], avail: boolean): void => {
        for (const symbol of symbols) {
          const row = this.assetRow(symbol)
          Doc.bind(row, 'click', (e: MouseEvent) => {
            e.stopPropagation()
            if (symbol === clickedSymbol) return this.hideAssetDropdown() // no change
            this.leaveEditMode()
            if (isBase) this.setCreationBase(symbol)
            else this.setCreationQuote(symbol)
          })
          if (!avail) row.classList.add('ghost')
          page.assetDropdown.appendChild(row)
        }
      }
      addOptions(options, true)
      addOptions(nonOptions, false)
      Doc.show(page.assetDropdown)
      const clicker = (e: MouseEvent): void => {
        if (Doc.mouseInElement(e, page.assetDropdown)) return
        this.hideAssetDropdown()
        Doc.unbind(document, 'click', clicker)
      }
      Doc.bind(document, 'click', clicker)
    }

    Doc.bind(page.baseSelect, 'click', (e: MouseEvent) => selectClicked(e, true))
    Doc.bind(page.quoteSelect, 'click', (e: MouseEvent) => selectClicked(e, false))

    Doc.bind(page.marketSelect, 'change', () => {
      const [host, name] = page.marketSelect.value?.split(' ') as string[]
      this.setMarketSubchoice(host, name)
    })

    Doc.bind(page.botTypeSelect, 'change', () => this.updateBotType())
    Doc.bind(page.manageCexBtn, 'click', () => this.showManageCexForm())
    Doc.bind(page.apiKeysSubmit, 'click', (e: Event) => {
      e.preventDefault()
      this.submitApiKeyForm()
    })

    this.arbProfitTriggerOpt = new XYRangeOption(arbProfitTriggerOption, '', this.arbRanges, () => { /* nothing */ })
    this.maxNumArbsOpt = new XYRangeOption(maxNumArbsOption, '', this.arbRanges, () => { /* nothing */ }, true, true)
    this.numEpochsLeaveOpenOpt = new XYRangeOption(numEpochsLeaveOpenOption, '', this.arbRanges, () => { /* nothing */ }, true, true)

    page.arbOptions.appendChild(this.arbProfitTriggerOpt.node)
    page.arbOptions.appendChild(this.maxNumArbsOpt.node)
    page.arbOptions.appendChild(this.numEpochsLeaveOpenOpt.node)

    this.gapMultiplierOpt = new XYRangeOption(gapMultiplierOption, '', this.gapRanges, () => this.createOptsUpdated())
    this.gapPercentOpt = new XYRangeOption(gapPercentOption, '', this.gapRanges, () => this.createOptsUpdated())
    this.driftToleranceOpt = new XYRangeOption(driftToleranceOption, '', this.createOpts, () => this.createOptsUpdated())
    this.biasOpt = new XYRangeOption(oracleBiasOption, '', this.createOpts, () => this.createOptsUpdated())
    this.weightOpt = new XYRangeOption(oracleWeightOption, '', this.createOpts, () => this.createOptsUpdated())

    page.gapOptions.appendChild(this.gapMultiplierOpt.node)
    Doc.hide(this.gapMultiplierOpt.node) // Default is GapStrategyPercentPlus
    page.gapOptions.appendChild(this.gapPercentOpt.node)
    page.gapOptions.appendChild(this.driftToleranceOpt.node)
    page.gapOptions.appendChild(this.weightOpt.node)
    page.gapOptions.appendChild(this.biasOpt.node)

    Doc.bind(page.showAdvanced, 'click', () => {
      State.storeLocal(State.optionsExpansionLK, true)
      Doc.hide(page.showAdvanced)
      Doc.show(page.hideAdvanced, page.gapOptions)
    })

    Doc.bind(page.hideAdvanced, 'click', () => {
      State.storeLocal(State.optionsExpansionLK, false)
      Doc.hide(page.hideAdvanced, page.gapOptions)
      Doc.show(page.showAdvanced)
    })

    Doc.bind(page.gapRunBttn, 'click', this.authedRoute(async (pw: string): Promise<void> => this.createGapBot(pw)))
    Doc.bind(page.arbRunBttn, 'click', this.authedRoute(async (pw: string): Promise<void> => this.createArbBot(pw)))

    Doc.bind(page.lotsInput, 'change', () => {
      page.lotsInput.value = String(Math.round(parseFloat(page.lotsInput.value ?? '0')))
    })

    Doc.bind(page.exitEditMode, 'click', () => this.leaveEditMode())

    Doc.bind(page.manualPriceBttn, 'click', () => {
      Doc.hide(page.basisPrice, page.manualPriceBttn)
      Doc.show(page.manualPriceInput)
      page.manualPriceInput.focus()
    })

    const setManualPrice = () => {
      const v = parseFloat(page.manualPriceInput.value ?? '0')
      page.basisPrice.textContent = Doc.formatFiveSigFigs(v)
      this.specifiedPrice = v
      this.setCurrentBasisPrice(v)
      Doc.show(page.basisPrice, page.manualPriceBttn)
      Doc.hide(page.manualPriceInput, page.noFiatBox)
    }

    const hideManualPrice = () => {
      page.manualPriceInput.value = ''
      Doc.show(page.basisPrice, page.manualPriceBttn)
      Doc.hide(page.manualPriceInput)
    }

    // Doc.bind(page.manualPriceInput, 'change', () => { setManualPrice() })

    Doc.bind(page.manualPriceInput, 'keyup', (e: KeyboardEvent) => {
      switch (e.key) {
        case 'Escape':
          hideManualPrice()
          break
        case 'Enter':
        case 'NumpadEnter':
          setManualPrice()
      }
    })

    Doc.bind(page.manualPriceInput, 'blur', () => {
      if (Doc.isHidden(page.basisPrice)) hideManualPrice()
    })

    Doc.bind(page.gapStrategySelect, 'change', () => this.updateGapStrategyInputs(this.currentReport))

    const submitPasswordForm = async () => {
      const pw = page.pwInput.value ?? ''
      if (pw === '') return
      const handler = this.pwHandler
      if (handler === null) return
      this.pwHandler = null
      await handler(pw)
      if (this.currentForm === page.pwForm) this.closePopups()
    }

    bindForm(page.pwForm, page.pwSubmit, () => submitPasswordForm())

    Doc.bind(page.pwInput, 'keyup', (e: KeyboardEvent) => {
      if (e.key !== 'Enter' && e.key !== 'NumpadEnter') return
      submitPasswordForm()
    })

    this.newWalletForm = new NewWalletForm(
      page.newWalletForm,
      assetID => this.newWalletCreated(assetID)
    )

    Doc.bind(page.createBaseWallet, 'click', () => {
      this.newWalletForm.setAsset(this.currentMarket.baseid)
      this.showForm(page.newWalletForm)
    })

    Doc.bind(page.createQuoteWallet, 'click', () => {
      this.newWalletForm.setAsset(this.currentMarket.quoteid)
      this.showForm(page.newWalletForm)
    })

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!this.currentForm) return
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        this.closePopups()
      }
    }

    const lastMkt = State.fetchLocal(State.lastMMMarketLK) as HostedMarket
    let mkt: HostedMarket | null = null
    if (lastMkt && lastMkt.host) {
      const xc = app().exchanges[lastMkt.host]
      if (xc) {
        const mktID = lastMkt.basesymbol + '_' + lastMkt.quotesymbol
        const xcMkt = xc.markets[mktID]
        if (xcMkt) mkt = Object.assign({ host: xc.host }, xcMkt)
      }
    }

    if (State.fetchLocal(State.optionsExpansionLK)) {
      Doc.show(page.hideAdvanced, page.gapOptions)
      Doc.hide(page.showAdvanced)
    }

    app().registerNoteFeeder({
      bot: (n: BotNote) => { this.handleBotNote(n) },
      cex: (n: CEXNote) => { this.handleCEXNote(n) }
    })

    this.setMarket([mkt ?? sortedMarkets()[0]])
    this.populateRunningPrograms()
    page.createBox.classList.remove('invisible')
    page.programsBox.classList.remove('invisible')

    Doc.bind(page.cexSelector, 'change', () => this.updateArbMarket())
    this.updateCEXSelector()
  }

  /* updateBotType updates which bot type settings are shown for */
  updateBotType (): void {
    const page = this.page
    if (page.botTypeSelect.value === gapBotType) {
      Doc.hide(page.arbBotSettings)
      Doc.show(page.gapBotSettings)
    } else {
      Doc.show(page.arbBotSettings)
      Doc.hide(page.gapBotSettings)
    }
  }

  /*
   * updateCEXSelector populates the cexSelector dropdown with the CEXes that
   * have been connected, and also updates the rest of the arb bot settings
   * based on what is populated in the cexSelector.
   */
  updateCEXSelector ():void {
    const page = this.page

    while (page.cexSelector.firstChild) {
      page.cexSelector.removeChild(page.cexSelector.firstChild)
    }
    app().user.cexes.forEach((cex: CEXReport) => {
      if (!cex.connected) return
      const opt = document.createElement('option')
      opt.value = cex.name
      opt.textContent = cex.name
      page.cexSelector.appendChild(opt)
    })

    if (page.cexSelector.children.length === 0) {
      Doc.hide(page.cexSelectorLbl, page.cexSelector, page.arbSettingsIfMarket, page.arbSettingsNoMarket)
      Doc.show(page.noConnectedCexLbl)
    } else {
      Doc.show(page.cexSelectorLbl, page.cexSelector, page.arbSettingsIfMarket, page.arbSettingsNoMarket)
      Doc.hide(page.noConnectedCexLbl)
    }

    this.updateArbMarket()
  }

  // selectedChecks returns the CEXReport for the CEX that is currently selected
  // by the cexSelector.
  selectedCEX (): (CEXReport|undefined) {
    const page = this.page
    const currentCEXName = page.cexSelector.value
    if (!currentCEXName) return undefined
    return app().user.cexes.find((cex: CEXReport) => currentCEXName === cex.name)
  }

  // cexHasMarket returns whether or not a CEX has a market for a base/quote
  // pair.
  cexHasMarket (cex: CEXReport, base: number, quote: number): boolean {
    for (const market of cex.markets) {
      if (market.base === base && market.quote === quote) {
        return true
      }
    }
    return false
  }

  // updateArbMarket checks if the current selected CEX contains the market
  // that the user wants to deploy a bot on. If so, then the balances on
  // the CEX of the relevant assets is displayed. Otherwise, a message is
  // displayed that the CEX does not contain this market.
  updateArbMarket (): void {
    const page = this.page
    const currentCEX = this.selectedCEX()
    if (!currentCEX) return
    const base = app().assets[this.currentMarket.baseid]
    const quote = app().assets[this.currentMarket.quoteid]
    const cexHasMarket = this.cexHasMarket(currentCEX, base.id, quote.id)
    if (cexHasMarket) {
      const baseBalance = currentCEX.balances[base.id]
      const quoteBalance = currentCEX.balances[quote.id]
      if (baseBalance) page.cexBalancesBase.textContent = String(baseBalance.available / base.unitInfo.conventional.conversionFactor)
      else page.cexBalancesBase.textContent = '0'
      if (quoteBalance) page.cexBalancesQuote.textContent = String(quoteBalance.available / quote.unitInfo.conventional.conversionFactor)
      else page.cexBalanceQuote.textContent = '0'
      page.cexBalancesBaseSymbol.textContent = base.symbol.toUpperCase()
      page.cexBalancesQuoteSymbol.textContent = quote.symbol.toUpperCase()
      page.balancesCexName.textContent = currentCEX.name
    } else {
      page.noMarketCexName.textContent = currentCEX.name
      page.noMarketBase.textContent = base.symbol.toUpperCase()
      page.noMarketQuote.textContent = quote.symbol.toUpperCase()
    }
    Doc.setVis(cexHasMarket, page.arbSettingsIfMarket)
    Doc.setVis(!cexHasMarket, page.arbSettingsNoMarket)
  }

  // updateManageCEXForm updates the form that allows the user to manage their
  // CEX keys based on whether the user has registered keys for a CEX and
  // whether or not the CEX is connected.
  updateManageCexForm (): void {
    const page = this.page
    Doc.hide(page.manageCEXErr)
    while (page.cexTableBody.firstChild) {
      page.cexTableBody.removeChild(page.cexTableBody.firstChild)
    }
    const registeredCEXes = app().user.cexes
    const findRegisteredCEX = (cex: string) : CEXReport|undefined => {
      return registeredCEXes.find(el => el.name === cex)
    }
    CEXes.forEach((cexName: string) => {
      const row = page.cexTableRow.cloneNode(true) as PageElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.cexName.textContent = cexName
      const registeredCEX = findRegisteredCEX(cexName)
      // Register/Update keys button.
      let apiKeysBtn : PageElement
      if (registeredCEX) {
        apiKeysBtn = page.updateApiKeysBtn.cloneNode(true) as PageElement
      } else {
        apiKeysBtn = page.registerApiKeysBtn.cloneNode(true) as PageElement
      }
      Doc.bind(apiKeysBtn, 'click', (e: Event) => {
        e.preventDefault()
        this.registeringNewApiKey = !registeredCEX
        this.showApiKeyForm(cexName)
      })
      tmpl.apiKeys.appendChild(apiKeysBtn)
      // Only show register keys button if not yet registered.
      if (!registeredCEX) {
        page.cexTableBody.append(row)
        return
      }
      // If not connected, only show update keys button and connect button.
      if (!registeredCEX.connected) {
        const connectBtn = page.connectCexBtn.cloneNode(true) as PageElement
        Doc.bind(connectBtn, 'click', (e: Event) => {
          e.preventDefault()
          this.connectCEX(cexName)
        })
        tmpl.connect.appendChild(connectBtn)
        page.cexTableBody.append(row)
        return
      }
      // If connected, show disconnect, balances, and markets buttons.
      const disconnectBtn = page.disconnectCexBtn.cloneNode(true) as PageElement
      Doc.bind(disconnectBtn, 'click', (e: Event) => {
        e.preventDefault()
        this.disconnectCEX(cexName)
      })
      tmpl.connect.appendChild(disconnectBtn)
      const balancesBtn = page.balancesBtn.cloneNode(true) as PageElement
      Doc.bind(balancesBtn, 'click', (e: Event) => {
        e.preventDefault()
        this.showBalancesForm(cexName)
      })
      tmpl.balances.appendChild(balancesBtn)
      const marketsBtn = page.marketsBtn.cloneNode(true) as PageElement
      Doc.bind(marketsBtn, 'click', (e: Event) => {
        e.preventDefault()
        this.showMarketsForm(cexName)
      })
      tmpl.markets.appendChild(marketsBtn)
      page.cexTableBody.append(row)
    })
  }

  /* connectCEX sets up a connection to a CEX */
  async connectCEX (cexName: string): Promise<void> {
    const page = this.page
    if (Doc.isDisplayed(page.connectCexSpinner)) return
    Doc.show(page.connectCexSpinner)
    const res = await postJSON('/api/connectcex', {
      name: cexName
    })
    Doc.hide(page.connectCexSpinner)
    if (!app().checkResponse(res as any)) {
      page.manageCEXErr.textContent = res.msg
      Doc.show(page.manageCEXErr)
      return
    }
    app().updateCEX(res.cex)
    this.updateManageCexForm()
    this.updateCEXSelector()
  }

  /* disconnectCEX end a connection to a CEX. */
  async disconnectCEX (cexName: string): Promise<void> {
    const page = this.page
    if (Doc.isDisplayed(page.connectCexSpinner)) return
    const res = await postJSON('/api/disconnectcex', {
      name: cexName
    })
    if (!app().checkResponse(res as any)) {
      page.manageCEXErr.textContent = res.msg
      Doc.show(page.manageCEXErr)
      return
    }
    app().disconnectCEX(cexName)
    this.updateManageCexForm()
    this.updateCEXSelector()
  }

  /* showManageCexForm displays the manageCexForm. */
  showManageCexForm ():void {
    this.updateManageCexForm()
    this.showForm(this.page.manageCexForm)
  }

  /* showApiKeyForm displays the apiKeysForm. */
  showApiKeyForm (cex: string):void {
    const page = this.page
    Doc.hide(page.apiKeysErr)
    page.apiKeyInput.value = ''
    page.apiSecretInput.value = ''
    page.apiKeysCexLbl.textContent = cex
    this.showForm(page.apiKeysForm)
  }

  /* updatedBalancesTable updates the values on the table displaying the user's
   * balances on a CEX
   */
  updateBalancesTable (cexName: string): void {
    const page = this.page
    while (page.balancesTableBody.firstChild) {
      page.balancesTableBody.removeChild(page.balancesTableBody.firstChild)
    }
    const cex = app().user.cexes.find((report) => report.name === cexName)
    if (!cex) {
      console.error(`could not find ${cexName}`)
      return
    }
    for (const assetID in cex.balances) {
      const asset = app().assets[assetID]
      if (!asset) {
        console.error(`non supported asset in cex balances: ${assetID}`)
        continue
      }
      const balanceRow = page.balancesTableRow.cloneNode(true) as PageElement
      const tmpl = Doc.parseTemplate(balanceRow)
      tmpl.assetLogo.src = Doc.logoPath(asset.symbol)
      tmpl.assetName.textContent = asset.symbol.toUpperCase()
      const balance = cex.balances[assetID]
      tmpl.available.textContent = String(balance.available / asset.unitInfo.conventional.conversionFactor)
      tmpl.locked.textContent = String(balance.locked / asset.unitInfo.conventional.conversionFactor)
      page.balancesTableBody.appendChild(balanceRow)
    }
  }

  /* showBalancesForm displays the form containing the user's balances on a CEX */
  showBalancesForm (cexName: string): void {
    const page = this.page
    page.cexBalancesFormLbl.textContent = cexName
    this.updateBalancesTable(cexName)
    this.showForm(page.balancesForm)
  }

  /* showMarketsForm displays the markets that a CEX has. */
  showMarketsForm (cexName: string): void {
    const page = this.page
    page.cexMarketsLbl.textContent = cexName

    const setMarkets = (markets: CEXMarket[]) => {
      while (page.marketsTableBody.firstChild) {
        page.marketsTableBody.removeChild(page.marketsTableBody.firstChild)
      }

      markets.forEach((market: CEXMarket) => {
        const base = app().assets[market.base]
        if (!base) {
          return
        }

        const quote = app().assets[market.quote]
        if (!quote) {
          return
        }

        const marketRow = page.marketsTableRow.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(marketRow)

        tmpl.baseAssetLogo.src = Doc.logoPath(base.symbol)
        tmpl.baseAssetName.textContent = base.symbol.toUpperCase()
        tmpl.quoteAssetLogo.src = Doc.logoPath(quote.symbol)
        tmpl.quoteAssetName.textContent = quote.symbol.toUpperCase()
        page.marketsTableBody.appendChild(marketRow)
      })
    }

    const cex = app().user.cexes.find((report) => report.name === cexName)
    if (!cex) {
      console.error(`could not find ${cexName}`)
      return
    }

    setMarkets(cex.markets)
    this.showForm(page.marketsForm)
  }

  /*
   * submitApiKeyForm either registers keys for a CEX or updates the keys
   * for a CEX that has not yet been registered.
   */
  async submitApiKeyForm (): Promise<void> {
    const page = this.page
    const endPoint = this.registeringNewApiKey ? '/api/registernewcex' : '/api/updatecexcreds'
    Doc.show(page.apiKeysSpinner)
    const res = await postJSON(endPoint, {
      name: page.apiKeysCexLbl.textContent,
      apiKey: page.apiKeyInput.value,
      apiSecret: page.apiSecretInput.value
    })
    Doc.hide(page.apiKeysSpinner)
    if (!app().checkResponse(res as any)) {
      page.apiKeysErr.textContent = res.msg
      Doc.show(page.apiKeysErr)
      return
    }
    app().updateCEX(res.cex)
    this.updateManageCexForm()
    this.updateCEXSelector()
    this.showManageCexForm()
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement): Promise<void> {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.pwForm, page.newWalletForm, page.manageCexForm, page.apiKeysForm,
      page.balancesForm, page.marketsForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  closePopups (): void {
    const page = this.page
    this.pwHandler = null
    page.pwInput.value = ''

    if (this.currentForm === page.apiKeysForm ||
        this.currentForm === page.balancesForm ||
        this.currentForm === page.marketsForm) {
      this.showForm(this.page.manageCexForm)
    } else {
      Doc.hide(this.page.forms)
      this.currentForm = null
    }
  }

  hideAssetDropdown (): void {
    const page = this.page
    page.assetDropdown.scrollTop = 0
    Doc.hide(page.assetDropdown)
  }

  assetRow (symbol: string): PageElement {
    const row = this.page.assetRowTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(row)
    tmpl.logo.src = Doc.logoPath(symbol)
    Doc.empty(tmpl.symbol)
    tmpl.symbol.appendChild(Doc.symbolize(symbol))
    return row
  }

  setCurrentReport (r: MarketReport | null) {
    this.currentReport = r
    this.setCurrentBasisPrice(r ? r.basisPrice : 0)
    this.updateGapStrategyInputs(r)

    // Set max value for absolute input
    const page = this.page
    if (r && r.basisPrice > 0) {
      this.fetchMaxBuy(Math.round(r.basisPrice / this.currentMarket.atomToConv * RateEncodingFactor))
      page.basisPrice.textContent = Doc.formatFiveSigFigs(r.basisPrice)
      Doc.show(page.absMaxBox, page.basisPrice)
      Doc.hide(page.manualPriceInput, page.noFiatBox)
      this.page.gapFactorMax.textContent = Doc.formatFiveSigFigs(r.basisPrice)
    } else {
      page.lotEstQuoteLots.textContent = '[no rate]'
      page.basisPrice.textContent = 'must set manually'
      Doc.hide(page.basisPrice, page.absMaxBox)
      Doc.show(page.manualPriceInput)
      page.manualPriceInput.focus()
      const u = app().user
      if (Object.keys(u.fiatRates).length === 0) Doc.show(page.noFiatBox)
    }

    if (r && r.breakEvenSpread > 0) {
      Doc.show(page.breakEvenGapBox)
      page.breakEvenGap.textContent = Doc.formatFiveSigFigs(r.breakEvenSpread)
    } else Doc.hide(page.breakEvenGapBox)
  }

  updateGapStrategyInputVisibility () {
    const page = this.page
    const gapStrategy = page.gapStrategySelect.value
    switch (gapStrategy) {
      case GapStrategyMultiplier:
        Doc.show(this.gapMultiplierOpt.node)
        Doc.hide(page.absInputBox, page.absMaxBox, this.gapPercentOpt.node)
        break
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        Doc.show(this.gapPercentOpt.node)
        Doc.hide(page.absInputBox, page.absMaxBox, this.gapMultiplierOpt.node)
        break
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus:
        Doc.hide(this.gapMultiplierOpt.node, this.gapPercentOpt.node)
        Doc.show(page.absInputBox, page.absMaxBox)
    }
  }

  updateGapStrategyInputs (r: MarketReport | null) {
    this.updateGapStrategyInputVisibility()
    switch (this.page.gapStrategySelect.value) {
      case GapStrategyMultiplier: {
        if (r && r.breakEvenSpread > 0) {
          gapMultiplierRange.start.y = r.breakEvenSpread
          gapMultiplierRange.end.y = r.breakEvenSpread * 100
          gapMultiplierRange.yUnit = this.rateUnit()
        } else {
          gapMultiplierRange.start.y = 100
          gapMultiplierRange.end.y = 10000
          gapMultiplierRange.yUnit = '%'
        }
        this.gapMultiplierOpt.handler.setConfig(gapMultiplierRange)
        break
      }
      case GapStrategyPercent:
      case GapStrategyPercentPlus: {
        if (r && r.breakEvenSpread > 0) {
          gapPercentRange.end.y = r.basisPrice
          gapPercentRange.yUnit = this.rateUnit()
          this.gapPercentOpt.handler.setConfig(gapPercentRange)
        } else {
          gapPercentRange.end.y = 10
          gapPercentRange.yUnit = '%'
        }
      }
    }
  }

  setCurrentBasisPrice (p: number) {
    if (p > 0) {
      const rateUnit = this.rateUnit()
      oracleBiasRange.start.y = -0.01 * p
      oracleBiasRange.end.y = 0.01 * p
      oracleBiasRange.yUnit = rateUnit
      driftToleranceRange.start.y = 0
      driftToleranceRange.end.y = p * 0.01
      driftToleranceRange.yUnit = rateUnit
    } else {
      oracleBiasRange.start.y = -1
      oracleBiasRange.end.y = 1
      oracleBiasRange.yUnit = '%'
      driftToleranceRange.start.y = 0
      driftToleranceRange.end.y = 1
      driftToleranceRange.yUnit = '%'
    }
    this.biasOpt.handler.setConfig(oracleBiasRange)
    this.driftToleranceOpt.handler.setConfig(driftToleranceRange)
  }

  rateUnit (): string {
    const quoteSymbol = this.currentMarket.quotesymbol.split('.')[0]
    const baseSymbol = this.currentMarket.basesymbol.split('.')[0]
    return `${quoteSymbol}/${baseSymbol}`
  }

  async setMarket (mkts: HostedMarket[], skipMarketUpdate?: boolean): Promise<void> {
    const page = this.page

    const mkt = mkts[0]
    this.currentMarket = mkt
    this.specifiedPrice = 0
    this.setCurrentReport(null)
    page.manualPriceInput.value = ''

    State.storeLocal(State.lastMMMarketLK, mkt)

    Doc.empty(page.baseSelect, page.quoteSelect)
    page.baseSelect.appendChild(this.assetRow(mkt.basesymbol))
    page.quoteSelect.appendChild(this.assetRow(mkt.quotesymbol))
    this.hideAssetDropdown()

    const addMarketSelect = (mkt: HostedMarket, el: PageElement) => {
      Doc.setContent(el,
        Doc.symbolize(mkt.basesymbol),
        new Text('-') as any,
        Doc.symbolize(mkt.quotesymbol),
        new Text(' @ ') as any,
        new Text(mkt.host) as any
      )
    }

    Doc.hide(page.marketSelect, page.marketOneChoice)
    if (mkts.length === 1) {
      Doc.show(page.marketOneChoice)
      addMarketSelect(mkt, page.marketOneChoice)
    } else {
      Doc.show(page.marketSelect)
      Doc.empty(page.marketSelect)
      for (const mkt of mkts) {
        const opt = document.createElement('option')
        page.marketSelect.appendChild(opt)
        opt.value = `${mkt.host} ${mkt.name}`
        addMarketSelect(mkt, opt)
      }
    }

    if (skipMarketUpdate) return

    Doc.setContent(page.lotEstBaseSymbol, Doc.symbolize(mkt.basesymbol))
    Doc.setContent(page.lotEstQuoteSymbol, Doc.symbolize(mkt.quotesymbol))

    const setNoWallet = (a: SupportedAsset, noWalletBox: PageElement): boolean => {
      Doc.show(noWalletBox)
      const tmpl = Doc.parseTemplate(noWalletBox)
      tmpl.assetLogo.src = Doc.logoPath(a.symbol)
      tmpl.assetName.textContent = a.name
      return false
    }

    Doc.hide(
      page.lotEstQuoteBox, page.lotEstQuoteNoWallet, page.lotEstBaseBox, page.lotEstBaseNoWallet, page.availHeader,
      page.lotEstimateBox, page.marketInfo, page.oraclesBox, page.lotsBox, page.gapOptions, page.advancedBox,
      page.botTypeDiv, page.gapBotSettings, page.arbBotSettings
    )
    const [b, q] = [app().assets[mkt.baseid], app().assets[mkt.quoteid]]
    if (b.wallet && q.wallet) {
      this.updateArbMarket()
      Doc.show(
        page.lotEstQuoteBox, page.lotEstBaseBox, page.availHeader, page.fetchingMarkets,
        page.lotsBox, page.advancedBox, page.botTypeDiv
      )
      this.updateBotType()
      if (State.fetchLocal(State.optionsExpansionLK)) Doc.show(page.options)
      const loaded = app().loading(page.gapOptions)
      const buy = this.fetchOracleAndMaxBuy()
      const sell = this.fetchMaxSell()
      await buy
      await sell
      loaded()
      Doc.show(page.lotEstimateBox, page.marketInfo, page.oraclesBox)
      Doc.hide(page.fetchingMarkets)
      return
    }
    Doc.show(page.lotEstimateBox)
    if (q.wallet) {
      page.lotEstQuoteLots.textContent = '0'
    } else setNoWallet(q, page.lotEstQuoteNoWallet)

    if (b.wallet) {
      page.lotEstBaseLots.textContent = '0'
    } else setNoWallet(b, page.lotEstBaseNoWallet)
  }

  async fetchMaxSell (): Promise<void> {
    const { currentMarket: mkt, page } = this
    const res = await postJSON('/api/maxsell', {
      host: mkt.host,
      base: mkt.baseid,
      quote: mkt.quoteid
    }) as MaxSell
    if (this.currentMarket !== mkt) return
    if (!app().checkResponse(res as any)) {
      page.lotEstBaseLots.textContent = '0'
      console.error(res)
      return
    }
    page.lotEstBaseLots.textContent = String(res.maxSell.swap.lots)
  }

  async fetchOracleAndMaxBuy (): Promise<void> {
    const { currentMarket: mkt, page } = this
    const res = await postJSON('/api/marketreport', { host: mkt.host, baseID: mkt.baseid, quoteID: mkt.quoteid })
    if (!app().checkResponse(res)) {
      page.lotEstQuoteLots.textContent = '0'
      console.error(res)
      return
    }
    const r = res.report as MarketReport
    Doc.hide(page.manualPriceBttn)
    this.setCurrentReport(r)

    if (!r.oracles || r.oracles.length === 0) return

    Doc.empty(page.oracles)
    let weight = 0
    let weightedSum = 0
    for (const o of r.oracles ?? []) {
      const tr = page.oracleTmpl.cloneNode(true) as PageElement
      page.oracles.appendChild(tr)
      const tmpl = Doc.parseTemplate(tr)
      tmpl.logo.src = 'img/' + o.host + '.png'
      tmpl.host.textContent = ExchangeNames[o.host]
      tmpl.volume.textContent = Doc.formatThreeSigFigs(o.usdVol)
      const price = (o.bestBuy + o.bestSell) / 2
      weightedSum += o.usdVol * price
      weight += o.usdVol
      tmpl.price.textContent = Doc.formatThreeSigFigs((o.bestBuy + o.bestSell) / 2)
    }
    page.avgPrice.textContent = Doc.formatFiveSigFigs(weightedSum / weight)
  }

  async fetchMaxBuy (rate: number): Promise<void> {
    const { currentMarket: mkt, page } = this
    const res = await postJSON('/api/maxbuy', {
      host: mkt.host,
      base: mkt.baseid,
      quote: mkt.quoteid,
      rate: rate ?? 0
    }) as MaxBuy
    if (this.currentMarket !== mkt) return
    page.lotEstQuoteLots.textContent = String(res.maxBuy.swap.lots)
  }

  setEditProgram (report: BotReport): void {
    const { createOpts, page } = this
    const pgm = report.program
    const [b, q] = [app().assets[pgm.baseID], app().assets[pgm.quoteID]]
    const mkt = app().exchanges[pgm.host].markets[`${b.symbol}_${q.symbol}`]
    this.setMarket([{
      host: pgm.host,
      ...mkt
    }], true)
    for (const p of Object.values(this.programs)) {
      if (p.programID !== report.programID) Doc.hide(p.div)
    }
    this.editProgram = report
    page.createBox.classList.add('edit')
    page.programsBox.classList.add('edit')
    Doc.hide(page.botTypeDiv)

    if (pgm.gapEngineCfg) {
      Doc.show(page.gapBotSettings)
      Doc.hide(page.arbBotSettings)
      page.lotsInput.value = String(pgm.gapEngineCfg.lots)
      createOpts.oracleWeighting = pgm.gapEngineCfg.oracleWeighting
      this.weightOpt.setValue(pgm.gapEngineCfg.oracleWeighting)
      createOpts.oracleBias = pgm.gapEngineCfg.oracleBias
      this.biasOpt.setValue(pgm.gapEngineCfg.oracleBias)
      createOpts.driftTolerance = pgm.gapEngineCfg.driftTolerance
      this.driftToleranceOpt.setValue(pgm.gapEngineCfg.driftTolerance)
      page.gapStrategySelect.value = pgm.gapEngineCfg.gapStrategy
      this.updateGapStrategyInputVisibility()
      this.createOptsUpdated()

      switch (pgm.gapEngineCfg.gapStrategy) {
        case GapStrategyPercent:
        case GapStrategyPercentPlus:
          this.gapPercentOpt.setValue(pgm.gapEngineCfg.gapFactor)
          break
        case GapStrategyMultiplier:
          this.gapMultiplierOpt.setValue(pgm.gapEngineCfg.gapFactor)
      }
    } else if (pgm.arbEngineCfg) {
      Doc.show(page.arbBotSettings, page.arbSettingsIfMarket)
      Doc.hide(page.gapBotSettings, page.cexBalances, page.cexBalancesLbl, page.arbSettingsNoMarket)

      this.arbProfitTriggerOpt.setValue(pgm.arbEngineCfg.profitTrigger)
      this.maxNumArbsOpt.setValue(pgm.arbEngineCfg.maxActiveArbs)
      this.numEpochsLeaveOpenOpt.setValue(pgm.arbEngineCfg.numEpochsLeaveOpen)
    }

    Doc.bind(page.programsBox, 'click', () => this.leaveEditMode())
  }

  leaveEditMode (): void {
    const page = this.page
    Doc.unbind(page.programsBox, 'click', () => this.leaveEditMode())
    for (const p of Object.values(this.programs)) Doc.show(p.div)
    page.createBox.classList.remove('edit')
    page.programsBox.classList.remove('edit')
    this.editProgram = null
    Doc.show(page.botTypeDiv, page.cexBalances, page.cexBalancesLbl)
    this.updateCEXSelector()
    this.setMarket([this.currentMarket])
  }

  populateRunningPrograms (): void {
    const page = this.page
    const bots = app().user.bots
    Doc.empty(page.runningPrograms)
    this.programs = {}
    for (const report of bots) page.runningPrograms.appendChild(this.programDiv(report))
    this.setProgramsHeader()
  }

  setProgramsHeader (): void {
    const page = this.page
    if (page.runningPrograms.children.length > 0) {
      Doc.show(page.programsHeader)
      Doc.hide(page.noProgramsMessage)
    } else {
      Doc.hide(page.programsHeader)
      Doc.show(page.noProgramsMessage)
    }
  }

  programDiv (report: BotReport): PageElement {
    const page = this.page
    const div = page.runningProgramTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    const startStop = async (endpoint: string, pw?: string): Promise<void> => {
      Doc.hide(tmpl.startErr)
      const loaded = app().loading(div)
      const res = await postJSON(endpoint, { programID: report.programID, appPW: pw })
      loaded()
      if (!app().checkResponse(res)) {
        tmpl.startErr.textContent = res.msg
        Doc.show(tmpl.startErr)
      }
    }
    Doc.bind(tmpl.pauseBttn, 'click', () => startStop('/api/stopbot'))
    Doc.bind(tmpl.startBttn, 'click', this.authedRoute(async (pw: string) => startStop('/api/startbot', pw)))
    Doc.bind(tmpl.retireBttn, 'click', () => startStop('/api/retirebot'))
    Doc.bind(tmpl.configureBttn, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      this.setEditProgram(this.programs[report.programID])
    })
    const [b, q] = [app().assets[report.program.baseID], app().assets[report.program.quoteID]]
    tmpl.base.appendChild(this.assetRow(b.symbol))
    tmpl.quote.appendChild(this.assetRow(q.symbol))
    tmpl.baseSymbol.textContent = b.symbol.toUpperCase()
    tmpl.quoteSymbol.textContent = q.symbol.toUpperCase()
    tmpl.host.textContent = report.program.host
    this.updateProgramDiv(tmpl, report)
    this.programs[report.programID] = Object.assign({ tmpl, div }, report)
    return div
  }

  authedRoute (handler: (pw: string) => Promise<void>): () => void {
    return async () => {
      if (State.passwordIsCached()) return await handler('')
      this.pwHandler = handler
      await this.showForm(this.page.pwForm)
    }
  }

  updateProgramDiv (tmpl: Record<string, PageElement>, report: BotReport): void {
    const pgm = report.program
    Doc.hide(tmpl.programRunning, tmpl.programPaused)
    if (report.running) Doc.show(tmpl.programRunning)
    else Doc.show(tmpl.programPaused)
    if (pgm.gapEngineCfg) {
      Doc.hide(tmpl.arbBotOpts)
      tmpl.lots.textContent = String(pgm.gapEngineCfg.lots)
      tmpl.boost.textContent = `${(pgm.gapEngineCfg.gapFactor * 100).toFixed(1)}%`
      tmpl.driftTolerance.textContent = `${(pgm.gapEngineCfg.driftTolerance * 100).toFixed(2)}%`
      tmpl.oracleWeight.textContent = `${(pgm.gapEngineCfg.oracleWeighting * 100).toFixed(0)}%`
      tmpl.oracleBias.textContent = `${(pgm.gapEngineCfg.oracleBias * 100).toFixed(1)}%`
    } else if (pgm.arbEngineCfg) {
      Doc.hide(tmpl.gapBotOpts)
      tmpl.cex.textContent = pgm.arbEngineCfg.cexName
      tmpl.profitTrigger.textContent = `${(pgm.arbEngineCfg.profitTrigger * 100).toFixed(1)}%`
      tmpl.maxArbs.textContent = String(pgm.arbEngineCfg.maxActiveArbs)
      tmpl.arbLength.textContent = String(pgm.arbEngineCfg.numEpochsLeaveOpen)
    }
  }

  setMarketSubchoice (host: string, name: string): void {
    if (host !== this.currentMarket.host || name !== this.currentMarket.name) this.leaveEditMode()
    this.currentMarket = Object.assign({ host }, app().exchanges[host].markets[name])
  }

  createOptsUpdated (): void {
    const opts = this.createOpts
    if (opts.oracleWeighting) Doc.show(this.biasOpt.node)
    else Doc.hide(this.biasOpt.node)
  }

  handleBotNote (n: BotNote): void {
    const page = this.page
    const r = n.report
    switch (n.topic) {
      case 'BotCreated':
        page.runningPrograms.prepend(this.programDiv(r))
        this.setProgramsHeader()
        break
      case 'BotRetired':
        this.programs[r.programID].div.remove()
        delete this.programs[r.programID]
        this.setProgramsHeader()
        break
      default: {
        const p = this.programs[r.programID]
        Object.assign(p, r)
        this.updateProgramDiv(p.tmpl, r)
      }
    }
  }

  handleCEXNote (note: CEXNote): void {
    this.updateCEXSelector()
    if (this.currentForm === this.page.balancesForm &&
      this.page.balancesCexName.textContent === note.cex.name) {
      this.updateBalancesTable(note.cex.name)
    }
  }

  setCreationBase (symbol: string) {
    const counterAsset = this.currentMarket.quotesymbol
    const markets = sortedMarkets()
    const options: HostedMarket[] = []
    // Best option: find an exact match.
    for (const mkt of markets) if (mkt.basesymbol === symbol && mkt.quotesymbol === counterAsset) options.push(mkt)
    // Next best option: same assets, reversed order.
    for (const mkt of markets) if (mkt.quotesymbol === symbol && mkt.basesymbol === counterAsset) options.push(mkt)
    // If we have exact matches, we're done.
    if (options.length > 0) return this.setMarket(options)
    // No exact matches. Must have selected a ghost-class market. Next best
    // option will be the first market where the selected asset is a base asset.
    for (const mkt of markets) if (mkt.basesymbol === symbol) return this.setMarket([mkt])
    // Last option: Market where this is the quote asset.
    for (const mkt of markets) if (mkt.quotesymbol === symbol) return this.setMarket([mkt])
  }

  setCreationQuote (symbol: string) {
    const counterAsset = this.currentMarket.basesymbol
    const markets = sortedMarkets()
    const options: HostedMarket[] = []
    for (const mkt of markets) if (mkt.quotesymbol === symbol && mkt.basesymbol === counterAsset) options.push(mkt)
    for (const mkt of markets) if (mkt.basesymbol === symbol && mkt.quotesymbol === counterAsset) options.push(mkt)
    if (options.length > 0) return this.setMarket(options)
    for (const mkt of markets) if (mkt.quotesymbol === symbol) return this.setMarket([mkt])
    for (const mkt of markets) if (mkt.basesymbol === symbol) return this.setMarket([mkt])
  }

  async createArbBot (appPW: string): Promise<void> {
    const { page, currentMarket } = this
    Doc.hide(page.createArbErr)

    const setError = (s: string) => {
      page.createArbErr.textContent = s
      Doc.show(page.createArbErr)
    }

    const arbEngineCfg : ArbEngineCfg = {
      cexName: page.cexSelector.value || '',
      profitTrigger: this.arbRanges.profitTrigger,
      maxActiveArbs: Math.round(this.arbRanges.maxActiveArbs),
      numEpochsLeaveOpen: Math.round(this.arbRanges.numEpochsLeaveOpen)
    }

    const makerProgram : MakerProgram = {
      host: currentMarket.host,
      baseID: currentMarket.baseid,
      quoteID: currentMarket.quoteid,
      arbEngineCfg: arbEngineCfg
    }

    const req = {
      botType: 'MakerV0',
      program: makerProgram,
      programID: 0,
      appPW: appPW
    }

    let endpoint = '/api/createbot'

    if (this.editProgram !== null) {
      req.programID = this.editProgram.programID
      endpoint = '/api/updatebotprogram'
    }

    const loaded = app().loading(page.botCreator)
    const res = await postJSON(endpoint, req)
    loaded()

    if (!app().checkResponse(res)) {
      setError(res.msg)
      return
    }

    this.leaveEditMode()
  }

  async createGapBot (appPW: string): Promise<void> {
    const { page, currentMarket, currentReport } = this

    Doc.hide(page.createGapErr)
    const setError = (s: string) => {
      page.createGapErr.textContent = s
      Doc.show(page.createGapErr)
    }

    const makerProgram : MakerProgram = {
      host: currentMarket.host,
      baseID: currentMarket.baseid,
      quoteID: currentMarket.quoteid
    }

    const lots = parseInt(page.lotsInput.value || '0')
    if (lots === 0) return setError('must specify > 0 lots')

    const gapEngineCfg : GapEngineCfg = Object.assign(this.createOpts)
    gapEngineCfg.lots = lots

    const strategy = page.gapStrategySelect.value
    gapEngineCfg.gapStrategy = strategy ?? ''

    switch (strategy) {
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus: {
        const r = parseFloat(page.absInput.value || '0')
        if (r === 0) return setError('gap must be specified for strategy = absolute')
        else if (currentReport?.basisPrice && r >= currentReport.basisPrice) return setError('gap width cannot be > current spot price')
        gapEngineCfg.gapFactor = r
        break
      }
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        gapEngineCfg.gapFactor = this.gapRanges.gapPercent
        break
      default:
        gapEngineCfg.gapFactor = this.gapRanges.gapMultiplier
    }

    makerProgram.gapEngineCfg = gapEngineCfg

    const req = {
      botType: 'MakerV0',
      program: makerProgram,
      programID: 0,
      appPW: appPW
    }

    let endpoint = '/api/createbot'

    if (this.editProgram !== null) {
      req.programID = this.editProgram.programID
      endpoint = '/api/updatebotprogram'
    } else {
      if (!this.currentReport || this.currentReport.basisPrice === 0) {
        if (this.specifiedPrice === 0) {
          setError('price must be set manually')
          return
        }
        gapEngineCfg.manualRate = this.specifiedPrice
      }
    }

    const loaded = app().loading(page.botCreator)
    const res = await postJSON(endpoint, req)
    loaded()

    if (!app().checkResponse(res)) {
      page.createGapErr.textContent = res.msg
      Doc.show(page.createGapErr)
      return
    }

    this.leaveEditMode()

    page.lotsInput.value = ''
  }

  newWalletCreated (assetID: number) {
    const m = this.currentMarket
    if (assetID === m.baseid) this.setCreationBase(m.basesymbol)
    else if (assetID === m.quoteid) this.setCreationQuote(m.quotesymbol)
    else return
    this.closePopups()
  }
}

function sortedMarkets (): HostedMarket[] {
  const mkts: HostedMarket[] = []
  const convertMarkets = (xc: Exchange): HostedMarket[] => {
    return Object.values(xc.markets).map((mkt: Market) => Object.assign({ host: xc.host }, mkt))
  }
  for (const xc of Object.values(app().user.exchanges)) mkts.push(...convertMarkets(xc))
  mkts.sort((a: Market, b: Market) => {
    if (!a.spot) {
      if (!b.spot) return a.name.localeCompare(b.name)
      return -1
    }
    if (!b.spot) return 1
    // Sort by lots.
    return b.spot.vol24 / b.lotsize - a.spot.vol24 / a.lotsize
  })
  return mkts
}

function createOption (opt: BaseOption): OrderOption {
  return {
    key: opt.key,
    displayname: opt.displayname,
    description: opt.description,
    default: opt.default,
    max: opt.max,
    min: opt.min,
    noecho: false,
    isboolean: false,
    isdate: false,
    disablewhenactive: false,
    isBirthdayConfig: false,
    noauth: false
  }
}

function createXYRange (baseOpt: BaseOption, xyRange: XYRange): OrderOption {
  const opt = createOption(baseOpt)
  opt.xyRange = xyRange
  return opt
}

const ExchangeNames: Record<string, string> = {
  'binance.com': 'Binance',
  'coinbase.com': 'Coinbase',
  'bittrex.com': 'Bittrex',
  'hitbtc.com': 'HitBTC',
  'exmo.com': 'EXMO'
}
