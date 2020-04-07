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
  static idel (el, id) {
    return el.querySelector(`#${id}`)
  }

  static bind (el, ev, f) {
    el.addEventListener(ev, f)
  }

  static unbind (el, ev, f) {
    el.removeEventListener(ev, f)
  }

  static noderize (html) {
    return parser.parseFromString(html, 'text/html')
  }

  static mouseInElement (e, el) {
    const rect = el.getBoundingClientRect()
    return e.pageX >= rect.left && e.pageX <= rect.right &&
      e.pageY >= rect.top && e.pageY <= rect.bottom
  }

  static empty (el) {
    while (el.firstChild) el.removeChild(el.firstChild)
  }

  static hide (...els) {
    for (const el of els) el.classList.add('d-hide')
  }

  static show (...els) {
    for (const el of els) el.classList.remove('d-hide')
  }

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

  static logoPath (symbol) {
    return `/img/coins/${symbol}.png`
  }
}

// Easing algorithms for animations.
var Easing = {
  linear: t => t,
  easeIn: t => t * t,
  easeOut: t => t * (2 - t),
  easeInHard: t => t * t * t,
  easeOutHard: t => (--t) * t * t + 1
}

// StateIcons are used for controlling wallets in various places.
export class StateIcons {
  constructor (box) {
    const stateIcon = (row, name) => row.querySelector(`[data-state=${name}]`)
    this.icons = {}
    this.icons.sleeping = stateIcon(box, 'sleeping')
    this.icons.locked = stateIcon(box, 'locked')
    this.icons.unlocked = stateIcon(box, 'unlocked')
    this.icons.nowallet = stateIcon(box, 'nowallet')
  }

  sleeping () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.nowallet)
    Doc.show(i.sleeping)
  }

  locked () {
    const i = this.icons
    Doc.hide(i.unlocked, i.nowallet, i.sleeping)
    Doc.show(i.locked)
  }

  unlocked () {
    const i = this.icons
    Doc.hide(i.locked, i.nowallet, i.sleeping)
    Doc.show(i.unlocked)
  }

  nowallet () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.sleeping)
    Doc.show(i.nowallet)
  }

  // reads the core.Wallet state and sets the icon visibility.
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

function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}
