import * as intl from './locales'
import {
  UnitInfo,
  LayoutMetrics,
  WalletState,
  PageElement
} from './registry'

const parser = new window.DOMParser()

const FPS = 30

const BipIDs: Record<number, string> = {
  0: 'btc',
  42: 'dcr',
  2: 'ltc',
  20: 'dgb',
  22: 'mona',
  28: 'vtc',
  3: 'doge',
  145: 'bch',
  60: 'eth',
  133: 'zec',
  60000: 'dextt.eth',
  60001: 'usdc.eth'
}

const BipSymbols = Object.values(BipIDs)

const intFormatter = new Intl.NumberFormat((navigator.languages as string[]))

const threeSigFigs = new Intl.NumberFormat((navigator.languages as string[]), {
  minimumSignificantDigits: 3,
  maximumSignificantDigits: 3
})

const fiveSigFigs = new Intl.NumberFormat((navigator.languages as string[]), {
  minimumSignificantDigits: 5,
  maximumSignificantDigits: 5
})

/* A cache for formatters used for Doc.formatCoinValue. */
const decimalFormatters = {}

/*
 * decimalFormatter gets the formatCoinValue formatter for the specified decimal
 * precision.
 */
function decimalFormatter (prec: number) {
  return formatter(decimalFormatters, 2, prec)
}

/* A cache for formatters used for Doc.formatFullPrecision. */
const fullPrecisionFormatters = {}

/*
 * fullPrecisionFormatter gets the formatFullPrecision formatter for the
 * specified decimal precision.
 */
function fullPrecisionFormatter (prec: number) {
  return formatter(fullPrecisionFormatters, prec, prec)
}

/*
 * formatter gets the formatter from the supplied cache if it already exists,
 * else creates it.
 */
function formatter (formatters: Record<string, Intl.NumberFormat>, min: number, max: number): Intl.NumberFormat {
  const k = `${min}-${max}`
  let fmt = formatters[k]
  if (!fmt) {
    fmt = new Intl.NumberFormat((navigator.languages as string[]), {
      minimumFractionDigits: min,
      maximumFractionDigits: max
    })
    formatters[k] = fmt
  }
  return fmt
}

/*
 * convertToConventional converts the value in atomic units to conventional
 * units.
 */
function convertToConventional (v: number, unitInfo?: UnitInfo) {
  let prec = 8
  if (unitInfo) {
    const f = unitInfo.conventional.conversionFactor
    v /= f
    prec = Math.round(Math.log10(f))
  }
  return [v, prec]
}

// Helpers for working with the DOM.
export default class Doc {
  /*
   * idel is the element with the specified id that is the descendent of the
   * specified node.
   */
  static idel (el: Document | Element, id: string): HTMLElement {
    return el.querySelector(`#${id}`) as HTMLElement
  }

  /* bind binds the function to the event for the element. */
  static bind (el: EventTarget, ev: string, f: EventListenerOrEventListenerObject): void {
    el.addEventListener(ev, f)
  }

  /* unbind removes the handler for the event from the element. */
  static unbind (el: EventTarget, ev: string, f: (e: Event) => void): void {
    el.removeEventListener(ev, f)
  }

  /* noderize creates a Document object from a string of HTML. */
  static noderize (html: string): Document {
    return parser.parseFromString(html, 'text/html')
  }

  /*
   * mouseInElement returns true if the position of mouse event, e, is within
   * the bounds of the specified element or any of its descendents.
   */
  static mouseInElement (e: MouseEvent, el: HTMLElement): boolean {
    if (el.contains(e.target as Node)) return true
    const rect = el.getBoundingClientRect()
    return e.pageX >= rect.left && e.pageX <= rect.right &&
      e.pageY >= rect.top && e.pageY <= rect.bottom
  }

  /*
   * layoutMetrics gets information about the elements position on the page.
   */
  static layoutMetrics (el: HTMLElement): LayoutMetrics {
    const box = el.getBoundingClientRect()
    const docEl = document.documentElement
    const top = box.top + docEl.scrollTop
    const left = box.left + docEl.scrollLeft
    const w = el.offsetWidth
    const h = el.offsetHeight
    return {
      bodyTop: top,
      bodyLeft: left,
      width: w,
      height: h,
      centerX: left + w / 2,
      centerY: top + h / 2
    }
  }

  static descendentMetrics (parent: PageElement, kid: PageElement): LayoutMetrics {
    const parentMetrics = Doc.layoutMetrics(parent)
    const kidMetrics = Doc.layoutMetrics(kid)
    return {
      bodyTop: kidMetrics.bodyTop - parentMetrics.bodyTop,
      bodyLeft: kidMetrics.bodyLeft - parentMetrics.bodyLeft,
      width: kidMetrics.width,
      height: kidMetrics.height,
      centerX: kidMetrics.centerX - parentMetrics.bodyLeft,
      centerY: kidMetrics.centerY - parentMetrics.bodyTop
    }
  }

  /* empty removes all child nodes from the specified element. */
  static empty (...els: Element[]) {
    for (const el of els) while (el.firstChild) el.removeChild(el.firstChild)
  }

  /*
   * setContent removes all child nodes from the specified element and appends
   * passed elements.
   */
  static setContent (ancestor: PageElement, ...kids: PageElement[]) {
    Doc.empty(ancestor)
    for (const k of kids) ancestor.appendChild(k)
  }

  /*
   * hide hides the specified elements. This is accomplished by adding the
   * bootstrap d-hide class to the element. Use Doc.show to undo.
   */
  static hide (...els: Element[]) {
    for (const el of els) el.classList.add('d-hide')
  }

  /*
   * show shows the specified elements. This is accomplished by removing the
   * bootstrap d-hide class as added with Doc.hide.
   */
  static show (...els: Element[]) {
    for (const el of els) el.classList.remove('d-hide')
  }

  /*
   * show or hide the specified elements, based on value of the truthiness of
   * vis.
   */
  static setVis (vis: any, ...els: Element[]) {
    if (vis) Doc.show(...els)
    else Doc.hide(...els)
  }

  /* isHidden returns true if the specified element is hidden */
  static isHidden (el: Element): boolean {
    return el.classList.contains('d-hide')
  }

  /* isDisplayed returns true if the specified element is not hidden */
  static isDisplayed (el: Element): boolean {
    return !el.classList.contains('d-hide')
  }

  /*
   * animate runs the supplied function, which should be a "progress" function
   * accepting one argument. The progress function will be called repeatedly
   * with the argument varying from 0.0 to 1.0. The exact path that animate
   * takes from 0.0 to 1.0 will vary depending on the choice of easing
   * algorithm. See the Easing object for the available easing algo choices. The
   * default easing algorithm is linear.
   */
  static async animate (duration: number, f: (progress: number) => void, easingAlgo?: string) {
    await new Animation(duration, f, easingAlgo).wait()
  }

  static applySelector (ancestor: HTMLElement, k: string): PageElement[] {
    return Array.from(ancestor.querySelectorAll(k)) as PageElement[]
  }

  static kids (ancestor: HTMLElement): PageElement[] {
    return Array.from(ancestor.children) as PageElement[]
  }

  static safeSelector (ancestor: HTMLElement, k: string): PageElement {
    const el = ancestor.querySelector(k)
    if (el) return el as PageElement
    console.warn(`no element found for selector '${k}' on element ->`, ancestor)
    return document.createElement('div')
  }

  /*
   * idDescendants creates an object mapping to elements which are descendants
   * of the ancestor and have id attributes. Elements are keyed by their id
   * value.
   */
  static idDescendants (ancestor: HTMLElement): Record<string, PageElement> {
    const d: Record<string, PageElement> = {}
    for (const el of Doc.applySelector(ancestor, '[id]')) d[el.id] = el
    return d
  }

  /*
   * formatCoinValue formats the value in atomic units into a string
   * representation in conventional units. If the value happens to be an
   * integer, no decimals are displayed. Trailing zeros may be truncated.
   */
  static formatCoinValue (vAtomic: number, unitInfo?: UnitInfo): string {
    const [v, prec] = convertToConventional(vAtomic, unitInfo)
    if (Number.isInteger(v)) return intFormatter.format(v)
    return decimalFormatter(prec).format(v)
  }

  static formatThreeSigFigs (v: number): string {
    if (v >= 1000) return intFormatter.format(Math.round(v))
    return threeSigFigs.format(v)
  }

  static formatFiveSigFigs (v: number, prec?: number): string {
    if (v >= 10000) return intFormatter.format(Math.round(v))
    else if (v < 1e5) return fullPrecisionFormatter(prec ?? 8 /* rate encoding factor */).format(v)
    return fiveSigFigs.format(v)
  }

  /*
   * formatFullPrecision formats the value in atomic units into a string
   * representation in conventional units using the full decimal precision
   * associated with the conventional unit's conversion factor.
   */
  static formatFullPrecision (vAtomic: number, unitInfo?: UnitInfo): string {
    const [v, prec] = convertToConventional(vAtomic, unitInfo)
    return fullPrecisionFormatter(prec).format(v)
  }

  /*
   * formatFiatConversion formats the value in atomic units to its representation in
   * conventional units and returns the fiat value as a string.
   */
  static formatFiatConversion (vAtomic: number, rate: number, unitInfo?: UnitInfo): string {
    if (!rate || rate === 0) return intl.prep(intl.ID_UNAVAILABLE)
    const prec = 2
    const [v] = convertToConventional(vAtomic, unitInfo)
    const value = v * rate
    return fullPrecisionFormatter(prec).format(value)
  }

  /*
   * logoPath creates a path to a png logo for the specified ticker symbol. If
   * the symbol is not a supported asset, the generic letter logo will be
   * requested instead.
   */
  static logoPath (symbol: string): string {
    if (BipSymbols.indexOf(symbol) === -1) symbol = symbol.substring(0, 1)
    return `/img/coins/${symbol}.png`
  }

  static bipSymbol (assetID: number): string {
    return BipIDs[assetID]
  }

  static logoPathFromID (assetID: number): string {
    return Doc.logoPath(BipIDs[assetID])
  }

  /*
   * symbolize creates a token-aware symbol element for the asset's symbol. For
   * non-token assets, this is simply a <span>SYMBOL</span>. For tokens, it'll
   * be <span><span>SYMBOL</span><sup>PARENT</sup></span>.
   */
  static symbolize (symbol: string): PageElement {
    const parts = symbol.split('.')
    const assetSymbol = document.createElement('span')
    assetSymbol.textContent = parts[0].toUpperCase()
    if (parts.length === 1) return assetSymbol
    const span = document.createElement('span')
    span.classList.add('token-aware-symbol')
    span.appendChild(assetSymbol)
    const parent = document.createElement('sup')
    parent.textContent = parts[1].toUpperCase()
    span.appendChild(parent)
    return span
  }

  /*
   * shortSymbol removes the short format of a symbol, with any parent chain
   * identifier removed
   */
  static shortSymbol (symbol: string): string {
    return symbol.split('.')[0].toUpperCase()
  }

  /*
  * cleanTemplates removes the elements from the DOM and deletes the id
  * attribute.
  */
  static cleanTemplates (...tmpls: HTMLElement[]) {
    tmpls.forEach(tmpl => {
      tmpl.remove()
      tmpl.removeAttribute('id')
    })
  }

  /*
  * tmplElement is a helper function for grabbing sub-elements of the market list
  * template.
  */
  static tmplElement (ancestor: Document | HTMLElement, s: string): PageElement {
    return ancestor.querySelector(`[data-tmpl="${s}"]`) || document.createElement('div')
  }

  /*
  * parseTemplate returns an object of data-tmpl elements, keyed by their
  * data-tmpl values.
  */
  static parseTemplate (ancestor: HTMLElement): Record<string, PageElement> {
    const d: Record<string, PageElement> = {}
    for (const el of Doc.applySelector(ancestor, '[data-tmpl]')) d[el.dataset.tmpl || ''] = el
    return d
  }

  /*
   * timeSince returns a string representation of the duration since the
   * specified unix timestamp.
   */
  static timeSince (t: number): string {
    return Doc.formatDuration((new Date().getTime()) - t)
  }

  /* formatDuration returns a string representation of the duration */
  static formatDuration (dur: number): string {
    let seconds = Math.floor(dur)
    let result = ''
    let count = 0
    const add = (n: number, s: string) => {
      if (n > 0 || count > 0) count++
      if (n > 0) result += `${n} ${s} `
      return count >= 2
    }
    let y, mo, d, h, m, s
    [y, seconds] = timeMod(seconds, aYear)
    if (add(y, 'y')) { return result }
    [mo, seconds] = timeMod(seconds, aMonth)
    if (add(mo, 'mo')) { return result }
    [d, seconds] = timeMod(seconds, aDay)
    if (add(d, 'd')) { return result }
    [h, seconds] = timeMod(seconds, anHour)
    if (add(h, 'h')) { return result }
    [m, seconds] = timeMod(seconds, aMinute)
    if (add(m, 'm')) { return result }
    [s, seconds] = timeMod(seconds, 1000)
    add(s, 's')
    return result || '0 s'
  }

  /*
   * disableMouseWheel can be used to disable the mouse wheel for any
   * input. It is very easy to unknowingly scroll up on a number input
   * and then submit an unexpected value. This function prevents the
   * scroll increment/decrement behavior for a wheel action on a
   * number input.
   */
  static disableMouseWheel (...inputFields: Element[]) {
    for (const inputField of inputFields) {
      inputField.addEventListener('wheel', (ev) => {
        ev.preventDefault()
      })
    }
  }

  // showFormError can be used to set and display error message on forms.
  static showFormError (el: PageElement, msg: any) {
    el.textContent = msg
    Doc.show(el)
  }
}

/*
 * Animation is a handler for starting and stopping animations.
 */
export class Animation {
  done: (() => void) | undefined
  endAnimation: boolean
  thread: Promise<void>
  static Forever: number

  constructor (duration: number, f: (progress: number) => void, easingAlgo?: string, done?: () => void) {
    this.done = done
    this.thread = this.run(duration, f, easingAlgo)
  }

  /*
   * run runs the animation function, increasing progress from 0 to 1 in a
   * manner dictated by easingAlgo.
   */
  async run (duration: number, f: (progress: number) => void, easingAlgo?: string) {
    duration = duration >= 0 ? duration : 1000 * 86400 * 365 * 10 // 10 years, in ms
    const easer = easingAlgo ? Easing[easingAlgo] : Easing.linear
    const start = new Date().getTime()
    const end = (duration === Animation.Forever) ? Number.MAX_SAFE_INTEGER : start + duration
    const range = end - start
    const frameDuration = 1000 / FPS
    let now = start
    this.endAnimation = false
    while (now < end) {
      if (this.endAnimation) return this.runCompletionFunction()
      f(easer((now - start) / range))
      await sleep(frameDuration)
      now = new Date().getTime()
    }
    f(1)
    this.runCompletionFunction()
  }

  /* wait returns a promise that will resolve when the animation completes. */
  async wait () {
    await this.thread
  }

  /* stop schedules the animation to exit at its next frame. */
  stop () {
    this.endAnimation = true
  }

  /*
   * stopAndWait stops the animations and returns a promise that will resolve
   * when the animation exits.
   */
  async stopAndWait () {
    this.stop()
    await this.wait()
  }

  /* runCompletionFunction runs any registered callback function */
  runCompletionFunction () {
    if (this.done) this.done()
  }
}
Animation.Forever = -1

/* Easing algorithms for animations. */
export const Easing: Record<string, (t: number) => number> = {
  linear: t => t,
  easeIn: t => t * t,
  easeOut: t => t * (2 - t),
  easeInHard: t => t * t * t,
  easeOutHard: t => (--t) * t * t + 1,
  easeOutElastic: t => {
    const c4 = (2 * Math.PI) / 3
    return t === 0
      ? 0
      : t === 1
        ? 1
        : Math.pow(2, -10 * t) * Math.sin((t * 10 - 0.75) * c4) + 1
  }
}

/* WalletIcons are used for controlling wallets in various places. */
export class WalletIcons {
  icons: Record<string, HTMLElement>
  status: Element

  constructor (box: HTMLElement) {
    const stateElement = (name: string) => box.querySelector(`[data-state=${name}]`) as HTMLElement
    this.icons = {}
    this.icons.sleeping = stateElement('sleeping')
    this.icons.locked = stateElement('locked')
    this.icons.unlocked = stateElement('unlocked')
    this.icons.nowallet = stateElement('nowallet')
    this.icons.syncing = stateElement('syncing')
    this.icons.nopeers = stateElement('nopeers')
    this.icons.disabled = stateElement('disabled')
    this.status = stateElement('status')
  }

  /* sleeping sets the icons to indicate that the wallet is not connected. */
  sleeping () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.nowallet, i.syncing, i.disabled)
    Doc.show(i.sleeping)
    if (this.status) this.status.textContent = intl.prep(intl.ID_OFF)
  }

  /*
   * locked sets the icons to indicate that the wallet is connected, but locked.
   */
  locked () {
    const i = this.icons
    Doc.hide(i.unlocked, i.nowallet, i.sleeping, i.disabled)
    Doc.show(i.locked)
    if (this.status) this.status.textContent = intl.prep(intl.ID_LOCKED)
  }

  /*
   * unlocked sets the icons to indicate that the wallet is connected and
   * unlocked.
   */
  unlocked () {
    const i = this.icons
    Doc.hide(i.locked, i.nowallet, i.sleeping, i.disabled)
    Doc.show(i.unlocked)
    if (this.status) this.status.textContent = intl.prep(intl.ID_READY)
  }

  /* nowallet sets the icons to indicate that no wallet exists. */
  nowallet () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.sleeping, i.syncing, i.disabled)
    Doc.show(i.nowallet)
    if (this.status) this.status.textContent = intl.prep(intl.ID_NO_WALLET)
  }

  /* set the icons to indicate that the wallet is disabled */
  disabled () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.sleeping, i.syncing, i.nowallet, i.nopeers)
    Doc.show(i.disabled)
    i.disabled.dataset.tooltip = intl.prep(intl.ID_DISABLED_MSG)
  }

  setSyncing (wallet: WalletState | null) {
    const syncIcon = this.icons.syncing
    if (!wallet || !wallet.running || wallet.disabled) {
      Doc.hide(syncIcon)
      return
    }

    if (wallet.peerCount === 0) {
      Doc.show(this.icons.nopeers)
      Doc.hide(syncIcon) // potentially misleading with no peers
      return
    }
    Doc.hide(this.icons.nopeers)

    if (!wallet.synced) {
      Doc.show(syncIcon)
      syncIcon.dataset.tooltip = intl.prep(intl.ID_WALLET_SYNC_PROGRESS, { syncProgress: (wallet.syncProgress * 100).toFixed(1) })
      return
    }
    Doc.hide(syncIcon)
  }

  /* reads the core.Wallet state and sets the icon visibility. */
  readWallet (wallet: WalletState | null) {
    this.setSyncing(wallet)
    if (!wallet) return this.nowallet()
    switch (true) {
      case (wallet.disabled):
        this.disabled()
        break
      case (!wallet.running):
        this.sleeping()
        break
      case (!wallet.open):
        this.locked()
        break
      case (wallet.open):
        this.unlocked()
        break
      default:
        console.error('wallet in unknown state', wallet)
    }
  }
}

/* sleep can be used by async functions to pause for a specified period. */
function sleep (ms: number) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

const aYear = 31536000000
const aMonth = 2592000000
const aDay = 86400000
const anHour = 3600000
const aMinute = 60000

/* timeMod returns the quotient and remainder of t / dur. */
function timeMod (t: number, dur: number) {
  const n = Math.floor(t / dur)
  return [n, t - n * dur]
}
