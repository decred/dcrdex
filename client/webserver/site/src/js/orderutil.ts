import Doc from './doc'
import * as intl from './locales'
import {
  app,
  Order,
  TradeForm,
  PageElement,
  OrderOption as OrderOpt,
  Match,
  XYRange
} from './registry'

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

/*
 * RateEncodingFactor is used when encoding an atomic exchange rate as an
 * integer. See docs on message-rate encoding @
 * https://github.com/decred/dcrdex/blob/master/spec/comm.mediawiki#Rate_Encoding
 */
export const RateEncodingFactor = 1e8

export function sellString (ord: Order) { return ord.sell ? 'sell' : 'buy' }
export function typeString (ord: Order) { return ord.type === Limit ? (ord.tif === ImmediateTiF ? 'limit (i)' : 'limit') : 'market' }

/* isMarketBuy will return true if the order is a market buy order. */
export function isMarketBuy (ord: Order) {
  return ord.type === Market && !ord.sell
}

/*
 * hasLiveMatches returns true if the order has matches that have not completed
 * settlement yet.
 */
export function hasLiveMatches (order: Order) {
  if (!order.matches) return false
  for (const match of order.matches) {
    if (!match.revoked && match.status < MakerRedeemed) return true
  }
  return false
}

/* statusString converts the order status to a string */
export function statusString (order: Order): string {
  const isLive = hasLiveMatches(order)
  switch (order.status) {
    case StatusUnknown: return intl.prep(intl.ID_UNKNOWN)
    case StatusEpoch: return intl.prep(intl.ID_EPOCH)
    case StatusBooked:
      if (order.cancelling) return intl.prep(intl.ID_CANCELING)
      return isLive ? `${intl.prep(intl.ID_BOOKED)}/${intl.prep(intl.ID_SETTLING)}` : intl.prep(intl.ID_BOOKED)
    case StatusExecuted:
      if (isLive) return intl.prep(intl.ID_SETTLING)
      return (order.filled === 0) ? intl.prep(intl.ID_NO_MATCH) : intl.prep(intl.ID_EXECUTED)
    case StatusCanceled:
      return isLive ? `${intl.prep(intl.ID_CANCELED)}/${intl.prep(intl.ID_SETTLING)}` : intl.prep(intl.ID_CANCELED)
    case StatusRevoked:
      return isLive ? `${intl.prep(intl.ID_REVOKED)}/${intl.prep(intl.ID_SETTLING)}` : intl.prep(intl.ID_REVOKED)
  }
  return ''
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

/*
 * matchStatusString is a string used to create a displayable string describing
 * describing the match status.
 */
export function matchStatusString (status: number, side: number) {
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

// Having the caller set these vars on load using an exported function makes
// life easier.
let orderOptTmpl: HTMLElement, booleanOptTmpl: HTMLElement, rangeOptTmpl: HTMLElement

// setOptionTemplates sets the package vars for the templates and application.
export function setOptionTemplates (page: Record<string, PageElement>) {
  [booleanOptTmpl, rangeOptTmpl, orderOptTmpl] = [page.booleanOptTmpl, page.rangeOptTmpl, page.orderOptTmpl]
}

interface OptionsReporters {
  enable: () => void
  disable: () => void
}

/*
 * OrderOption is a base class for option elements. OrderOptions stores some
 * common parameters and monitors the toggle switch, calling the child class's
 * enable/disable methods when the user manually turns the option on or off.
 */
class OrderOption {
  opt: OrderOpt
  order: TradeForm
  node: HTMLElement
  tmpl: Record<string, PageElement>
  on: boolean

  constructor (opt: OrderOpt, order: TradeForm, isSwapOption: boolean, report: OptionsReporters) {
    this.opt = opt
    this.order = order
    const node = this.node = orderOptTmpl.cloneNode(true) as HTMLElement
    const tmpl = this.tmpl = Doc.parseTemplate(node)
    tmpl.optName.textContent = opt.displayname
    tmpl.tooltip.dataset.tooltip = opt.description

    const isBaseChain = (isSwapOption && order.sell) || (!isSwapOption && !order.sell)
    const symbol = isBaseChain ? this.baseSymbol() : this.quoteSymbol()
    tmpl.chainIcon.src = Doc.logoPath(symbol)

    this.on = false
    Doc.bind(node, 'click', () => {
      if (this.on) return
      this.on = true
      node.classList.add('selected')
      report.enable()
    })
    Doc.bind(tmpl.toggle, 'click', e => {
      if (!this.on) return
      e.stopPropagation()
      this.on = false
      node.classList.remove('selected')
      report.disable()
    })
  }

  quoteSymbol () {
    return dexAssetSymbol(this.order.host, this.order.quote)
  }

  baseSymbol () {
    return dexAssetSymbol(this.order.host, this.order.base)
  }
}

/*
 * BooleanOrderOption is a simple on/off option with a short summary of it's
 * effects. BooleanOrderOption is the handler for a *BooleanConfig from
 * client/asset.
 */
class BooleanOrderOption extends OrderOption {
  control: HTMLElement
  changed: () => void

  constructor (opt: OrderOpt, order: TradeForm, changed: () => void, isSwapOption: boolean) {
    super(opt, order, isSwapOption, {
      enable: () => this.enable(),
      disable: () => this.disable()
    })
    this.changed = () => changed()
    const cfg = opt.boolean
    const control = this.control = booleanOptTmpl.cloneNode(true) as HTMLElement
    // Append to parent's options div.
    this.tmpl.controls.appendChild(control)
    const tmpl = Doc.parseTemplate(control)
    tmpl.reason.textContent = cfg.reason
    this.on = typeof order.options[opt.key] !== 'undefined' ? order.options[opt.key] : opt.default
    if (this.on) this.node.classList.add('selected')
  }

  store () {
    if (this.on === this.opt.default) delete this.order.options[this.opt.key]
    else this.order.options[this.opt.key] = this.on
    this.changed()
  }

  enable () {
    this.store()
  }

  disable () {
    this.store()
  }
}

/*
 * XYRangeOrderOption is an order option that contains an XYRangeHandler.
 * The logic for handling the slider to is defined in XYRangeHandler so
 * that the slider can be used without being contained in an order option.
 */
class XYRangeOrderOption extends OrderOption {
  handler: XYRangeHandler
  x: number
  changed: () => void

  constructor (opt: OrderOpt, order: TradeForm, changed: () => void, isSwapOption: boolean) {
    super(opt, order, isSwapOption, {
      enable: () => this.enable(),
      disable: () => this.disable()
    })
    this.changed = changed
    const cfg = opt.xyRange
    const setVal = order.options[opt.key]
    this.on = typeof setVal !== 'undefined'
    if (this.on) {
      this.node.classList.add('selected')
      this.x = setVal
    } else {
      this.x = opt.default
    }
    const onUpdate = (x: number) => {
      this.x = x
      this.order.options[this.opt.key] = x
    }
    const onChange = () => { this.changed() }
    const selected = () => { this.node.classList.add('selected') }
    this.handler = new XYRangeHandler(cfg, this.x, onUpdate, onChange, selected)
    this.tmpl.controls.appendChild(this.handler.control)
  }

  enable () {
    this.order.options[this.opt.key] = this.x
    this.changed()
  }

  disable () {
    delete this.order.options[this.opt.key]
    this.changed()
  }
}

/*
 * XYRangeHandler is the handler for an *XYRange from client/asset. XYRange
 * has a slider which allows adjusting the x and y, linearly between two limits.
 * The user can also manually enter values for x or y.
 */
export class XYRangeHandler {
  control: HTMLElement
  x: number
  updated: (x:number, y:number) => void
  changed: () => void
  selected: () => void

  constructor (cfg: XYRange, initVal: number, updated: (x:number, y:number) => void, changed: () => void, selected: () => void, roundY?: boolean) {
    const control = this.control = rangeOptTmpl.cloneNode(true) as HTMLElement
    const tmpl = Doc.parseTemplate(control)

    this.changed = changed
    this.selected = selected
    this.updated = updated

    const { slider, handle } = tmpl

    const rangeX = cfg.end.x - cfg.start.x
    const rangeY = cfg.end.y - cfg.start.y
    const normalizeX = (x: number) => (x - cfg.start.x) / rangeX

    // r, x, and y will be updated by the various input event handlers. r is
    // x (or y) normalized on its range, e.g. [x_min, x_max] -> [0, 1]
    let r = normalizeX(initVal)
    let x = this.x = initVal
    let y = r * rangeY + cfg.start.y

    const number = new Intl.NumberFormat((navigator.languages as string[]), {
      minimumSignificantDigits: 3,
      maximumSignificantDigits: 3
    })

    // accept needs to be called anytime a handler updates x, y, and r.
    const accept = (skipUpdate?: boolean) => {
      if (roundY) y = Math.round(y)
      tmpl.x.textContent = number.format(x)
      tmpl.y.textContent = number.format(y)
      if (roundY) tmpl.y.textContent = `${y}`
      handle.style.left = `calc(${r * 100}% - ${r * 14}px)`
      this.x = x
      if (!skipUpdate) this.updated(x, y)
    }

    // Set up the handlers for the x and y text input fields.
    const clickOutX = (e: MouseEvent) => {
      if (e.type !== 'change' && e.target === tmpl.xInput) return
      const s = tmpl.xInput.value
      if (s) {
        const xx = parseFloat(s)
        if (!isNaN(xx)) {
          x = clamp(xx, cfg.start.x, cfg.end.x)
          r = normalizeX(x)
          y = r * rangeY + cfg.start.y
          accept()
        }
      }
      Doc.hide(tmpl.xInput)
      Doc.show(tmpl.x)
      Doc.unbind(document, 'click', clickOutX)
      this.changed()
    }

    Doc.bind(tmpl.x, 'click', e => {
      Doc.hide(tmpl.x)
      Doc.show(tmpl.xInput)
      tmpl.xInput.focus()
      tmpl.xInput.value = number.format(x)
      Doc.bind(document, 'click', clickOutX)
      e.stopPropagation()
    })

    Doc.bind(tmpl.xInput, 'change', clickOutX)

    const clickOutY = (e: MouseEvent) => {
      if (e.type !== 'change' && e.target === tmpl.yInput) return
      const s = tmpl.yInput.value
      if (s) {
        const yy = parseFloat(s)
        if (!isNaN(yy)) {
          y = clamp(yy, cfg.start.y, cfg.end.y)
          r = (y - cfg.start.y) / rangeY
          x = cfg.start.x + r * rangeX
          accept()
        }
      }
      Doc.hide(tmpl.yInput)
      Doc.show(tmpl.y)
      Doc.unbind(document, 'click', clickOutY)
      this.changed()
    }

    Doc.bind(tmpl.y, 'click', e => {
      Doc.hide(tmpl.y)
      Doc.show(tmpl.yInput)
      tmpl.yInput.focus()
      tmpl.yInput.value = number.format(y)
      Doc.bind(document, 'click', clickOutY)
      e.stopPropagation()
    })

    Doc.bind(tmpl.yInput, 'change', clickOutY)

    // Read the slider.
    Doc.bind(handle, 'mousedown', (e: MouseEvent) => {
      if (e.button !== 0) return
      e.preventDefault()
      this.selected()
      const startX = e.pageX
      const w = slider.clientWidth - handle.offsetWidth
      const startLeft = normalizeX(x) * w
      const left = (ee: MouseEvent) => Math.max(Math.min(startLeft + (ee.pageX - startX), w), 0)
      const trackMouse = (ee: MouseEvent) => {
        ee.preventDefault()
        r = left(ee) / w
        x = r * rangeX + cfg.start.x
        y = r * rangeY + cfg.start.y
        accept()
      }
      const mouseUp = (ee: MouseEvent) => {
        trackMouse(ee)
        Doc.unbind(document, 'mousemove', trackMouse)
        Doc.unbind(document, 'mouseup', mouseUp)
        this.changed()
      }
      Doc.bind(document, 'mousemove', trackMouse)
      Doc.bind(document, 'mouseup', mouseUp)
    })

    tmpl.rangeLblStart.textContent = cfg.start.label
    tmpl.rangeLblEnd.textContent = cfg.end.label
    tmpl.xUnit.textContent = cfg.xUnit
    tmpl.yUnit.textContent = cfg.yUnit
    accept(true)
  }
}

/*
 * optionElement is a getter for an element matching the *OrderOption from
 * client/asset. change is a function with no arguments that is called when the
 * returned option's value has changed.
 */
export function optionElement (opt: OrderOpt, order: TradeForm, change: () => void, isSwap: boolean): HTMLElement {
  switch (true) {
    case !!opt.boolean:
      return new BooleanOrderOption(opt, order, change, isSwap).node
    case !!opt.xyRange:
      return new XYRangeOrderOption(opt, order, change, isSwap).node
    default:
      console.error('no option type specified', opt)
  }
  console.error('unknown option type', opt)
  return document.createElement('div')
}

function dexAssetSymbol (host: string, assetID: number) {
  return app().exchanges[host].assets[assetID].symbol
}

const clamp = (v: number, min: number, max: number) => v < min ? min : v > max ? max : v
