const darkModeCK = 'darkMode'

// State is a set of static methods for working with the user state. It has
// utilities for setting and retrieving cookies and storing user configuration
// to localStorage.
export default class State {
  static setCookie (cname, cvalue) {
    var d = new Date()
    // Set cookie to expire in ten years.
    d.setTime(d.getTime() + (86400 * 365 * 10 * 1000))
    var expires = 'expires=' + d.toUTCString()
    document.cookie = cname + '=' + cvalue + ';' + expires + ';path=/'
  }

  static dark (dark) {
    this.setCookie(darkModeCK, dark ? '1' : '0')
    if (dark) {
      document.body.classList.add('dark')
    } else {
      document.body.classList.remove('dark')
    }
  }

  static isDark () {
    return document.cookie.split(';').filter((item) => item.includes(`${darkModeCK}=1`)).length
  }

  static store (k, v) {
    window.localStorage.setItem(k, JSON.stringify(v))
  }

  static fetch (k) {
    const v = window.localStorage.getItem(k)
    if (v !== null) {
      return JSON.parse(v)
    }
    return null
  }
}
