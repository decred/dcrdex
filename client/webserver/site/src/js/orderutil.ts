import * as intl from './locales'
import {
  app,
  Order,
  TradeForm,
  OrderOption,
  Match
} from './registry'
import { BooleanOption, XYRangeOption } from './opts'
import Doc from './doc'

export const Limit = 1 // TODO: Delete for the versions below
export const Market = 2 // TODO: Delete for the versions below
export const Cancel = 3 // TODO: Delete for the versions below

export const OrderTypeLimit = 1
export const OrderTypeMarket = 2
export const OrderTypeCancel = 3

/* The time-in-force specifiers are a mirror of dex/order.TimeInForce. */
export const ImmediateTiF = 0
export const StandingTiF = 1

/* The order statuses are a mirror of dex/order.OrderStatus. */
export const StatusUnknown = 0
export const StatusEpoch = 1
export const StatusBooked = 2
export const StatusExecuted = 3
export const StatusCanceled = 4
export const StatusRevoked = 5

/* The match statuses are a mirror of dex/order.MatchStatus. */
export const NewlyMatched = 0
export const MakerSwapCast = 1
export const TakerSwapCast = 2
export const MakerRedeemed = 3
export const MatchComplete = 4
export const MatchConfirmed = 5

/* The match sides are a mirror of dex/order.MatchSide. */
export const Maker = 0 // TODO: Delete for the versions below
export const Taker = 1 // TODO: Delete for the versions below

export const MatchSideMaker = 0
export const MatchSideTaker = 1

/*
 * RateEncodingFactor is used when encoding an atomic exchange rate as an
 * integer. See docs on message-rate encoding @
 * https://github.com/decred/dcrdex/blob/master/spec/comm.mediawiki#Rate_Encoding
 */
export const RateEncodingFactor = 1e8

export function sellString (ord: Order) {
  const key = ord.sell ? intl.ID_SELL : intl.ID_BUY
  const lang = document.documentElement.lang.toLowerCase()
  return intl.prep(key).toLocaleLowerCase(lang)
}

export function typeString (ord: Order) {
  return ord.type === Limit ? (ord.tif === ImmediateTiF ? intl.prep(intl.ID_LIMIT_ORDER_IMMEDIATE_TIF) : intl.prep(intl.ID_LIMIT_ORDER)) : intl.prep(intl.ID_MARKET_ORDER)
}

/* isMarketBuy will return true if the order is a market buy order. */
export function isMarketBuy (ord: Order) {
  return ord.type === Market && !ord.sell
}

/*
 * hasActiveMatches returns true if the order has matches that have not completed
 * settlement yet.
 */
export function hasActiveMatches (order: Order) {
  if (!order.matches) return false
  for (const match of order.matches) {
    if (match.active) return true
  }
  return false
}

/**
 * statusString converts the order status to a string.
 *
 * IMPORTANT: we have similar function in Golang, it must match this one exactly,
 * when updating make sure to update both!
 */
export function statusString (order: Order): string {
  if (!order.id) return intl.prep(intl.ID_ORDER_SUBMITTING) // order ID is empty.
  const isLive = hasActiveMatches(order)
  switch (order.status) {
    case StatusUnknown: return intl.prep(intl.ID_UNKNOWN)
    case StatusEpoch: return intl.prep(intl.ID_EPOCH)
    case StatusBooked:
      if (order.cancelling) return intl.prep(intl.ID_CANCELING)
      return isLive ? `${intl.prep(intl.ID_BOOKED)}/${intl.prep(intl.ID_SETTLING)}` : intl.prep(intl.ID_BOOKED)
    case StatusExecuted:
      if (isLive) return intl.prep(intl.ID_SETTLING)
      if (filled(order) === 0 && order.type !== Cancel) return intl.prep(intl.ID_NO_MATCH)
      return intl.prep(intl.ID_EXECUTED)
    case StatusCanceled:
      if (isLive) return `${intl.prep(intl.ID_CANCELED)}/${intl.prep(intl.ID_SETTLING)}`
      if (filled(order) > 0) return 'canceled/partially filled'
      return intl.prep(intl.ID_CANCELED)
    case StatusRevoked:
      return isLive ? `${intl.prep(intl.ID_REVOKED)}/${intl.prep(intl.ID_SETTLING)}` : intl.prep(intl.ID_REVOKED)
  }
  return intl.prep(intl.ID_UNKNOWN)
}

/* filled sums the quantities of non-cancel matches available. */
export function filled (order: Order) {
  if (!order.matches) return 0
  const qty = isMarketBuy(order) ? (m: Match) => m.qty * m.rate / RateEncodingFactor : (m: Match) => m.qty
  return order.matches.reduce((filled, match) => {
    if (match.isCancel) return filled
    return filled + qty(match)
  }, 0)
}

/* settled sums the quantities of the matches that have completed. */
export function settled (order: Order) {
  if (!order.matches) return 0
  const qty = isMarketBuy(order) ? (m: Match) => m.qty * m.rate / RateEncodingFactor : (m: Match) => m.qty
  return order.matches.reduce((settled, match) => {
    if (match.isCancel) return settled
    const redeemed = (match.side === Maker && match.status >= MakerRedeemed) ||
      (match.side === Taker && match.status >= MatchComplete)
    return redeemed ? settled + qty(match) : settled
  }, 0)
}

/* averageRateString returns a formatting string containing the average rate of
the matches that have been filled for a market order. */
export function averageMarketOrderRateString (ord: Order): string {
  if (!ord.matches?.length) return intl.prep(intl.ID_MARKET_ORDER)
  let rateStr = Doc.formatCoinValue(app().conventionalRate(ord.baseID, ord.quoteID, averageRate(ord)))
  if (ord.matches.length > 1) rateStr = '~ ' + rateStr // "~" only makes sense if the order has more than one match.
  return rateStr
}

/* averageRate returns a the average rate of the matches that have been filled
in an order. */
export function averageRate (ord: Order): number {
  if (!ord.matches?.length) return 0
  let rateProduct = 0
  let baseQty = 0
  for (const m of ord.matches) {
    baseQty += m.qty
    rateProduct += (m.rate * m.qty) // order ~ 1e16
  }
  return rateProduct / baseQty
}

/* baseToQuote returns the quantity of the quote asset. */
export function baseToQuote (rate: number, base: number) : number {
  return rate * base / RateEncodingFactor
}

/* orderPortion returns a string stating the percentage of the order a match
   makes up. */
export function orderPortion (order: Order, match: Match) : string {
  let matchQty = match.qty
  if (isMarketBuy(order)) {
    matchQty = baseToQuote(match.rate, match.qty)
  }
  return ((matchQty / order.qty) * 100).toFixed(1) + ' %'
}

/*
 * matchStatusString is a string used to create a displayable string describing
 * describing the match status.
 */
export function matchStatusString (m: Match) {
  if (m.revoked) {
    // When revoked, match status is less important than pending action if still
    // active, or the outcome if inactive.
    if (m.active) {
      if (m.redeem) return revokedMatchStatus(intl.ID_MATCH_STATUS_REDEMPTION_SENT) // must require confirmation if active
      // If maker and we have not redeemed, waiting to refund, assuming it's not
      // revoked while waiting for confs on an unspent/unexpired taker swap.
      if (m.side === Maker) return revokedMatchStatus(intl.ID_MATCH_STATUS_REFUND_PENDING)
      // As taker, resolution depends on maker's actions while waiting to refund.
      if (m.counterRedeem) return revokedMatchStatus(intl.ID_MATCH_STATUS_REDEEM_PENDING) // this should be very brief if we see the maker's redeem
      return revokedMatchStatus(intl.ID_MATCH_STATUS_REFUND_PENDING) // may switch to redeem if maker redeems on the sly
    }
    if (m.refund) {
      return revokedMatchStatus(intl.ID_MATCH_STATUS_REFUNDED)
    }
    if (m.redeem) {
      return revokedMatchStatus(intl.ID_MATCH_STATUS_REDEMPTION_CONFIRMED)
    }
    return revokedMatchStatus(intl.ID_MATCH_STATUS_COMPLETE) // i.e. we sent no swap
  }

  switch (m.status) {
    case NewlyMatched:
      return intl.prep(intl.ID_MATCH_STATUS_NEWLY_MATCHED)
    case MakerSwapCast:
      return intl.prep(intl.ID_MATCH_STATUS_MAKER_SWAP_CAST)
    case TakerSwapCast:
      return intl.prep(intl.ID_MATCH_STATUS_TAKER_SWAP_CAST)
    case MakerRedeemed:
      if (m.side === Maker) {
        return intl.prep(intl.ID_MATCH_STATUS_REDEMPTION_SENT)
      }
      return intl.prep(intl.ID_MATCH_STATUS_MAKER_REDEEMED)
    case MatchComplete:
      return intl.prep(intl.ID_MATCH_STATUS_REDEMPTION_SENT)
    case MatchConfirmed:
      return intl.prep(intl.ID_MATCH_STATUS_REDEMPTION_CONFIRMED)
  }
  return intl.prep(intl.ID_UNKNOWN)
}

// revokedMatchStatus is a helper function that returns the revoked match status
// string.
function revokedMatchStatus (matchStatus: string): string {
  return intl.prep(intl.ID_MATCH_STATUS_REVOKED, { status: intl.prep(matchStatus) })
}

/*
 * optionElement is a getter for an element matching the *OrderOption from
 * client/asset. change is a function with no arguments that is called when the
 * returned option's value has changed.
 */
export function optionElement (opt: OrderOption, order: TradeForm, change: () => void, isSwap: boolean): HTMLElement {
  const isBaseChain = (isSwap && order.sell) || (!isSwap && !order.sell)
  const symbol = isBaseChain ? dexAssetSymbol(order.host, order.base) : dexAssetSymbol(order.host, order.quote)

  switch (true) {
    case !!opt.boolean:
      return new BooleanOption(opt, symbol, order.options, change).node
    case !!opt.xyRange:
      return new XYRangeOption(opt, symbol, order.options, change).node
    default:
      console.error('no option type specified', opt)
  }
  console.error('unknown option type', opt)
  return document.createElement('div')
}

function dexAssetSymbol (host: string, assetID: number): string {
  return app().exchanges[host].assets[assetID].symbol
}

export function isCancellable (ord: Order): boolean {
  return ord.type === Limit && ord.tif === StandingTiF && ord.status < StatusExecuted
}

export function orderTypeText (ordType: number): string {
  switch (ordType) {
    case OrderTypeLimit:
      return intl.prep(intl.ID_LIMIT_ORDER)
    case OrderTypeMarket:
      return intl.prep(intl.ID_MARKET_ORDER)
    default: // OrderTypeCancel
      return intl.prep(intl.ID_CANCEL_ORDER)
  }
}
