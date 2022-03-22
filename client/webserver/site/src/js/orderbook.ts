import {
  MarketOrderBook,
  MiniOrder
} from './registry'

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
    if (ord.qtyAtomic === 0) return
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
