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
const noteCacheSize = 100

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

// unathedPages are pages that don't require authorization to load.
// These are endpoints outside of the requireLogin block in webserver.New.
const unauthedPages = ['register', 'login', 'settings']

// Application is the main javascript web application for the Decred DEX client.
export default class Application {
  constructor () {
    this.notes = []
    this.pokes = []
    // The "user" is a large data structure that contains nearly all state
    // information, including exchanges, markets, wallets, and orders.
    this.user = {
      accounts: {},
      wallets: {}
    }
    this.commitHash = commitHash
    this.showPopups = State.getCookie('popups') === '1'
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
    // Don't fetch the user until we know what page we're on.
    await this.fetchUser()
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
    // If it's not a page that requires auth, skip the error notification.
    const skipNote = unauthedPages.indexOf(this.main.dataset.handler) > -1
    if (!this.checkResponse(user, skipNote)) return
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
    Doc.hide(this.page.noteBox, this.page.profileBox)
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
    const handlerID = this.main.dataset.handler
    if (!handlerID) {
      console.error('cannot attach to content with no specified handler')
      return
    }
    this.attachCommon(this.main)
    if (this.loadedPage) this.loadedPage.unload()
    const constructor = constructors[handlerID]
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
        let left = lyt.centerX - this.tooltip.offsetWidth / 2
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
    this.popupNotes = idel(document.body, 'popupNotes')
    this.popupTmpl = Doc.tmplElement(this.popupNotes, 'note')
    this.popupTmpl.remove()
    this.tooltip = idel(document.body, 'tooltip')
    const pg = this.page = Doc.parsePage(this.header, [
      'noteIndicator', 'noteList', 'noteTmpl', 'marketsMenuEntry',
      'walletsMenuEntry', 'noteMenuEntry', 'loader', 'profileMenuEntry',
      'profileIndicator', 'profileSignout', 'innerNoteIcon', 'innerProfileIcon',
      'noteBox', 'profileBox', 'pokeCat', 'noteCat', 'pokeBox', 'pokeList',
      'pokeTmpl'
    ])
    delete pg.noteTmpl.id
    pg.noteTmpl.remove()
    delete pg.pokeTmpl.id
    pg.pokeTmpl.remove()
    pg.loader.remove()
    pg.loader.style.backgroundColor = 'rgba(0, 0, 0, 0.5)'
    Doc.show(pg.loader)

    bind(pg.noteMenuEntry, 'click', async () => {
      Doc.hide(pg.pokeList)
      Doc.show(pg.noteList)
      this.ackNotes()
      pg.noteCat.classList.add('active')
      pg.pokeCat.classList.remove('active')
      this.showDropdown(pg.noteMenuEntry, pg.noteBox)
      Doc.hide(pg.noteIndicator)
      for (const note of this.notes) {
        if (note.acked) {
          note.el.classList.remove('firstview')
        }
      }
      this.setNoteTimes(pg.noteList)
      this.setNoteTimes(pg.pokeList)
      this.storeNotes()
    })

    bind(pg.profileMenuEntry, 'click', () => {
      this.showDropdown(pg.profileMenuEntry, pg.profileBox)
    })

    bind(pg.innerNoteIcon, 'click', () => { Doc.hide(pg.noteBox) })
    bind(pg.innerProfileIcon, 'click', () => { Doc.hide(pg.profileBox) })

    bind(pg.profileSignout, 'click', async e => await this.signOut())

    bind(pg.pokeCat, 'click', () => {
      this.setNoteTimes(pg.pokeList)
      pg.pokeCat.classList.add('active')
      pg.noteCat.classList.remove('active')
      Doc.hide(pg.noteList)
      Doc.show(pg.pokeList)
      this.ackNotes()
    })

    bind(pg.noteCat, 'click', () => {
      this.setNoteTimes(pg.noteList)
      pg.noteCat.classList.add('active')
      pg.pokeCat.classList.remove('active')
      Doc.hide(pg.pokeList)
      Doc.show(pg.noteList)
      this.ackNotes()
    })
  }

  /*
   * showDropdown sets the position and visibility of the specified dropdown
   * dialog according to the position of its icon button.
   */
  showDropdown (icon, dialog) {
    const ico = icon.getBoundingClientRect()
    Doc.hide(this.page.noteBox, this.page.profileBox)
    Doc.show(dialog)
    dialog.style.right = `${window.innerWidth - ico.left - ico.width + 11}px`
    dialog.style.top = `${ico.top - 9}px`

    const hide = e => {
      if (!Doc.mouseInElement(e, dialog)) {
        Doc.hide(dialog)
        unbind(document, 'click', hide)
        if (dialog === this.page.noteBox && Doc.isDisplayed(this.page.noteList)) {
          this.ackNotes()
        }
      }
    }
    bind(document, 'click', hide)
  }

  ackNotes () {
    const acks = []
    for (const note of this.notes) {
      if (note.acked) {
        note.el.classList.remove('firstview')
      } else {
        note.acked = true
        if (note.id && note.severity > ntfn.POKE) acks.push(note.id)
      }
    }
    if (acks.length) ws.request('acknotes', acks)
    Doc.hide(this.page.noteIndicator)
  }

  setNoteTimes (noteList) {
    for (const el of Array.from(noteList.children)) {
      el.querySelector('span.note-time').textContent = Doc.timeSince(el.note.stamp)
    }
  }

  /*
   * bindInternalNavigation hijacks navigation by click on any local links that
   * are descendents of ancestor.
   */
  bindInternalNavigation (ancestor) {
    const pageURL = new URL(window.location)
    ancestor.querySelectorAll('a').forEach(a => {
      if (!a.href) return
      const url = new URL(a.href)
      if (url.origin === pageURL.origin) {
        const token = url.pathname.substring(1)
        const params = {}
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
    this.notes = []
    Doc.empty(this.page.noteList)
    for (let i = 0; i < notes.length; i++) {
      this.prependNoteElement(notes[i], true)
    }
    this.storeNotes()
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
        // Updates given order in market's orders list if it finds it.
        // Returns a bool which indicates if order was found.
        const updateOrder = (mkt, ord) => {
          for (const i in mkt.orders || []) {
            if (mkt.orders[i].id === ord.id) {
              mkt.orders[i] = ord
              return true
            }
          }
          return false
        }
        // If the notification order already exists we update it.
        // In case market's orders list is empty or the notification order isn't
        // part of it we add it manually as this means the order was
        // just placed.
        if (!mkt.orders) mkt.orders = [order]
        else if (!updateOrder(mkt, order)) mkt.orders.push(order)
        break
      }
      case 'balance': {
        const wallet = this.user.assets &&
          this.user.assets[note.assetID].wallet
        if (wallet) wallet.balance = note.balance
        break
      }
      case 'feepayment':
        this.handleFeePaymentNote(note)
        break
      case 'walletstate':
      case 'walletconfig': {
        const wallet = note.wallet
        this.assets[wallet.assetID].wallet = wallet
        this.walletMap[wallet.assetID] = wallet
        const balances = this.main.querySelectorAll(`[data-balance-target="${wallet.assetID}"]`)
        balances.forEach(el => { el.textContent = (wallet.balance.available / 1e8).toFixed(8) })
        break
      }
      case 'match': {
        const ord = this.order(note.orderID)
        if (ord) updateMatch(ord, note.match)
        break
      }
    }

    // Inform the page.
    if (this.loadedPage) this.loadedPage.notify(note)
    // Discard data notifications.
    if (note.severity < ntfn.POKE) return
    // Poke notifications have their own display.
    if (this.showPopups) {
      const span = this.popupTmpl.cloneNode(true)
      Doc.tmplElement(span, 'text').textContent = `${note.subject}: ${note.details}`
      const indicator = Doc.tmplElement(span, 'indicator')
      if (note.severity === ntfn.POKE) {
        Doc.hide(indicator)
      } else setSeverityClass(indicator, note.severity)
      const pn = this.popupNotes
      pn.appendChild(span)
      // These take up screen space. Only show max 5 at a time.
      while (pn.children.length > 5) pn.removeChild(pn.firstChild)
      setTimeout(async () => {
        await Doc.animate(500, progress => {
          span.style.opacity = 1 - progress
        })
        span.remove()
      }, 6000)
    }
    // Success and higher severity go to the bell dropdown.
    if (note.severity === ntfn.POKE) this.prependPokeElement(note)
    else this.prependNoteElement(note)
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

  prependPokeElement (note) {
    this.pokes.push(note)
    while (this.pokes.length > noteCacheSize) this.pokes.shift()
    const el = this.makePoke(note)
    this.prependListElement(this.page.pokeList, note, el)
  }

  prependNoteElement (note, skipSave) {
    this.notes.push(note)
    while (this.notes.length > noteCacheSize) this.notes.shift()
    const noteList = this.page.noteList
    const el = this.makeNote(note)
    this.prependListElement(noteList, note, el)
    if (!skipSave) this.storeNotes()
    // Set the indicator color.
    if (this.notes.length === 0 || (Doc.isDisplayed(this.page.noteBox) && Doc.isDisplayed(noteList))) return
    let unacked = 0
    const severity = this.notes.reduce((s, note) => {
      if (!note.acked) unacked++
      if (!note.acked && note.severity > s) return note.severity
      return s
    }, ntfn.IGNORE)
    const ni = this.page.noteIndicator
    setSeverityClass(ni, severity)
    if (unacked) {
      ni.textContent = (unacked > noteCacheSize - 1) ? `${noteCacheSize - 1}+` : unacked
      Doc.show(ni)
    } else Doc.hide(ni)
  }

  prependListElement (noteList, note, el) {
    note.el = el
    el.note = note
    noteList.prepend(el)
    while (noteList.children.length > noteCacheSize) noteList.removeChild(noteList.lastChild)
    this.setNoteTimes(noteList)
  }

  /*
   * makeNote constructs a single notification element for the drop-down
   * notification list.
   */
  makeNote (note) {
    const el = this.page.noteTmpl.cloneNode(true)
    if (note.severity > ntfn.POKE) {
      const cls = note.severity === ntfn.SUCCESS ? 'good' : note.severity === ntfn.WARNING ? 'warn' : 'bad'
      el.querySelector('div.note-indicator').classList.add(cls)
    }

    el.querySelector('div.note-subject').textContent = note.subject
    el.querySelector('div.note-details').textContent = note.details
    return el
  }

  makePoke (note) {
    const el = this.page.pokeTmpl.cloneNode(true)
    const d = new Date(note.stamp)
    Doc.tmplElement(el, 'dateTime').textContent = `${d.toLocaleDateString()}, ${d.toLocaleTimeString()}`
    Doc.tmplElement(el, 'details').textContent = `${note.subject}: ${note.details}`
    return el
  }

  /*
   * loading appends the loader to the specified element and displays the
   * loading icon. The loader will block all interaction with the specified
   * element until Application.loaded is called.
   */
  loading (el) {
    const loader = this.page.loader.cloneNode(true)
    el.appendChild(loader)
    return () => { loader.remove() }
  }

  /* orders retrieves a list of orders for the specified dex and market. */
  orders (host, mktID) {
    let o = this.user.exchanges[host].markets[mktID].orders
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
      Doc.hide(this.page.profileBox)
      return
    }
    State.clearAllStore()
    State.removeAuthCK()
    window.location.href = '/login'
  }
}

/* getSocketURI returns the websocket URI for the client. */
function getSocketURI () {
  const protocol = (window.location.protocol === 'https:') ? 'wss' : 'ws'
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

function setSeverityClass (el, severity) {
  el.classList.remove('bad', 'warn', 'good')
  el.classList.add(severityClassMap[severity])
}

/* updateMatch updates the match in or adds the match to the order. */
function updateMatch (order, match) {
  for (const i in order.matches) {
    const m = order.matches[i]
    if (m.matchID === match.matchID) {
      order.matches[i] = match
      return
    }
  }
  order.matches = order.matches || []
  order.matches.push(match)
}
