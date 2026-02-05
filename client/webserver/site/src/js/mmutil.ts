import {
  app,
  PageElement,
  BotConfig,
  MMBotStatus,
  CEXConfig,
  MarketMakingStatus,
  ExchangeBalance,
  RunStats,
  MarketWithHost,
  RunningBotInventory,
  Spot,
  OrderPlacement,
  Token,
  UnitInfo,
  MarketReport,
  BotBalanceAllocation,
  BalanceNote,
  BotBalance,
  Order,
  BotProblems,
  EpochReportNote,
  OrderReport,
  EpochReport,
  TradePlacement,
  SupportedAsset,
  CEXProblemsNote,
  CEXProblems,
  AutoRebalanceConfig
} from './registry'
import { getJSON, postJSON } from './http'
import Doc, { clamp } from './doc'
import * as OrderUtil from './orderutil'
import { Chart, Region, Extents, Translator } from './charts'
import * as intl from './locales'
import { Forms } from './forms'

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
  },
  'Bitget': {
    name: 'Bitget',
    logo: '/img/bitget.com.png'
  },
  'Coinbase': {
    name: 'Coinbase',
    logo: '/img/coinbase.com.png'
  },
  'MEXC': {
    name: 'MEXC',
    logo: '/img/mexc.com.png'
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
   * updateRunningBot updates the BotConfig and inventory for a running bot.
   */
  async updateRunningBot (cfg: BotConfig, diffs: BotBalanceAllocation, autoRebalanceCfg?: AutoRebalanceConfig) {
    const req: any = { cfg, diffs }
    if (autoRebalanceCfg) req.autoRebalanceCfg = autoRebalanceCfg
    return postJSON('/api/updaterunningbot', req)
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

  async maxFundingFees (market: MarketWithHost, maxBuyPlacements: number, maxSellPlacements: number, baseOptions: Record<string, string>, quoteOptions: Record<string, string>) {
    return postJSON('/api/maxfundingfees', { market, maxBuyPlacements, maxSellPlacements, baseOptions, quoteOptions })
  }

  async startBot (config: MarketWithHost) {
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

  async availableBalances (market: MarketWithHost, cexBaseID: number, cexQuoteID: number, cexName?: string) : Promise<{ dexBalances: Record<number, number>, cexBalances: Record<number, number> }> {
    return await postJSON('/api/availablebalances', { market, cexBaseID, cexQuoteID, cexName })
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

export function feeAssetID (assetID: number) {
  const asset = app().assets[assetID]
  if (asset.token) return asset.token.parentID
  return assetID
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
  rateStep: number
  baseFeeFiatRate: number
  quoteFeeFiatRate: number
  marketReport: MarketReport

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
    const { lotsize: lotSize, ratestep: rateStep } = markets[this.mktID]
    this.lotSize = lotSize
    this.lotSizeConv = lotSize / bui.conventional.conversionFactor
    this.rateStep = rateStep
    this.quoteLot = calculateQuoteLot(lotSize, baseID, quoteID)
    this.quoteLotConv = this.quoteLot / qui.conventional.conversionFactor

    this.baseFeeFiatRate = app().fiatRatesMap[baseFeeID]
    this.quoteFeeFiatRate = app().fiatRatesMap[quoteFeeID]

    if (cfg.arbMarketMakingConfig) {
      this.botType = botTypeArbMM
    } else if (cfg.simpleArbConfig) {
      this.botType = botTypeBasicArb
    } else if (cfg.basicMarketMakingConfig) {
      this.botType = botTypeBasicMM
    }
  }

  async initialize () {
    const { host, baseID, quoteID, lotSizeConv, quoteLotConv } = this
    const res = await MM.report(host, baseID, quoteID)
    const r = this.marketReport = res.report as MarketReport
    this.lotSizeUSD = lotSizeConv * r.baseFiatRate
    this.quoteLotUSD = quoteLotConv * r.quoteFiatRate
  }

  status () {
    const { baseID, quoteID } = this
    const botStatus = app().mmStatus.bots.find((s: MMBotStatus) => s.config.baseID === baseID && s.config.quoteID === quoteID)
    if (!botStatus) return { botCfg: {} as BotConfig, running: false, stopping: false, runStats: {} as RunStats }
    const { config: botCfg, running, stopping, runStats, latestEpoch, cexProblems } = botStatus
    return { botCfg, running, stopping, runStats, latestEpoch, cexProblems }
  }

  /*
  * adjustedBalances calculates the user's available balances and fee-asset
  * balances for a market, with consideration for currently running bots.
  */
  adjustedBalances () {
    const {
      baseID, quoteID, baseFeeID, quoteFeeID, cexName,
      baseFactor, quoteFactor, baseFeeFactor, quoteFeeFactor
    } = this
    const [baseWallet, quoteWallet] = [app().walletMap[baseID], app().walletMap[quoteID]]
    const [bInv, qInv] = [runningBotInventory(baseID), runningBotInventory(quoteID)]

    // In these available balance calcs, only subtract the available balance of
    // running bots, since the locked/reserved/immature is already subtracted
    // from the wallet's total available balance.
    let cexBaseAvail = 0
    let cexQuoteAvail = 0
    let cexBaseBalance: ExchangeBalance | undefined
    let cexQuoteBalance: ExchangeBalance | undefined
    if (cexName) {
      const cex = app().mmStatus.cexes[cexName]
      if (!cex) throw Error('where\'s the cex status?')
      cexBaseBalance = cex.balances[baseID]
      cexQuoteBalance = cex.balances[quoteID]
    }
    if (cexBaseBalance) cexBaseAvail = (cexBaseBalance.available || 0) - bInv.cex.avail
    if (cexQuoteBalance) cexQuoteAvail = (cexQuoteBalance.available || 0) - qInv.cex.avail
    const [dexBaseAvail, dexQuoteAvail] = [baseWallet.balance.available - bInv.dex.avail, quoteWallet.balance.available - qInv.dex.avail]
    const baseAvail = dexBaseAvail + cexBaseAvail
    const quoteAvail = dexQuoteAvail + cexQuoteAvail
    const baseFeeWallet = baseFeeID === baseID ? baseWallet : app().walletMap[baseFeeID]
    const quoteFeeWallet = quoteFeeID === quoteID ? quoteWallet : app().walletMap[quoteFeeID]

    let [baseFeeAvail, dexBaseFeeAvail, cexBaseFeeAvail] = [baseAvail, dexBaseAvail, cexBaseAvail]
    if (baseFeeID !== baseID) {
      const bFeeInv = runningBotInventory(baseID)
      dexBaseFeeAvail = baseFeeWallet.balance.available - bFeeInv.dex.total
      if (cexBaseBalance) cexBaseFeeAvail = (cexBaseBalance.available || 0) - bFeeInv.cex.total
      baseFeeAvail = dexBaseFeeAvail + cexBaseFeeAvail
    }
    let [quoteFeeAvail, dexQuoteFeeAvail, cexQuoteFeeAvail] = [quoteAvail, dexQuoteAvail, cexQuoteAvail]
    if (quoteFeeID !== quoteID) {
      const qFeeInv = runningBotInventory(quoteID)
      dexQuoteFeeAvail = quoteFeeWallet.balance.available - qFeeInv.dex.total
      if (cexQuoteBalance) cexQuoteFeeAvail = (cexQuoteBalance.available || 0) - qFeeInv.cex.total
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
}

export type RunningMMDisplayElements = {
  orderReportForm: PageElement
  dexBalancesRowTmpl: PageElement
  placementRowTmpl: PageElement
  placementAmtRowTmpl: PageElement
}

export class RunningMarketMakerDisplay {
  div: PageElement
  page: Record<string, PageElement>
  mkt: BotMarket
  startTime: number
  ticker: any
  currentForm: PageElement
  forms: Forms
  latestEpoch?: EpochReport
  cexProblems?: CEXProblems
  orderReportFormEl: PageElement
  orderReportForm: Record<string, PageElement>
  displayedOrderReportFormSide: 'buys' | 'sells'
  dexBalancesRowTmpl: PageElement
  placementRowTmpl: PageElement
  placementAmtRowTmpl: PageElement
  loaderCleanup: (() => void) | null = null

  constructor (div: PageElement, forms: Forms, elements: RunningMMDisplayElements, page: string) {
    this.div = div
    this.page = Doc.parseTemplate(div)
    this.orderReportFormEl = elements.orderReportForm
    this.orderReportForm = Doc.idDescendants(elements.orderReportForm)
    this.dexBalancesRowTmpl = elements.dexBalancesRowTmpl
    this.placementRowTmpl = elements.placementRowTmpl
    this.placementAmtRowTmpl = elements.placementAmtRowTmpl
    Doc.cleanTemplates(this.dexBalancesRowTmpl, this.placementRowTmpl, this.placementAmtRowTmpl)
    this.forms = forms
    Doc.bind(this.page.stopBttn, 'click', () => this.stop())
    Doc.bind(this.page.runLogsBttn, 'click', () => {
      const { mkt: { baseID, quoteID, host }, startTime } = this
      app().loadPage('mmlogs', { baseID, quoteID, host, startTime, returnPage: page })
    })
    Doc.bind(this.page.settingsBttn, 'click', () => {
      const { mkt: { baseID, quoteID, host } } = this
      app().loadPage('mmsettings', { baseID, quoteID, host })
    })
    Doc.bind(this.page.buyOrdersBttn, 'click', () => this.showOrderReport('buys'))
    Doc.bind(this.page.sellOrdersBttn, 'click', () => this.showOrderReport('sells'))
  }

  async stop () {
    const { mkt: { host, baseID, quoteID } } = this
    await MM.stopBot({ host, baseID: baseID, quoteID: quoteID })
    // Refresh status to get the new stopping state and show spinner
    await app().fetchMMStatus()
    this.update()
  }

  async setMarket (host: string, baseID: number, quoteID: number) {
    const botStatus = app().mmStatus.bots.find(({ config: c }: MMBotStatus) => c.baseID === baseID && c.quoteID === quoteID && c.host === host)
    if (!botStatus) return
    const mkt = new BotMarket(botStatus.config)
    await mkt.initialize()
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

  handleEpochReportNote (n: EpochReportNote) {
    if (!this.mkt) return
    const { baseID, quoteID, host } = this.mkt
    if (n.baseID !== baseID || n.quoteID !== quoteID || n.host !== host) return
    if (!n.report) return
    this.latestEpoch = n.report
    if (this.forms.currentForm === this.orderReportFormEl && this.forms.currentFormID === this.mkt.id) {
      const orderReport = this.displayedOrderReportFormSide === 'buys' ? n.report.buysReport : n.report.sellsReport
      if (orderReport) this.updateOrderReport(orderReport, this.displayedOrderReportFormSide, n.report.epochNum)
      else this.forms.close()
    }
    this.update()
  }

  handleCexProblemsNote (n: CEXProblemsNote) {
    if (!this.mkt) return
    const { baseID, quoteID, host } = this.mkt
    if (n.baseID !== baseID || n.quoteID !== quoteID || n.host !== host) return
    this.cexProblems = n.problems
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
    const { botCfg: { cexName, basicMarketMakingConfig: bmmCfg }, stopping, runStats, latestEpoch, cexProblems } = this.mkt.status()
    const { cexBaseID, cexQuoteID } = this.mkt.cfg

    this.latestEpoch = latestEpoch
    this.cexProblems = cexProblems

    // Show loading spinner on stop button while bot is stopping
    if (stopping && !this.loaderCleanup) {
      this.loaderCleanup = app().loading(page.stopBttn)
    } else if (!stopping && this.loaderCleanup) {
      this.loaderCleanup()
      this.loaderCleanup = null
    }

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
      const cexBaseInv = summedBalance(runStats.cexBalances[cexBaseID]) / baseFactor
      page.cexBaseInventory.textContent = Doc.formatFourSigFigs(cexBaseInv)
      page.cexBaseInventoryFiat.textContent = Doc.formatFourSigFigs(cexBaseInv * baseFiatRate, 2)
      const cexQuoteInv = summedBalance(runStats.cexBalances[cexQuoteID]) / quoteFactor
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

    Doc.setVis(latestEpoch?.buysReport, page.buyOrdersReportBox)
    if (latestEpoch?.buysReport) {
      const allPlaced = allOrdersPlaced(latestEpoch.buysReport)
      Doc.setVis(allPlaced, page.buyOrdersSuccess)
      Doc.setVis(!allPlaced, page.buyOrdersFailed)
    }

    Doc.setVis(latestEpoch?.sellsReport, page.sellOrdersReportBox)
    if (latestEpoch?.sellsReport) {
      const allPlaced = allOrdersPlaced(latestEpoch.sellsReport)
      Doc.setVis(allPlaced, page.sellOrdersSuccess)
      Doc.setVis(!allPlaced, page.sellOrdersFailed)
    }

    const preOrderProblemMessages = botProblemMessages(latestEpoch?.preOrderProblems, this.mkt.cexName, this.mkt.host)
    const cexErrorMessages = cexProblemMessages(this.cexProblems)
    const allMessages = [...preOrderProblemMessages, ...cexErrorMessages]
    Doc.setVis(allMessages.length > 0, page.preOrderProblemsBox)
    Doc.empty(page.preOrderProblemsBox)
    for (const msg of allMessages) {
      const spanEl = document.createElement('span') as PageElement
      spanEl.textContent = `- ${msg}`
      page.preOrderProblemsBox.appendChild(spanEl)
    }
  }

  updateOrderReport (report: OrderReport, side: 'buys' | 'sells', epochNum: number) {
    const form = this.orderReportForm
    const sideTxt = side === 'buys' ? intl.prep(intl.ID_BUY) : intl.prep(intl.ID_SELL)
    form.orderReportTitle.textContent = intl.prep(intl.ID_ORDER_REPORT_TITLE, { side: sideTxt, epochNum: `${epochNum}` })

    Doc.setVis(report.error, form.orderReportError)
    Doc.setVis(!report.error, form.orderReportDetails)
    if (report.error) {
      const problemMessages = botProblemMessages(report.error, this.mkt.cexName, this.mkt.host)
      Doc.empty(form.orderReportError)
      for (const msg of problemMessages) {
        const spanEl = document.createElement('span') as PageElement
        spanEl.textContent = `- ${msg}`
        form.orderReportError.appendChild(spanEl)
      }
      return
    }

    Doc.empty(form.dexBalancesBody, form.placementsBody)
    const createRow = (assetID: number): [PageElement, number] => {
      const row = this.dexBalancesRowTmpl.cloneNode(true) as HTMLElement
      const rowTmpl = Doc.parseTemplate(row)
      const asset = app().assets[assetID]
      rowTmpl.asset.textContent = asset.symbol.toUpperCase()
      rowTmpl.assetLogo.src = Doc.logoPath(asset.symbol)
      const unitInfo = asset.unitInfo
      const available = report.availableDexBals[assetID] ? report.availableDexBals[assetID].available : 0
      const required = report.requiredDexBals[assetID] ? report.requiredDexBals[assetID] : 0
      const remaining = report.remainingDexBals[assetID] ? report.remainingDexBals[assetID] : 0
      const pending = report.availableDexBals[assetID] ? report.availableDexBals[assetID].pending : 0
      const locked = report.availableDexBals[assetID] ? report.availableDexBals[assetID].locked : 0
      const used = report.usedDexBals[assetID] ? report.usedDexBals[assetID] : 0
      rowTmpl.available.textContent = Doc.formatCoinValue(available, unitInfo)
      rowTmpl.locked.textContent = Doc.formatCoinValue(locked, unitInfo)
      rowTmpl.required.textContent = Doc.formatCoinValue(required, unitInfo)
      rowTmpl.remaining.textContent = Doc.formatCoinValue(remaining, unitInfo)
      rowTmpl.pending.textContent = Doc.formatCoinValue(pending, unitInfo)
      rowTmpl.used.textContent = Doc.formatCoinValue(used, unitInfo)
      const deficiency = safeSub(required, available)
      rowTmpl.deficiency.textContent = Doc.formatCoinValue(deficiency, unitInfo)
      if (deficiency > 0) rowTmpl.deficiency.classList.add('text-warning')
      const deficiencyWithPending = safeSub(deficiency, pending)
      rowTmpl.deficiencyWithPending.textContent = Doc.formatCoinValue(deficiencyWithPending, unitInfo)
      if (deficiencyWithPending > 0) rowTmpl.deficiencyWithPending.classList.add('text-warning')
      return [row, deficiency]
    }
    const setDeficiencyVisibility = (deficiency: boolean, rows: HTMLElement[]) => {
      Doc.setVis(deficiency, form.dexDeficiencyHeader, form.dexDeficiencyWithPendingHeader)
      for (const row of rows) {
        const rowTmpl = Doc.parseTemplate(row)
        Doc.setVis(deficiency, rowTmpl.deficiency, rowTmpl.deficiencyWithPending)
      }
    }
    const assetIDs = [this.mkt.baseID, this.mkt.quoteID]
    if (!assetIDs.includes(this.mkt.baseFeeID)) assetIDs.push(this.mkt.baseFeeID)
    if (!assetIDs.includes(this.mkt.quoteFeeID)) assetIDs.push(this.mkt.quoteFeeID)
    let totalDeficiency = 0
    const rows : PageElement[] = []
    for (const assetID of assetIDs) {
      const [row, deficiency] = createRow(assetID)
      totalDeficiency += deficiency
      form.dexBalancesBody.appendChild(row)
      rows.push(row)
    }
    setDeficiencyVisibility(totalDeficiency > 0, rows)

    Doc.setVis(this.mkt.cexName, form.cexSection, form.counterTradeRateHeader, form.multiHopRatesHeader, form.requiredCEXHeader, form.usedCEXHeader)
    let cexAsset: SupportedAsset
    if (this.mkt.cexName) {
      const isMultiHop = this.mkt.cfg.arbMarketMakingConfig && this.mkt.cfg.arbMarketMakingConfig.multiHop
      Doc.setVis(isMultiHop, form.multiHopRatesHeader)
      Doc.setVis(!isMultiHop, form.counterTradeRateHeader)
      const cexDisplayInfo = CEXDisplayInfos[this.mkt.cexName]
      if (cexDisplayInfo) {
        form.cexLogo.src = cexDisplayInfo.logo
        form.cexBalancesTitle.textContent = intl.prep(intl.ID_CEX_BALANCES, { cexName: cexDisplayInfo.name })
      } else {
        console.error(`CEXDisplayInfo not found for ${this.mkt.cexName}`)
      }
      const cexAssetID = side === 'buys' ? this.mkt.baseID : this.mkt.quoteID
      cexAsset = app().assets[cexAssetID]
      form.cexAsset.textContent = cexAsset.symbol.toUpperCase()
      form.cexAssetLogo.src = Doc.logoPath(cexAsset.symbol)
      const availableCexBal = report.availableCexBal ? report.availableCexBal.available : 0
      const requiredCexBal = report.requiredCexBal ? report.requiredCexBal : 0
      const remainingCexBal = report.remainingCexBal ? report.remainingCexBal : 0
      const pendingCexBal = report.availableCexBal ? report.availableCexBal.pending : 0
      const reservedCexBal = report.availableCexBal ? report.availableCexBal.reserved : 0
      const usedCexBal = report.usedCexBal ? report.usedCexBal : 0
      const deficiencyCexBal = safeSub(requiredCexBal, availableCexBal)
      const deficiencyWithPendingCexBal = safeSub(deficiencyCexBal, pendingCexBal)
      form.cexAvailable.textContent = Doc.formatCoinValue(availableCexBal, cexAsset.unitInfo)
      form.cexLocked.textContent = Doc.formatCoinValue(reservedCexBal, cexAsset.unitInfo)
      form.cexRequired.textContent = Doc.formatCoinValue(requiredCexBal, cexAsset.unitInfo)
      form.cexRemaining.textContent = Doc.formatCoinValue(remainingCexBal, cexAsset.unitInfo)
      form.cexPending.textContent = Doc.formatCoinValue(pendingCexBal, cexAsset.unitInfo)
      form.cexUsed.textContent = Doc.formatCoinValue(usedCexBal, cexAsset.unitInfo)
      const deficient = deficiencyCexBal > 0
      Doc.setVis(deficient, form.cexDeficiencyHeader, form.cexDeficiencyWithPendingHeader,
        form.cexDeficiency, form.cexDeficiencyWithPending)
      if (deficient) {
        form.cexDeficiency.textContent = Doc.formatCoinValue(deficiencyCexBal, cexAsset.unitInfo)
        form.cexDeficiencyWithPending.textContent = Doc.formatCoinValue(deficiencyWithPendingCexBal, cexAsset.unitInfo)
        if (deficiencyWithPendingCexBal > 0) form.cexDeficiencyWithPending.classList.add('text-warning')
        else form.cexDeficiencyWithPending.classList.remove('text-warning')
      }
    }

    let anyErrors = false
    for (const placement of report.placements) if (placement.error) { anyErrors = true; break }
    Doc.setVis(anyErrors, form.errorHeader)
    const createPlacementRow = (placement: TradePlacement, priority: number): PageElement => {
      const row = this.placementRowTmpl.cloneNode(true) as HTMLElement
      const rowTmpl = Doc.parseTemplate(row)
      const baseUI = app().assets[this.mkt.baseID].unitInfo
      const quoteUI = app().assets[this.mkt.quoteID].unitInfo
      rowTmpl.priority.textContent = String(priority)
      rowTmpl.rate.textContent = Doc.formatRateFullPrecision(placement.rate, baseUI, quoteUI, this.mkt.rateStep)
      rowTmpl.lots.textContent = String(placement.lots)
      rowTmpl.standingLots.textContent = String(placement.standingLots)
      rowTmpl.orderedLots.textContent = String(placement.orderedLots)
      if (placement.standingLots + placement.orderedLots < placement.lots) {
        rowTmpl.lots.classList.add('text-warning')
        rowTmpl.standingLots.classList.add('text-warning')
        rowTmpl.orderedLots.classList.add('text-warning')
      }
      if (this.mkt.cfg.arbMarketMakingConfig && this.mkt.cfg.arbMarketMakingConfig.multiHop) {
        Doc.hide(rowTmpl.counterTradeRate)
        Doc.show(rowTmpl.multiHopRates)
        const multiHopCfg = this.mkt.cfg.arbMarketMakingConfig.multiHop
        let leg1: [number, number], leg2: [number, number]
        if (side === 'buys') {
          leg1 = multiHopCfg.baseAssetMarket
          leg2 = multiHopCfg.quoteAssetMarket
        } else {
          leg1 = multiHopCfg.quoteAssetMarket
          leg2 = multiHopCfg.baseAssetMarket
        }
        const [leg1Base, leg1Quote] = leg1
        const [leg2Base, leg2Quote] = leg2
        const leg1BaseSymbol = app().assets[leg1Base].symbol.toUpperCase()
        const leg1QuoteSymbol = app().assets[leg1Quote].symbol.toUpperCase()
        const leg2BaseSymbol = app().assets[leg2Base].symbol.toUpperCase()
        const leg2QuoteSymbol = app().assets[leg2Quote].symbol.toUpperCase()
        rowTmpl.aggRate.textContent = `${Doc.formatRateFullPrecision(placement.counterTradeRate, baseUI, quoteUI, this.mkt.rateStep)} ${app().assets[this.mkt.baseID].symbol.toUpperCase()}/${app().assets[this.mkt.quoteID].symbol.toUpperCase()}`
        rowTmpl.leg1Rate.textContent = `${Doc.formatRateFullPrecision(placement.multiHopRates[0], app().assets[leg1Base].unitInfo, app().assets[leg1Quote].unitInfo, this.mkt.rateStep)} ${leg1BaseSymbol}/${leg1QuoteSymbol}`
        rowTmpl.leg2Rate.textContent = `${Doc.formatRateFullPrecision(placement.multiHopRates[1], app().assets[leg2Base].unitInfo, app().assets[leg2Quote].unitInfo, this.mkt.rateStep)} ${leg2BaseSymbol}/${leg2QuoteSymbol}`
      } else {
        Doc.show(rowTmpl.counterTradeRate)
        Doc.hide(rowTmpl.multiHopRates)
        rowTmpl.counterTradeRate.textContent = `${Doc.formatRateFullPrecision(placement.counterTradeRate, baseUI, quoteUI, this.mkt.rateStep)} ${app().assets[this.mkt.baseID].symbol.toUpperCase()}/${app().assets[this.mkt.quoteID].symbol.toUpperCase()}`
      }
      for (const assetID of assetIDs) {
        const asset = app().assets[assetID]
        const unitInfo = asset.unitInfo
        const requiredAmt = placement.requiredDex[assetID] ? placement.requiredDex[assetID] : 0
        const usedAmt = placement.usedDex[assetID] ? placement.usedDex[assetID] : 0
        const requiredRow = this.placementAmtRowTmpl.cloneNode(true) as HTMLElement
        const requiredRowTmpl = Doc.parseTemplate(requiredRow)
        const usedRow = this.placementAmtRowTmpl.cloneNode(true) as HTMLElement
        const usedRowTmpl = Doc.parseTemplate(usedRow)
        requiredRowTmpl.amt.textContent = Doc.formatCoinValue(requiredAmt, unitInfo)
        requiredRowTmpl.assetLogo.src = Doc.logoPath(asset.symbol)
        requiredRowTmpl.assetSymbol.textContent = asset.symbol.toUpperCase()
        usedRowTmpl.amt.textContent = Doc.formatCoinValue(usedAmt, unitInfo)
        usedRowTmpl.assetLogo.src = Doc.logoPath(asset.symbol)
        usedRowTmpl.assetSymbol.textContent = asset.symbol.toUpperCase()
        rowTmpl.requiredDEX.appendChild(requiredRow)
        rowTmpl.usedDEX.appendChild(usedRow)
      }
      Doc.setVis(this.mkt.cexName, rowTmpl.requiredCEX, rowTmpl.usedCEX)
      if (this.mkt.cexName) {
        const requiredAmt = Doc.formatCoinValue(placement.requiredCex, cexAsset.unitInfo)
        rowTmpl.requiredCEX.textContent = `${requiredAmt} ${cexAsset.symbol.toUpperCase()}`
        const usedAmt = Doc.formatCoinValue(placement.usedCex, cexAsset.unitInfo)
        rowTmpl.usedCEX.textContent = `${usedAmt} ${cexAsset.symbol.toUpperCase()}`
      }
      Doc.setVis(anyErrors, rowTmpl.error)
      if (placement.error) {
        const errMessages = botProblemMessages(placement.error, this.mkt.cexName, this.mkt.host)
        rowTmpl.error.textContent = errMessages.join('\n')
      }
      return row
    }
    for (let i = 0; i < report.placements.length; i++) {
      form.placementsBody.appendChild(createPlacementRow(report.placements[i], i + 1))
    }
  }

  showOrderReport (side: 'buys' | 'sells') {
    if (!this.latestEpoch) return
    const report = side === 'buys' ? this.latestEpoch.buysReport : this.latestEpoch.sellsReport
    if (!report) return
    this.updateOrderReport(report, side, this.latestEpoch.epochNum)
    this.displayedOrderReportFormSide = side
    this.forms.show(this.orderReportFormEl, this.mkt.id)
  }

  readBook () {
    if (!this.mkt) return
    const { page, mkt: { host, mktID } } = this
    const orders = app().exchanges[host].markets[mktID].orders || []
    page.nBookedOrders.textContent = String(orders.filter((ord: Order) => ord.status === OrderUtil.StatusBooked).length)
  }
}

function allOrdersPlaced (report: OrderReport) {
  if (report.error) return false
  for (let i = 0; i < report.placements.length; i++) {
    const placement = report.placements[i]
    if (placement.orderedLots + placement.standingLots < placement.lots) return false
    if (placement.error) return false
  }
  return true
}

function setSignedValue (v: number, vEl: PageElement, signEl: PageElement, maxDecimals?: number) {
  vEl.textContent = Doc.formatFourSigFigs(v, maxDecimals)
  signEl.classList.toggle('ico-plus', v > 0)
  signEl.classList.toggle('text-good', v > 0)
  // signEl.classList.toggle('ico-minus', v < 0)
}

function botProblemMessages (problems: BotProblems | undefined, cexName: string, dexHost: string): string[] {
  if (!problems) return []
  const msgs: string[] = []

  if (problems.walletNotSynced) {
    for (const [assetID, notSynced] of Object.entries(problems.walletNotSynced)) {
      if (notSynced) {
        msgs.push(intl.prep(intl.ID_WALLET_NOT_SYNCED, { assetSymbol: app().assets[Number(assetID)].symbol.toUpperCase() }))
      }
    }
  }

  if (problems.noWalletPeers) {
    for (const [assetID, noPeers] of Object.entries(problems.noWalletPeers)) {
      if (noPeers) {
        msgs.push(intl.prep(intl.ID_WALLET_NO_PEERS, { assetSymbol: app().assets[Number(assetID)].symbol.toUpperCase() }))
      }
    }
  }

  if (problems.accountSuspended) {
    msgs.push(intl.prep(intl.ID_ACCOUNT_SUSPENDED, { dexHost: dexHost }))
  }

  if (problems.userLimitTooLow) {
    msgs.push(intl.prep(intl.ID_USER_LIMIT_TOO_LOW, { dexHost: dexHost }))
  }

  if (problems.noPriceSource) {
    msgs.push(intl.prep(intl.ID_NO_PRICE_SOURCE))
  }

  if (problems.cexOrderbookUnsynced) {
    msgs.push(intl.prep(intl.ID_CEX_ORDERBOOK_UNSYNCED, { cexName: cexName }))
  }

  if (problems.causesSelfMatch) {
    msgs.push(intl.prep(intl.ID_CAUSES_SELF_MATCH))
  }

  if (problems.unknownError) {
    msgs.push(problems.unknownError)
  }

  return msgs
}

function cexProblemMessages (problems: CEXProblems | undefined): string[] {
  if (!problems) return []
  const msgs: string[] = []
  if (problems.depositErr) {
    for (const [assetID, depositErr] of Object.entries(problems.depositErr)) {
      msgs.push(intl.prep(intl.ID_DEPOSIT_ERROR,
        {
          assetSymbol: app().assets[Number(assetID)].symbol.toUpperCase(),
          time: new Date(depositErr.stamp * 1000).toLocaleString(),
          error: depositErr.error
        }))
    }
  }
  if (problems.withdrawErr) {
    for (const [assetID, withdrawErr] of Object.entries(problems.withdrawErr)) {
      msgs.push(intl.prep(intl.ID_WITHDRAW_ERROR,
        {
          assetSymbol: app().assets[Number(assetID)].symbol.toUpperCase(),
          time: new Date(withdrawErr.stamp * 1000).toLocaleString(),
          error: withdrawErr.error
        }))
    }
  }
  if (problems.tradeErr) {
    msgs.push(intl.prep(intl.ID_CEX_TRADE_ERROR,
      {
        time: new Date(problems.tradeErr.stamp * 1000).toLocaleString(),
        error: problems.tradeErr.error
      }))
  }
  return msgs
}

function safeSub (a: number, b: number) {
  return a - b > 0 ? a - b : 0
}

window.mmstatus = function () : Promise<MarketMakingStatus> {
  return MM.status()
}
