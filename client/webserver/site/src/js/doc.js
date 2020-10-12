const parser = new window.DOMParser()

const FPS = 30

const BipIDs = {
  0: 'btc',
  42: 'dcr',
  2: 'ltc',
  22: 'mona',
  28: 'vtc',
  3: 'doge'
}

const BipSymbols = Object.values(BipIDs)

// Parameters for printing asset values.
const fullPrecisionSpecs = {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 8,
  minimumFractionDigits: 8,
  maximumFractionDigits: 8
}

// Helpers for working with the DOM.
export default class Doc {
  /*
   * idel is the element with the specified id that is the descendent of the
   * specified node.
   */
  static idel (el, id) {
    return el.querySelector(`#${id}`)
  }

  /* bind binds the function to the event for the element. */
  static bind (el, ev, f) {
    el.addEventListener(ev, f)
  }

  /* unbind removes the handler for the event from the element. */
  static unbind (el, ev, f) {
    el.removeEventListener(ev, f)
  }

  /* noderize creates a Document object from a string of HTML. */
  static noderize (html) {
    return parser.parseFromString(html, 'text/html')
  }

  /*
   * mouseInElement returns true if the position of mouse event, e, is within
   * the bounds of the specified element.
   */
  static mouseInElement (e, el) {
    const rect = el.getBoundingClientRect()
    return e.pageX >= rect.left && e.pageX <= rect.right &&
      e.pageY >= rect.top && e.pageY <= rect.bottom
  }

  /*
   * layoutMetrics gets information about the elements position on the page.
   */
  static layoutMetrics (el) {
    var box = el.getBoundingClientRect()
    var docEl = document.documentElement
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

  /* empty removes all child nodes from the specified element. */
  static empty (el) {
    while (el.firstChild) el.removeChild(el.firstChild)
  }

  /*
   * hide hides the specified elements. This is accomplished by adding the
   * bootstrap d-hide class to the element. Use Doc.show to undo.
   */
  static hide (...els) {
    for (const el of els) el.classList.add('d-hide')
  }

  /*
   * show shows the specified elements. This is accomplished by removing the
   * bootstrap d-hide class as added with Doc.hide.
   */
  static show (...els) {
    for (const el of els) el.classList.remove('d-hide')
  }

  /* isHidden returns true if the specified element is hidden */
  static isHidden (el) {
    return el.classList.contains('d-hide')
  }

  /* isDisplayed returns true if the specified element is not hidden */
  static isDisplayed (el) {
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
  static async animate (duration, f, easingAlgo) {
    const easer = easingAlgo ? Easing[easingAlgo] : Easing.linear
    const start = new Date().getTime()
    const end = start + duration
    const range = end - start
    const frameDuration = 1000 / FPS
    var now = start
    while (now < end) {
      f(easer((now - start) / range))
      await sleep(frameDuration)
      now = new Date().getTime()
    }
    f(1)
  }

  /*
   * parsePage constructs a page object from the supplied list of id strings.
   * The properties of the returned object have names matching the supplied
   * id strings, with the corresponding value being the Element object. It is
   * not an error if an element does not exist for an id in the list.
   */
  static parsePage (main, ids) {
    const get = s => Doc.idel(main, s)
    const page = {}
    ids.forEach(id => { page[id] = get(id) })
    return page
  }

  // formatCoinValue formats the asset value to a string.
  static formatCoinValue (x) {
    var [whole, frac] = x.toLocaleString('en-us', fullPrecisionSpecs).split('.')
    // toLocalString gives precedence to minimumSignificantDigits, so the result
    // can have no fractional part, despite the minimumFractionDigits setting.
    if (!frac) return whole
    // ... or it can have more than 8 fractional digits, despite of the
    // maximumFractionDigits setting.
    frac = frac.substring(0, 8)
    if (frac === '00000000') return whole
    // Trim trailing zeros.
    return `${whole}.${frac.replace(/,+$/, '')}`
  }

  /*
   * logoPath creates a path to a png logo for the specified ticker symbol. If
   * the symbol is not a supported asset, the generic letter logo will be
   * requested instead.
   */
  static logoPath (symbol) {
    if (BipSymbols.indexOf(symbol) === -1) symbol = symbol.substring(0, 1)
    return `/img/coins/${symbol}.png`
  }

  /*
  * tmplElement is a helper function for grabbing sub-elements of the market list
  * template.
  */
  static tmplElement (ancestor, s) {
    return ancestor.querySelector(`[data-tmpl="${s}"]`)
  }

  /*
   * timeSince returns a string representation of the duration since the specified
   * unix timestamp.
   */
  static timeSince (t) {
    var seconds = Math.floor(((new Date().getTime()) - t))
    var result = ''
    var count = 0
    const add = (n, s) => {
      if (n > 0 || count > 0) count++
      if (n > 0) result += `${n} ${s} `
      return count >= 2
    }
    var y, mo, d, h, m, s
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
  static disableMouseWheel (...inputFields) {
    for (const inputField of inputFields) {
      inputField.addEventListener('wheel', (ev) => {
        ev.preventDefault()
      })
    }
  }
}

/* Easing algorithms for animations. */
var Easing = {
  linear: t => t,
  easeIn: t => t * t,
  easeOut: t => t * (2 - t),
  easeInHard: t => t * t * t,
  easeOutHard: t => (--t) * t * t + 1
}

/* WalletIcons are used for controlling wallets in various places. */
export class WalletIcons {
  constructor (box) {
    const stateElement = (row, name) => row.querySelector(`[data-state=${name}]`)
    this.icons = {}
    this.icons.sleeping = stateElement(box, 'sleeping')
    this.icons.locked = stateElement(box, 'locked')
    this.icons.unlocked = stateElement(box, 'unlocked')
    this.icons.nowallet = stateElement(box, 'nowallet')
    this.status = stateElement(box, 'status')
  }

  /* sleeping sets the icons to indicate that the wallet is not connected. */
  sleeping () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.nowallet)
    Doc.show(i.sleeping)
    if (this.status) this.status.textContent = 'off'
  }

  /*
   * locked sets the icons to indicate that the wallet is connected, but locked.
   */
  locked () {
    const i = this.icons
    Doc.hide(i.unlocked, i.nowallet, i.sleeping)
    Doc.show(i.locked)
    if (this.status) this.status.textContent = 'locked'
  }

  /*
   * unlocked sets the icons to indicate that the wallet is connected and
   * unlocked.
   */
  unlocked () {
    const i = this.icons
    Doc.hide(i.locked, i.nowallet, i.sleeping)
    Doc.show(i.unlocked)
    if (this.status) this.status.textContent = 'ready'
  }

  /* sleeping sets the icons to indicate that no wallet exists. */
  nowallet () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.sleeping)
    Doc.show(i.nowallet)
    if (this.status) this.status.textContent = 'no wallet'
  }

  /* reads the core.Wallet state and sets the icon visibility. */
  readWallet (wallet) {
    switch (true) {
      case (!wallet):
        this.nowallet()
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
function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

const aYear = 31536000000
const aMonth = 2592000000
const aDay = 86400000
const anHour = 3600000
const aMinute = 60000

/* timeMod returns the quotient and remainder of t / dur. */
function timeMod (t, dur) {
  const n = Math.floor(t / dur)
  return [n, t - n * dur]
}
