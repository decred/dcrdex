import Doc from './doc'

export const Limit = 1
export const Market = 2
export const CANCEL = 3

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

/* The match sides are a mirror of dex/order.MatchSide. */
export const Maker = 0
export const Taker = 1

export function sellString (ord) { return ord.sell ? 'sell' : 'buy' }
export function typeString (ord) { return ord.type === Limit ? (ord.tif === ImmediateTiF ? 'limit (i)' : 'limit') : 'market' }
export function rateString (ord) { return ord.type === Market ? 'market' : Doc.formatCoinValue(ord.rate / 1e8) }

/* isMarketBuy will return true if the order is a market buy order. */
export function isMarketBuy (ord) {
  return ord.type === Market && !ord.sell
}

/*
 * hasLiveMatches returns true if the order has matches that have not completed
 * settlement yet.
 */
export function hasLiveMatches (order) {
  if (!order.matches) return false
  for (const match of order.matches) {
    if (!match.revoked && match.status < MakerRedeemed) return true
  }
  return false
}

/* statusString converts the order status to a string */
export function statusString (order) {
  const isLive = hasLiveMatches(order)
  switch (order.status) {
    case StatusUnknown: return 'unknown'
    case StatusEpoch: return 'epoch'
    case StatusBooked:
      if (order.cancelling) return 'cancelling'
      return isLive ? 'booked/settling' : 'booked'
    case StatusExecuted:
      if (isLive) return 'settling'
      return (order.filled === 0) ? 'expired' : 'executed'
    case StatusCanceled: return isLive ? 'canceled/settling' : 'canceled'
    case StatusRevoked: return isLive ? 'revoked/settling' : 'revoked'
  }
}

/* settled sums the quantities of the matches that have completed. */
export function settled (order) {
  if (!order.matches) return 0
  const qty = isMarketBuy(order) ? m => m.qty * m.rate * 1e-8 : m => m.qty
  return order.matches.reduce((settled, match) => {
    if (match.isCancel) return settled
    const redeemed = (match.side === Maker && match.status >= MakerRedeemed) ||
      (match.side === Taker && match.status >= MatchComplete)
    return redeemed ? settled + qty(match) : settled
  }, 0)
}

/*
 * matchStatusString is a string used to create a displayable string describing
 * describing the match status.
 */
export function matchStatusString (status, side) {
  switch (status) {
    case NewlyMatched:
      return '(0 / 4) Newly Matched'
    case MakerSwapCast:
      return '(1 / 4) First Swap Sent'
    case TakerSwapCast:
      return '(2 / 4) Second Swap Sent'
    case MakerRedeemed:
      if (side === Maker) {
        return 'Match Complete'
      }
      return '(3 / 4) Maker Redeemed'
    case MatchComplete:
      return 'Match Complete'
  }
  return 'Unknown Order Status'
}
