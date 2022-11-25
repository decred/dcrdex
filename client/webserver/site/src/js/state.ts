// State is a set of static methods for working with the user state. It has
// utilities for setting and retrieving cookies and storing user configuration
// to localStorage.
export default class State {
  static DarkModeCK = 'darkMode'
  static AuthCK = 'dexauth'
  static PopupsCK = 'popups'
  static PwKeyCK = 'sessionkey'
  static LeftMarketDockCK = 'leftmarketdock'
  static SelectedAssetCK = 'selectedAsset'

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

  /* showLeftMarketDock returns whether or not user wants left market doc shown. */
  static showLeftMarketDock () {
    return this.getCookie(State.LeftMarketDockCK) === '1'
  }

  static removeAuthCK () {
    document.cookie = `${State.AuthCK}=;expires=Thu, 01 Jan 1970 00:00:01 GMT;`
  }

  /*
   * selectedAsset returns selected asset ID or null if none user hasn't selected one
   * yet.
   */
  static selectedAsset (): number | null {
    const assetIDStr = State.getCookie(State.SelectedAssetCK)
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

// Setting defaults here, unless specific cookie value was already chosen by the user.
if (State.getCookie(State.DarkModeCK) === null) State.setCookie(State.DarkModeCK, '1')
if (State.getCookie(State.PopupsCK) === null) State.setCookie(State.PopupsCK, '1')
if (State.getCookie(State.LeftMarketDockCK) === null) State.setCookie(State.LeftMarketDockCK, '1')
