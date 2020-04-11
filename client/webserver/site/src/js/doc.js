const parser = new window.DOMParser()

const FPS = 30

// Parameters for printing asset values.
const coinValueSpecs = {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 6,
  maximumFractionDigits: 8
}

// Helpers for working with the DOM.
export default class Doc {
  /*
   * idel is the element with the specified id that is the descendent of the
   * specificed node.
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

  /*
   * animate runs the supplied function, which should be a "progress" function
   * accepting one argument. The progress function will be called repeatedly
   * with the argument varying from 0.0 to 1.0. The exact path that animate
   * takes from 0.0 to 1.0 will vary depending on the choice of easing
   * algorithm. See the Easing object for the available easing algo choices.
   */
  static async animate (duration, f, easingAlgo) {
    const easer = easingAlgo ? Easing[easingAlgo] : Easing.linear
    // key is a string referencing any property of Meter.data.
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
    return x.toLocaleString('en-us', coinValueSpecs)
  }

  /* logoPath creates a path to a png logo for the specified ticker symbol. */
  static logoPath (symbol) {
    return `/img/coins/${symbol}.png`
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

/* StateIcons are used for controlling wallets in various places. */
export class StateIcons {
  constructor (box) {
    const stateIcon = (row, name) => row.querySelector(`[data-state=${name}]`)
    this.icons = {}
    this.icons.sleeping = stateIcon(box, 'sleeping')
    this.icons.locked = stateIcon(box, 'locked')
    this.icons.unlocked = stateIcon(box, 'unlocked')
    this.icons.nowallet = stateIcon(box, 'nowallet')
  }

  /* sleeping sets the icons to indicate that the wallet is not connected. */
  sleeping () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.nowallet)
    Doc.show(i.sleeping)
  }

  /*
   * locked sets the icons to indicate that the wallet is connected, but locked.
   */
  locked () {
    const i = this.icons
    Doc.hide(i.unlocked, i.nowallet, i.sleeping)
    Doc.show(i.locked)
  }

  /*
   * unlocked sets the icons to indicate that the wallet is connected and
   * unlocked.
   */
  unlocked () {
    const i = this.icons
    Doc.hide(i.locked, i.nowallet, i.sleeping)
    Doc.show(i.unlocked)
  }

  /* sleeping sets the icons to indicate that no wallet exists. */
  nowallet () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.sleeping)
    Doc.show(i.nowallet)
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
