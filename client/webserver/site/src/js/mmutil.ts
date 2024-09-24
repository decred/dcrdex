import {
  app,
  PageElement,
  BotConfig,
  MMBotStatus,
  CEXConfig,
  MarketMakingStatus,
  ExchangeBalance,
  RunStats,
  StartConfig,
  MarketWithHost,
  RunningBotInventory,
  Spot,
  OrderPlacement,
  Token,
  UnitInfo,
  MarketReport,
  BotBalanceAllocation,
  ProjectedAlloc,
  BalanceNote,
  BotBalance,
  Order,
  LotFeeRange,
  BookingFees
} from './registry'
import { getJSON, postJSON } from './http'
import Doc, { clamp } from './doc'
import * as OrderUtil from './orderutil'
import { Chart, Region, Extents, Translator } from './charts'

export const GapStrategyMultiplier = 'multiplier'
export const GapStrategyAbsolute = 'absolute'
export const GapStrategyAbsolutePlus = 'absolute-plus'
export const GapStrategyPercent = 'percent'
export const GapStrategyPercentPlus = 'percent-plus'

export const botTypeBasicMM = 'basicMM'
export const botTypeArbMM = 'arbMM'
export const botTypeBasicArb = 'basicArb'

export interface CEXDisplayInfo {
  name: string
  logo: string
}

export const CEXDisplayInfos: Record<string, CEXDisplayInfo> = {
  'Binance': {
    name: 'Binance',
    logo: '/img/binance.com.png'
  },
  'BinanceUS': {
    name: 'Binance U.S.',
    logo: '/img/binance.us.png'
  }
}

/*
 * MarketMakerBot is the front end representation of the server's
 * mm.MarketMaker. MarketMakerBot is a singleton assigned to MM below.
 */
class MarketMakerBot {
  cexBalanceCache: Record<string, Record<number, ExchangeBalance>> = {}

  /*
   * updateBotConfig appends or updates the specified BotConfig.
   */
  async updateBotConfig (cfg: BotConfig) {
    return postJSON('/api/updatebotconfig', cfg)
  }

  /*
   * updateCEXConfig appends or updates the specified CEXConfig.
   */
  async updateCEXConfig (cfg: CEXConfig) {
    return postJSON('/api/updatecexconfig', cfg)
  }

  async removeBotConfig (host: string, baseID: number, quoteID: number) {
    return postJSON('/api/removebotconfig', { host, baseID, quoteID })
  }

  async report (host: string, baseID: number, quoteID: number) {
    return postJSON('/api/marketreport', { host, baseID, quoteID })
  }

  async startBot (config: StartConfig) {
    return await postJSON('/api/startmarketmakingbot', { config })
  }

  async stopBot (market: MarketWithHost) : Promise<void> {
    await postJSON('/api/stopmarketmakingbot', { market })
  }

  async status () : Promise<MarketMakingStatus> {
    return (await getJSON('/api/marketmakingstatus')).status
  }

  // botStats returns the RunStats for a running bot with the specified parameters.
  botStats (baseID: number, quoteID: number, host: string, startTime: number): RunStats | undefined {
    for (const botStatus of Object.values(app().mmStatus.bots)) {
      if (!botStatus.runStats) continue
      const runStats = botStatus.runStats
      const cfg = botStatus.config
      if (cfg.baseID === baseID && cfg.quoteID === quoteID && cfg.host === host && runStats.startTime === startTime) {
        return runStats
      }
    }
  }

  cachedCexBalance (cexName: string, assetID: number): ExchangeBalance | undefined {
    return this.cexBalanceCache[cexName]?.[assetID]
  }

  async cexBalance (cexName: string, assetID: number): Promise<ExchangeBalance> {
    if (!this.cexBalanceCache[cexName]) this.cexBalanceCache[cexName] = {}
    const cexBalance = (await postJSON('/api/cexbalance', { cexName, assetID })).cexBalance
    this.cexBalanceCache[cexName][assetID] = cexBalance
    return cexBalance
  }
}

// MM is the front end representation of the server's mm.MarketMaker.
export const MM = new MarketMakerBot()

export function runningBotInventory (assetID: number): RunningBotInventory {
  return app().mmStatus.bots.reduce((v, { runStats, running }) => {
    if (!running || !runStats) return v
    const { dexBalances: d, cexBalances: c } = runStats
    v.cex.locked += c[assetID]?.locked ?? 0
    v.cex.locked += c[assetID]?.reserved ?? 0
    v.cex.avail += c[assetID]?.available ?? 0
    v.cex.total = v.cex.avail + v.cex.locked
    v.dex.locked += d[assetID]?.locked ?? 0
    v.dex.locked += d[assetID]?.reserved ?? 0
    v.dex.avail += d[assetID]?.available ?? 0
    v.dex.total = v.dex.avail + v.dex.locked
    v.avail += (d[assetID]?.available ?? 0) + (c[assetID]?.available ?? 0)
    v.locked += (d[assetID]?.locked ?? 0) + (c[assetID]?.locked ?? 0)
    return v
  }, { avail: 0, locked: 0, cex: { avail: 0, locked: 0, total: 0 }, dex: { avail: 0, locked: 0, total: 0 } })
}

export function setMarketElements (ancestor: PageElement, baseID: number, quoteID: number, host: string) {
  Doc.setText(ancestor, '[data-host]', host)
  const { unitInfo: bui, name: baseName, symbol: baseSymbol, token: baseToken } = app().assets[baseID]
  Doc.setText(ancestor, '[data-base-name]', baseName)
  Doc.setSrc(ancestor, '[data-base-logo]', Doc.logoPath(baseSymbol))
  Doc.setText(ancestor, '[data-base-ticker]', bui.conventional.unit)
  const { unitInfo: baseFeeUI, name: baseFeeName, symbol: baseFeeSymbol } = app().assets[baseToken ? baseToken.parentID : baseID]
  Doc.setText(ancestor, '[data-base-fee-name]', baseFeeName)
  Doc.setSrc(ancestor, '[data-base-fee-logo]', Doc.logoPath(baseFeeSymbol))
  Doc.setText(ancestor, '[data-base-fee-ticker]', baseFeeUI.conventional.unit)
  const { unitInfo: qui, name: quoteName, symbol: quoteSymbol, token: quoteToken } = app().assets[quoteID]
  Doc.setText(ancestor, '[data-quote-name]', quoteName)
  Doc.setSrc(ancestor, '[data-quote-logo]', Doc.logoPath(quoteSymbol))
  Doc.setText(ancestor, '[data-quote-ticker]', qui.conventional.unit)
  const { unitInfo: quoteFeeUI, name: quoteFeeName, symbol: quoteFeeSymbol } = app().assets[quoteToken ? quoteToken.parentID : quoteID]
  Doc.setText(ancestor, '[data-quote-fee-name]', quoteFeeName)
  Doc.setSrc(ancestor, '[data-quote-fee-logo]', Doc.logoPath(quoteFeeSymbol))
  Doc.setText(ancestor, '[data-quote-fee-ticker]', quoteFeeUI.conventional.unit)
}

export function setCexElements (ancestor: PageElement, cexName: string) {
  const dinfo = CEXDisplayInfos[cexName]
  Doc.setText(ancestor, '[data-cex-name]', dinfo.name)
  Doc.setSrc(ancestor, '[data-cex-logo]', dinfo.logo)
  for (const img of Doc.applySelector(ancestor, '[data-cex-logo]')) Doc.show(img)
}

export function calculateQuoteLot (lotSize: number, baseID: number, quoteID: number, spot?: Spot) {
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

export interface PlacementChartConfig {
  cexName: string
  botType: string
  baseFiatRate: number
  dict: {
    profit: number
    buyPlacements: OrderPlacement[]
    sellPlacements: OrderPlacement[]
  }
}

export class PlacementsChart extends Chart {
  cfg: PlacementChartConfig
  loadedCEX: string
  cexLogo: HTMLImageElement

  constructor (parent: PageElement) {
    super(parent, {
      resize: () => this.resized(),
      click: (/* e: MouseEvent */) => { /* pass */ },
      zoom: (/* bigger: boolean */) => { /* pass */ }
    })
  }

  resized () {
    this.render()
  }

  draw () { /* pass */ }

  setMarket (cfg: PlacementChartConfig) {
    this.cfg = cfg
    const { loadedCEX, cfg: { cexName } } = this
    if (cexName && cexName !== loadedCEX) {
      this.loadedCEX = cexName
      this.cexLogo = new Image()
      Doc.bind(this.cexLogo, 'load', () => { this.render() })
      this.cexLogo.src = CEXDisplayInfos[cexName || ''].logo
    }
    this.render()
  }

  render () {
    const { ctx, canvas, theme, cfg } = this
    if (canvas.width === 0 || !cfg) return
    const { dict: { buyPlacements, sellPlacements, profit }, baseFiatRate, botType } = cfg
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
      const ps = [...buyPlacements, ...sellPlacements]
      const matchBuffer = ps.reduce((sum: number, p: OrderPlacement) => sum + p.gapFactor, 0) / ps.length
      fauxSpacer = 0.01 * (1 + matchBuffer)
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
        const firstX = tools.x((isBasicMM ? firstPt.gapFactor : profit + fauxSpacer) * sideFactor)
        ctx.moveTo(firstX, y0)
        let cumulativeLots = 0
        for (let i = 0; i < placements.length; i++) {
          const p = placements[i]
          // For arb-mm, we don't know exactly
          const rawX = isBasicMM ? p.gapFactor : profit + (i + 1) * fauxSpacer
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

export function hostedMarketID (host: string, baseID: number, quoteID: number) {
  return `${host}-${baseID}-${quoteID}` // same as MarketWithHost.String()
}

export function liveBotConfig (host: string, baseID: number, quoteID: number): BotConfig | undefined {
  const s = liveBotStatus(host, baseID, quoteID)
  if (s) return s.config
}

export function liveBotStatus (host: string, baseID: number, quoteID: number): MMBotStatus | undefined {
  const statuses = (app().mmStatus.bots || []).filter((s: MMBotStatus) => {
    return s.config.baseID === baseID && s.config.quoteID === quoteID && s.config.host === host
  })
  if (statuses.length) return statuses[0]
}

interface Lotter {
  lots: number
}

function sumLots (lots: number, p: Lotter) {
  return lots + p.lots
}

interface AllocationProjection {
  bProj: ProjectedAlloc
  qProj: ProjectedAlloc
  alloc: Record<number, number>
}

function emptyProjection (): ProjectedAlloc {
  return { book: 0, bookingFees: 0, swapFeeReserves: 0, cex: 0, orderReserves: 0, slippageBuffer: 0 }
}

export class BotMarket {
  cfg: BotConfig
  host: string
  baseID: number
  baseSymbol: string
  baseTicker: string
  baseFeeID: number
  baseIsAccountLocker: boolean
  baseFeeSymbol: string
  baseFeeTicker: string
  baseToken?: Token
  quoteID: number
  quoteSymbol: string
  quoteTicker: string
  quoteFeeID: number
  quoteIsAccountLocker: boolean
  quoteFeeSymbol: string
  quoteFeeTicker: string
  quoteToken?: Token
  botType: string
  cexName: string
  dinfo: CEXDisplayInfo
  alloc: BotBalanceAllocation
  proj: AllocationProjection
  bui: UnitInfo
  baseFactor: number
  baseFeeUI: UnitInfo
  baseFeeFactor: number
  qui: UnitInfo
  quoteFactor: number
  quoteFeeUI: UnitInfo
  quoteFeeFactor: number
  id: string // includes host
  mktID: string
  lotSize: number
  lotSizeConv: number
  lotSizeUSD: number
  quoteLot: number
  quoteLotConv: number
  quoteLotUSD: number
  baseFeeFiatRate: number
  quoteFeeFiatRate: number
  baseLots: number
  quoteLots: number
  marketReport: MarketReport
  cexBaseBalance?: ExchangeBalance
  cexQuoteBalance?: ExchangeBalance
  nBuyPlacements: number
  nSellPlacements: number

  constructor (cfg: BotConfig) {
    const host = this.host = cfg.host
    const baseID = this.baseID = cfg.baseID
    const quoteID = this.quoteID = cfg.quoteID
    this.cexName = cfg.cexName
    const status = app().mmStatus.bots.find(({ config: c }: MMBotStatus) => c.baseID === baseID && c.quoteID === quoteID && c.host === host)
    if (!status) throw Error('where\'s the bot status?')
    this.cfg = status.config

    const { token: baseToken, symbol: baseSymbol, unitInfo: bui } = app().assets[baseID]
    this.baseSymbol = baseSymbol
    this.baseTicker = bui.conventional.unit
    this.bui = bui
    this.baseFactor = bui.conventional.conversionFactor
    this.baseToken = baseToken
    const baseFeeID = this.baseFeeID = baseToken ? baseToken.parentID : baseID
    const { unitInfo: baseFeeUI, symbol: baseFeeSymbol, wallet: baseWallet } = app().assets[this.baseFeeID]
    const traitAccountLocker = 1 << 14
    this.baseIsAccountLocker = (baseWallet.traits & traitAccountLocker) > 0
    this.baseFeeUI = baseFeeUI
    this.baseFeeTicker = baseFeeUI.conventional.unit
    this.baseFeeSymbol = baseFeeSymbol
    this.baseFeeFactor = this.baseFeeUI.conventional.conversionFactor

    const { token: quoteToken, symbol: quoteSymbol, unitInfo: qui } = app().assets[quoteID]
    this.quoteSymbol = quoteSymbol
    this.quoteTicker = qui.conventional.unit
    this.qui = qui
    this.quoteFactor = qui.conventional.conversionFactor
    this.quoteToken = quoteToken
    const quoteFeeID = this.quoteFeeID = quoteToken ? quoteToken.parentID : quoteID
    const { unitInfo: quoteFeeUI, symbol: quoteFeeSymbol, wallet: quoteWallet } = app().assets[this.quoteFeeID]
    this.quoteIsAccountLocker = (quoteWallet.traits & traitAccountLocker) > 0
    this.quoteFeeUI = quoteFeeUI
    this.quoteFeeTicker = quoteFeeUI.conventional.unit
    this.quoteFeeSymbol = quoteFeeSymbol
    this.quoteFeeFactor = this.quoteFeeUI.conventional.conversionFactor

    this.id = hostedMarketID(host, baseID, quoteID)
    this.mktID = `${baseSymbol}_${quoteSymbol}`

    const { markets } = app().exchanges[host]
    const { lotsize: lotSize } = markets[this.mktID]
    this.lotSize = lotSize
    this.lotSizeConv = lotSize / bui.conventional.conversionFactor
    this.quoteLot = calculateQuoteLot(lotSize, baseID, quoteID)
    this.quoteLotConv = this.quoteLot / qui.conventional.conversionFactor

    this.baseFeeFiatRate = app().fiatRatesMap[baseFeeID]
    this.quoteFeeFiatRate = app().fiatRatesMap[quoteFeeID]

    if (cfg.arbMarketMakingConfig) {
      this.botType = botTypeArbMM
      this.baseLots = cfg.arbMarketMakingConfig.sellPlacements.reduce(sumLots, 0)
      this.quoteLots = cfg.arbMarketMakingConfig.buyPlacements.reduce(sumLots, 0)
      this.nBuyPlacements = cfg.arbMarketMakingConfig.buyPlacements.length
      this.nSellPlacements = cfg.arbMarketMakingConfig.sellPlacements.length
    } else if (cfg.simpleArbConfig) {
      this.botType = botTypeBasicArb
      this.baseLots = cfg.uiConfig.simpleArbLots as number
      this.quoteLots = cfg.uiConfig.simpleArbLots as number
    } else if (cfg.basicMarketMakingConfig) { // basicmm
      this.botType = botTypeBasicMM
      this.baseLots = cfg.basicMarketMakingConfig.sellPlacements.reduce(sumLots, 0)
      this.quoteLots = cfg.basicMarketMakingConfig.buyPlacements.reduce(sumLots, 0)
      this.nBuyPlacements = cfg.basicMarketMakingConfig.buyPlacements.length
      this.nSellPlacements = cfg.basicMarketMakingConfig.sellPlacements.length
    }
  }

  async initialize (startupBalanceCache: Record<number, Promise<ExchangeBalance>>) {
    const { host, baseID, quoteID, lotSizeConv, quoteLotConv, cexName } = this
    const res = await MM.report(host, baseID, quoteID)
    const r = this.marketReport = res.report as MarketReport
    this.lotSizeUSD = lotSizeConv * r.baseFiatRate
    this.quoteLotUSD = quoteLotConv * r.quoteFiatRate
    this.proj = this.projectedAllocations()

    if (cexName) {
      const b = startupBalanceCache[baseID] = startupBalanceCache[baseID] || MM.cexBalance(cexName, baseID)
      const q = startupBalanceCache[quoteID] = startupBalanceCache[quoteID] || MM.cexBalance(cexName, quoteID)
      this.cexBaseBalance = await b
      this.cexQuoteBalance = await q
    }
  }

  status () {
    const { baseID, quoteID } = this
    const botStatus = app().mmStatus.bots.find((s: MMBotStatus) => s.config.baseID === baseID && s.config.quoteID === quoteID)
    if (!botStatus) return { botCfg: {} as BotConfig, running: false, runStats: {} as RunStats }
    const { config: botCfg, running, runStats } = botStatus
    return { botCfg, running, runStats }
  }

  /*
  * adjustedBalances calculates the user's available balances and fee-asset
  * balances for a market, with consideration for currently running bots.
  */
  adjustedBalances () {
    const {
      baseID, quoteID, baseFeeID, quoteFeeID, cexBaseBalance, cexQuoteBalance,
      baseFactor, quoteFactor, baseFeeFactor, quoteFeeFactor
    } = this
    const [baseWallet, quoteWallet] = [app().walletMap[baseID], app().walletMap[quoteID]]
    const [bInv, qInv] = [runningBotInventory(baseID), runningBotInventory(quoteID)]
    // In these available balance calcs, only subtract the available balance of
    // running bots, since the locked/reserved/immature is already subtracted
    // from the wallet's total available balance.
    const [cexBaseAvail, cexQuoteAvail] = [(cexBaseBalance?.available || 0) - bInv.cex.avail, (cexQuoteBalance?.available || 0) - qInv.cex.avail]
    const [dexBaseAvail, dexQuoteAvail] = [baseWallet.balance.available - bInv.dex.avail, quoteWallet.balance.available - qInv.dex.avail]
    const baseAvail = dexBaseAvail + cexBaseAvail
    const quoteAvail = dexQuoteAvail + cexQuoteAvail
    const baseFeeWallet = baseFeeID === baseID ? baseWallet : app().walletMap[baseFeeID]
    const quoteFeeWallet = quoteFeeID === quoteID ? quoteWallet : app().walletMap[quoteFeeID]

    let [baseFeeAvail, dexBaseFeeAvail, cexBaseFeeAvail] = [baseAvail, dexBaseAvail, cexBaseAvail]
    if (baseFeeID !== baseID) {
      const bFeeInv = runningBotInventory(baseID)
      dexBaseFeeAvail = baseFeeWallet.balance.available - bFeeInv.dex.total
      cexBaseFeeAvail = (cexBaseBalance?.available || 0) - bFeeInv.cex.total
      baseFeeAvail = dexBaseFeeAvail + cexBaseFeeAvail
    }
    let [quoteFeeAvail, dexQuoteFeeAvail, cexQuoteFeeAvail] = [quoteAvail, dexQuoteAvail, cexQuoteAvail]
    if (quoteFeeID !== quoteID) {
      const qFeeInv = runningBotInventory(quoteID)
      dexQuoteFeeAvail = quoteFeeWallet.balance.available - qFeeInv.dex.total
      cexQuoteFeeAvail = (cexQuoteBalance?.available || 0) - qFeeInv.cex.total
      quoteFeeAvail = dexQuoteFeeAvail + cexQuoteFeeAvail
    }
    return { // convert to conventioanl.
      baseAvail: baseAvail / baseFactor,
      quoteAvail: quoteAvail / quoteFactor,
      dexBaseAvail: dexBaseAvail / baseFactor,
      dexQuoteAvail: dexQuoteAvail / quoteFactor,
      cexBaseAvail: cexBaseAvail / baseFactor,
      cexQuoteAvail: cexQuoteAvail / quoteFactor,
      baseFeeAvail: baseFeeAvail / baseFeeFactor,
      quoteFeeAvail: quoteFeeAvail / quoteFeeFactor,
      dexBaseFeeAvail: dexBaseFeeAvail / baseFeeFactor,
      dexQuoteFeeAvail: dexQuoteFeeAvail / quoteFeeFactor,
      cexBaseFeeAvail: cexBaseFeeAvail / baseFeeFactor,
      cexQuoteFeeAvail: cexQuoteFeeAvail / quoteFeeFactor
    }
  }

  /*
   * feesAndCommit generates a snapshot of current market fees, as well as a
   * "commit", which is the funding dedicated to being on order. The commit
   * values do not include booking fees, order reserves, etc. just the order
   * quantity.
   */
  feesAndCommit () {
    const {
      baseID, quoteID, marketReport: { baseFees, quoteFees }, lotSize,
      baseLots, quoteLots, baseFeeID, quoteFeeID, baseIsAccountLocker, quoteIsAccountLocker,
      cfg: { uiConfig: { baseConfig, quoteConfig } }
    } = this

    return feesAndCommit(
      baseID, quoteID, baseFees, quoteFees, lotSize, baseLots, quoteLots,
      baseFeeID, quoteFeeID, baseIsAccountLocker, quoteIsAccountLocker,
      baseConfig.orderReservesFactor, quoteConfig.orderReservesFactor
    )
  }

  /*
   * projectedAllocations calculates the required asset allocations from the
   * user's configuration settings and the current market state.
   */
  projectedAllocations () {
    const {
      cfg: { uiConfig: { quoteConfig, baseConfig } },
      baseFactor, quoteFactor, baseID, quoteID, lotSizeConv, quoteLotConv,
      baseFeeFactor, quoteFeeFactor, baseFeeID, quoteFeeID, baseToken,
      quoteToken, cexName
    } = this
    const { commit, fees } = this.feesAndCommit()

    const bProj = emptyProjection()
    const qProj = emptyProjection()

    bProj.book = commit.dex.base.lots * lotSizeConv
    qProj.book = commit.cex.base.lots * quoteLotConv

    bProj.orderReserves = Math.max(commit.cex.base.val, commit.dex.base.val) * baseConfig.orderReservesFactor / baseFactor
    qProj.orderReserves = Math.max(commit.cex.quote.val, commit.dex.quote.val) * quoteConfig.orderReservesFactor / quoteFactor

    if (cexName) {
      bProj.cex = commit.cex.base.lots * lotSizeConv
      qProj.cex = commit.cex.quote.lots * quoteLotConv
    }

    bProj.bookingFees = fees.base.bookingFees / baseFeeFactor
    qProj.bookingFees = fees.quote.bookingFees / quoteFeeFactor

    if (baseToken) bProj.swapFeeReserves = fees.base.tokenFeesPerSwap * baseConfig.swapFeeN / baseFeeFactor
    if (quoteToken) qProj.swapFeeReserves = fees.quote.tokenFeesPerSwap * quoteConfig.swapFeeN / quoteFeeFactor
    qProj.slippageBuffer = (qProj.book + qProj.cex + qProj.orderReserves) * quoteConfig.slippageBufferFactor

    const alloc: Record<number, number> = {}
    const addAlloc = (assetID: number, amt: number) => { alloc[assetID] = (alloc[assetID] ?? 0) + amt }
    addAlloc(baseID, Math.round((bProj.book + bProj.cex + bProj.orderReserves) * baseFactor))
    addAlloc(baseFeeID, Math.round((bProj.bookingFees + bProj.swapFeeReserves) * baseFeeFactor))
    addAlloc(quoteID, Math.round((qProj.book + qProj.cex + qProj.orderReserves + qProj.slippageBuffer) * quoteFactor))
    addAlloc(quoteFeeID, Math.round((qProj.bookingFees + qProj.swapFeeReserves) * quoteFeeFactor))

    return { qProj, bProj, alloc }
  }

  /*
   * fundingState examines the projected allocations and the user's wallet
   * balances to determine whether the user can fund the bot fully, unbalanced,
   * or starved, and what funding source options might be available.
   */
  fundingState () {
    const {
      proj: { bProj, qProj }, baseID, quoteID, baseFeeID, quoteFeeID,
      cfg: { uiConfig: { cexRebalance } }, cexName
    } = this
    const {
      baseAvail, quoteAvail, dexBaseAvail, dexQuoteAvail, cexBaseAvail, cexQuoteAvail,
      dexBaseFeeAvail, dexQuoteFeeAvail
    } = this.adjustedBalances()

    const canRebalance = Boolean(cexName && cexRebalance)

    // Three possible states.
    // 1. We have the funding in the projection, and its in the right places.
    //    Give them some options for which wallet to pull order reserves from,
    //    but they can start immediately..
    // 2. We have the funding, but it's in the wrong place or the wrong asset,
    //    but we have deposits and withdraws enabled. We can offer them the
    //    option to start in an unbalanced state.
    // 3. We don't have the funds. We offer them an option to start in a
    //    starved state.
    const cexMinBaseAlloc = bProj.cex
    let [dexMinBaseAlloc, transferableBaseAlloc, dexBaseFeeReq] = [bProj.book, 0, 0]
    // Only add booking fees if this is the fee asset.
    if (baseID === baseFeeID) dexMinBaseAlloc += bProj.bookingFees
    // Base asset is a token.
    else dexBaseFeeReq += bProj.bookingFees + bProj.swapFeeReserves
    // If we can rebalance, the order reserves could potentially be withdrawn.
    if (canRebalance) transferableBaseAlloc += bProj.orderReserves
    // If we can't rebalance, order reserves are required in dex balance.
    else dexMinBaseAlloc += bProj.orderReserves
    // Handle the special case where the base asset it the quote asset's fee
    // asset.
    if (baseID === quoteFeeID) {
      if (canRebalance) transferableBaseAlloc += qProj.bookingFees + qProj.swapFeeReserves
      else dexMinBaseAlloc += qProj.bookingFees + qProj.swapFeeReserves
    }

    let [dexMinQuoteAlloc, cexMinQuoteAlloc, transferableQuoteAlloc, dexQuoteFeeReq] = [qProj.book, qProj.cex, 0, 0]
    if (quoteID === quoteFeeID) dexMinQuoteAlloc += qProj.bookingFees
    else dexQuoteFeeReq += qProj.bookingFees + qProj.swapFeeReserves
    if (canRebalance) transferableQuoteAlloc += qProj.orderReserves + qProj.slippageBuffer
    else {
      // The slippage reserves reserves should be split between cex and dex.
      dexMinQuoteAlloc += qProj.orderReserves
      const basis = qProj.book + qProj.cex + qProj.orderReserves
      dexMinQuoteAlloc += (qProj.book + qProj.orderReserves) / basis * qProj.slippageBuffer
      cexMinQuoteAlloc += qProj.cex / basis * qProj.slippageBuffer
    }
    if (quoteID === baseFeeID) {
      if (canRebalance) transferableQuoteAlloc += bProj.bookingFees + bProj.swapFeeReserves
      else dexMinQuoteAlloc += bProj.bookingFees + bProj.swapFeeReserves
    }

    const dexBaseFunded = dexBaseAvail >= dexMinBaseAlloc
    const cexBaseFunded = cexBaseAvail >= cexMinBaseAlloc
    const dexQuoteFunded = dexQuoteAvail >= dexMinQuoteAlloc
    const cexQuoteFunded = cexQuoteAvail >= cexMinQuoteAlloc
    const totalBaseReq = dexMinBaseAlloc + cexMinBaseAlloc + transferableBaseAlloc
    const totalQuoteReq = dexMinQuoteAlloc + cexMinQuoteAlloc + transferableQuoteAlloc
    const baseFundedAndBalanced = dexBaseFunded && cexBaseFunded && baseAvail >= totalBaseReq
    const quoteFundedAndBalanced = dexQuoteFunded && cexQuoteFunded && quoteAvail >= totalQuoteReq
    const baseFeesFunded = dexBaseFeeAvail >= dexBaseFeeReq
    const quoteFeesFunded = dexQuoteFeeAvail >= dexQuoteFeeReq

    const fundedAndBalanced = baseFundedAndBalanced && quoteFundedAndBalanced && baseFeesFunded && quoteFeesFunded

    // Are we funded but not balanced, but able to rebalance with a cex?
    let fundedAndNotBalanced = !fundedAndBalanced
    if (!fundedAndBalanced) {
      const ordersFunded = baseAvail >= totalBaseReq && quoteAvail >= totalQuoteReq
      const feesFunded = baseFeesFunded && quoteFeesFunded
      fundedAndNotBalanced = ordersFunded && feesFunded && canRebalance
    }

    return {
      base: {
        dex: {
          avail: dexBaseAvail,
          req: dexMinBaseAlloc,
          funded: dexBaseFunded
        },
        cex: {
          avail: cexBaseAvail,
          req: cexMinBaseAlloc,
          funded: cexBaseFunded
        },
        transferable: transferableBaseAlloc,
        fees: {
          avail: dexBaseFeeAvail,
          req: dexBaseFeeReq,
          funded: baseFeesFunded
        },
        fundedAndBalanced: baseFundedAndBalanced,
        fundedAndNotBalanced: !baseFundedAndBalanced && baseAvail >= totalBaseReq && canRebalance
      },
      quote: {
        dex: {
          avail: dexQuoteAvail,
          req: dexMinQuoteAlloc,
          funded: dexQuoteFunded
        },
        cex: {
          avail: cexQuoteAvail,
          req: cexMinQuoteAlloc,
          funded: cexQuoteFunded
        },
        transferable: transferableQuoteAlloc,
        fees: {
          avail: dexQuoteFeeAvail,
          req: dexQuoteFeeReq,
          funded: quoteFeesFunded
        },
        fundedAndBalanced: quoteFundedAndBalanced,
        fundedAndNotBalanced: !quoteFundedAndBalanced && quoteAvail >= totalQuoteReq && canRebalance
      },
      fundedAndBalanced,
      fundedAndNotBalanced,
      starved: !fundedAndBalanced && !fundedAndNotBalanced
    }
  }
}

export class RunningMarketMakerDisplay {
  div: PageElement
  page: Record<string, PageElement>
  mkt: BotMarket
  startTime: number
  ticker: any

  constructor (div: PageElement, page: string) {
    this.div = div
    this.page = Doc.parseTemplate(div)
    Doc.bind(this.page.stopBttn, 'click', () => this.stop())
    Doc.bind(this.page.runLogsBttn, 'click', () => {
      const { mkt: { baseID, quoteID, host }, startTime } = this
      app().loadPage('mmlogs', { baseID, quoteID, host, startTime, returnPage: page })
    })
  }

  async stop () {
    const { page, mkt: { host, baseID, quoteID } } = this
    const loaded = app().loading(page.stopBttn)
    await MM.stopBot({ host, baseID: baseID, quoteID: quoteID })
    loaded()
  }

  async setMarket (host: string, baseID: number, quoteID: number) {
    const botStatus = app().mmStatus.bots.find(({ config: c }: MMBotStatus) => c.baseID === baseID && c.quoteID === quoteID && c.host === host)
    if (!botStatus) return
    const mkt = new BotMarket(botStatus.config)
    await mkt.initialize({})
    this.setBotMarket(mkt)
  }

  async setBotMarket (mkt: BotMarket) {
    this.mkt = mkt
    const {
      page, div, mkt: {
        host, baseID, quoteID, baseFeeID, quoteFeeID, cexName, baseFeeSymbol,
        quoteFeeSymbol, baseFeeTicker, quoteFeeTicker, cfg, baseFactor, quoteFactor
      }
    } = this
    setMarketElements(div, baseID, quoteID, host)
    Doc.setVis(baseFeeID !== baseID, page.baseFeeReservesBox)
    Doc.setVis(quoteFeeID !== quoteID, page.quoteFeeReservesBox)
    Doc.setVis(Boolean(cexName), ...Doc.applySelector(div, '[data-cex-show]'))
    page.baseFeeLogo.src = Doc.logoPath(baseFeeSymbol)
    page.baseFeeTicker.textContent = baseFeeTicker
    page.quoteFeeLogo.src = Doc.logoPath(quoteFeeSymbol)
    page.quoteFeeTicker.textContent = quoteFeeTicker

    const basicCfg = cfg.basicMarketMakingConfig
    const gapStrategy = basicCfg?.gapStrategy ?? GapStrategyPercent
    let gapFactor = cfg.arbMarketMakingConfig?.profit ?? cfg.simpleArbConfig?.profitTrigger ?? 0
    if (basicCfg) {
      const buys = [...basicCfg.buyPlacements].sort((a: OrderPlacement, b: OrderPlacement) => a.gapFactor - b.gapFactor)
      const sells = [...basicCfg.sellPlacements].sort((a: OrderPlacement, b: OrderPlacement) => a.gapFactor - b.gapFactor)
      if (buys.length > 0) {
        if (sells.length > 0) {
          gapFactor = (buys[0].gapFactor + sells[0].gapFactor) / 2
        } else {
          gapFactor = buys[0].gapFactor
        }
      } else gapFactor = sells[0].gapFactor
    }
    Doc.hide(page.profitLabel, page.gapLabel, page.multiplierLabel, page.profitUnit, page.gapUnit, page.multiplierUnit)
    switch (gapStrategy) {
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        Doc.show(page.profitLabel, page.profitUnit)
        page.gapFactor.textContent = (gapFactor * 100).toFixed(2)
        break
      case GapStrategyMultiplier:
        Doc.show(page.multiplierLabel, page.multiplierUnit)
        page.gapFactor.textContent = (gapFactor * 100).toFixed(2)
        break
      default:
        page.gapFactor.textContent = Doc.formatFourSigFigs(gapFactor / OrderUtil.RateEncodingFactor * baseFactor / quoteFactor)
    }

    this.update()
    this.readBook()
  }

  handleBalanceNote (n: BalanceNote) {
    if (!this.mkt) return
    const { baseID, quoteID, baseFeeID, quoteFeeID } = this.mkt
    if (n.assetID !== baseID && n.assetID !== baseFeeID && n.assetID !== quoteID && n.assetID !== quoteFeeID) return
    this.update()
  }

  setTicker () {
    this.page.runTime.textContent = Doc.hmsSince(this.startTime)
  }

  update () {
    const {
      div, page, mkt: {
        baseID, quoteID, baseFeeID, quoteFeeID, baseFactor, quoteFactor, baseFeeFactor,
        quoteFeeFactor, marketReport: { baseFiatRate, quoteFiatRate }
      }
    } = this
    // Get fresh stats
    const { botCfg: { cexName, basicMarketMakingConfig: bmmCfg }, runStats } = this.mkt.status()
    Doc.hide(page.stats, page.cexRow, page.pendingDepositBox, page.pendingWithdrawalBox)
    if (!runStats) {
      if (this.ticker) {
        clearInterval(this.ticker)
        this.ticker = undefined
      }
      return
    } else if (!this.ticker) {
      this.startTime = runStats.startTime
      this.setTicker()
      this.ticker = setInterval(() => this.setTicker(), 1000)
    }

    Doc.show(page.stats)
    setSignedValue(runStats.profitLoss.profitRatio * 100, page.profit, page.profitSign, 2)
    setSignedValue(runStats.profitLoss.profit, page.profitLoss, page.plSign, 2)
    this.startTime = runStats.startTime

    const summedBalance = (b: BotBalance) => {
      if (!b) return 0
      return b.available + b.locked + b.pending + b.reserved
    }

    const dexBaseInv = summedBalance(runStats.dexBalances[baseID]) / baseFactor
    page.walletBaseInventory.textContent = Doc.formatFourSigFigs(dexBaseInv)
    page.walletBaseInvFiat.textContent = Doc.formatFourSigFigs(dexBaseInv * baseFiatRate, 2)
    const dexQuoteInv = summedBalance(runStats.dexBalances[quoteID]) / quoteFactor
    page.walletQuoteInventory.textContent = Doc.formatFourSigFigs(dexQuoteInv)
    page.walletQuoteInvFiat.textContent = Doc.formatFourSigFigs(dexQuoteInv * quoteFiatRate, 2)

    Doc.setVis(cexName, page.cexRow)
    if (cexName) {
      Doc.show(page.pendingDepositBox, page.pendingWithdrawalBox)
      setCexElements(div, cexName)
      const cexBaseInv = summedBalance(runStats.cexBalances[baseID]) / baseFactor
      page.cexBaseInventory.textContent = Doc.formatFourSigFigs(cexBaseInv)
      page.cexBaseInventoryFiat.textContent = Doc.formatFourSigFigs(cexBaseInv * baseFiatRate, 2)
      const cexQuoteInv = summedBalance(runStats.cexBalances[quoteID]) / quoteFactor
      page.cexQuoteInventory.textContent = Doc.formatFourSigFigs(cexQuoteInv)
      page.cexQuoteInventoryFiat.textContent = Doc.formatFourSigFigs(cexQuoteInv * quoteFiatRate, 2)
    }

    if (baseFeeID !== baseID) {
      const feeBalance = summedBalance(runStats.dexBalances[baseFeeID]) / baseFeeFactor
      page.baseFeeReserves.textContent = Doc.formatFourSigFigs(feeBalance)
    }
    if (quoteFeeID !== quoteID) {
      const feeBalance = summedBalance(runStats.dexBalances[quoteFeeID]) / quoteFeeFactor
      page.quoteFeeReserves.textContent = Doc.formatFourSigFigs(feeBalance)
    }

    page.pendingDeposits.textContent = String(Math.round(runStats.pendingDeposits))
    page.pendingWithdrawals.textContent = String(Math.round(runStats.pendingWithdrawals))
    page.completedMatches.textContent = String(Math.round(runStats.completedMatches))
    Doc.setVis(runStats.tradedUSD, page.tradedUSDBox)
    if (runStats.tradedUSD > 0) page.tradedUSD.textContent = Doc.formatFourSigFigs(runStats.tradedUSD)
    Doc.setVis(baseFiatRate, page.roundTripFeesBox)
    if (baseFiatRate) page.roundTripFeesUSD.textContent = Doc.formatFourSigFigs((runStats.feeGap?.roundTripFees / baseFactor * baseFiatRate) || 0)
    const basisPrice = app().conventionalRate(baseID, quoteID, runStats.feeGap?.basisPrice || 0)
    page.basisPrice.textContent = Doc.formatFourSigFigs(basisPrice)

    const displayFeeGap = !bmmCfg || bmmCfg.gapStrategy === GapStrategyAbsolutePlus || bmmCfg.gapStrategy === GapStrategyPercentPlus
    Doc.setVis(displayFeeGap, page.feeGapBox)
    if (displayFeeGap) {
      const feeGap = app().conventionalRate(baseID, quoteID, runStats.feeGap?.feeGap || 0)
      page.feeGap.textContent = Doc.formatFourSigFigs(feeGap)
      page.feeGapPct.textContent = (feeGap / basisPrice * 100 || 0).toFixed(2)
    }
    Doc.setVis(bmmCfg, page.gapStrategyBox)
    if (bmmCfg) page.gapStrategy.textContent = bmmCfg.gapStrategy

    const remoteGap = app().conventionalRate(baseID, quoteID, runStats.feeGap?.remoteGap || 0)
    Doc.setVis(remoteGap, page.remoteGapBox)
    if (remoteGap) {
      page.remoteGap.textContent = Doc.formatFourSigFigs(remoteGap)
      page.remoteGapPct.textContent = (remoteGap / basisPrice * 100 || 0).toFixed(2)
    }
  }

  readBook () {
    if (!this.mkt) return
    const { page, mkt: { host, mktID } } = this
    const orders = app().exchanges[host].markets[mktID].orders || []
    page.nBookedOrders.textContent = String(orders.filter((ord: Order) => ord.status === OrderUtil.StatusBooked).length)
  }
}

function setSignedValue (v: number, vEl: PageElement, signEl: PageElement, maxDecimals?: number) {
  vEl.textContent = Doc.formatFourSigFigs(v, maxDecimals)
  signEl.classList.toggle('ico-plus', v > 0)
  signEl.classList.toggle('text-good', v > 0)
  // signEl.classList.toggle('ico-minus', v < 0)
}

export function feesAndCommit (
  baseID: number, quoteID: number, baseFees: LotFeeRange, quoteFees: LotFeeRange,
  lotSize: number, baseLots: number, quoteLots: number, baseFeeID: number, quoteFeeID: number,
  baseIsAccountLocker: boolean, quoteIsAccountLocker: boolean, baseOrderReservesFactor: number,
  quoteOrderReservesFactor: number
) {
  const quoteLot = calculateQuoteLot(lotSize, baseID, quoteID)
  const [cexBaseLots, cexQuoteLots] = [quoteLots, baseLots]
  const commit = {
    dex: {
      base: {
        lots: baseLots,
        val: baseLots * lotSize
      },
      quote: {
        lots: quoteLots,
        val: quoteLots * quoteLot
      }
    },
    cex: {
      base: {
        lots: cexBaseLots,
        val: cexBaseLots * lotSize
      },
      quote: {
        lots: cexQuoteLots,
        val: cexQuoteLots * quoteLot
      }
    }
  }

  let baseTokenFeesPerSwap = 0
  let baseRedeemReservesPerLot = 0
  if (baseID !== baseFeeID) { // token
    baseTokenFeesPerSwap += baseFees.estimated.swap
    if (baseFeeID === quoteFeeID) baseTokenFeesPerSwap += quoteFees.estimated.redeem
  }
  let baseBookingFeesPerLot = baseFees.max.swap
  if (baseID === quoteFeeID) baseBookingFeesPerLot += quoteFees.max.redeem
  if (baseIsAccountLocker) {
    baseBookingFeesPerLot += baseFees.max.refund
    if (!quoteIsAccountLocker && baseFeeID !== quoteFeeID) baseRedeemReservesPerLot = baseFees.max.redeem
  }

  let quoteTokenFeesPerSwap = 0
  let quoteRedeemReservesPerLot = 0
  if (quoteID !== quoteFeeID) {
    quoteTokenFeesPerSwap += quoteFees.estimated.swap
    if (quoteFeeID === baseFeeID) quoteTokenFeesPerSwap += baseFees.estimated.redeem
  }
  let quoteBookingFeesPerLot = quoteFees.max.swap
  if (quoteID === baseFeeID) quoteBookingFeesPerLot += baseFees.max.redeem
  if (quoteIsAccountLocker) {
    quoteBookingFeesPerLot += quoteFees.max.refund
    if (!baseIsAccountLocker && quoteFeeID !== baseFeeID) quoteRedeemReservesPerLot = quoteFees.max.redeem
  }

  const baseReservesFactor = 1 + baseOrderReservesFactor
  const quoteReservesFactor = 1 + quoteOrderReservesFactor

  const baseBookingFees = (baseBookingFeesPerLot * baseLots) * baseReservesFactor
  const baseRedeemFees = (baseRedeemReservesPerLot * quoteLots) * quoteReservesFactor
  const quoteBookingFees = (quoteBookingFeesPerLot * quoteLots) * quoteReservesFactor
  const quoteRedeemFees = (quoteRedeemReservesPerLot * baseLots) * baseReservesFactor

  const fees: BookingFees = {
    base: {
      ...baseFees,
      bookingFeesPerLot: baseBookingFeesPerLot,
      bookingFeesPerCounterLot: baseRedeemReservesPerLot,
      bookingFees: baseBookingFees + baseRedeemFees,
      swapReservesFactor: baseReservesFactor,
      redeemReservesFactor: quoteReservesFactor,
      tokenFeesPerSwap: baseTokenFeesPerSwap
    },
    quote: {
      ...quoteFees,
      bookingFeesPerLot: quoteBookingFeesPerLot,
      bookingFeesPerCounterLot: quoteRedeemReservesPerLot,
      bookingFees: quoteBookingFees + quoteRedeemFees,
      swapReservesFactor: quoteReservesFactor,
      redeemReservesFactor: baseReservesFactor,
      tokenFeesPerSwap: quoteTokenFeesPerSwap
    }
  }

  return { commit, fees }
}
