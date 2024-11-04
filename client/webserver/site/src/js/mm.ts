import {
  app,
  PageElement,
  MMBotStatus,
  RunStatsNote,
  RunEventNote,
  StartConfig,
  OrderPlacement,
  AutoRebalanceConfig,
  CEXNotification,
  EpochReportNote,
  CEXProblemsNote,
  MarketWithHost,
  BotBalanceAllocation
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
  PlacementsChart,
  BotMarket,
  hostedMarketID,
  RunningMarketMakerDisplay,
  RunningMMDisplayElements,
  PerLot,
  AvailableFunds,
  AllocationResult,
  requiredFunds
} from './mmutil'
import Doc from './doc'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { Forms, CEXConfigurationForm, AllocationForm } from './forms'
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
  allocationForm: AllocationForm

  constructor (main: HTMLElement) {
    super()

    this.bots = {}
    this.sortedBots = []
    this.cexes = {}

    const page = this.page = Doc.idDescendants(main)

    Doc.cleanTemplates(page.botTmpl, page.botRowTmpl, page.exchangeRowTmpl)

    this.forms = new Forms(page.forms)
    this.cexConfigForm = new CEXConfigurationForm(page.cexConfigForm, (cexName: string, success: boolean) => this.cexConfigured(cexName, success))
    this.allocationForm = new AllocationForm(page.allocationForm,
      (allocation: AllocationResult, running: boolean, autoRebalance: AutoRebalanceConfig | undefined) => this.allocationSubmit(allocation, running, autoRebalance))
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
    Doc.bind(page.allocationForm, 'submit', (e: Event) => { e.preventDefault() })

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

  async allocationSubmit (allocation: AllocationResult, running: boolean, autoRebalance: AutoRebalanceConfig | undefined) {
    const bot = this.bots[this.allocationForm.botID]
    if (!bot) return
    if (running) {
      try {
        await bot.updateInventory(allocation)
      } catch (e) {
        this.page.allocationErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: e.msg })
        Doc.show(this.page.allocationErr)
        return
      }
      this.forms.close()
    } else bot.allocationSubmit(allocation, autoRebalance)
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
  placementsChart: PlacementsChart
  row: BotRow
  runDisplay: RunningMarketMakerDisplay

  constructor (pg: MarketMakerPage, runningMMElements: RunningMMDisplayElements, status: MMBotStatus) {
    super(status.config)
    this.pg = pg
    const { baseID, quoteID, host, botType, nBuyPlacements, nSellPlacements, cexName } = this
    this.id = hostedMarketID(host, baseID, quoteID)

    const div = this.div = pg.page.botTmpl.cloneNode(true) as PageElement
    const page = this.page = Doc.parseTemplate(div)

    this.runDisplay = new RunningMarketMakerDisplay(page.onBox, pg.forms, runningMMElements, 'mm', () => { this.allocate() })

    setMarketElements(div, baseID, quoteID, host)
    if (cexName) setCexElements(div, cexName)

    if (botType === botTypeArbMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
    } else if (botType === botTypeBasicArb) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
    } else if (botType === botTypeBasicMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
    }

    Doc.setVis(botType !== botTypeBasicArb, page.placementsChartBox)
    if (botType !== botTypeBasicArb) {
      this.placementsChart = new PlacementsChart(page.placementsChart)
      page.buyPlacementCount.textContent = String(nBuyPlacements)
      page.sellPlacementCount.textContent = String(nSellPlacements)
    }

    Doc.bind(page.allocationBttn, 'click', () => this.allocate())
    Doc.bind(page.reconfigureBttn, 'click', () => this.reconfigure())
    Doc.bind(page.removeBttn, 'click', () => this.pg.confirmRemoveCfg(status.config))
    Doc.bind(page.marketLink, 'click', () => app().loadPage('markets', { host, baseID, quoteID }))

    const tr = pg.page.botRowTmpl.cloneNode(true) as PageElement
    setMarketElements(tr, baseID, quoteID, host)
    const tmpl = Doc.parseTemplate(tr)
    this.row = { tr, tmpl }
    Doc.bind(tmpl.allocateBttn, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      this.allocate()
      pg.showBot(this.id)
    })
    Doc.bind(tr, 'click', () => pg.showBot(this.id))

    this.initialize()
  }

  async initialize () {
    await super.initialize()
    this.runDisplay.setBotMarket(this)
    const {
      page, host, cexName, botType, div,
      cfg: { arbMarketMakingConfig, basicMarketMakingConfig }, mktID,
      baseFactor, quoteFactor, marketReport: { baseFiatRate }
    } = this

    if (botType !== botTypeBasicArb) {
      let buyPlacements: OrderPlacement[] = []
      let sellPlacements: OrderPlacement[] = []
      let profit = 0
      if (arbMarketMakingConfig) {
        buyPlacements = arbMarketMakingConfig.buyPlacements.map((p) => ({ lots: p.lots, gapFactor: p.multiplier }))
        sellPlacements = arbMarketMakingConfig.sellPlacements.map((p) => ({ lots: p.lots, gapFactor: p.multiplier }))
        profit = arbMarketMakingConfig.profit
      } else if (basicMarketMakingConfig) {
        buyPlacements = basicMarketMakingConfig.buyPlacements
        sellPlacements = basicMarketMakingConfig.sellPlacements
        let bestBuy: OrderPlacement | undefined
        let bestSell : OrderPlacement | undefined
        if (buyPlacements.length > 0) bestBuy = buyPlacements.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
        if (sellPlacements.length > 0) bestSell = sellPlacements.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
        if (bestBuy && bestSell) {
          profit = (bestBuy.gapFactor + bestSell.gapFactor) / 2
        } else if (bestBuy) {
          profit = bestBuy.gapFactor
        } else if (bestSell) {
          profit = bestSell.gapFactor
        }
      }
      const marketConfig = { cexName: cexName as string, botType, baseFiatRate: baseFiatRate, dict: { profit, buyPlacements, sellPlacements } }
      this.placementsChart.setMarket(marketConfig)
    }

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
    Doc.setVis(!running, tmpl.allocateBttnBox)
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

  updateRunningDisplay () {
    this.runDisplay.update()
  }

  updateIdleDisplay () {
    const {
      page, baseID, quoteID, bui, qui, baseFactor, quoteFactor, baseFeeID, quoteFeeID, cexName,
      marketReport: { baseFiatRate, quoteFiatRate }, baseFeeFiatRate, quoteFeeFiatRate
    } = this
    const { perBuyLot, perSellLot } = this.perLotRequirements()

    const minAllocation = requiredFunds(this.buyLots, this.sellLots, 0, 0, 0, perBuyLot, perSellLot, this.marketReport,
      this.baseIsAccountLocker, this.quoteIsAccountLocker, this.baseID, this.quoteID, this.baseFeeID, this.quoteFeeID,
      0, 0) // TODO: update funding fees

    const totalBase = minAllocation.dex[baseID][0] + minAllocation.cex[baseID][0]
    page.baseAlloc.textContent = Doc.formatFullPrecision(totalBase, bui)
    const baseUSD = totalBase / baseFactor * baseFiatRate
    let totalUSD = baseUSD
    page.baseAllocUSD.textContent = Doc.formatFourSigFigs(baseUSD)
    page.baseDexAlloc.textContent = Doc.formatFullPrecision(minAllocation.dex[baseID][0], bui)
    Doc.setVis(this.cexName, page.baseCexAllocBox)
    if (cexName) {
      page.baseCexAlloc.textContent = Doc.formatFullPrecision(minAllocation.cex[baseID][0], bui)
    }

    const totalQuote = minAllocation.dex[quoteID][0] + minAllocation.cex[quoteID][0]
    page.quoteAlloc.textContent = Doc.formatFullPrecision(totalQuote, qui)
    const quoteUSD = totalQuote / quoteFactor * quoteFiatRate
    totalUSD += quoteUSD
    page.quoteAllocUSD.textContent = Doc.formatFourSigFigs(quoteUSD)
    page.quoteDexAlloc.textContent = Doc.formatFullPrecision(minAllocation.dex[quoteID][0], qui)
    Doc.setVis(cexName, page.quoteCexAllocBox)
    if (cexName) {
      page.quoteCexAlloc.textContent = Doc.formatFullPrecision(minAllocation.cex[quoteID][0], qui)
    }

    Doc.setVis(baseFeeID !== baseID && baseFeeID !== quoteID, page.baseFeeDexAllocBox)
    if (baseFeeID !== baseID && baseFeeID !== quoteID) {
      const baseFeeDexAlloc = minAllocation.dex[baseFeeID][0]
      page.baseFeeDexAlloc.textContent = Doc.formatFullPrecision(baseFeeDexAlloc, this.baseFeeUI)
      page.baseTokenFeeAlloc.textContent = Doc.formatFullPrecision(baseFeeDexAlloc, this.baseFeeUI)
      totalUSD += baseFeeDexAlloc / this.baseFeeFactor * baseFeeFiatRate
      page.baseTokenAllocUSD.textContent = Doc.formatFourSigFigs(baseFeeDexAlloc / this.baseFeeFactor * baseFeeFiatRate)
    }

    Doc.setVis(quoteFeeID !== baseID && quoteFeeID !== quoteID, page.quoteFeeDexAllocBox)
    if (quoteFeeID !== baseID && quoteFeeID !== quoteID) {
      const quoteFeeDexAlloc = minAllocation.dex[quoteFeeID][0]
      page.quoteFeeDexAlloc.textContent = Doc.formatFullPrecision(quoteFeeDexAlloc, this.quoteFeeUI)
      page.quoteTokenFeeAlloc.textContent = Doc.formatFullPrecision(quoteFeeDexAlloc, this.quoteFeeUI)
      totalUSD += quoteFeeDexAlloc / this.quoteFeeFactor * quoteFeeFiatRate
      page.quoteTokenAllocUSD.textContent = Doc.formatFourSigFigs(quoteFeeDexAlloc / this.quoteFeeFactor * quoteFeeFiatRate)
    }

    page.totalAllocUSD.textContent = Doc.formatFourSigFigs(totalUSD)
  }

  perLotRequirements (): { perSellLot: PerLot, perBuyLot: PerLot } {
    const { baseID, quoteID, baseFeeID, quoteFeeID, lotSize, quoteLot, marketReport, baseIsAccountLocker, quoteIsAccountLocker } = this

    const perSellLot: PerLot = { cex: {}, dex: {} }
    perSellLot.dex[baseID] = lotSize
    perSellLot.dex[baseFeeID] = (perSellLot.dex[baseFeeID] ?? 0) + marketReport.baseFees.max.swap
    perSellLot.cex[quoteID] = quoteLot
    if (baseIsAccountLocker) perSellLot.dex[baseFeeID] = (perSellLot.dex[baseFeeID] ?? 0) + marketReport.baseFees.max.refund
    if (quoteIsAccountLocker) perSellLot.dex[quoteFeeID] = (perSellLot.dex[quoteFeeID] ?? 0) + marketReport.quoteFees.max.redeem

    const perBuyLot: PerLot = { cex: {}, dex: {} }
    perBuyLot.dex[quoteID] = quoteLot
    perBuyLot.dex[quoteFeeID] = (perBuyLot.dex[quoteFeeID] ?? 0) + marketReport.quoteFees.max.swap
    perBuyLot.cex[baseID] = lotSize
    if (baseIsAccountLocker) perBuyLot.dex[baseFeeID] = (perBuyLot.dex[baseFeeID] ?? 0) + marketReport.baseFees.max.redeem
    if (quoteIsAccountLocker) perBuyLot.dex[quoteFeeID] = (perBuyLot.dex[quoteFeeID] ?? 0) + marketReport.quoteFees.max.refund

    return { perSellLot, perBuyLot }
  }

  /*
   * allocate opens a dialog to choose funding sources (if applicable) and
   * confirm allocations and start the bot.
   */
  async allocate () {
    const {
      baseID, quoteID, host, cexName, bui, qui, baseFeeID, quoteFeeID, marketReport, page,
      quoteFeeUI, baseFeeUI, lotSize, quoteLot, baseIsAccountLocker, quoteIsAccountLocker
    } = this

    if (cexName) {
      const cex = app().mmStatus.cexes[cexName]
      if (!cex || !cex.connected) {
        page.offError.textContent = intl.prep(intl.ID_CEX_NOT_CONNECTED, { cexName })
        Doc.showTemporarily(3000, page.offError)
        return
      }
    }

    const assetIDs = Array.from(new Set([baseID, quoteID, baseFeeID, quoteFeeID]))
    const availableFundsRes = await MM.availableBalances({ host, baseID, quoteID }, cexName)
    const availableFunds: AvailableFunds = { dex: {}, cex: {} }
    for (const assetID of assetIDs) {
      const dexBal = availableFundsRes.dexBalances[assetID] ?? 0
      const cexBal = availableFundsRes.cexBalances[assetID] ?? 0
      availableFunds.dex[assetID] = dexBal
      if (availableFunds.cex) availableFunds.cex[assetID] = cexBal
    }

    const maxFundingFeesRes = await MM.maxFundingFees({ host, baseID, quoteID })
    const { buyFees: buyFundingFees, sellFees: sellFundingFees } = maxFundingFeesRes

    const canRebalance = Boolean(cexName)

    const { runStats } = this.status()

    this.pg.allocationForm.init(baseID, quoteID, host, baseFeeID, quoteFeeID,
      cexName, bui, qui, baseFeeUI, quoteFeeUI, marketReport, lotSize, quoteLot,
      baseIsAccountLocker, quoteIsAccountLocker, availableFunds, canRebalance, this.id, this.sellLots, this.buyLots,
      buyFundingFees, sellFundingFees, runStats)

    this.pg.forms.show(this.pg.page.allocationForm)
  }

  allocationSubmit (allocation: AllocationResult, autoRebalance: AutoRebalanceConfig | undefined) {
    const botBalanceAllocation: BotBalanceAllocation = { dex: {}, cex: {} }
    const assetIDs = Array.from(new Set([this.baseID, this.quoteID, this.baseFeeID, this.quoteFeeID]))
    for (const assetID of assetIDs) {
      botBalanceAllocation.dex[assetID] = allocation.dex[assetID] ? allocation.dex[assetID][0] : 0
      botBalanceAllocation.cex[assetID] = allocation.cex[assetID] ? allocation.cex[assetID][0] : 0
    }
    this.start(botBalanceAllocation, autoRebalance)
  }

  async updateInventory (allocation: AllocationResult) {
    const botBalanceAllocation: BotBalanceAllocation = { dex: {}, cex: {} }
    const assetIDs = Array.from(new Set([this.baseID, this.quoteID, this.baseFeeID, this.quoteFeeID]))
    for (const assetID of assetIDs) {
      botBalanceAllocation.dex[assetID] = allocation.dex[assetID] ? allocation.dex[assetID][0] : 0
      botBalanceAllocation.cex[assetID] = allocation.cex[assetID] ? allocation.cex[assetID][0] : 0
    }
    const res = await MM.updateBotInventory({ host: this.host, baseID: this.baseID, quoteID: this.quoteID }, botBalanceAllocation)
    if (!app().checkResponse(res)) throw res
  }

  async start (alloc: BotBalanceAllocation, autoRebalance: AutoRebalanceConfig | undefined) {
    const { baseID, quoteID, host, cexName } = this

    Doc.hide(this.pg.page.allocationErrMsg)
    if (cexName && !app().mmStatus.cexes[cexName]?.connected) {
      this.pg.page.allocationErrMsg.textContent = `${cexName} not connected`
      Doc.show(this.pg.page.allocationErrMsg)
      return
    }

    // round allocations values.
    for (const m of [alloc.dex, alloc.cex]) {
      for (const [assetID, v] of Object.entries(m)) m[parseInt(assetID)] = Math.round(v)
    }

    const startConfig: StartConfig = {
      baseID: baseID,
      quoteID: quoteID,
      host: host,
      alloc: alloc
    }
    if (cexName) startConfig.autoRebalance = autoRebalance

    try {
      app().log('mm', 'starting mm bot', startConfig)
      const res = await MM.startBot(startConfig)
      if (!app().checkResponse(res)) throw res
    } catch (e) {
      this.pg.page.allocationErrMsg.textContent = intl.prep(intl.ID_API_ERROR, e)
      Doc.show(this.pg.page.allocationErrMsg)
      return
    }

    this.pg.forms.close()
  }

  autoRebalanceSettings (): AutoRebalanceConfig {
    /* const {
      proj: { bProj, qProj, alloc }, baseFeeID, quoteFeeID, cfg: { uiConfig: { baseConfig, quoteConfig } },
      baseID, quoteID, cexName, mktID
    } = this

    const totalBase = alloc[baseID]
    let dexMinBase = bProj.book
    if (baseID === baseFeeID) dexMinBase += bProj.bookingFees
    if (baseID === quoteFeeID) dexMinBase += qProj.bookingFees
    let dexMinQuote = qProj.book
    if (quoteID === quoteFeeID) dexMinQuote += qProj.bookingFees
    if (quoteID === baseFeeID) dexMinQuote += bProj.bookingFees
    const maxBase = Math.max(totalBase - dexMinBase, totalBase - bProj.cex)
    const totalQuote = alloc[quoteID]
    const maxQuote = Math.max(totalQuote - dexMinQuote, totalQuote - qProj.cex)
    if (maxBase < 0 || maxQuote < 0) {
      throw Error(`rebalance math doesn't work: ${JSON.stringify({ bProj, qProj, maxBase, maxQuote })}`)
    }
    const cex = app().mmStatus.cexes[cexName]
    const mkt = cex.markets[mktID]
    const [minB, maxB] = [mkt.baseMinWithdraw, Math.max(mkt.baseMinWithdraw * 2, maxBase)]
    const minBaseTransfer = Math.round(minB + baseConfig.transferFactor * (maxB - minB))
    const [minQ, maxQ] = [mkt.quoteMinWithdraw, Math.max(mkt.quoteMinWithdraw * 2, maxQuote)]
    const minQuoteTransfer = Math.round(minQ + quoteConfig.transferFactor * (maxQ - minQ)) */
    return { minBaseTransfer: 0, minQuoteTransfer: 0 }
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
