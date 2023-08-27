import {
  app,
  PageElement,
  Market,
  Exchange,
  BotConfig,
  MarketWithHost,
  BotStartStopNote,
  MMStartStopNote,
  BalanceNote,
  SupportedAsset
} from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { setOptionTemplates } from './opts'
import { bind as bindForm, NewWalletForm } from './forms'

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

    Doc.bind(page.addBotBtn, 'click', () => { this.showAddBotForm() })
    Doc.bind(page.startBotsBtn, 'click', () => { this.showPWSubmitForm() })
    Doc.bind(page.stopBotsBtn, 'click', () => { this.stopBots() })
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

    const status = await app().getMarketMakingStatus()
    const running = status.running
    const botConfigs = this.botConfigs = await app().getMarketMakingConfig()
    app().registerNoteFeeder({
      botstartstop: (note: BotStartStopNote) => { this.handleBotStartStopNote(note) },
      mmstartstop: (note: MMStartStopNote) => { this.handleMMStartStopNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) }
    })

    const noBots = !botConfigs || botConfigs.length === 0
    Doc.setVis(noBots, page.noBotsHeader)
    Doc.setVis(!noBots, page.botTable)
    if (noBots) return

    Doc.setVis(running, page.stopBotsBtn)
    Doc.setVis(!running, page.startBotsBtn, page.addBotBtn)
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
    Doc.setVis(note.running, page.stopBotsBtn, page.runningHeader)
    Doc.setVis(!note.running, page.startBotsBtn, page.addBotBtn, page.enabledHeader,
      page.baseBalanceHeader, page.quoteBalanceHeader, page.removeHeader)
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
        if (marketStr(cfg.host, cfg.baseAsset, cfg.quoteAsset) === mktID) return cfg
      }
    }
    const tableRows = this.page.botTableBody.children
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      const rowTmpl = Doc.parseTemplate(row)
      const cfg = getBotConfig(row.id)
      if (!cfg) continue
      if (cfg.baseAsset === note.assetID) {
        rowTmpl.baseBalance.textContent = this.walletBalanceStr(note.assetID, cfg.baseBalance)
      } else if (cfg.quoteAsset === note.assetID) {
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

    const mmCfg = await app().getMarketMakingConfig()
    const existingMarkets : Record<string, boolean> = {}
    for (const cfg of mmCfg) {
      existingMarkets[marketStr(cfg.host, cfg.baseAsset, cfg.quoteAsset)] = true
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
    console.log(JSON.stringify(filteredMkts))
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
      row.id = marketStr(botCfg.host, botCfg.baseAsset, botCfg.quoteAsset)
      const rowTmpl = Doc.parseTemplate(row)
      const thisBotRunning = runningBots.some((bot) => {
        return bot.host === botCfg.host &&
          bot.base === botCfg.baseAsset &&
          bot.quote === botCfg.quoteAsset
      })
      this.setTableRowRunning(rowTmpl, running, thisBotRunning)

      const baseSymbol = app().assets[botCfg.baseAsset].symbol
      const quoteSymbol = app().assets[botCfg.quoteAsset].symbol
      const baseLogoPath = Doc.logoPath(baseSymbol)
      const quoteLogoPath = Doc.logoPath(quoteSymbol)

      rowTmpl.enabledCheckbox.checked = !botCfg.disabled
      rowTmpl.enabledCheckbox.onclick = async () => {
        app().setMarketMakingEnabled(botCfg.host, botCfg.baseAsset, botCfg.quoteAsset, !!rowTmpl.enabledCheckbox.checked)
      }
      rowTmpl.host.textContent = botCfg.host
      rowTmpl.baseMktLogo.src = baseLogoPath
      rowTmpl.quoteMktLogo.src = quoteLogoPath
      rowTmpl.baseSymbol.textContent = baseSymbol.toUpperCase()
      rowTmpl.quoteSymbol.textContent = quoteSymbol.toUpperCase()
      rowTmpl.botType.textContent = 'Market Maker'
      rowTmpl.baseBalance.textContent = this.walletBalanceStr(botCfg.baseAsset, botCfg.baseBalance)
      rowTmpl.quoteBalance.textContent = this.walletBalanceStr(botCfg.quoteAsset, botCfg.quoteBalance)
      rowTmpl.baseBalanceLogo.src = baseLogoPath
      rowTmpl.quoteBalanceLogo.src = quoteLogoPath
      rowTmpl.remove.onclick = async () => {
        await app().removeMarketMakingConfig(botCfg)
        row.remove()
        const mmCfg = await app().getMarketMakingConfig()
        const noBots = !mmCfg || !mmCfg.length
        Doc.setVis(noBots, page.noBotsHeader)
        Doc.setVis(!noBots, page.botTable)
      }
      rowTmpl.settings.onclick = () => {
        app().loadPage(`mmsettings?host=${botCfg.host}&base=${botCfg.baseAsset}&quote=${botCfg.quoteAsset}`)
      }
      page.botTableBody.appendChild(row)
    }
  }

  async showAddBotForm () {
    const sortedMarkets = await this.sortedMarkets()
    if (sortedMarkets.length === 0) return
    const initialMarkets = []
    const base = sortedMarkets[0].basesymbol
    const quote = sortedMarkets[0].quotesymbol
    for (const mkt of sortedMarkets) if (mkt.basesymbol === base && mkt.quotesymbol === quote) initialMarkets.push(mkt)
    this.setMarket(initialMarkets)
    this.showForm(this.page.addBotForm)
  }

  async showPWSubmitForm () {
    const page = this.page
    this.showForm(page.pwForm)
  }

  async startBots () {
    const page = this.page
    try {
      const pw = page.pwInput.value
      this.page.pwInput.value = ''
      this.closePopups()
      await app().startMarketMaking(pw || '')
    } catch (e) {
      page.mmErr.textContent = e.message
      Doc.show(page.mmErr)
      setTimeout(() => { Doc.hide(page.mmErr) }, 3000)
    }
  }

  async stopBots () {
    await app().stopMarketMaking()
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
