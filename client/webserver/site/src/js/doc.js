var parser = new window.DOMParser()

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
}
