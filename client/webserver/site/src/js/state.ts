const darkModeCK = 'darkMode'
const authCK = 'dexauth'
const popupsCK = 'popups'
const pwKeyCK = 'sessionkey'

// State is a set of static methods for working with the user state. It has
// utilities for setting and retrieving cookies and storing user configuration
// to localStorage.
export default class State {
  static setCookie (cname: string, cvalue: string) {
    const d = new Date()
    // Set cookie to expire in ten years.
    d.setTime(d.getTime() + (86400 * 365 * 10 * 1000))
    const expires = 'expires=' + d.toUTCString()
    document.cookie = cname + '=' + cvalue + ';' + expires + ';path=/'
  }

  /*
   * getCookie returns the value at the specified cookie name, otherwise null.
   */
  static getCookie (cname: string) {
    for (const cstr of document.cookie.split(';')) {
      const [k, v] = cstr.split('=')
      if (k.trim() === cname) return v
    }
    return null
  }

  /* dark sets the dark-mode cookie. */
  static dark (dark: boolean) {
    this.setCookie(darkModeCK, dark ? '1' : '0')
    if (dark) {
      document.body.classList.add('dark')
    } else {
      document.body.classList.remove('dark')
    }
  }

  /*
   * isDark returns true if the dark-mode cookie is currently set to '1' = true.
   */
  static isDark () {
    return document.cookie.split(';').filter((item) => item.includes(`${darkModeCK}=1`)).length
  }

  /* passwordIsCached returns whether or not there is a cached password in the cookies. */
  static passwordIsCached () {
    return !!this.getCookie(pwKeyCK)
  }

  /* store puts the key-value pair into Window.localStorage. */
  static store (k: string, v: any) {
    window.localStorage.setItem(k, JSON.stringify(v))
  }

  /* clearAllStore remove all the key-value pair in Window.localStorage. */
  static clearAllStore () {
    window.localStorage.clear()
  }

  static removeAuthCK () {
    document.cookie = `${authCK}=;expires=Thu, 01 Jan 1970 00:00:01 GMT;`
  }

  /*
  * fetch fetches the value associated with the key in Window.localStorage, or
  * null if the no value exists for the key.
  */
  static fetch (k: string) {
    const v = window.localStorage.getItem(k)
    if (v !== null) {
      return JSON.parse(v)
    }
    return null
  }
}

// If the dark-mode cookie is not set, set it to dark mode on.
if (State.getCookie(darkModeCK) === null) State.setCookie(darkModeCK, '1')
if (State.getCookie(popupsCK) === null) State.setCookie(popupsCK, '1')
