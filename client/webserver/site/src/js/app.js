import Doc from './doc'
import State from './state'
import RegistrationPage from './register'
import LoginPage from './login'
import WalletsPage from './wallets'
import SettingsPage from './settings'
import MarketsPage from './markets'
import OrdersPage from './orders'
import OrderPage from './order'
import { getJSON, postJSON } from './http'
import commitHash from 'commitHash'
import * as ntfn from './notifications'
import ws from './ws'

const idel = Doc.idel // = element by id
const bind = Doc.bind
const unbind = Doc.unbind

const notificationRoute = 'notify'
const loggersKey = 'loggers'
const recordersKey = 'recorders'

/* constructors is a map to page constructors. */
const constructors = {
  login: LoginPage,
  register: RegistrationPage,
  markets: MarketsPage,
  wallets: WalletsPage,
  settings: SettingsPage,
  orders: OrdersPage,
  order: OrderPage
}

// Application is the main javascript web application for the Decred DEX client.
export default class Application {
  constructor () {
    this.notes = []
    this.user = {
      accounts: {},
      wallets: {}
    }
    this.commitHash = commitHash
    console.log('Decred DEX Client App, Build', this.commitHash.substring(0, 7))

    // Loggers can be enabled by setting a truthy value to the loggerID using
    // enableLogger. Settings are stored across sessions. See docstring for the
    // log method for more info.
    this.loggers = State.fetch(loggersKey) || {}
    window.enableLogger = (loggerID, state) => {
      if (state) this.loggers[loggerID] = true
      else delete this.loggers[loggerID]
      State.store(loggersKey, this.loggers)
      return `${loggerID} logger ${state ? 'enabled' : 'disabled'}`
    }
    // Enable logging from anywhere.
    window.log = (...a) => { this.log(...a) }

    // Recorders can record log messages, and then save them to file on request.
    const recorderKeys = State.fetch(recordersKey) || []
    this.recorders = {}
    for (const loggerID of recorderKeys) {
      console.log('recording', loggerID)
      this.recorders[loggerID] = []
    }
    window.recordLogger = (loggerID, on) => {
      if (on) this.recorders[loggerID] = []
      else delete this.recorders[loggerID]
      State.store(recordersKey, Object.keys(this.recorders))
      return `${loggerID} recorder ${on ? 'enabled' : 'disabled'}`
    }
    window.dumpLogger = loggerID => {
      const record = this.recorders[loggerID]
      if (!record) return `no recorder for logger ${loggerID}`
      const a = document.createElement('a')
      a.href = `data:application/octet-stream;base64,${window.btoa(JSON.stringify(record, null, 4))}`
      a.download = `${loggerID}.json`
      document.body.appendChild(a)
      a.click()
      setTimeout(() => {
        document.body.removeChild(a)
      }, 0)
    }
  }

  /**
   * Start the application. This is the only thing done from the index.js entry
   * point. Read the id = main element and attach handlers.
   */
  async start () {
    // The "user" is a large data structure that contains nearly all state
    // information, including exchanges, markets, wallets, and orders. It must
    // be loaded immediately.
    await this.fetchUser()
    // Handle back navigation from the browser.
    bind(window, 'popstate', (e) => {
      const page = e.state.page
      if (!page && page !== '') return
      this.loadPage(page, e.state.data, true)
    })
    // The main element is the interchangeable part of the page that doesn't
    // include the header. Main should define a data-handler attribute
    // associated with  one of the available constructors.
    this.main = idel(document, 'main')
    const handler = this.main.dataset.handler
    // The application is free to respond with a page that differs from the
    // one requested in the omnibox, e.g. routing though a login page. Set the
    // current URL state based on the actual page.
    const url = new URL(window.location)
    if (handlerFromPath(url.pathname) !== handler) {
      url.pathname = `/${handler}`
      url.search = ''
      window.history.replaceState({ page: handler }, '', url)
    }
    // Attach stuff.
    this.attachHeader()
    this.attachCommon(this.header)
    this.attach()
    // Load recent notifications from Window.localStorage.
    const notes = State.fetch('notifications')
    this.setNotes(notes || [])
    // Connect the websocket and register the notification route.
    ws.connect(getSocketURI(), this.reconnected)
    ws.registerRoute(notificationRoute, note => {
      this.notify(note)
    })
  }

  /*
   * reconnected is called by the websocket client when a reconnection is made.
   */
  reconnected () {
    window.location.reload() // This triggers another websocket disconnect/connect (!)
  }

  /*
   * Fetch and save the user, which is the primary core state that must be
   * maintained by the Application.
   */
  async fetchUser () {
    const user = await getJSON('/api/user')
    if (!this.checkResponse(user)) return
    this.user = user
    this.assets = user.assets
    this.exchanges = user.exchanges
    this.walletMap = {}
    for (const [assetID, asset] of Object.entries(user.assets)) {
      if (asset.wallet) {
        this.walletMap[assetID] = asset.wallet
      }
    }
    this.updateMenuItemsDisplay()
    return user
  }

  /* Load the page from the server. Insert and bind the DOM. */
  async loadPage (page, data, skipPush) {
    // Close some menus and tooltips.
    this.tooltip.style.left = '-10000px'
    this.page.noteBox.style.display = 'none'
    this.page.noteIndicator.style.display = 'none'
    this.page.profileBox.style.display = 'none'
    // Parse the request.
    const url = new URL(`/${page}`, window.location.origin)
    const requestedHandler = handlerFromPath(page)
    // Fetch and parse the page.
    const response = await window.fetch(url)
    if (!response.ok) return false
    const html = await response.text()
    const doc = Doc.noderize(html)
    const main = idel(doc, 'main')
    const delivered = main.dataset.handler
    // Append the request to the page history.
    if (!skipPush) {
      const path = delivered === requestedHandler ? url.toString() : `/${delivered}`
      window.history.pushState({ page: page, data: data }, delivered, path)
    }
    // Insert page and attach handlers.
    document.title = doc.title
    this.main.replaceWith(main)
    this.main = main
    this.attach(data)
    return true
  }

  /* attach binds the common handlers and calls the page constructor. */
  attach (data) {
    var handlerID = this.main.dataset.handler
    if (!handlerID) {
      console.error('cannot attach to content with no specified handler')
      return
    }
    this.attachCommon(this.main)
    if (this.loadedPage) this.loadedPage.unload()
    var constructor = constructors[handlerID]
    if (constructor) this.loadedPage = new constructor(this, this.main, data)
    else this.loadedPage = null

    // Bind the tooltips.
    this.bindTooltips(this.main)
  }

  bindTooltips (ancestor) {
    ancestor.querySelectorAll('[data-tooltip]').forEach(el => {
      bind(el, 'mouseenter', () => {
        this.tooltip.textContent = el.dataset.tooltip
        const lyt = Doc.layoutMetrics(el)
        var left = lyt.centerX - this.tooltip.offsetWidth / 2
        if (left < 0) left = 5
        if (left + this.tooltip.offsetWidth > document.body.offsetWidth) {
          left = document.body.offsetWidth - this.tooltip.offsetWidth - 5
        }
        this.tooltip.style.left = `${left}px`
        this.tooltip.style.top = `${lyt.bodyTop - this.tooltip.offsetHeight - 5}px`
      })
      bind(el, 'mouseleave', () => {
        this.tooltip.style.left = '-10000px'
      })
    })
  }

  /* attachHeader attaches the header element, which unlike the main element,
   * isn't replaced during page navigation.
   */
  attachHeader () {
    this.header = idel(document.body, 'header')
    this.pokeNotes = idel(document.body, 'pokeNote')
    this.pokeTmpl = Doc.tmplElement(this.pokeNotes, 'note')
    this.pokeTmpl.remove()
    this.tooltip = idel(document.body, 'tooltip')
    const pg = this.page = Doc.parsePage(this.header, [
      'noteIndicator', 'noteBox', 'noteList', 'noteTemplate',
      'marketsMenuEntry', 'walletsMenuEntry', 'noteMenuEntry', 'loader',
      'profileMenuEntry', 'profileIndicator', 'profileBox', 'profileSignout'
    ])
    pg.noteIndicator.style.display = 'none'
    delete pg.noteTemplate.id
    pg.noteTemplate.remove()
    pg.loader.remove()
    pg.loader.style.backgroundColor = 'rgba(0, 0, 0, 0.5)'
    Doc.show(pg.loader)
    var hide = (el, e, f) => {
      if (!Doc.mouseInElement(e, el)) {
        el.style.display = 'none'
        unbind(document, f)
      }
    }
    const hideNotes = e => { hide(pg.noteBox, e, hideNotes) }
    const hideProfile = e => { hide(pg.profileBox, e, hideProfile) }

    bind(pg.noteMenuEntry, 'click', async e => {
      bind(document, 'click', hideNotes)
      pg.noteBox.style.display = 'block'
      pg.noteIndicator.style.display = 'none'
      const acks = []
      for (const note of this.notes) {
        if (!note.acked) {
          note.acked = true
          if (note.id && note.severity > ntfn.POKE) acks.push(note.id)
        }
        note.el.querySelector('div.note-time').textContent = Doc.timeSince(note.stamp)
      }
      this.storeNotes()
      if (acks.length) ws.request('acknotes', acks)
    })
    if (this.notes.length === 0) {
      pg.noteList.textContent = 'no notifications'
    }
    bind(pg.profileMenuEntry, 'click', async e => {
      bind(document, 'click', hideProfile)
      pg.profileBox.style.display = 'block'
    })
    bind(pg.profileSignout, 'click', async e => await this.signOut())
  }

  /*
   * bindInternalNavigation hijacks navigation by click on any local links that
   * are descendents of ancestor.
   * */
  bindInternalNavigation (ancestor) {
    const pageURL = new URL(window.location)
    ancestor.querySelectorAll('a').forEach(a => {
      if (!a.href) return
      const url = new URL(a.href)
      if (url.origin === pageURL.origin) {
        const token = url.pathname.substring(1)
        var params = {}
        if (url.search) {
          url.searchParams.forEach((v, k) => {
            params[k] = v
          })
        }
        Doc.bind(a, 'click', e => {
          e.preventDefault()
          this.loadPage(token, params)
        })
      }
    })
  }

  /*
   * storeNotes stores the list of notifications in Window.localStorage. The
   * actual stored list is stripped of information not necessary for display.
   */
  storeNotes () {
    State.store('notifications', this.notes.map(n => {
      return {
        subject: n.subject,
        details: n.details,
        severity: n.severity,
        stamp: n.stamp,
        id: n.id,
        acked: n.acked
      }
    }))
  }

  /*
   * updateMenuItemsDisplay should be called when the user has signed in or out,
   * and when the user registers a DEX.
   */
  updateMenuItemsDisplay () {
    const pg = this.page
    if (!pg) {
      // initial page load, header elements not yet attached but menu items
      // would already be hidden/displayed as appropriate.
      return
    }
    if (!this.user.authed) {
      Doc.hide(pg.noteMenuEntry, pg.walletsMenuEntry, pg.marketsMenuEntry, pg.profileMenuEntry)
      return
    }
    Doc.show(pg.noteMenuEntry, pg.walletsMenuEntry, pg.profileMenuEntry)
    if (Object.keys(this.user.exchanges).length > 0) {
      Doc.show(pg.marketsMenuEntry)
    } else {
      Doc.hide(pg.marketsMenuEntry)
    }
  }

  /* attachCommon scans the provided node and handles some common bindings. */
  attachCommon (node) {
    this.bindInternalNavigation(node)
  }

  /*
   * updateExchangeRegistration updates the information for the exchange
   * registration payment
   */
  updateExchangeRegistration (dexAddr, isPaid, confs) {
    const dex = this.exchanges[dexAddr]

    if (isPaid) {
      // setting the null value in the 'confs' field indicates that the fee
      // payment was completed
      dex.confs = null
      return
    }

    dex.confs = confs
  }

  /*
   * handleFeePaymentNote is the handler for the 'feepayment'-type notification, which
   * is used to update the dex registration status.
   */
  handleFeePaymentNote (note) {
    switch (note.subject) {
      case 'regupdate':
        this.updateExchangeRegistration(note.dex, false, note.confirmations)
        break
      case 'Account registered':
        this.updateExchangeRegistration(note.dex, true)
        break
      default:
        break
    }
  }

  /*
   * setNotes sets the current notification cache and populates the notification
   * display.
   */
  setNotes (notes) {
    this.log('notes', 'setNotes', notes)
    this.notes = notes
    this.setNoteElements()
  }

  /*
   * notify is the top-level handler for notifications received from the client.
   * Notifications are propagated to the loadedPage.
   */
  notify (note) {
    // Handle type-specific updates.
    this.log('notes', 'notify', note)
    switch (note.type) {
      case 'order': {
        const order = note.order
        const mkt = this.user.exchanges[order.host].markets[order.market]
        if (mkt.orders) {
          for (const i in mkt.orders) {
            if (mkt.orders[i].id === order.id) {
              mkt.orders[i] = order
              break
            }
          }
        }
        break
      }
      case 'balance': {
        const wallet = this.user.assets[note.assetID].wallet
        if (wallet) wallet.balance = note.balance
        break
      }
      case 'feepayment':
        this.handleFeePaymentNote(note)
        break
      case 'walletstate': {
        const wallet = note.wallet
        this.assets[wallet.assetID].wallet = wallet
        this.walletMap[wallet.assetID] = wallet
        const balances = this.main.querySelectorAll(`[data-balance-target="${wallet.assetID}"]`)
        balances.forEach(el => { el.textContent = (wallet.balance.available / 1e8).toFixed(8) })
      }
    }

    // Inform the page.
    if (this.loadedPage) this.loadedPage.notify(note)
    // Discard data notifications.
    if (note.severity < ntfn.POKE) return
    // Poke notifications have their own display.
    if (note.severity === ntfn.POKE) {
      const span = this.pokeTmpl.cloneNode(true)
      span.textContent = `${note.subject}: ${note.details}`
      this.pokeNotes.appendChild(span)
      setTimeout(async () => {
        await Doc.animate(500, progress => {
          span.style.opacity = 1 - progress
        })
        span.remove()
      }, 6000)
      return
    }
    // Success and higher severity go to the bell dropdown.
    this.notes.push(note)
    this.setNoteElements()
  }

  /*
   * log prints to the console if a logger has been enabled. Loggers are created
   * implicitly by passing a loggerID to log. i.e. you don't create a logger,
   * you just log to it. Loggers are enabled by invoking a global function,
   * enableLogger(loggerID, onOffBoolean), from the browser's js console. Your
   * choices are stored across sessions. Some common and useful loggers are
   * listed below, but this list is not meant to be comprehensive.
   *
   * LoggerID   Description
   * --------   -----------
   * notes      Notifications of all levels.
   * book       Order book feed.
   * ws.........Websocket connection status changes.
   */
  log (loggerID, ...msg) {
    if (this.loggers[loggerID]) console.log(`${nowString()}[${loggerID}]:`, ...msg)
    if (this.recorders[loggerID]) {
      this.recorders[loggerID].push({
        time: nowString(),
        msg: msg
      })
    }
  }

  /* setNoteElements re-builds the drop-down notification list. */
  setNoteElements () {
    const noteList = this.page.noteList
    while (this.notes.length > 10) this.notes.shift()
    Doc.empty(noteList)
    for (let i = this.notes.length - 1; i >= 0; i--) {
      const note = this.notes[i]
      const noteEl = this.makeNote(note)
      note.el = noteEl
      noteList.appendChild(noteEl)
    }
    this.storeNotes()
    // Set the indicator color.
    if (this.notes.length === 0) return
    const severity = this.notes.reduce((s, v) => {
      if (!v.acked && v.severity > s) return v.severity
      return s
    }, ntfn.IGNORE)
    setSeverityClass(this.page.noteIndicator, severity)
  }

  /*
   * makeNote constructs a single notification element for the drop-down
   * notification list.
   */
  makeNote (note) {
    const el = this.page.noteTemplate.cloneNode(true)
    if (note.severity > ntfn.POKE) {
      const cls = note.severity === ntfn.SUCCESS ? 'good' : note.severity === ntfn.WARNING ? 'warn' : 'bad'
      el.querySelector('div.note-indicator').classList.add(cls)
    }

    el.querySelector('div.note-subject').textContent = note.subject
    el.querySelector('div.note-details').textContent = note.details
    return el
  }

  /*
   * loading appends the loader to the specified element and displays the
   * loading icon. The loader will block all interaction with the specified
   * element until Application.loaded is called.
   */
  loading (el) {
    el.appendChild(this.page.loader)
  }

  /* loaded hides the loading element as shown with Application.loading. */
  loaded () {
    this.page.loader.remove()
  }

  /* orders retrieves a list of orders for the specified dex and market. */
  orders (host, mktID) {
    var o = this.user.exchanges[host].markets[mktID].orders
    if (!o) {
      o = []
      this.user.exchanges[host].markets[mktID].orders = o
    }
    return o
  }

  /* order attempts to locate an order by order ID. */
  order (oid) {
    for (const xc of Object.values(this.user.exchanges)) {
      for (const market of Object.values(xc.markets)) {
        if (!market.orders) continue
        for (const ord of market.orders) {
          if (ord.id === oid) return ord
        }
      }
    }
  }

  /*
   * checkResponse checks the response object as returned from the functions in
   * the http module. If the response indicates that the request failed, a
   * message will be displayed in the drop-down notifications and false will be
   * returned.
   */
  checkResponse (resp, skipNote) {
    if (!resp.requestSuccessful || !resp.ok) {
      if (this.user.inited && !skipNote) this.notify(ntfn.make('API error', resp.msg, ntfn.ERROR))
      return false
    }
    return true
  }

  /**
   * signOut call to /api/logout, if response with no errors occurred clear all
   * store, remove auth, darkMode cookies and reload the page, otherwise will
   * show a notification
   */
  async signOut () {
    const res = await postJSON('/api/logout')
    if (!this.checkResponse(res)) {
      this.page.profileBox.style.display = 'none'
      return
    }
    State.clearAllStore()
    State.removeAuthCK()
    window.location.href = '/login'
  }
}

/* getSocketURI returns the websocket URI for the client. */
function getSocketURI () {
  var protocol = (window.location.protocol === 'https:') ? 'wss' : 'ws'
  return `${protocol}://${window.location.host}/ws`
}

/*
 * severityClassMap maps a notification severity level to a CSS class that
 * assigns a background color.
 */
const severityClassMap = {
  [ntfn.SUCCESS]: 'good',
  [ntfn.ERROR]: 'bad',
  [ntfn.WARNING]: 'warn'
}

/*
 * setSeverityClass sets the element's CSS class based on the specified
 * notification severity level.
 */
function setSeverityClass (el, severity) {
  el.classList.remove('bad', 'warn', 'good')
  const cls = severityClassMap[severity]
  if (cls) el.classList.add(cls)
  el.style.display = cls ? 'block' : 'none'
}

/* handlerFromPath parses the handler name from the path. */
function handlerFromPath (path) {
  return path.replace(/^\//, '').split('/')[0].split('?')[0].split('#')[0]
}

/* nowString creates a string formatted like HH:MM:SS.xxx */
function nowString () {
  const stamp = new Date()
  const h = stamp.getHours().toString().padStart(2, '0')
  const m = stamp.getMinutes().toString().padStart(2, '0')
  const s = stamp.getSeconds().toString().padStart(2, '0')
  const ms = stamp.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}
