const parser = new window.DOMParser()

const FPS = 30

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
}

var Easing = {
  linear: t => t,
  easeIn: t => t * t,
  easeOut: t => t * (2 - t),
  easeInHard: t => t * t * t,
  easeOutHard: t => (--t) * t * t + 1
}

function sleep (ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}
