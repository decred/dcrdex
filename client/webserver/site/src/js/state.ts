// State is a set of static methods for working with the user state. It has
// utilities for setting and retrieving cookies and storing user configuration
// to localStorage.
export default class State {
  // Cookie keys.
  static DarkModeCK = 'darkMode'
  static AuthCK = 'dexauth'
  static PopupsCK = 'popups'
  static PwKeyCK = 'sessionkey'
  // Local storage keys (for data that we don't need at the server).
  static LeftMarketDockLK = 'leftmarketdock'
  static SelectedAssetLK = 'selectedasset'

  static orderDisclaimerAckedLK = 'ordAck'

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

  /*
   * isDark returns true if the dark-mode cookie is currently set to '1' = true.
   */
  static isDark () {
    return document.cookie.split(';').filter((item) => item.includes(`${State.DarkModeCK}=1`)).length
  }

  /* passwordIsCached returns whether or not there is a cached password in the cookies. */
  static passwordIsCached () {
    return !!this.getCookie(State.PwKeyCK)
  }

  static removeAuthCK () {
    document.cookie = `${State.AuthCK}=;expires=Thu, 01 Jan 1970 00:00:01 GMT;`
  }

  /* showLeftMarketDock returns whether or not user wants left market doc shown. */
  static showLeftMarketDock () {
    return this.fetch(State.LeftMarketDockLK) === '1'
  }

  /*
   * selectedAsset returns selected asset ID or null if none user hasn't selected one
   * yet.
   */
  static selectedAsset (): number | null {
    const assetIDStr = State.fetch(State.SelectedAssetLK)
    if (!assetIDStr) {
      return null
    }
    return Number(assetIDStr)
  }

  /* store puts the key-value pair into Window.localStorage. */
  static store (k: string, v: any) {
    window.localStorage.setItem(k, JSON.stringify(v))
  }

  /* clearAllStore remove all the key-value pair in Window.localStorage. */
  static clearAllStore () {
    window.localStorage.clear()
  }

  /*
  * fetch the value associated with the key in Window.localStorage, or
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

// Setting defaults here, unless specific cookie (or local storage) value was already chosen by the user.
if (State.getCookie(State.DarkModeCK) === null) State.setCookie(State.DarkModeCK, '1')
if (State.getCookie(State.PopupsCK) === null) State.setCookie(State.PopupsCK, '1')
if (State.fetch(State.LeftMarketDockLK) === null) State.store(State.LeftMarketDockLK, '1')
