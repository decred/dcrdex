export default class OrderBook {
  constructor (market, baseSymbol, quoteSymbol) {
    this.base = market.base
    this.baseSymbol = baseSymbol
    this.quote = market.quote
    this.quoteSymbol = quoteSymbol
    // Books are sorted mid-gap first.
    this.buys = market.book.buys || []
    this.sells = market.book.sells || []
  }

  /* add adds an order to the order book. */
  add (ord) {
    const side = ord.sell ? this.sells : this.buys
    side.splice(findIdx(side, ord.rate, !ord.sell), 0, ord)
  }

  /* remove removes an order from the order book. */
  remove (token) {
    if (this.removeFromSide(this.sells, token)) return
    this.removeFromSide(this.buys, token)
  }

  /* removeFromSide removes an order from the list of orders. */
  removeFromSide (side, token) {
    const [ord, i] = this.findOrder(side, token)
    if (ord) {
      side.splice(i, 1)
      return true
    }
    return false
  }

  /* findOrder finds an order in a specified side */
  findOrder (side, token) {
    for (const i in side) {
      if (side[i].token === token) {
        return [side[i], i]
      }
    }
    return [null, -1]
  }

  /* updates the remaining quantity of an order. */
  updateRemaining (token, qty) {
    if (this.updateRemainingSide(this.sells, token, qty)) return
    this.updateRemainingSide(this.buys, token, qty)
  }

  /*
   * updateRemainingSide looks for the order in the side and updates the
   * quantity, returning true on success, false if order not found.
   */
  updateRemainingSide (side, token, qty) {
    const ord = this.findOrder(side, token)[0]
    if (ord) {
      ord.qty = qty
      return true
    }
    return false
  }

  /*
   * setEpoch sets the current epoch and clear any orders from previous epochs.
   */
  setEpoch (epochIdx) {
    const approve = ord => ord.epoch === 0 || ord.epoch === epochIdx
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
}

/*
 * findIdx find the index at which to insert the order into the list of orders.
 */
function findIdx (side, rate, less) {
  for (const i in side) {
    if ((side[i].rate < rate) === less) return i
  }
  return side.length
}
