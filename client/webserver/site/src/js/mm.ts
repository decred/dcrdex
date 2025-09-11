import {
  app,
  PageElement,
  MMBotStatus,
  RunStatsNote,
  RunEventNote,
  EpochReportNote,
  CEXProblemsNote,
  MarketWithHost,
  CEXNotification
} from './registry'
import {
  MM,
  CEXDisplayInfo,
  CEXDisplayInfos,
  botTypeBasicArb,
  botTypeArbMM,
  botTypeBasicMM,
  setMarketElements,
  setCexElements,
  BotMarket,
  hostedMarketID,
  RunningMarketMakerDisplay,
  RunningMMDisplayElements
} from './mmutil'
import Doc from './doc'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { Forms, CEXConfigurationForm } from './forms'
import * as intl from './locales'
const mediumBreakpoint = 768

interface CEXRow {
  cexName: string
  tr: PageElement
  tmpl: Record<string, PageElement>
  dinfo: CEXDisplayInfo
}

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  currentForm: HTMLElement
  keyup: (e: KeyboardEvent) => void
  cexConfigForm: CEXConfigurationForm
  bots: Record<string, Bot>
  sortedBots: Bot[]
  cexes: Record<string, CEXRow>
  twoColumn: boolean
  runningMMDisplayElements: RunningMMDisplayElements
  removingCfg: MarketWithHost | undefined

  constructor (main: HTMLElement) {
    super()

    this.bots = {}
    this.sortedBots = []
    this.cexes = {}

    const page = this.page = Doc.idDescendants(main)

    Doc.cleanTemplates(page.botTmpl, page.botRowTmpl, page.exchangeRowTmpl)

    this.forms = new Forms(page.forms)
    this.cexConfigForm = new CEXConfigurationForm(page.cexConfigForm, (cexName: string, success: boolean) => this.cexConfigured(cexName, success))
    this.runningMMDisplayElements = {
      orderReportForm: page.orderReportForm,
      dexBalancesRowTmpl: page.dexBalancesRowTmpl,
      placementRowTmpl: page.placementRowTmpl,
      placementAmtRowTmpl: page.placementAmtRowTmpl
    }
    Doc.cleanTemplates(page.dexBalancesRowTmpl, page.placementRowTmpl, page.placementAmtRowTmpl)

    Doc.bind(page.newBot, 'click', () => { this.newBot() })
    Doc.bind(page.archivedLogsBtn, 'click', () => { app().loadPage('mmarchives') })
    Doc.bind(page.confirmRemoveConfigBttn, 'click', () => { this.removeCfg() })

    this.twoColumn = window.innerWidth >= mediumBreakpoint
    const ro = new ResizeObserver(() => { this.resized() })
    ro.observe(main)

    for (const [cexName, dinfo] of Object.entries(CEXDisplayInfos)) {
      const tr = page.exchangeRowTmpl.cloneNode(true) as PageElement
      page.cexRows.appendChild(tr)
      const tmpl = Doc.parseTemplate(tr)
      const configure = () => {
        this.cexConfigForm.setCEX(cexName)
        this.forms.show(page.cexConfigForm)
      }
      Doc.bind(tmpl.configureBttn, 'click', configure)
      Doc.bind(tmpl.reconfigBttn, 'click', configure)
      Doc.bind(tmpl.errConfigureBttn, 'click', configure)
      const row = this.cexes[cexName] = { tr, tmpl, dinfo, cexName }
      this.updateCexRow(row)
    }

    this.setup()
  }

  resized () {
    const useTwoColumn = window.innerWidth >= 768
    if (useTwoColumn !== this.twoColumn) {
      this.twoColumn = useTwoColumn
      this.clearBotBoxes()
      for (const { div } of this.sortedBots) this.appendBotBox(div)
    }
  }

  async setup () {
    const page = this.page
    const mmStatus = app().mmStatus

    const botConfigs = mmStatus.bots.map((s: MMBotStatus) => s.config)
    app().registerNoteFeeder({
      runstats: (note: RunStatsNote) => { this.handleRunStatsNote(note) },
      runevent: (note: RunEventNote) => {
        const bot = this.bots[hostedMarketID(note.host, note.baseID, note.quoteID)]
        if (bot) return bot.handleRunStats()
      },
      epochreport: (note: EpochReportNote) => {
        const bot = this.bots[hostedMarketID(note.host, note.baseID, note.quoteID)]
        if (bot) bot.handleEpochReportNote(note)
      },
      cexproblems: (note: CEXProblemsNote) => {
        const bot = this.bots[hostedMarketID(note.host, note.baseID, note.quoteID)]
        if (bot) bot.handleCexProblemsNote(note)
      },
      cexnote: (note: CEXNotification) => { this.handleCEXNote(note) }
      // TODO bot start-stop notification
    })

    const noBots = !botConfigs || botConfigs.length === 0
    Doc.setVis(noBots, page.noBots)
    if (noBots) return
    page.noBots.remove()

    const sortedBots = [...mmStatus.bots].sort((a: MMBotStatus, b: MMBotStatus) => {
      if (a.running && !b.running) return -1
      if (b.running && !a.running) return 1
      // If none are running, just do something to get a resonably reproducible
      // sort.
      if (!a.running && !b.running) return (a.config.baseID + a.config.quoteID) - (b.config.baseID + b.config.quoteID)
      // Both are running. Sort by run time.
      return (b.runStats?.startTime ?? 0) - (a.runStats?.startTime ?? 0)
    })

    for (const botStatus of sortedBots) this.addBot(botStatus)
  }

  async handleCEXNote (n: CEXNotification) {
    switch (n.topic) {
      case 'BalanceUpdate':
        return this.handleCEXBalanceUpdate(n.cexName /* , n.note */)
    }
  }

  async handleCEXBalanceUpdate (cexName: string /* , note: CEXBalanceUpdate */) {
    const cexRow = this.cexes[cexName]
    if (cexRow) this.updateCexRow(cexRow)
  }

  async handleRunStatsNote (note: RunStatsNote) {
    const { baseID, quoteID, host } = note
    const bot = this.bots[hostedMarketID(host, baseID, quoteID)]
    if (bot) return bot.handleRunStats()
    this.addBot(app().botStatus(host, baseID, quoteID) as MMBotStatus)
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  addBot (botStatus: MMBotStatus) {
    const { page, bots, sortedBots } = this
    // Make sure the market still exists.
    const { config: { baseID, quoteID, host } } = botStatus
    const [baseSymbol, quoteSymbol] = [app().assets[baseID].symbol, app().assets[quoteID].symbol]
    const mktID = `${baseSymbol}_${quoteSymbol}`
    if (!app().exchanges[host]?.markets[mktID]) return
    const bot = new Bot(this, this.runningMMDisplayElements, botStatus)
    page.botRows.appendChild(bot.row.tr)
    sortedBots.push(bot)
    bots[bot.id] = bot
    this.appendBotBox(bot.div)
  }

  confirmRemoveCfg (mwh: MarketWithHost) {
    const page = this.page
    this.removingCfg = mwh
    Doc.hide(page.removeCfgErr)
    const { unitInfo: { conventional: { unit: baseTicker } } } = app().assets[mwh.baseID]
    const { unitInfo: { conventional: { unit: quoteTicker } } } = app().assets[mwh.quoteID]
    page.confirmRemoveCfgMsg.textContent = intl.prep(intl.ID_DELETE_BOT, { host: mwh.host, baseTicker, quoteTicker })
    this.forms.show(this.page.confirmRemoveForm)
  }

  async removeCfg () {
    const page = this.page
    if (!this.removingCfg) { this.forms.close(); return }
    const resp = await MM.removeBotConfig(this.removingCfg.host, this.removingCfg.baseID, this.removingCfg.quoteID)
    if (!app().checkResponse(resp)) {
      page.removeCfgErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: resp.msg })
      Doc.show(page.removeCfgErr)
      return
    }
    await app().fetchMMStatus()
    app().loadPage('mm')
  }

  appendBotBox (div: PageElement) {
    const { page: { boxZero, boxOne }, twoColumn } = this
    const useZeroth = !twoColumn || (boxZero.children.length + boxOne.children.length) % 2 === 0
    const box = useZeroth ? boxZero : boxOne
    box.append(div)
  }

  clearBotBoxes () {
    const { page: { boxOne, boxZero } } = this
    while (boxZero.children.length > 1) boxZero.removeChild(boxZero.lastChild as Element)
    while (boxOne.children.length > 0) boxOne.removeChild(boxOne.lastChild as Element)
  }

  showBot (botID: string) {
    const { sortedBots } = this
    const idx = sortedBots.findIndex((bot: Bot) => bot.id === botID)
    sortedBots.splice(idx, 1)
    sortedBots.unshift(this.bots[botID])
    this.clearBotBoxes()
    for (const { div } of sortedBots) this.appendBotBox(div)
    const div = this.bots[botID].div
    Doc.animate(250, (p: number) => {
      div.style.opacity = `${p}`
      div.style.transform = `scale(${0.8 + 0.2 * p})`
    })
  }

  newBot () {
    app().loadPage('mmsettings')
  }

  async cexConfigured (cexName: string, success: boolean) {
    await app().fetchMMStatus()
    this.updateCexRow(this.cexes[cexName])
    if (success) this.forms.close()
  }

  updateCexRow (row: CEXRow) {
    const { tmpl, dinfo, cexName } = row
    tmpl.logo.src = dinfo.logo
    tmpl.name.textContent = dinfo.name
    const status = app().mmStatus.cexes[cexName]
    Doc.setVis(!status, tmpl.unconfigured)
    Doc.setVis(status && !status.connectErr, tmpl.configured)
    Doc.setVis(status?.connectErr, tmpl.connectErrBox)
    if (status?.connectErr) {
      tmpl.connectErr.textContent = 'connection error'
      tmpl.connectErr.dataset.tooltip = status.connectErr
    }
    tmpl.logo.classList.toggle('greyscale', !status)
    if (!status) return
    let usdBal = 0
    const cexSymbolAdded : Record<string, boolean> = {} // avoid double counting tokens or counting both eth and weth
    for (const [assetIDStr, bal] of Object.entries(status.balances)) {
      const assetID = parseInt(assetIDStr)
      const cexSymbol = Doc.bipCEXSymbol(assetID)
      if (cexSymbolAdded[cexSymbol]) continue
      cexSymbolAdded[cexSymbol] = true
      const { unitInfo } = app().assets[assetID]
      const fiatRate = app().fiatRatesMap[assetID]
      if (fiatRate) usdBal += fiatRate * (bal.available + bal.locked) / unitInfo.conventional.conversionFactor
    }
    tmpl.usdBalance.textContent = Doc.formatFourSigFigs(usdBal)
  }

  percentageBalanceStr (assetID: number, balance: number, percentage: number): string {
    const asset = app().assets[assetID]
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue((balance * percentage) / 100, unitInfo)
    return `${Doc.formatFourSigFigs(percentage)}% - ${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * walletBalanceStr returns a string like "50% - 0.0001 BTC" representing
   * the percentage of a wallet's balance selected in the market maker setting,
   * and the amount of that asset in the wallet.
   */
  walletBalanceStr (assetID: number, percentage: number): string {
    const { wallet: { balance: { available } } } = app().assets[assetID]
    return this.percentageBalanceStr(assetID, available, percentage)
  }
}

interface BotRow {
  tr: PageElement
  tmpl: Record<string, PageElement>
}

class Bot extends BotMarket {
  pg: MarketMakerPage
  div: PageElement
  page: Record<string, PageElement>
  row: BotRow
  runDisplay: RunningMarketMakerDisplay

  constructor (pg: MarketMakerPage, runningMMElements: RunningMMDisplayElements, status: MMBotStatus) {
    super(status.config)
    this.pg = pg
    const { baseID, quoteID, host, botType, cexName } = this
    this.id = hostedMarketID(host, baseID, quoteID)

    const div = this.div = pg.page.botTmpl.cloneNode(true) as PageElement
    const page = this.page = Doc.parseTemplate(div)

    this.runDisplay = new RunningMarketMakerDisplay(page.onBox, pg.forms, runningMMElements, 'mm')

    setMarketElements(div, baseID, quoteID, host)
    if (cexName) setCexElements(div, cexName)

    if (botType === botTypeArbMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
    } else if (botType === botTypeBasicArb) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
    } else if (botType === botTypeBasicMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
    }

    Doc.bind(page.reconfigureBttn, 'click', () => this.reconfigure())
    Doc.bind(page.removeBttn, 'click', () => this.pg.confirmRemoveCfg(status.config))
    Doc.bind(page.marketLink, 'click', () => app().loadPage('markets', { host, baseID, quoteID }))

    const tr = pg.page.botRowTmpl.cloneNode(true) as PageElement
    setMarketElements(tr, baseID, quoteID, host)
    const tmpl = Doc.parseTemplate(tr)
    this.row = { tr, tmpl }
    Doc.bind(tr, 'click', () => pg.showBot(this.id))

    this.initialize()
  }

  async initialize () {
    await super.initialize()
    this.runDisplay.setBotMarket(this)
    const {
      page, host, cexName, botType, div, mktID,
      baseFactor, quoteFactor, marketReport: { baseFiatRate }
    } = this

    Doc.setVis(botType !== botTypeBasicMM, page.cexDataBox)
    if (botType !== botTypeBasicMM) {
      const cex = app().mmStatus.cexes[cexName]
      if (cex) {
        const mkt = cex.markets ? cex.markets[mktID] : undefined
        Doc.setVis(mkt?.day, page.cexDataBox)
        if (mkt?.day) {
          const day = mkt.day
          page.cexPrice.textContent = Doc.formatFourSigFigs(day.lastPrice)
          page.cexVol.textContent = Doc.formatFourSigFigs(baseFiatRate * day.vol)
        }
      }
    }
    Doc.setVis(Boolean(cexName), ...Doc.applySelector(div, '[data-cex-show]'))

    const { spot } = app().exchanges[host].markets[mktID]
    if (spot) {
      Doc.show(page.dexDataBox)
      const c = OrderUtil.RateEncodingFactor / baseFactor * quoteFactor
      page.dexPrice.textContent = Doc.formatFourSigFigs(spot.rate / c)
      page.dexVol.textContent = Doc.formatFourSigFigs(spot.vol24 / baseFactor * baseFiatRate)
    }

    this.updateDisplay()
    this.updateTableRow()
    Doc.hide(page.loadingBg)
  }

  updateTableRow () {
    const { row: { tmpl } } = this
    const { running, runStats } = this.status()
    Doc.setVis(running, tmpl.profitLossBox)
    if (runStats) {
      tmpl.profitLoss.textContent = Doc.formatFourSigFigs(runStats.profitLoss.profit, 2)
    }
  }

  updateDisplay () {
    const { page, marketReport: { baseFiatRate, quoteFiatRate }, baseFeeFiatRate, quoteFeeFiatRate } = this
    if ([baseFiatRate, quoteFiatRate, baseFeeFiatRate, quoteFeeFiatRate].some((r: number) => !r)) {
      Doc.hide(page.onBox, page.offBox)
      Doc.show(page.noFiatDisplay)
      return
    }
    const { running } = this.status()
    Doc.setVis(running, page.onBox)
    Doc.setVis(!running, page.offBox)
    if (running) this.updateRunningDisplay()
    else this.updateIdleDisplay()
  }

  async updateIdleDisplay () {
    const { page, baseID, quoteID, host, cexName, bui, qui, baseFeeUI, quoteFeeUI, baseFeeID, quoteFeeID } = this
    const availableBalances = await MM.availableBalances({ host, baseID, quoteID }, this.cfg.cexBaseID, this.cfg.cexQuoteID, cexName)
    const { dexBalances, cexBalances } = availableBalances

    const baseDexBalance = Doc.formatCoinValue(dexBalances[baseID] ?? 0, bui)
    page.baseDexBalance.textContent = baseDexBalance
    let totalBaseBalance = dexBalances[baseID] ?? 0

    const quoteDexBalance = Doc.formatCoinValue(dexBalances[quoteID] ?? 0, qui)
    page.quoteDexBalance.textContent = quoteDexBalance
    let totalQuoteBalance = dexBalances[quoteID] ?? 0

    Doc.setVis(cexName, page.baseCexBalanceSection, page.quoteCexBalanceSection)
    if (cexName) {
      const baseCexBalance = Doc.formatCoinValue(cexBalances[baseID] ?? 0, bui)
      page.baseCexBalance.textContent = baseCexBalance
      totalBaseBalance += cexBalances[baseID] ?? 0
      const quoteCexBalance = Doc.formatCoinValue(cexBalances[quoteID] ?? 0, qui)
      page.quoteCexBalance.textContent = quoteCexBalance
      totalQuoteBalance += cexBalances[quoteID] ?? 0
    }

    page.baseTotalBalance.textContent = Doc.formatCoinValue(totalBaseBalance, bui)
    page.quoteTotalBalance.textContent = Doc.formatCoinValue(totalQuoteBalance, qui)

    const baseFeeAssetIsTraded = baseFeeID === baseID || baseFeeID === quoteID
    const quoteFeeAssetIsTraded = quoteFeeID === baseID || quoteFeeID === quoteID

    if (baseFeeAssetIsTraded) {
      Doc.hide(page.baseFeeBalanceSection)
    } else {
      Doc.show(page.baseFeeBalanceSection)
      const baseFeeBalance = Doc.formatCoinValue(dexBalances[baseFeeID] ?? 0, baseFeeUI)
      page.baseFeeBalance.textContent = baseFeeBalance
    }

    if (quoteFeeAssetIsTraded) {
      Doc.hide(page.quoteFeeBalanceSection)
    } else {
      Doc.show(page.quoteFeeBalanceSection)
      const quoteFeeBalance = Doc.formatCoinValue(dexBalances[quoteFeeID] ?? 0, quoteFeeUI)
      page.quoteFeeBalance.textContent = quoteFeeBalance
    }
  }

  updateRunningDisplay () {
    this.runDisplay.update()
  }

  reconfigure () {
    const { host, baseID, quoteID, cexName, botType, page } = this
    if (cexName) {
      const cex = app().mmStatus.cexes[cexName]
      if (!cex || !cex.connected) {
        page.offError.textContent = intl.prep(intl.ID_CEX_NOT_CONNECTED, { cexName })
        Doc.showTemporarily(3000, page.offError)
        return
      }
    }
    app().loadPage('mmsettings', { host, baseID, quoteID, cexName, botType })
  }

  handleEpochReportNote (note: EpochReportNote) {
    this.runDisplay.handleEpochReportNote(note)
  }

  handleCexProblemsNote (note: CEXProblemsNote) {
    this.runDisplay.handleCexProblemsNote(note)
  }

  handleRunStats () {
    this.updateDisplay()
    this.updateTableRow()
    this.runDisplay.readBook()
  }
}
