// State is a set of static methods for working with the user state. It has
// utilities for setting and retrieving cookies and storing user configuration
// to localStorage.
export default class State {
  // Cookie keys.
  static darkModeLK = 'darkMode'
  static authCK = 'dexauth'
  static pwKeyCK = 'sessionkey'
  // Local storage keys (for data that we don't need at the server).
  static popupsLK = 'popups'
  static loggersLK = 'loggers'
  static recordersLK = 'recorders'
  static lastMarketLK = 'selectedMarket'
  static depthZoomLK = 'depthZoom'
  static lastMMMarketLK = 'mmMarket'
  static optionsExpansionLK = 'mmOptsExpand'
  static leftMarketDockLK = 'leftmarketdock'
  static selectedAssetLK = 'selectedasset'
  static notificationsLK = 'notifications'
  static pokesLK = 'pokes'
  static orderDisclaimerAckedLK = 'ordAck'
  static lastCandleDurationLK = 'lastCandleDuration'

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
   * removeCookie tells the browser to stop using cookie. It's not enough to simply
   * erase cookie value because browser will still send it to the server (with empty
   * value), and that's not what server expects.
   */
  static removeCookie (cKey: string) {
    document.cookie = `${cKey}=;expires=Thu, 01 Jan 1970 00:00:01 GMT;`
  }

  /*
   * isDark returns true if the dark-mode cookie is currently set to '1' = true.
   */
  static isDark (): boolean {
    return State.fetchLocal(State.darkModeLK) === '1'
  }

  /* passwordIsCached returns whether or not there is a cached password in the cookies. */
  static passwordIsCached () {
    return !!this.getCookie(State.pwKeyCK)
  }

  /* storeLocal puts the key-value pair into Window.localStorage. */
  static storeLocal (k: string, v: any) {
    window.localStorage.setItem(k, JSON.stringify(v))
  }

  /*
  * fetchLocal the value associated with the key in Window.localStorage, or
  * null if the no value exists for the key.
  */
  static fetchLocal (k: string) {
    const v = window.localStorage.getItem(k)
    if (v !== null) {
      return JSON.parse(v)
    }
    return null
  }

  /* removeLocal removes the key-value pair from Window.localStorage. */
  static removeLocal (k: string) {
    window.localStorage.removeItem(k)
  }
}

// Setting defaults here, unless specific cookie (or local storage) value was already chosen by the user.
if (State.fetchLocal(State.darkModeLK) === null) State.storeLocal(State.darkModeLK, '1')
if (State.fetchLocal(State.popupsLK) === null) State.storeLocal(State.popupsLK, '1')
if (State.fetchLocal(State.leftMarketDockLK) === null) State.storeLocal(State.leftMarketDockLK, '1')
