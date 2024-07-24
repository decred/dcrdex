import {
  app,
  PageElement,
  MMBotStatus,
  RunStatsNote,
  RunEventNote,
  ExchangeBalance,
  StartConfig,
  OrderPlacement,
  AutoRebalanceConfig,
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
  PlacementsChart,
  BotMarket,
  hostedMarketID,
  RunningMarketMakerDisplay
} from './mmutil'
import Doc, { MiniSlider } from './doc'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { Forms, CEXConfigurationForm } from './forms'
import * as intl from './locales'

const mediumBreakpoint = 768

interface FundingSlider {
  left: {
    cex: number
    dex: number
  }
  right: {
    cex: number
    dex: number
  }
  cexRange: number
  dexRange: number
}

const newSlider = () => {
  return {
    left: {
      cex: 0,
      dex: 0
    },
    right: {
      cex: 0,
      dex: 0
    },
    cexRange: 0,
    dexRange: 0
  }
}

interface FundingSource {
  avail: number
  req: number
  funded: boolean
}

interface FundingOutlook {
  dex: FundingSource
  cex: FundingSource
  transferable: number
  fees: {
    avail: number
    req: number
    funded: boolean
  },
  fundedAndBalanced: boolean
  fundedAndNotBalanced: boolean
}

function parseFundingOptions (f: FundingOutlook): [number, number, FundingSlider | undefined] {
  const { cex: { avail: cexAvail, req: cexReq }, dex: { avail: dexAvail, req: dexReq }, transferable } = f

  let proposedDex = Math.min(dexAvail, dexReq)
  let proposedCex = Math.min(cexAvail, cexReq)
  let slider: FundingSlider | undefined
  if (f.fundedAndNotBalanced) {
    // We have everything we need, but not where we need it, and we can
    // deposit and withdraw.
    if (dexAvail > dexReq) {
      // We have too much dex-side, so we'll have to draw on dex to balance
      // cex's shortcomings.
      const cexShort = cexReq - cexAvail
      const dexRemain = dexAvail - dexReq
      if (dexRemain < cexShort) {
        // We did something really bad with math to get here.
        throw Error('bad math has us with dex surplus + cex underfund invalid remains')
      }
      proposedDex += cexShort + transferable
    } else {
      // We don't have enough on dex, but we have enough on cex to cover the
      // short.
      const dexShort = dexReq - dexAvail
      const cexRemain = cexAvail - cexReq
      if (cexRemain < dexShort) {
        throw Error('bad math got us with cex surplus + dex underfund invalid remains')
      }
      proposedCex += dexShort + transferable
    }
  } else if (f.fundedAndBalanced) {
    // This asset is fully funded, but the user may choose to fund order
    // reserves either cex or dex.
    if (transferable > 0) {
      const dexRemain = dexAvail - dexReq
      const cexRemain = cexAvail - cexReq

      slider = newSlider()

      if (cexRemain > transferable && dexRemain > transferable) {
        // Either one could fully fund order reserves. Let the user choose.
        slider.left.cex = transferable + cexReq
        slider.left.dex = dexReq
        slider.right.cex = cexReq
        slider.right.dex = transferable + dexReq
      } else if (dexRemain < transferable && cexRemain < transferable) {
        // => implied that cexRemain + dexRemain > transferable.
        // CEX can contribute SOME and DEX can contribute SOME.
        slider.left.cex = transferable - dexRemain + cexReq
        slider.left.dex = dexRemain + dexReq
        slider.right.cex = cexRemain + cexReq
        slider.right.dex = transferable - cexRemain + dexReq
      } else if (dexRemain > transferable) {
        // So DEX has enough to cover reserves, but CEX could potentially
        // constribute SOME. NOT ALL.
        slider.left.cex = cexReq
        slider.left.dex = transferable + dexReq
        slider.right.cex = cexRemain + cexReq
        slider.right.dex = transferable - cexRemain + dexReq
      } else {
        // CEX has enough to cover reserves, but DEX could contribute SOME,
        // NOT ALL.
        slider.left.cex = transferable - dexRemain + cexReq
        slider.left.dex = dexRemain + dexReq
        slider.right.cex = transferable + cexReq
        slider.right.dex = dexReq
      }
      // We prefer the slider right in the center.
      slider.cexRange = slider.right.cex - slider.left.cex
      slider.dexRange = slider.right.dex - slider.left.dex
      proposedDex = slider.left.dex + (slider.dexRange / 2)
      proposedCex = slider.left.cex + (slider.cexRange / 2)
    }
  } else { // starved
    if (cexAvail < cexReq) {
      proposedDex = Math.min(dexAvail, dexReq + transferable + (cexReq - cexAvail))
    } else if (dexAvail < dexReq) {
      proposedCex = Math.min(cexAvail, cexReq + transferable + (dexReq - dexAvail))
    } else { // just transferable wasn't covered
      proposedDex = Math.min(dexAvail, dexReq + transferable)
      proposedCex = Math.min(cexAvail, dexReq + cexReq + transferable - proposedDex)
    }
  }
  return [proposedDex, proposedCex, slider]
}

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

  constructor (main: HTMLElement) {
    super()

    this.bots = {}
    this.sortedBots = []
    this.cexes = {}

    const page = this.page = Doc.idDescendants(main)

    Doc.cleanTemplates(page.botTmpl, page.botRowTmpl, page.exchangeRowTmpl)

    this.forms = new Forms(page.forms)
    this.cexConfigForm = new CEXConfigurationForm(page.cexConfigForm, (cexName: string) => this.cexConfigured(cexName))

    Doc.bind(page.newBot, 'click', () => { this.newBot() })
    Doc.bind(page.archivedLogsBtn, 'click', () => { app().loadPage('mmarchives') })

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

    const startupBalanceCache: Record<number, Promise<ExchangeBalance>> = {}

    for (const botStatus of sortedBots) this.addBot(botStatus, startupBalanceCache)
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

  addBot (botStatus: MMBotStatus, startupBalanceCache?: Record<number, Promise<ExchangeBalance>>) {
    const { page, bots, sortedBots } = this
    // Make sure the market still exists.
    const { config: { baseID, quoteID, host } } = botStatus
    const [baseSymbol, quoteSymbol] = [app().assets[baseID].symbol, app().assets[quoteID].symbol]
    const mktID = `${baseSymbol}_${quoteSymbol}`
    if (!app().exchanges[host]?.markets[mktID]) return
    const bot = new Bot(this, botStatus, startupBalanceCache)
    page.botRows.appendChild(bot.row.tr)
    sortedBots.push(bot)
    bots[bot.id] = bot
    this.appendBotBox(bot.div)
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

  async cexConfigured (cexName: string) {
    await app().fetchMMStatus()
    this.updateCexRow(this.cexes[cexName])
    this.forms.close()
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
  baseAllocSlider: MiniSlider
  quoteAllocSlider: MiniSlider
  row: BotRow
  runDisplay: RunningMarketMakerDisplay

  constructor (pg: MarketMakerPage, status: MMBotStatus, startupBalanceCache?: Record<number, Promise<ExchangeBalance>>) {
    super(status.config)
    startupBalanceCache = startupBalanceCache ?? {}
    this.pg = pg
    const { baseID, quoteID, host, botType, nBuyPlacements, nSellPlacements, cexName } = this
    this.id = hostedMarketID(host, baseID, quoteID)

    const div = this.div = pg.page.botTmpl.cloneNode(true) as PageElement
    const page = this.page = Doc.parseTemplate(div)
    this.runDisplay = new RunningMarketMakerDisplay(page.onBox)

    setMarketElements(div, baseID, quoteID, host)
    if (cexName) setCexElements(div, cexName)

    if (botType === botTypeArbMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
    } else if (botType === botTypeBasicArb) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
    } else if (botType === botTypeBasicMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
    }

    Doc.setVis(botType !== botTypeBasicArb, page.placementsChartBox, page.baseTokenSwapFeesBox)
    if (botType !== botTypeBasicArb) {
      this.placementsChart = new PlacementsChart(page.placementsChart)
      page.buyPlacementCount.textContent = String(nBuyPlacements)
      page.sellPlacementCount.textContent = String(nSellPlacements)
    }

    Doc.bind(page.startBttn, 'click', () => this.start())
    Doc.bind(page.allocationBttn, 'click', () => this.allocate())
    Doc.bind(page.reconfigureBttn, 'click', () => this.reconfigure())
    Doc.bind(page.goBackFromAllocation, 'click', () => this.hideAllocationDialog())
    Doc.bind(page.marketLink, 'click', () => app().loadPage('markets', { host, baseID, quoteID }))

    this.baseAllocSlider = new MiniSlider(page.baseAllocSlider, () => { /* callback set later */ })
    this.quoteAllocSlider = new MiniSlider(page.quoteAllocSlider, () => { /* callback set later */ })

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

    this.initialize(startupBalanceCache)
  }

  async initialize (startupBalanceCache: Record<number, Promise<ExchangeBalance>>) {
    await super.initialize(startupBalanceCache)
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
        const bestBuy = basicMarketMakingConfig.buyPlacements.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
        const bestSell = basicMarketMakingConfig.sellPlacements.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
        profit = (bestBuy.gapFactor + bestSell.gapFactor) / 2
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
      tmpl.profitLoss.textContent = Doc.formatFourSigFigs(runStats.profitLoss.profit)
    }
  }

  updateDisplay () {
    const { page } = this
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
      page, proj: { alloc, qProj, bProj }, baseID, quoteID, cexName, bui, qui, baseFeeID,
      quoteFeeID, baseFactor, quoteFactor, baseFeeFactor, quoteFeeFactor,
      marketReport: { baseFiatRate, quoteFiatRate }, cfg: { uiConfig: { baseConfig, quoteConfig } },
      quoteFeeUI, baseFeeUI
    } = this
    page.baseAlloc.textContent = Doc.formatFullPrecision(alloc[baseID], bui)
    const baseUSD = alloc[baseID] / baseFactor * baseFiatRate
    let totalUSD = baseUSD
    page.baseAllocUSD.textContent = Doc.formatFourSigFigs(baseUSD)
    page.baseBookAlloc.textContent = Doc.formatFullPrecision(bProj.book * baseFactor, bui)
    page.baseOrderReservesAlloc.textContent = Doc.formatFullPrecision(bProj.orderReserves * baseFactor, bui)
    page.baseOrderReservesPct.textContent = String(Math.round(baseConfig.orderReservesFactor * 100))
    Doc.setVis(cexName, page.baseCexAllocBox)
    if (cexName) page.baseCexAlloc.textContent = Doc.formatFullPrecision(bProj.cex * baseFactor, bui)
    Doc.setVis(baseFeeID === baseID, page.baseBookingFeesAllocBox)
    Doc.setVis(baseFeeID !== baseID, page.baseTokenFeesAllocBox)
    if (baseFeeID === baseID) {
      const bookingFees = baseID === quoteFeeID ? bProj.bookingFees + qProj.bookingFees : bProj.bookingFees
      page.baseBookingFeesAlloc.textContent = Doc.formatFullPrecision(bookingFees * baseFeeFactor, baseFeeUI)
    } else {
      const feeAlloc = alloc[baseFeeID]
      page.baseTokenFeeAlloc.textContent = Doc.formatFullPrecision(feeAlloc, baseFeeUI)
      const baseFeeUSD = feeAlloc / baseFeeFactor * app().fiatRatesMap[baseFeeID]
      totalUSD += baseFeeUSD
      page.baseTokenAllocUSD.textContent = Doc.formatFourSigFigs(baseFeeUSD)
      const withQuote = baseFeeID === quoteFeeID
      const bookingFees = bProj.bookingFees + (withQuote ? qProj.bookingFees : 0)
      page.baseTokenBookingFees.textContent = Doc.formatFullPrecision(bookingFees * baseFeeFactor, baseFeeUI)
      page.baseTokenSwapFeeN.textContent = String(baseConfig.swapFeeN + (withQuote ? quoteConfig.swapFeeN : 0))
      const swapReserves = bProj.swapFeeReserves + (withQuote ? qProj.swapFeeReserves : 0)
      page.baseTokenSwapFees.textContent = Doc.formatFullPrecision(swapReserves * baseFeeFactor, baseFeeUI)
    }

    page.quoteAlloc.textContent = Doc.formatFullPrecision(alloc[quoteID], qui)
    const quoteUSD = alloc[quoteID] / quoteFactor * quoteFiatRate
    totalUSD += quoteUSD
    page.quoteAllocUSD.textContent = Doc.formatFourSigFigs(quoteUSD)
    page.quoteBookAlloc.textContent = Doc.formatFullPrecision(qProj.book * quoteFactor, qui)
    page.quoteOrderReservesAlloc.textContent = Doc.formatFullPrecision(qProj.orderReserves * quoteFactor, qui)
    page.quoteOrderReservesPct.textContent = String(Math.round(quoteConfig.orderReservesFactor * 100))
    page.quoteSlippageAlloc.textContent = Doc.formatFullPrecision(qProj.slippageBuffer * quoteFactor, qui)
    page.slippageBufferFactor.textContent = String(Math.round(quoteConfig.slippageBufferFactor * 100))
    Doc.setVis(cexName, page.quoteCexAllocBox)
    if (cexName) page.quoteCexAlloc.textContent = Doc.formatFullPrecision(qProj.cex * quoteFactor, qui)
    Doc.setVis(quoteID === quoteFeeID, page.quoteBookingFeesAllocBox)
    Doc.setVis(quoteFeeID !== quoteID && quoteFeeID !== baseFeeID, page.quoteTokenFeesAllocBox)
    if (quoteID === quoteFeeID) {
      const bookingFees = quoteID === baseFeeID ? bProj.bookingFees + qProj.bookingFees : qProj.bookingFees
      page.quoteBookingFeesAlloc.textContent = Doc.formatFullPrecision(bookingFees * quoteFeeFactor, quoteFeeUI)
    } else if (quoteFeeID !== baseFeeID) {
      page.quoteTokenFeeAlloc.textContent = Doc.formatFullPrecision(alloc[quoteFeeID], quoteFeeUI)
      const quoteFeeUSD = alloc[quoteFeeID] / quoteFeeFactor * app().fiatRatesMap[quoteFeeID]
      totalUSD += quoteFeeUSD
      page.quoteTokenAllocUSD.textContent = Doc.formatFourSigFigs(quoteFeeUSD)
      page.quoteTokenBookingFees.textContent = Doc.formatFullPrecision(qProj.bookingFees * quoteFeeFactor, quoteFeeUI)
      page.quoteTokenSwapFeeN.textContent = String(quoteConfig.swapFeeN)
      page.quoteTokenSwapFees.textContent = Doc.formatFullPrecision(qProj.swapFeeReserves * quoteFeeFactor, quoteFeeUI)
    }
    page.totalAllocUSD.textContent = Doc.formatFourSigFigs(totalUSD)
  }

  /*
   * allocate opens a dialog to choose funding sources (if applicable) and
   * confirm allocations and start the bot.
   */
  allocate () {
    const f = this.fundingState()
    const {
      page, marketReport: { baseFiatRate, quoteFiatRate }, baseID, quoteID,
      baseFeeID, quoteFeeID, baseFeeFiatRate, quoteFeeFiatRate, cexName,
      baseFactor, quoteFactor, baseFeeFactor, quoteFeeFactor
    } = this

    const [proposedDexBase, proposedCexBase, baseSlider] = parseFundingOptions(f.base)
    const [proposedDexQuote, proposedCexQuote, quoteSlider] = parseFundingOptions(f.quote)

    const alloc = this.alloc = {
      dex: {
        [baseID]: proposedDexBase * baseFactor,
        [quoteID]: proposedDexQuote * quoteFactor
      },
      cex: {
        [baseID]: proposedCexBase * baseFactor,
        [quoteID]: proposedCexQuote * quoteFactor
      }
    }

    alloc.dex[baseFeeID] = Math.min((alloc.dex[baseFeeID] ?? 0) + (f.base.fees.req * baseFeeFactor), f.base.fees.avail * baseFeeFactor)
    alloc.dex[quoteFeeID] = Math.min((alloc.dex[quoteFeeID] ?? 0) + (f.quote.fees.req * quoteFeeFactor), f.quote.fees.avail * quoteFeeFactor)

    let totalUSD = (alloc.dex[baseID] / baseFactor * baseFiatRate) + (alloc.dex[quoteID] / quoteFactor * quoteFiatRate)
    totalUSD += (alloc.cex[baseID] / baseFactor * baseFiatRate) + (alloc.cex[quoteID] / quoteFactor * quoteFiatRate)
    if (baseFeeID !== baseID) totalUSD += alloc.dex[baseFeeID] / baseFeeFactor * baseFeeFiatRate
    if (quoteFeeID !== quoteID && quoteFeeID !== baseFeeID) totalUSD += alloc.dex[quoteFeeID] / quoteFeeFactor * quoteFeeFiatRate
    page.allocUSD.textContent = Doc.formatFourSigFigs(totalUSD)

    Doc.setVis(cexName, ...Doc.applySelector(page.allocationDialog, '[data-cex-only]'))
    Doc.setVis(f.fundedAndBalanced, page.fundedAndBalancedBox)
    Doc.setVis(f.base.transferable + f.quote.transferable > 0, page.hasTransferable)
    Doc.setVis(f.fundedAndNotBalanced, page.fundedAndNotBalancedBox)
    Doc.setVis(f.starved, page.starvedBox)
    page.startBttn.classList.toggle('go', f.fundedAndBalanced)
    page.startBttn.classList.toggle('warning', !f.fundedAndBalanced)
    page.proposedDexBaseAlloc.classList.toggle('text-warning', !(f.base.fundedAndBalanced || f.base.fundedAndNotBalanced))
    page.proposedDexQuoteAlloc.classList.toggle('text-warning', !(f.quote.fundedAndBalanced || f.quote.fundedAndNotBalanced))

    const setBaseProposal = (dex: number, cex: number) => {
      page.proposedDexBaseAlloc.textContent = Doc.formatFourSigFigs(dex)
      page.proposedDexBaseAllocUSD.textContent = Doc.formatFourSigFigs(dex * baseFiatRate)
      page.proposedCexBaseAlloc.textContent = Doc.formatFourSigFigs(cex)
      page.proposedCexBaseAllocUSD.textContent = Doc.formatFourSigFigs(cex * baseFiatRate)
    }
    setBaseProposal(proposedDexBase, proposedCexBase)

    Doc.setVis(baseSlider, page.baseAllocSlider)
    if (baseSlider) {
      const dexRange = baseSlider.right.dex - baseSlider.left.dex
      const cexRange = baseSlider.right.cex - baseSlider.left.cex
      this.baseAllocSlider.setValue(0.5)
      this.baseAllocSlider.changed = (r: number) => {
        const dexAlloc = baseSlider.left.dex + r * dexRange
        const cexAlloc = baseSlider.left.cex + r * cexRange
        alloc.dex[baseID] = dexAlloc * baseFeeFactor
        alloc.cex[baseID] = cexAlloc * baseFeeFactor
        setBaseProposal(dexAlloc, cexAlloc)
      }
    }

    const setQuoteProposal = (dex: number, cex: number) => {
      page.proposedDexQuoteAlloc.textContent = Doc.formatFourSigFigs(dex)
      page.proposedDexQuoteAllocUSD.textContent = Doc.formatFourSigFigs(dex * quoteFiatRate)
      page.proposedCexQuoteAlloc.textContent = Doc.formatFourSigFigs(cex)
      page.proposedCexQuoteAllocUSD.textContent = Doc.formatFourSigFigs(cex * quoteFiatRate)
    }
    setQuoteProposal(proposedDexQuote, proposedCexQuote)

    Doc.setVis(quoteSlider, page.quoteAllocSlider)
    if (quoteSlider) {
      const dexRange = quoteSlider.right.dex - quoteSlider.left.dex
      const cexRange = quoteSlider.right.cex - quoteSlider.left.cex
      this.quoteAllocSlider.setValue(0.5)
      this.quoteAllocSlider.changed = (r: number) => {
        const dexAlloc = quoteSlider.left.dex + r * dexRange
        const cexAlloc = quoteSlider.left.cex + r * cexRange
        alloc.dex[quoteID] = dexAlloc * quoteFeeFactor
        alloc.cex[quoteID] = cexAlloc * quoteFeeFactor
        setQuoteProposal(dexAlloc, cexAlloc)
      }
    }

    Doc.setVis(baseFeeID !== baseID, ...Doc.applySelector(page.allocationDialog, '[data-base-token-fees]'))
    if (baseFeeID !== baseID) {
      const reqFees = f.base.fees.req + (baseFeeID === quoteFeeID ? f.quote.fees.req : 0)
      const proposedFees = Math.min(reqFees, f.base.fees.avail)
      page.proposedDexBaseFeeAlloc.textContent = Doc.formatFourSigFigs(proposedFees)
      page.proposedDexBaseFeeAllocUSD.textContent = Doc.formatFourSigFigs(proposedFees * baseFeeFiatRate)
      page.proposedDexBaseFeeAlloc.classList.toggle('text-warning', !f.base.fees.funded)
    }

    const needQuoteTokenFees = quoteFeeID !== quoteID && quoteFeeID !== baseFeeID
    Doc.setVis(needQuoteTokenFees, ...Doc.applySelector(page.allocationDialog, '[data-quote-token-fees]'))
    if (needQuoteTokenFees) {
      const proposedFees = Math.min(f.quote.fees.req, f.quote.fees.avail)
      page.proposedDexQuoteFeeAlloc.textContent = Doc.formatFourSigFigs(proposedFees)
      page.proposedDexQuoteFeeAllocUSD.textContent = Doc.formatFourSigFigs(proposedFees * quoteFeeFiatRate)
      page.proposedDexQuoteFeeAlloc.classList.toggle('text-warning', !f.quote.fees.funded)
    }

    Doc.show(page.allocationDialog)
    const closeDialog = (e: MouseEvent) => {
      if (Doc.mouseInElement(e, page.allocationDialog)) return
      this.hideAllocationDialog()
      Doc.unbind(document, 'click', closeDialog)
    }
    Doc.bind(document, 'click', closeDialog)
  }

  hideAllocationDialog () {
    Doc.hide(this.page.allocationDialog)
  }

  async start () {
    const { page, alloc, baseID, quoteID, host, cexName, cfg: { uiConfig: { cexRebalance } } } = this

    Doc.hide(page.errMsg)
    if (cexName && !app().mmStatus.cexes[cexName]?.connected) {
      page.errMsg.textContent = `${cexName} not connected`
      Doc.show(page.errMsg)
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
    if (cexRebalance) startConfig.autoRebalance = this.autoRebalanceSettings()

    try {
      app().log('mm', 'starting mm bot', startConfig)
      const res = await MM.startBot(startConfig)
      if (!app().checkResponse(res)) throw res
    } catch (e) {
      page.errMsg.textContent = intl.prep(intl.ID_API_ERROR, e)
      Doc.show(page.errMsg)
      return
    }
    this.hideAllocationDialog()
  }

  autoRebalanceSettings (): AutoRebalanceConfig {
    const {
      proj: { bProj, qProj, alloc }, baseFeeID, quoteFeeID, cfg: { uiConfig: { baseConfig, quoteConfig } },
      baseID, quoteID, cexName, mktID, bui, qui
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
    const [baseMinWithdraw, quoteMinWithdraw] = [mkt.baseMinWithdraw * bui.conventional.conversionFactor, mkt.quoteMinWithdraw * qui.conventional.conversionFactor]
    const [minB, maxB] = [baseMinWithdraw, Math.max(baseMinWithdraw * 2, maxBase)]
    const minBaseTransfer = minB + baseConfig.transferFactor * (maxB - minB)
    const [minQ, maxQ] = [quoteMinWithdraw, Math.max(quoteMinWithdraw * 2, maxQuote)]
    const minQuoteTransfer = minQ + quoteConfig.transferFactor * (maxQ - minQ)
    return { minBaseTransfer, minQuoteTransfer }
  }

  reconfigure () {
    const { host, baseID, quoteID, cexName, botType } = this
    app().loadPage('mmsettings', { host, baseID, quoteID, cexName, botType })
  }

  handleRunStats () {
    this.updateDisplay()
    this.updateTableRow()
  }
}
