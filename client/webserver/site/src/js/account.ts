import Doc from './doc'
import {
  OrderTypeLimit,
  OrderTypeMarket,
  OrderTypeCancel,
  StatusEpoch,
  StatusBooked,
  RateEncodingFactor,
  MatchSideMaker,
  MakerRedeemed,
  TakerSwapCast,
  ImmediateTiF
} from './orderutil'
import {
  app,
  PageElement,
  ExchangeAuth,
  Order,
  Market
} from './registry'

export const bondReserveMultiplier = 2 // Reserves for next bond
export const perTierBaseParcelLimit = 2
export const parcelLimitScoreMultiplier = 3

export class ReputationMeter {
  page: Record<string, PageElement>
  host: string

  constructor (div: PageElement) {
    this.page = Doc.parseTemplate(div)
    Doc.cleanTemplates(this.page.penaltyMarkerTmpl)
  }

  setHost (host: string) {
    this.host = host
  }

  update () {
    const { page, host } = this
    const { auth, maxScore, penaltyThreshold } = app().exchanges[host]
    const { rep: { score } } = auth

    const displayTier = strongTier(auth)

    const minScore = displayTier ? displayTier * penaltyThreshold * -1 : penaltyThreshold * -1 // Just for looks
    const warnPct = 25
    const scorePct = 100 - warnPct
    page.scoreWarn.style.width = `${warnPct}%`
    const pos = score >= 0 ? warnPct + (score / maxScore) * scorePct : warnPct - (Math.min(warnPct, score / minScore * warnPct))

    page.scorePointer.style.left = `${pos}%`
    page.scoreMin.textContent = String(minScore)
    page.scoreMax.textContent = String(maxScore)
    const bonus = limitBonus(score, maxScore)
    page.limitBonus.textContent = bonus.toFixed(1)
    for (const m of Doc.applySelector(page.scoreTray, '.penalty-marker')) m.remove()
    if (displayTier > 1) {
      const markerPct = warnPct / displayTier
      for (let i = 1; i < displayTier; i++) {
        const div = page.penaltyMarkerTmpl.cloneNode(true) as PageElement
        page.scoreTray.appendChild(div)
        div.style.left = `${markerPct * i}%`
      }
    }
    page.score.textContent = String(score)
    page.scoreData.classList.remove('negative', 'positive')
    if (score > 0) page.scoreData.classList.add('positive')
    else page.scoreData.classList.add('negative')
  }
}

/*
 * strongTier is the effective tier, with some respect for bond overlap, such
 * that we don't count weak bonds that have already had their replacements
 * confirmed.
 */
export function strongTier (auth: ExchangeAuth): number {
  const { weakStrength, targetTier, effectiveTier } = auth
  if (effectiveTier > targetTier) {
    const diff = effectiveTier - targetTier
    if (weakStrength >= diff) return targetTier
    return targetTier + (diff - weakStrength)
  }
  return effectiveTier
}

export function likelyTaker (ord: Order, rate: number): boolean {
  if (ord.type === OrderTypeMarket || ord.tif === ImmediateTiF) return true
  // Must cross the spread to be a taker (not so conservative).
  if (rate === 0) return false
  if (ord.sell) return ord.rate < rate
  return ord.rate > rate
}

const preparcelQuantity = (ord: Order, mkt?: Market, midGap?: number) => {
  const qty = ord.qty - ord.filled
  if (ord.type === OrderTypeLimit) return qty
  if (ord.sell) return qty * ord.rate / RateEncodingFactor
  const rate = midGap || mkt?.spot?.rate || 0
  // Caller should not call this for market orders without a mkt arg.
  if (!mkt) return 0
  // This is tricky. The server will use the mid-gap rate to convert the
  // order qty. We don't have a mid-gap rate, only a spot rate.
  if (rate && (mkt?.spot?.bookVolume || 0) > 0) return qty * RateEncodingFactor / rate
  return mkt.lotsize // server uses same fallback if book is empty
}

export function epochWeight (ord: Order, mkt: Market, midGap?: number) {
  if (ord.status !== StatusEpoch) return 0
  const qty = preparcelQuantity(ord, mkt, midGap)
  const rate = midGap || mkt.spot?.rate || 0
  if (likelyTaker(ord, rate)) return qty * 2
  return qty
}

function bookWeight (ord: Order) {
  if (ord.status !== StatusBooked) return 0
  return preparcelQuantity(ord)
}

function settlingWeight (ord: Order) {
  let sum = 0
  for (const m of (ord.matches || [])) {
    if (m.side === MatchSideMaker) {
      if (m.status > MakerRedeemed) continue
    } else if (m.status > TakerSwapCast) continue
    sum += m.qty
  }
  return sum
}

function parcelWeight (ord: Order, mkt: Market, midGap?: number) {
  if (ord.type === OrderTypeCancel) return 0
  return epochWeight(ord, mkt, midGap) + bookWeight(ord) + settlingWeight(ord)
}

// function roundParcels (p: number): number {
//   return Math.floor(Math.round((p * 1e8)) / 1e8)
// }

function limitBonus (score: number, maxScore: number): number {
  return score > 0 ? 1 + score / maxScore * (parcelLimitScoreMultiplier - 1) : 1
}

export function tradingLimits (host: string): [number, number] { // [usedParcels, parcelLimit]
  const { auth, maxScore, markets } = app().exchanges[host]
  const { rep: { score } } = auth
  const tier = strongTier(auth)

  let usedParcels = 0
  for (const mkt of Object.values(markets)) {
    let mktWeight = 0
    for (const ord of (mkt.inflight || [])) mktWeight += parcelWeight(ord, mkt)
    for (const ord of (mkt.orders || [])) mktWeight += parcelWeight(ord, mkt)
    usedParcels += (mktWeight / (mkt.parcelsize * mkt.lotsize))
  }
  const parcelLimit = perTierBaseParcelLimit * limitBonus(score, maxScore) * tier
  return [usedParcels, parcelLimit]
}
