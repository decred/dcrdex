import {
  MarketOrderBook,
  MiniOrder
} from './registry'

import { RateEncodingFactor } from './orderutil'

export interface MarketOrderEstimate {
  avgRate: number // VWAP fill rate (msgRate-encoded)
  worstRate: number // Worst (last) fill rate
  midGapRate: number // Mid-gap rate for comparison
  slippagePct: number // |avgRate - midGap| / midGap * 100
  worstSlippagePct: number // |worstRate - midGap| / midGap * 100
  filled: boolean // Book can fully fill the order?
  bookDepthPct: number // % of book side consumed (by base qty)
  levelsConsumed: number // Number of price levels consumed
  receivedEstimate: number // Estimated received amount (base atoms for buy, quote atoms for sell)
}

export default class OrderBook {
  base: number
  baseSymbol: string
  quote: number
  quoteSymbol: string
  buys: MiniOrder[]
  sells: MiniOrder[]

  constructor (mktBook: MarketOrderBook, baseSymbol: string, quoteSymbol: string) {
    this.base = mktBook.base
    this.baseSymbol = baseSymbol
    this.quote = mktBook.quote
    this.quoteSymbol = quoteSymbol
    // Books are sorted mid-gap first.
    this.buys = mktBook.book.buys || []
    this.sells = mktBook.book.sells || []
  }

  /* add adds an order to the order book. */
  add (ord: MiniOrder) {
    if (ord.qtyAtomic === 0) {
      // TODO: Somebody, for the love of god, figure out why the hell this helps
      // with the ghost orders problem. As far as I know, this order is a booked
      // order that had more than one match in an epoch and completely filled.
      // Because the first match didn't exhaust the order, there would be a
      // 'update_remaining' notification scheduled for the order. But by the
      // time OrderRouter generates the notification long after matching, the
      // order has zero qty left to fill. It's all good though, kinda, because
      // the notification is quickly followed with an 'unbook_order'
      // notification. I have tried my damnedest to catch an update_remaining
      // note without an accompanying unbook_order note, and have thus failed.
      // Yet, this fix somehow seems to work. It's infuriating, tbh.
      window.log('zeroqty', 'zero quantity order encountered', ord)
      return
    }
    const side = ord.sell ? this.sells : this.buys
    side.splice(findIdx(side, ord.rate, !ord.sell), 0, ord)
  }

  /* remove removes an order from the order book. */
  remove (token: string) {
    if (this.removeFromSide(this.sells, token)) return
    this.removeFromSide(this.buys, token)
  }

  /* removeFromSide removes an order from the list of orders. */
  removeFromSide (side: MiniOrder[], token: string) {
    const [ord, i] = this.findOrder(side, token)
    if (ord) {
      side.splice(i, 1)
      return true
    }
    return false
  }

  /* findOrder finds an order in a specified side */
  findOrder (side: MiniOrder[], token: string): [MiniOrder | null, number] {
    for (let i = 0; i < side.length; i++) {
      if (side[i].token === token) {
        return [side[i], i]
      }
    }
    return [null, -1]
  }

  /* updates the remaining quantity of an order. */
  updateRemaining (token: string, qty: number, qtyAtomic: number) {
    if (this.updateRemainingSide(this.sells, token, qty, qtyAtomic)) return
    this.updateRemainingSide(this.buys, token, qty, qtyAtomic)
  }

  /*
   * updateRemainingSide looks for the order in the side and updates the
   * quantity, returning true on success, false if order not found.
   */
  updateRemainingSide (side: MiniOrder[], token: string, qty: number, qtyAtomic: number) {
    const ord = this.findOrder(side, token)[0]
    if (ord) {
      ord.qty = qty
      ord.qtyAtomic = qtyAtomic
      return true
    }
    return false
  }

  /*
   * setEpoch sets the current epoch and clear any orders from previous epochs.
   */
  setEpoch (epochIdx: number) {
    const approve = (ord: MiniOrder) => ord.epoch === undefined || ord.epoch === 0 || ord.epoch === epochIdx
    this.sells = this.sells.filter(approve)
    this.buys = this.buys.filter(approve)
  }

  /* empty will return true if both the buys and sells lists are empty. */
  empty () {
    return !this.sells.length && !this.buys.length
  }

  /* count is the total count of both buy and sell orders. */
  count () {
    return this.sells.length + this.buys.length
  }

  /* bestGapOrder will return the best non-epoch order if one exists, or the
   * best epoch order if there are only epoch orders, or null if there are no
   * orders.
   */
  bestGapOrder (side: MiniOrder[]) {
    let best = null
    for (const ord of side) {
      if (!ord.epoch) return ord
      if (!best) {
        best = ord
      }
    }
    return best
  }

  bestGapBuy () {
    return this.bestGapOrder(this.buys)
  }

  bestGapSell () {
    return this.bestGapOrder(this.sells)
  }

  /*
   * estimateMarketOrder walks the order book to estimate fill quality for a
   * market order, returning slippage, VWAP, worst rate, and book depth info.
   * For sell orders, qty is in base-asset atoms. For buy orders, qty is in
   * quote-asset atoms.
   */
  estimateMarketOrder (sell: boolean, qty: number): MarketOrderEstimate | null {
    // For a sell, we consume buy orders. For a buy, sell orders.
    const side = sell ? this.buys : this.sells
    if (!side || !side.length || qty <= 0) return null

    // Compute mid-gap rate from best non-epoch orders.
    const bestBuy = this.bestGapBuy()
    const bestSell = this.bestGapSell()
    if (!bestBuy && !bestSell) return null
    const midGapRate = bestBuy && bestSell
      ? (bestBuy.msgRate + bestSell.msgRate) / 2
      : bestBuy ? bestBuy.msgRate : bestSell ? bestSell.msgRate : 0

    if (midGapRate <= 0) return null

    const isMarketBuy = !sell
    let remainingQty = qty
    let weightedSum = 0
    let baseQtySum = 0
    let worstRate = 0
    let levelsConsumed = 0
    let filledBaseQty = 0
    let totalBookBaseQty = 0

    for (const ord of side) {
      if (!ord.epoch) totalBookBaseQty += ord.qtyAtomic
    }

    let filled = false
    for (const ord of side) {
      if (remainingQty <= 0) break
      // Skip epoch orders for estimation — they may not be matched.
      if (ord.epoch) continue
      levelsConsumed++
      worstRate = ord.msgRate

      if (isMarketBuy) {
        // qty is in quote atoms. Each order costs qtyAtomic * msgRate / RateEncodingFactor quote atoms.
        const quoteCost = ord.qtyAtomic * ord.msgRate / RateEncodingFactor
        if (quoteCost >= remainingQty) {
          // Partial fill
          const baseUsed = remainingQty * RateEncodingFactor / ord.msgRate
          weightedSum += baseUsed * ord.msgRate
          baseQtySum += baseUsed
          filledBaseQty += baseUsed
          remainingQty = 0
          filled = true
        } else {
          weightedSum += ord.qtyAtomic * ord.msgRate
          baseQtySum += ord.qtyAtomic
          filledBaseQty += ord.qtyAtomic
          remainingQty -= quoteCost
        }
      } else {
        // qty is in base atoms.
        const orderQty = ord.qtyAtomic
        if (orderQty >= remainingQty) {
          weightedSum += remainingQty * ord.msgRate
          baseQtySum += remainingQty
          filledBaseQty += remainingQty
          remainingQty = 0
          filled = true
        } else {
          weightedSum += orderQty * ord.msgRate
          baseQtySum += orderQty
          filledBaseQty += orderQty
          remainingQty -= orderQty
        }
      }
    }

    if (baseQtySum === 0) return null

    const avgRate = weightedSum / baseQtySum
    const bookDepthPct = totalBookBaseQty > 0 ? (filledBaseQty / totalBookBaseQty) * 100 : 0
    const slippagePct = Math.abs(avgRate - midGapRate) / midGapRate * 100
    const worstSlippagePct = Math.abs(worstRate - midGapRate) / midGapRate * 100

    // For buy, it's base atoms received; for sell, it's quote atoms received.
    const receivedEstimate = sell
      ? baseQtySum * avgRate / RateEncodingFactor
      : baseQtySum

    return {
      avgRate,
      worstRate,
      midGapRate,
      slippagePct,
      worstSlippagePct,
      filled,
      bookDepthPct,
      levelsConsumed,
      receivedEstimate
    }
  }
}

/*
 * findIdx find the index at which to insert the order into the list of orders.
 */
function findIdx (side: MiniOrder[], rate: number, less: boolean): number {
  for (let i = 0; i < side.length; i++) {
    if ((side[i].rate < rate) === less) return i
  }
  return side.length
}
