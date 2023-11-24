import {
  app,
  PageElement,
  Market,
  Exchange,
  BotConfig,
  CEXConfig,
  MarketWithHost,
  BotStartStopNote,
  MMStartStopNote,
  BalanceNote,
  SupportedAsset,
  MarketMakingConfig,
  MarketMakingStatus,
  CEXMarket
} from './registry'
import { getJSON, postJSON } from './http'
import Doc from './doc'
import State from './state'
import BasePage from './basepage'
import { setOptionTemplates } from './opts'
import { bind as bindForm, NewWalletForm } from './forms'
import * as intl from './locales'

interface CEXDisplayInfo {
  name: string
  logo: string
}

export const CEXDisplayInfos: Record<string, CEXDisplayInfo> = {
  'Binance': {
    name: 'Binance',
    logo: 'binance.com.png'
  },
  'BinanceUS': {
    name: 'Binance U.S.',
    logo: 'binance.us.png'
  }
}

interface HostedMarket extends Market {
  host: string
}

const animationLength = 300

function marketStr (host: string, baseID: number, quoteID: number): string {
  return `${host}-${baseID}-${quoteID}`
}

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  currentForm: HTMLElement
  keyup: (e: KeyboardEvent) => void
  currentNewMarket: HostedMarket
  newWalletForm: NewWalletForm
  botConfigs: BotConfig[]

  constructor (main: HTMLElement) {
    super()

    const page = this.page = Doc.idDescendants(main)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        this.closePopups()
      }
    }

    this.newWalletForm = new NewWalletForm(
      page.newWalletForm,
      () => { this.addBotSubmit() }
    )

    Doc.bind(page.addBotBtnNoExisting, 'click', () => { this.showAddBotForm() })
    Doc.bind(page.addBotBtnWithExisting, 'click', () => { this.showAddBotForm() })
    Doc.bind(page.startBotsBtn, 'click', () => { this.start() })
    Doc.bind(page.stopBotsBtn, 'click', () => { MM.stop() })
    Doc.bind(page.hostSelect, 'change', () => { this.selectMarketHost() })
    bindForm(page.addBotForm, page.addBotSubmit, () => { this.addBotSubmit() })
    bindForm(page.pwForm, page.pwSubmit, () => this.startBots())

    setOptionTemplates(page)

    Doc.cleanTemplates(page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl,
      page.assetRowTmpl, page.botTableRowTmpl)

    Doc.bind(page.baseSelect, 'click', (e: MouseEvent) => this.selectMarketAssetClicked(e, true))
    Doc.bind(page.quoteSelect, 'click', (e: MouseEvent) => this.selectMarketAssetClicked(e, false))

    this.setup()
  }

  async setup () {
    const page = this.page

    const status = await MM.status()
    const running = status.running
    const marketMakingCfg = await MM.config()

    const botConfigs = this.botConfigs = marketMakingCfg.botConfigs || []
    app().registerNoteFeeder({
      botstartstop: (note: BotStartStopNote) => { this.handleBotStartStopNote(note) },
      mmstartstop: (note: MMStartStopNote) => { this.handleMMStartStopNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) }
    })

    const noBots = !botConfigs || botConfigs.length === 0
    Doc.setVis(noBots, page.noBots)
    Doc.setVis(!noBots, page.botTable, page.onOff)
    if (noBots) return

    page.onIndicator.classList.add(running ? 'on' : 'off')
    page.onIndicator.classList.remove(running ? 'off' : 'on')
    Doc.setVis(running, page.stopBotsBtn, page.onMsg)
    Doc.setVis(!running, page.startBotsBtn, page.offMsg)
    this.setupBotTable(botConfigs, running, status.runningBots)
  }

  handleBotStartStopNote (note: BotStartStopNote) {
    const tableRows = this.page.botTableBody.children
    const rowID = marketStr(note.host, note.base, note.quote)
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      if (row.id === rowID) {
        const rowTmpl = Doc.parseTemplate(row)
        this.setTableRowRunning(rowTmpl, undefined, note.running)
        return
      }
    }
  }

  handleMMStartStopNote (note: MMStartStopNote) {
    const page = this.page
    page.onIndicator.classList.add(note.running ? 'on' : 'off')
    page.onIndicator.classList.remove(note.running ? 'off' : 'on')
    Doc.setVis(note.running, page.stopBotsBtn, page.runningHeader, page.onMsg)
    Doc.setVis(!note.running, page.startBotsBtn, page.addBotBtnNoExisting, page.enabledHeader,
      page.baseBalanceHeader, page.quoteBalanceHeader, page.removeHeader, page.offMsg)
    const tableRows = page.botTableBody.children
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      const rowTmpl = Doc.parseTemplate(row)
      this.setTableRowRunning(rowTmpl, note.running, undefined)
    }
  }

  handleBalanceNote (note: BalanceNote) {
    const getBotConfig = (mktID: string): BotConfig | undefined => {
      for (const cfg of this.botConfigs) {
        if (marketStr(cfg.host, cfg.baseID, cfg.quoteID) === mktID) return cfg
      }
    }
    const tableRows = this.page.botTableBody.children
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      const rowTmpl = Doc.parseTemplate(row)
      const cfg = getBotConfig(row.id)
      if (!cfg) continue
      if (cfg.baseID === note.assetID) {
        rowTmpl.baseBalance.textContent = this.walletBalanceStr(note.assetID, cfg.baseBalance)
      } else if (cfg.quoteID === note.assetID) {
        rowTmpl.quoteBalance.textContent = this.walletBalanceStr(note.assetID, cfg.quoteBalance)
      }
    }
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  /**
   * The functions below all handle the interactions on the add bot form.
   */

  async selectMarketAssetClicked (e: MouseEvent, isBase: boolean): Promise<void> {
    const page = this.page
    e.stopPropagation()
    const select = isBase ? page.baseSelect : page.quoteSelect
    const m = Doc.descendentMetrics(page.addBotForm, select)
    page.assetDropdown.style.left = `${m.bodyLeft}px`
    page.assetDropdown.style.top = `${m.bodyTop}px`

    const counterAsset = isBase ? this.currentNewMarket.quoteid : this.currentNewMarket.baseid
    const clickedSymbol = isBase ? this.currentNewMarket.basesymbol : this.currentNewMarket.quotesymbol

    // Look through markets for other base assets for the counter asset.
    const matches: Set<string> = new Set()
    const otherAssets: Set<string> = new Set()

    for (const mkt of await this.sortedMarkets()) {
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
        const assetID = Doc.bipIDFromSymbol(symbol)
        if (assetID === undefined) continue // unsupported asset
        const asset = app().assets[assetID]
        const row = this.assetRow(asset)
        Doc.bind(row, 'click', (e: MouseEvent) => {
          e.stopPropagation()
          if (symbol === clickedSymbol) return this.hideAssetDropdown() // no change
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

  selectMarketHost (): void {
    if (this.currentNewMarket && this.page.hostSelect.value) {
      this.currentNewMarket.host = this.page.hostSelect.value
    }
  }

  assetRow (asset: SupportedAsset): PageElement {
    const row = this.page.assetRowTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(row)
    tmpl.logo.src = Doc.logoPath(asset.symbol)
    Doc.empty(tmpl.symbol)
    tmpl.symbol.appendChild(Doc.symbolize(asset))
    return row
  }

  hideAssetDropdown (): void {
    const page = this.page
    page.assetDropdown.scrollTop = 0
    Doc.hide(page.assetDropdown)
  }

  async setMarket (mkts: HostedMarket[]): Promise<void> {
    const page = this.page

    const mkt = mkts[0]
    this.currentNewMarket = mkt

    Doc.empty(page.baseSelect, page.quoteSelect)

    const baseAsset = app().assets[mkt.baseid]
    const quoteAsset = app().assets[mkt.quoteid]

    page.baseSelect.appendChild(this.assetRow(baseAsset))
    page.quoteSelect.appendChild(this.assetRow(quoteAsset))
    this.hideAssetDropdown()

    const addHostSelect = (mkt: HostedMarket, el: PageElement) => {
      Doc.setContent(el,
        Doc.symbolize(baseAsset),
        new Text('-') as any,
        Doc.symbolize(quoteAsset),
        new Text(' @ ') as any,
        new Text(mkt.host) as any
      )
    }

    Doc.hide(page.hostSelect, page.marketOneChoice)
    if (mkts.length === 1) {
      Doc.show(page.marketOneChoice)
      addHostSelect(mkt, page.marketOneChoice)
    } else {
      Doc.show(page.hostSelect)
      Doc.empty(page.hostSelect)
      for (const mkt of mkts) {
        const opt = document.createElement('option')
        page.hostSelect.appendChild(opt)
        opt.value = `${mkt.host}`
        addHostSelect(mkt, opt)
      }
    }
  }

  async setCreationBase (symbol: string) {
    const counterAsset = this.currentNewMarket.quotesymbol
    const markets = await this.sortedMarkets()
    const getAllMarketsWithAssets = (base: string, quote: string) : HostedMarket[] => {
      const mkts: HostedMarket[] = []
      for (const mkt of markets) if (mkt.basesymbol === base && mkt.quotesymbol === quote) mkts.push(mkt)
      return mkts
    }
    // Best option: find an exact match.
    let options = getAllMarketsWithAssets(symbol, counterAsset)
    if (options.length > 0) return this.setMarket(options)

    // Next best option: same assets, reversed order.
    options = getAllMarketsWithAssets(counterAsset, symbol)
    if (options.length > 0) return this.setMarket(options)

    // No exact matches. Must have selected a ghost-class market. Next best
    // option will be the first market where the selected asset is a base asset.
    let newCounterAsset: string | undefined
    for (const mkt of markets) if (mkt.basesymbol === symbol) newCounterAsset = mkt.quotesymbol
    if (newCounterAsset) {
      return this.setMarket(getAllMarketsWithAssets(symbol, newCounterAsset))
    }

    // Last option: Market where this is the quote asset.
    let newBaseAsset : string | undefined
    for (const mkt of markets) if (mkt.quotesymbol === symbol) newBaseAsset = mkt.basesymbol
    if (newBaseAsset) {
      return this.setMarket(getAllMarketsWithAssets(newBaseAsset, symbol))
    }

    console.error(`No market found for ${symbol}`)
  }

  async setCreationQuote (symbol: string) {
    const counterAsset = this.currentNewMarket.basesymbol
    const markets = await this.sortedMarkets()
    const getAllMarketsWithAssets = (base: string, quote: string) : HostedMarket[] => {
      const mkts: HostedMarket[] = []
      for (const mkt of markets) if (mkt.basesymbol === base && mkt.quotesymbol === quote) mkts.push(mkt)
      return mkts
    }
    // Best option: find an exact match.
    let options = getAllMarketsWithAssets(counterAsset, symbol)
    if (options.length > 0) return this.setMarket(options)

    // Next best option: same assets, reversed order.
    options = getAllMarketsWithAssets(symbol, counterAsset)
    if (options.length > 0) return this.setMarket(options)

    // No exact matches. Must have selected a ghost-class market. Next best
    // option will be the first market where the selected asset is a base asset.
    let newCounterAsset: string | undefined
    for (const mkt of markets) if (mkt.quotesymbol === symbol) newCounterAsset = mkt.quotesymbol
    if (newCounterAsset) {
      return this.setMarket(getAllMarketsWithAssets(newCounterAsset, symbol))
    }

    // Last option: Market where this is the quote asset.
    let newQuoteAsset : string | undefined
    for (const mkt of markets) if (mkt.basesymbol === symbol) newQuoteAsset = mkt.quotesymbol
    if (newQuoteAsset) {
      return this.setMarket(getAllMarketsWithAssets(symbol, newQuoteAsset))
    }

    console.error(`No market found for ${symbol}`)
  }

  /*
  * sortedMarkets returns a list of markets that do not yet have a market maker.
  */
  async sortedMarkets (): Promise<HostedMarket[]> {
    const mkts: HostedMarket[] = []
    const convertMarkets = (xc: Exchange): HostedMarket[] => {
      if (!xc.markets) return []
      return Object.values(xc.markets).map((mkt: Market) => Object.assign({ host: xc.host }, mkt))
    }
    for (const xc of Object.values(app().user.exchanges)) mkts.push(...convertMarkets(xc))

    const mmCfg = await MM.config()
    const botCfgs = mmCfg.botConfigs || []
    const existingMarkets : Record<string, boolean> = {}
    for (const cfg of botCfgs) {
      existingMarkets[marketStr(cfg.host, cfg.baseID, cfg.quoteID)] = true
    }
    const filteredMkts = mkts.filter((mkt) => {
      return !existingMarkets[marketStr(mkt.host, mkt.baseid, mkt.quoteid)]
    })
    filteredMkts.sort((a: Market, b: Market) => {
      if (!a.spot) {
        if (!b.spot) return a.name.localeCompare(b.name)
        return -1
      }
      if (!b.spot) return 1
      // Sort by lots.
      return b.spot.vol24 / b.lotsize - a.spot.vol24 / a.lotsize
    })
    return filteredMkts
  }

  addBotSubmit () {
    const currMkt = this.currentNewMarket
    if (!app().walletMap[currMkt.baseid]) {
      this.newWalletForm.setAsset(currMkt.baseid)
      this.showForm(this.page.newWalletForm)
      return
    }
    if (!app().walletMap[currMkt.quoteid]) {
      this.newWalletForm.setAsset(currMkt.quoteid)
      this.showForm(this.page.newWalletForm)
      return
    }

    app().loadPage(`mmsettings?host=${currMkt.host}&base=${currMkt.baseid}&&quote=${currMkt.quoteid}`)
  }

  /*
   * walletBalanceStr returns a string like "50% - 0.0001 BTC" representing
   * the percentage of a wallet's balance selected in the market maker setting,
   * and the amount of that asset in the wallet.
   */
  walletBalanceStr (assetID: number, percentage: number): string {
    const asset = app().assets[assetID]
    const wallet = asset.wallet
    const balance = wallet.balance.available
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue((balance * percentage) / 100, unitInfo)
    return `${percentage}% - ${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * setTableRowRunning sets the visibility of elements on a market row depending
   * on whether the entire market maker is running, and specifically whether the
   * the current bot is running.
   */
  setTableRowRunning (rowTmpl: Record<string, PageElement>, mmRunning: boolean | undefined, thisBotRunning: boolean | undefined) {
    if (mmRunning !== undefined) {
      Doc.setVis(mmRunning, rowTmpl.running)
      Doc.setVis(!mmRunning, rowTmpl.enabled, rowTmpl.baseBalanceTd, rowTmpl.quoteBalanceTd, rowTmpl.removeTd)
    }
    if (thisBotRunning !== undefined) {
      Doc.setVis(thisBotRunning, rowTmpl.runningIcon)
      Doc.setVis(!thisBotRunning, rowTmpl.notRunningIcon)
    }
  }

  /*
   * setupBotTable populates the table of market maker bots.
   */
  setupBotTable (botConfigs: BotConfig[], running: boolean, runningBots: MarketWithHost[]) {
    const page = this.page
    Doc.empty(page.botTableBody)

    Doc.setVis(running, page.runningHeader)
    Doc.setVis(!running, page.enabledHeader, page.baseBalanceHeader, page.quoteBalanceHeader, page.removeHeader)

    for (const botCfg of botConfigs) {
      const row = page.botTableRowTmpl.cloneNode(true) as PageElement
      row.id = marketStr(botCfg.host, botCfg.baseID, botCfg.quoteID)
      const rowTmpl = Doc.parseTemplate(row)
      const thisBotRunning = runningBots.some((bot) => {
        return bot.host === botCfg.host &&
          bot.base === botCfg.baseID &&
          bot.quote === botCfg.quoteID
      })
      this.setTableRowRunning(rowTmpl, running, thisBotRunning)

      const baseSymbol = app().assets[botCfg.baseID].symbol
      const quoteSymbol = app().assets[botCfg.quoteID].symbol
      const baseLogoPath = Doc.logoPath(baseSymbol)
      const quoteLogoPath = Doc.logoPath(quoteSymbol)

      rowTmpl.enabledCheckbox.checked = !botCfg.disabled
      Doc.bind(rowTmpl.enabledCheckbox, 'click', async () => {
        MM.setEnabled(botCfg.host, botCfg.baseID, botCfg.quoteID, !!rowTmpl.enabledCheckbox.checked)
      })
      rowTmpl.host.textContent = botCfg.host
      rowTmpl.baseMktLogo.src = baseLogoPath
      rowTmpl.quoteMktLogo.src = quoteLogoPath
      rowTmpl.baseSymbol.textContent = baseSymbol.toUpperCase()
      rowTmpl.quoteSymbol.textContent = quoteSymbol.toUpperCase()
      rowTmpl.baseBalance.textContent = this.walletBalanceStr(botCfg.baseID, botCfg.baseBalance)
      rowTmpl.quoteBalance.textContent = this.walletBalanceStr(botCfg.quoteID, botCfg.quoteBalance)
      rowTmpl.baseBalanceLogo.src = baseLogoPath
      rowTmpl.quoteBalanceLogo.src = quoteLogoPath
      if (botCfg.arbMarketMakingConfig) {
        rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
        Doc.show(rowTmpl.cexLink)
        const dinfo = CEXDisplayInfos[botCfg.arbMarketMakingConfig.cexName]
        rowTmpl.cexLogo.src = '/img/' + dinfo.logo
        rowTmpl.cexName.textContent = dinfo.name
      } else {
        rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
      }

      Doc.bind(rowTmpl.removeTd, 'click', async () => {
        await MM.removeMarketMakingConfig(botCfg)
        row.remove()
        const mmCfg = await MM.config()
        const noBots = !mmCfg || !mmCfg.botConfigs || mmCfg.botConfigs.length === 0
        Doc.setVis(noBots, page.noBots)
        Doc.setVis(!noBots, page.botTable, page.onOff)
      })
      Doc.bind(rowTmpl.settings, 'click', () => {
        app().loadPage(`mmsettings?host=${botCfg.host}&base=${botCfg.baseID}&quote=${botCfg.quoteID}`)
      })
      page.botTableBody.appendChild(row)
    }
  }

  async showAddBotForm () {
    const sortedMarkets = await this.sortedMarkets()
    if (sortedMarkets.length === 0) return
    const { baseid: baseID, quoteid: quoteID } = sortedMarkets.filter((mkt: HostedMarket) => Boolean(app().assets[mkt.baseid]) && Boolean(app().assets[mkt.quoteid]))[0]
    const initialMarkets = sortedMarkets.filter((mkt: HostedMarket) => mkt.baseid === baseID && mkt.quoteid === quoteID)
    this.setMarket(initialMarkets)
    this.showForm(this.page.addBotForm)
  }

  async start () {
    if (State.passwordIsCached()) this.startBots()
    else this.showForm(this.page.pwForm)
  }

  async startBots () {
    const page = this.page
    Doc.hide(page.mmErr)
    const appPW = page.pwInput.value || ''
    this.page.pwInput.value = ''
    this.closePopups()
    const res = await MM.start(appPW)
    if (!app().checkResponse(res)) {
      page.mmErr.textContent = res.msg
      Doc.show(page.mmErr)
    }
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement): Promise<void> {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.pwForm, page.newWalletForm, page.addBotForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  closePopups (): void {
    Doc.hide(this.page.forms)
  }
}

/*
 * MarketMakerBot is the front end representaion of the server's mm.MarketMaker.
 * MarketMakerBot is a singleton assigned to MM below.
 */
class MarketMakerBot {
  cfg: MarketMakingConfig
  st: MarketMakingStatus | undefined
  mkts: Record<string, CEXMarket[]>

  constructor () {
    this.mkts = {}
  }

  handleStartStopNote (n: MMStartStopNote) {
    if (!this.st) return
    this.st.running = n.running
  }

  /*
   * config returns the curret MarketMakingConfig. config will fetch the
   * MarketMakingConfig once. Future updates occur in updateCEXConfig and
   * updateBotConfig. after a page handler calls config during intitialization,
   * the the config can be accessed directly thr MM.cfg to avoid the async
   * function.
   */
  async config () : Promise<MarketMakingConfig> {
    if (this.cfg) return this.cfg
    const res = await getJSON('/api/marketmakingconfig')
    if (!app().checkResponse(res)) {
      throw new Error('failed to fetch market making config')
    }
    this.cfg = res.cfg
    this.mkts = res.mkts
    return this.cfg
  }

  /*
   * updateBotConfig appends or updates the specified BotConfig, then updates
   * the cached MarketMakingConfig,
   */
  async updateBotConfig (cfg: BotConfig) : Promise<void> {
    const res = await postJSON('/api/updatebotconfig', cfg)
    if (!app().checkResponse(res)) {
      throw res.msg || Error(res)
    }
    this.cfg = res.cfg
  }

  /*
   * updateCEXConfig appends or updates the specified CEXConfig, then updates
   * the cached MarketMakingConfig,
   */
  async updateCEXConfig (cfg: CEXConfig) : Promise<void> {
    const res = await postJSON('/api/updatecexconfig', cfg)
    if (res.err) {
      throw new Error(res.err)
    }
    this.cfg = res.cfg
    this.mkts[cfg.name] = res.mkts || []
  }

  async removeMarketMakingConfig (cfg: BotConfig) : Promise<void> {
    const res = await postJSON('/api/removemarketmakingconfig', {
      host: cfg.host,
      baseAsset: cfg.baseID,
      quoteAsset: cfg.quoteID
    })
    if (res.err) {
      throw new Error(res.err)
    }
    this.cfg = res.cfg
  }

  async report (baseID: number, quoteID: number) {
    return postJSON('/api/marketreport', { baseID, quoteID })
  }

  async setEnabled (host: string, baseID: number, quoteID: number, enabled: boolean) : Promise<void> {
    const botCfgs = this.cfg.botConfigs || []
    const mktCfg = botCfgs.find((cfg : BotConfig) => {
      return cfg.host === host && cfg.baseID === baseID && cfg.quoteID === quoteID
    })
    if (!mktCfg) {
      throw new Error('market making config not found')
    }
    mktCfg.disabled = !enabled
    await this.updateBotConfig(mktCfg)
  }

  async start (appPW: string) {
    return await postJSON('/api/startmarketmaking', { appPW })
  }

  async stop () : Promise<void> {
    await postJSON('/api/stopmarketmaking')
  }

  async status () : Promise<MarketMakingStatus> {
    if (this.st !== undefined) return this.st
    const res = await getJSON('/api/marketmakingstatus')
    if (!app().checkResponse(res)) {
      throw new Error('failed to fetch market making status')
    }
    const status = {} as MarketMakingStatus
    status.running = !!res.running
    status.runningBots = res.runningBots
    this.st = status
    return status
  }

  cexConfigured (cexName: string) {
    return (this.cfg.cexConfigs || []).some((cfg: CEXConfig) => cfg.name === cexName)
  }
}

// MM is the front end representation of the server's mm.MarketMaker.
export const MM = new MarketMakerBot()
