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
  SupportedAsset
} from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { setOptionTemplates, XYRangeOption } from './opts'
import State from './state'
import { bind as bindForm, NewWalletForm } from './forms'
import { RateEncodingFactor } from './orderutil'

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

const animationLength = 300

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  data: any
  createOpts: Record<string, number>
  gapRanges: Record<string, number>
  currentMarket: HostedMarket
  currentForm: PageElement
  keyup: (e: KeyboardEvent) => void
  programs: Record<number, LiveProgram>
  editProgram: BotReport | null
  gapMultiplierOpt: XYRangeOption
  gapPercentOpt: XYRangeOption
  driftToleranceOpt: XYRangeOption
  biasOpt: XYRangeOption
  weightOpt: XYRangeOption
  pwHandler: ((pw: string) => Promise<void>) | null
  newWalletForm: NewWalletForm
  specifiedPrice: number
  currentReport: MarketReport | null

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

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })

    setOptionTemplates(page)

    Doc.cleanTemplates(page.assetRowTmpl, page.booleanOptTmpl, page.rangeOptTmpl,
      page.orderOptTmpl, page.runningProgramTmpl, page.oracleTmpl)

    const selectClicked = (e: MouseEvent, isBase: boolean): void => {
      e.stopPropagation()
      const select = isBase ? page.baseSelect : page.quoteSelect
      const m = Doc.descendentMetrics(main, select)
      page.assetDropdown.style.left = `${m.bodyLeft}px`
      page.assetDropdown.style.top = `${m.bodyTop}px`

      const counterAsset = isBase ? this.currentMarket.quoteid : this.currentMarket.baseid
      const clickedSymbol = isBase ? this.currentMarket.basesymbol : this.currentMarket.quotesymbol

      // Look through markets for other base assets for the counter asset.
      const matches: Set<SupportedAsset> = new Set()
      const otherAssets: Set<SupportedAsset> = new Set()

      for (const mkt of sortedMarkets()) {
        otherAssets.add(app().assets[mkt.baseid])
        otherAssets.add(app().assets[mkt.quoteid])
        const [firstID, secondID] = isBase ? [mkt.quoteid, mkt.baseid] : [mkt.baseid, mkt.quoteid]
        if (firstID === counterAsset) matches.add(app().assets[secondID])
        else if (secondID === counterAsset) matches.add(app().assets[firstID])
      }

      const options = Array.from(matches)
      options.sort((a: SupportedAsset, b: SupportedAsset) => a.symbol.localeCompare(b.symbol))
      for (const symbol of options) otherAssets.delete(symbol)
      const nonOptions = Array.from(otherAssets)
      nonOptions.sort((a: SupportedAsset, b: SupportedAsset) => a.symbol.localeCompare(b.symbol))

      Doc.empty(page.assetDropdown)
      const addOptions = (assets: SupportedAsset[], avail: boolean): void => {
        for (const a of assets) {
          const row = this.assetRow(a)
          Doc.bind(row, 'click', (e: MouseEvent) => {
            e.stopPropagation()
            if (a.symbol === clickedSymbol) return this.hideAssetDropdown() // no change
            this.leaveEditMode()
            if (isBase) this.setCreationBase(a.symbol)
            else this.setCreationQuote(a.symbol)
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

    this.gapMultiplierOpt = new XYRangeOption(gapMultiplierOption, '', this.gapRanges, () => this.createOptsUpdated())
    this.gapPercentOpt = new XYRangeOption(gapPercentOption, '', this.gapRanges, () => this.createOptsUpdated())
    this.driftToleranceOpt = new XYRangeOption(driftToleranceOption, '', this.createOpts, () => this.createOptsUpdated())
    this.biasOpt = new XYRangeOption(oracleBiasOption, '', this.createOpts, () => this.createOptsUpdated())
    this.weightOpt = new XYRangeOption(oracleWeightOption, '', this.createOpts, () => this.createOptsUpdated())

    page.options.appendChild(this.gapMultiplierOpt.node)
    Doc.hide(this.gapMultiplierOpt.node) // Default is GapStrategyPercentPlus
    page.options.appendChild(this.gapPercentOpt.node)
    page.options.appendChild(this.driftToleranceOpt.node)
    page.options.appendChild(this.weightOpt.node)
    page.options.appendChild(this.biasOpt.node)

    Doc.bind(page.showAdvanced, 'click', () => {
      State.storeLocal(State.optionsExpansionLK, true)
      Doc.hide(page.showAdvanced)
      Doc.show(page.hideAdvanced, page.options)
    })

    Doc.bind(page.hideAdvanced, 'click', () => {
      State.storeLocal(State.optionsExpansionLK, false)
      Doc.hide(page.hideAdvanced, page.options)
      Doc.show(page.showAdvanced)
    })

    Doc.bind(page.runBttn, 'click', this.authedRoute(async (pw: string): Promise<void> => this.createBot(pw)))

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
      page.basisPrice.textContent = Doc.formatFourSigFigs(v)
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
      Doc.show(page.hideAdvanced, page.options)
      Doc.hide(page.showAdvanced)
    }

    app().registerNoteFeeder({
      bot: (n: BotNote) => { this.handleBotNote(n) }
    })

    this.setMarket([mkt ?? sortedMarkets()[0]])
    this.populateRunningPrograms()
    page.createBox.classList.remove('invisible')
    page.programsBox.classList.remove('invisible')
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement): Promise<void> {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.pwForm, page.newWalletForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  closePopups (): void {
    this.pwHandler = null
    this.page.pwInput.value = ''
    Doc.hide(this.page.forms)
  }

  hideAssetDropdown (): void {
    const page = this.page
    page.assetDropdown.scrollTop = 0
    Doc.hide(page.assetDropdown)
  }

  assetRow (asset: SupportedAsset): PageElement {
    const row = this.page.assetRowTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(row)
    tmpl.logo.src = Doc.logoPath(asset.symbol)
    Doc.empty(tmpl.symbol)
    tmpl.symbol.appendChild(Doc.symbolize(asset))
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
      page.basisPrice.textContent = Doc.formatFourSigFigs(r.basisPrice)
      Doc.show(page.absMaxBox, page.basisPrice)
      Doc.hide(page.manualPriceInput, page.noFiatBox)
      this.page.gapFactorMax.textContent = Doc.formatFourSigFigs(r.basisPrice)
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
      page.breakEvenGap.textContent = Doc.formatFourSigFigs(r.breakEvenSpread)
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
    page.baseSelect.appendChild(this.assetRow(app().assets[mkt.baseid]))
    page.quoteSelect.appendChild(this.assetRow(app().assets[mkt.quoteid]))
    this.hideAssetDropdown()

    const [b, q] = [app().assets[mkt.baseid], app().assets[mkt.quoteid]]

    const addMarketSelect = (mkt: HostedMarket, el: PageElement) => {
      const [b, q] = [app().assets[mkt.baseid], app().assets[mkt.quoteid]]
      Doc.setContent(el,
        Doc.symbolize(b),
        new Text('-') as any,
        Doc.symbolize(q),
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

    Doc.setContent(page.lotEstBaseSymbol, Doc.symbolize(b))
    Doc.setContent(page.lotEstQuoteSymbol, Doc.symbolize(q))

    const setNoWallet = (a: SupportedAsset, noWalletBox: PageElement): boolean => {
      Doc.show(noWalletBox)
      const tmpl = Doc.parseTemplate(noWalletBox)
      tmpl.assetLogo.src = Doc.logoPath(a.symbol)
      tmpl.assetName.textContent = a.name
      return false
    }

    Doc.hide(
      page.lotEstQuoteBox, page.lotEstQuoteNoWallet, page.lotEstBaseBox, page.lotEstBaseNoWallet, page.availHeader,
      page.lotEstimateBox, page.marketInfo, page.oraclesBox, page.lotsBox, page.options, page.advancedBox
    )
    if (b.wallet && q.wallet) {
      Doc.show(
        page.lotEstQuoteBox, page.lotEstBaseBox, page.availHeader, page.fetchingMarkets,
        page.lotsBox, page.advancedBox
      )
      if (State.fetchLocal(State.optionsExpansionLK)) Doc.show(page.options)
      const loaded = app().loading(page.options)
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
      tmpl.volume.textContent = Doc.formatFourSigFigs(o.usdVol)
      const price = (o.bestBuy + o.bestSell) / 2
      weightedSum += o.usdVol * price
      weight += o.usdVol
      tmpl.price.textContent = Doc.formatFourSigFigs((o.bestBuy + o.bestSell) / 2)
    }
    page.avgPrice.textContent = Doc.formatFourSigFigs(weightedSum / weight)
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
    page.lotsInput.value = String(pgm.lots)
    createOpts.oracleWeighting = pgm.oracleWeighting
    this.weightOpt.setValue(pgm.oracleWeighting)
    createOpts.oracleBias = pgm.oracleBias
    this.biasOpt.setValue(pgm.oracleBias)
    createOpts.driftTolerance = pgm.driftTolerance
    this.driftToleranceOpt.setValue(pgm.driftTolerance)
    page.gapStrategySelect.value = pgm.gapStrategy
    this.updateGapStrategyInputVisibility()
    this.createOptsUpdated()

    switch (pgm.gapStrategy) {
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        this.gapPercentOpt.setValue(pgm.gapFactor)
        break
      case GapStrategyMultiplier:
        this.gapMultiplierOpt.setValue(pgm.gapFactor)
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
    tmpl.base.appendChild(this.assetRow(b))
    tmpl.quote.appendChild(this.assetRow(q))
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
    tmpl.lots.textContent = String(pgm.lots)
    tmpl.boost.textContent = `${(pgm.gapFactor * 100).toFixed(1)}%`
    tmpl.driftTolerance.textContent = `${(pgm.driftTolerance * 100).toFixed(2)}%`
    tmpl.oracleWeight.textContent = `${(pgm.oracleWeighting * 100).toFixed(0)}%`
    tmpl.oracleBias.textContent = `${(pgm.oracleBias * 100).toFixed(1)}%`
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

  async createBot (appPW: string): Promise<void> {
    const { page, currentMarket, currentReport } = this

    Doc.hide(page.createErr)
    const setError = (s: string) => {
      page.createErr.textContent = s
      Doc.show(page.createErr)
    }

    const lots = parseInt(page.lotsInput.value || '0')
    if (lots === 0) return setError('must specify > 0 lots')
    const makerProgram = Object.assign({
      host: currentMarket.host,
      baseID: currentMarket.baseid,
      quoteID: currentMarket.quoteid
    }, this.createOpts, { lots, gapStrategy: '' })

    const strategy = page.gapStrategySelect.value
    makerProgram.gapStrategy = strategy ?? ''

    const req = {
      botType: 'MakerV0',
      program: makerProgram,
      programID: 0,
      appPW: appPW,
      manualRate: 0
    }

    switch (strategy) {
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus: {
        const r = parseFloat(page.absInput.value || '0')
        if (r === 0) return setError('gap must be specified for strategy = absolute')
        else if (currentReport?.basisPrice && r >= currentReport.basisPrice) return setError('gap width cannot be > current spot price')
        makerProgram.gapFactor = r
        break
      }
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        makerProgram.gapFactor = this.gapRanges.gapPercent
        break
      default:
        makerProgram.gapFactor = this.gapRanges.gapMultiplier
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
        req.program.manualRate = this.specifiedPrice
      }
    }

    const loaded = app().loading(page.botCreator)
    const res = await postJSON(endpoint, req)
    loaded()

    if (!app().checkResponse(res)) {
      page.createErr.textContent = res.msg
      Doc.show(page.createErr)
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
    isBirthdayConfig: false
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
