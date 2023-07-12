import Doc from './doc'
import State from './state'
import RegistrationPage from './register'
import LoginPage from './login'
import WalletsPage from './wallets'
import SettingsPage from './settings'
import MarketsPage from './markets'
import OrdersPage from './orders'
import OrderPage from './order'
import DexSettingsPage from './dexsettings'
import InitPage from './init'
import { RateEncodingFactor, StatusExecuted, hasActiveMatches } from './orderutil'
import { getJSON, postJSON, Errors } from './http'
import * as ntfn from './notifications'
import ws from './ws'
import * as intl from './locales'
import {
  User,
  SupportedAsset,
  Exchange,
  WalletState,
  BondNote,
  CoreNote,
  OrderNote,
  Market,
  Order,
  Match,
  BalanceNote,
  WalletConfigNote,
  MatchNote,
  ConnEventNote,
  SpotPriceNote,
  BotNote,
  UnitInfo,
  WalletDefinition,
  WalletBalance,
  LogMessage,
  NoteElement,
  BalanceResponse,
  APIResponse,
  RateNote,
  BotReport,
  InFlightOrder
} from './registry'

const idel = Doc.idel // = element by id
const bind = Doc.bind
const unbind = Doc.unbind

const notificationRoute = 'notify'
const noteCacheSize = 100

interface Page {
  unload (): void
}

interface PageClass {
  new (main: HTMLElement, data: any): Page;
}

interface CoreNotePlus extends CoreNote {
  el: HTMLElement // Added in app
}

/* constructors is a map to page constructors. */
const constructors: Record<string, PageClass> = {
  login: LoginPage,
  register: RegistrationPage,
  markets: MarketsPage,
  wallets: WalletsPage,
  settings: SettingsPage,
  orders: OrdersPage,
  order: OrderPage,
  dexsettings: DexSettingsPage,
  init: InitPage
}

// Application is the main javascript web application for the Decred DEX client.
export default class Application {
  notes: CoreNotePlus[]
  pokes: CoreNotePlus[]
  user: User
  seedGenTime: number
  commitHash: string
  showPopups: boolean
  loggers: Record<string, boolean>
  recorders: Record<string, LogMessage[]>
  main: HTMLElement
  header: HTMLElement
  headerSpace: HTMLElement
  assets: Record<number, SupportedAsset>
  exchanges: Record<string, Exchange>
  walletMap: Record<number, WalletState>
  fiatRatesMap: Record<number, number>
  tooltip: HTMLElement
  page: Record<string, HTMLElement>
  loadedPage: Page | null
  popupNotes: HTMLElement
  popupTmpl: HTMLElement
  noteReceivers: Record<string, (n: CoreNote) => void>[]

  constructor () {
    this.notes = []
    this.pokes = []
    this.seedGenTime = 0
    this.commitHash = process.env.COMMITHASH || ''
    this.noteReceivers = []
    this.fiatRatesMap = {}
    this.showPopups = State.fetchLocal(State.popupsLK) === '1'

    console.log('Decred DEX Client App, Build', this.commitHash.substring(0, 7))

    // Loggers can be enabled by setting a truthy value to the loggerID using
    // enableLogger. Settings are stored across sessions. See docstring for the
    // log method for more info.
    this.loggers = State.fetchLocal(State.loggersLK) || {}
    window.enableLogger = (loggerID, state) => {
      if (state) this.loggers[loggerID] = true
      else delete this.loggers[loggerID]
      State.storeLocal(State.loggersLK, this.loggers)
      return `${loggerID} logger ${state ? 'enabled' : 'disabled'}`
    }
    // Enable logging from anywhere.
    window.log = (loggerID, ...a) => { this.log(loggerID, ...a) }

    // Recorders can record log messages, and then save them to file on request.
    const recorderKeys = State.fetchLocal(State.recordersLK) || []
    this.recorders = {}
    for (const loggerID of recorderKeys) {
      console.log('recording', loggerID)
      this.recorders[loggerID] = []
    }
    window.recordLogger = (loggerID, on) => {
      if (on) this.recorders[loggerID] = []
      else delete this.recorders[loggerID]
      State.storeLocal(State.recordersLK, Object.keys(this.recorders))
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

    // use user current locale set by backend
    intl.setLocale()
  }

  /**
   * Start the application. This is the only thing done from the index.js entry
   * point. Read the id = main element and attach handlers.
   */
  async start () {
    // Handle back navigation from the browser.
    bind(window, 'popstate', (e: PopStateEvent) => {
      const page = e.state.page
      if (!page && page !== '') return
      this.loadPage(page, e.state.data, true)
    })
    // The main element is the interchangeable part of the page that doesn't
    // include the header. Main should define a data-handler attribute
    // associated with one of the available constructors.
    this.main = idel(document, 'main')
    const handler = this.main.dataset.handler
    // Don't fetch the user until we know what page we're on.
    await this.fetchUser()
    // The application is free to respond with a page that differs from the
    // one requested in the omnibox, e.g. routing though a login page. Set the
    // current URL state based on the actual page.
    const url = new URL(window.location.href)
    if (handlerFromPath(url.pathname) !== handler) {
      url.pathname = `/${handler}`
      url.search = ''
      window.history.replaceState({ page: handler }, '', url)
    }
    // Attach stuff.
    this.attachHeader()
    this.attachCommon(this.header)
    this.attach({})
    // Load recent notifications from Window.localStorage.
    const notes = State.fetchLocal(State.notificationsLK)
    this.setNotes(notes || [])
    // Connect the websocket and register the notification route.
    ws.connect(getSocketURI(), this.reconnected)
    ws.registerRoute(notificationRoute, (note: CoreNote) => {
      this.notify(note)
    })
  }

  /*
   * reconnected is called by the websocket client when a reconnection is made.
   */
  reconnected () {
    window.location.reload() // This triggers another websocket disconnect/connect (!)
    // a fetchUser() and loadPage(window.history.state.page) might work
  }

  /*
   * Fetch and save the user, which is the primary core state that must be
   * maintained by the Application.
   */
  async fetchUser (): Promise<User | void> {
    const resp: APIResponse = await getJSON('/api/user')
    if (!this.checkResponse(resp)) return
    const user = (resp as any) as User
    this.seedGenTime = user.seedgentime
    this.user = user
    this.assets = user.assets
    this.exchanges = user.exchanges
    this.walletMap = {}
    this.fiatRatesMap = user.fiatRates
    for (const [assetID, asset] of (Object.entries(user.assets) as [any, SupportedAsset][])) {
      if (asset.wallet) {
        this.walletMap[assetID] = asset.wallet
      }
    }

    this.updateMenuItemsDisplay()
    return user
  }

  authed () {
    return this.user && this.user.authed
  }

  /* Load the page from the server. Insert and bind the DOM. */
  async loadPage (page: string, data?: any, skipPush?: boolean): Promise<boolean> {
    // Close some menus and tooltips.
    this.tooltip.style.left = '-10000px'
    Doc.hide(this.page.noteBox, this.page.profileBox)
    // Parse the request.
    const url = new URL(`/${page}`, window.location.origin)
    const requestedHandler = handlerFromPath(page)
    // Fetch and parse the page.
    const response = await window.fetch(url.toString())
    if (!response.ok) return false
    const html = await response.text()
    const doc = Doc.noderize(html)
    const main = idel(doc, 'main')
    const delivered = main.dataset.handler
    // Append the request to the page history.
    if (!skipPush) {
      const path = delivered === requestedHandler ? url.toString() : `/${delivered}`
      window.history.pushState({ page: page, data: data }, '', path)
    }
    // Insert page and attach handlers.
    document.title = doc.title
    this.main.replaceWith(main)
    this.main = main
    this.noteReceivers = []
    Doc.empty(this.headerSpace)
    this.attach(data)
    return true
  }

  /* attach binds the common handlers and calls the page constructor. */
  attach (data: any) {
    const handlerID = this.main.dataset.handler
    if (!handlerID) {
      console.error('cannot attach to content with no specified handler')
      return
    }
    this.attachCommon(this.main)
    if (this.loadedPage) this.loadedPage.unload()
    const constructor = constructors[handlerID]
    if (constructor) this.loadedPage = new constructor(this.main, data)
    else this.loadedPage = null

    // Bind the tooltips.
    this.bindTooltips(this.main)
  }

  bindTooltips (ancestor: HTMLElement) {
    ancestor.querySelectorAll('[data-tooltip]').forEach((el: HTMLElement) => {
      bind(el, 'mouseenter', () => {
        this.tooltip.textContent = el.dataset.tooltip || ''
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
    this.headerSpace = Doc.idel(this.header, 'headerSpace')
    this.popupNotes = idel(document.body, 'popupNotes')
    this.popupTmpl = Doc.tmplElement(this.popupNotes, 'note')
    if (this.popupTmpl) this.popupTmpl.remove()
    else console.error('popupTmpl element not found')
    this.tooltip = idel(document.body, 'tooltip')
    const page = this.page = Doc.idDescendants(this.header)
    page.noteTmpl.removeAttribute('id')
    page.noteTmpl.remove()
    page.pokeTmpl.removeAttribute('id')
    page.pokeTmpl.remove()
    page.loader.remove()
    Doc.show(page.loader)

    bind(page.noteBell, 'click', async () => {
      Doc.hide(page.pokeList)
      Doc.show(page.noteList)
      this.ackNotes()
      page.noteCat.classList.add('active')
      page.pokeCat.classList.remove('active')
      this.showDropdown(page.noteBell, page.noteBox)
      Doc.hide(page.noteIndicator)
      for (const note of this.notes) {
        if (note.acked) {
          note.el.classList.remove('firstview')
        }
      }
      this.setNoteTimes(page.noteList)
      this.setNoteTimes(page.pokeList)
      this.storeNotes()
    })

    bind(page.burgerIcon, 'click', () => {
      Doc.hide(page.logoutErr)
      this.showDropdown(page.burgerIcon, page.profileBox)
    })

    bind(page.innerNoteIcon, 'click', () => { Doc.hide(page.noteBox) })
    bind(page.innerBurgerIcon, 'click', () => { Doc.hide(page.profileBox) })

    bind(page.profileSignout, 'click', async () => await this.signOut())

    bind(page.pokeCat, 'click', () => {
      this.setNoteTimes(page.pokeList)
      page.pokeCat.classList.add('active')
      page.noteCat.classList.remove('active')
      Doc.hide(page.noteList)
      Doc.show(page.pokeList)
      this.ackNotes()
    })

    bind(page.noteCat, 'click', () => {
      this.setNoteTimes(page.noteList)
      page.noteCat.classList.add('active')
      page.pokeCat.classList.remove('active')
      Doc.hide(page.pokeList)
      Doc.show(page.noteList)
      this.ackNotes()
    })
  }

  /*
   * showDropdown sets the position and visibility of the specified dropdown
   * dialog according to the position of its icon button.
   */
  showDropdown (icon: HTMLElement, dialog: HTMLElement) {
    const ico = icon.getBoundingClientRect()
    Doc.hide(this.page.noteBox, this.page.profileBox)
    Doc.show(dialog)
    dialog.style.right = `${window.innerWidth - ico.left - ico.width + 5}px`
    dialog.style.top = `${ico.top - 4}px`

    const hide = (e: MouseEvent) => {
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

  setNoteTimes (noteList: HTMLElement) {
    for (const el of (Array.from(noteList.children) as NoteElement[])) {
      Doc.safeSelector(el, 'span.note-time').textContent = Doc.timeSince(el.note.stamp)
    }
  }

  /*
   * bindInternalNavigation hijacks navigation by click on any local links that
   * are descendants of ancestor.
   */
  bindInternalNavigation (ancestor: HTMLElement) {
    const pageURL = new URL(window.location.href)
    ancestor.querySelectorAll('a').forEach(a => {
      if (!a.href) return
      const url = new URL(a.href)
      if (url.origin === pageURL.origin) {
        const token = url.pathname.substring(1)
        const params: Record<string, string> = {}
        if (url.search) {
          url.searchParams.forEach((v, k) => {
            params[k] = v
          })
        }
        Doc.bind(a, 'click', (e: Event) => {
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
    State.storeLocal(State.notificationsLK, this.notes.map(n => {
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
    const { page, user } = this
    if (!page) {
      // initial page load, header elements not yet attached but menu items
      // would already be hidden/displayed as appropriate.
      return
    }
    const authed = user && user.authed
    if (!authed) {
      page.profileBox.classList.remove('authed')
      Doc.hide(page.noteBell, page.walletsMenuEntry, page.marketsMenuEntry)
      return
    }
    page.profileBox.classList.add('authed')
    Doc.show(page.noteBell, page.walletsMenuEntry)
    if (Object.keys(user.exchanges).length > 0) {
      Doc.show(page.marketsMenuEntry)
    } else {
      Doc.hide(page.marketsMenuEntry)
    }
  }

  /* attachCommon scans the provided node and handles some common bindings. */
  attachCommon (node: HTMLElement) {
    this.bindInternalNavigation(node)
  }

  /*
   * updateBondConfs updates the information for a pending bond.
   */
  updateBondConfs (dexAddr: string, coinID: string, confs: number, assetID: number) {
    const dex = this.exchanges[dexAddr]
    const symbol = this.assets[assetID].symbol
    dex.pendingBonds[coinID] = { confs, assetID, symbol }
  }

  updateTier (host: string, tier: number) {
    this.exchanges[host].tier = tier
  }

  /*
   * handleBondNote is the handler for the 'bondpost'-type notification, which
   * is used to update the dex tier and registration status.
   */
  handleBondNote (note: BondNote) {
    switch (note.topic) {
      case 'RegUpdate':
        if (note.coinID !== null) { // should never be null for RegUpdate
          this.updateBondConfs(note.dex, note.coinID, note.confirmations, note.asset)
        }
        break
      case 'BondConfirmed':
        if (note.tier !== null) { // should never be null for BondConfirmed
          this.updateTier(note.dex, note.tier)
        }
        break
      default:
        break
    }
  }

  /*
   * setNotes sets the current notification cache and populates the notification
   * display.
   */
  setNotes (notes: CoreNote[]) {
    this.log('notes', 'setNotes', notes)
    this.notes = []
    Doc.empty(this.page.noteList)
    for (let i = 0; i < notes.length; i++) {
      this.prependNoteElement(notes[i], true)
    }
    this.storeNotes()
  }

  updateUser (note: CoreNote) {
    const { user, assets, walletMap } = this
    if (note.type === 'fiatrateupdate') {
      this.fiatRatesMap = (note as RateNote).fiatRates
      return
    }
    // Some notes can be received before we get a User during login.
    if (!user) return
    switch (note.type) {
      case 'order': {
        const orderNote = note as OrderNote
        const order = orderNote.order
        const mkt = user.exchanges[order.host].markets[order.market]
        const tempID = orderNote.tempID

        // Ensure market's inflight orders list is updated.
        if (note.topic === 'AsyncOrderSubmitted') {
          const inFlight = order as InFlightOrder
          inFlight.tempID = tempID
          if (!mkt.inflight) mkt.inflight = [inFlight]
          else mkt.inflight.push(inFlight)
          break
        } else if (note.topic === 'AsyncOrderFailure') {
          mkt.inflight = mkt.inflight.filter(ord => ord.tempID !== tempID)
          break
        } else {
          for (const i in mkt.inflight || []) {
            if (!(mkt.inflight[i].tempID === tempID)) continue
            mkt.inflight = mkt.inflight.filter(ord => ord.tempID !== tempID)
            break
          }
        }

        // Updates given order in market's orders list if it finds it.
        // Returns a bool which indicates if order was found.
        const updateOrder = (mkt: Market, ord: Order) => {
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
        const n: BalanceNote = note as BalanceNote
        const asset = user.assets[n.assetID]
        // Balance updates can come before the user is fetched after login.
        if (!asset) break
        const w = asset.wallet
        if (w) w.balance = n.balance
        break
      }
      case 'bondpost':
        this.handleBondNote(note as BondNote)
        break
      case 'walletstate':
      case 'walletconfig': {
        // assets can be null if failed to connect to dex server.
        if (!assets) return
        const wallet = (note as WalletConfigNote).wallet
        const asset = assets[wallet.assetID]
        asset.wallet = wallet
        walletMap[wallet.assetID] = wallet
        break
      }
      case 'match': {
        const n = note as MatchNote
        const ord = this.order(n.orderID)
        if (ord) updateMatch(ord, n.match)
        break
      }
      case 'conn': {
        const n = note as ConnEventNote
        const xc = user.exchanges[n.host]
        if (xc) xc.connectionStatus = n.connectionStatus
        break
      }
      case 'spots': {
        const n = note as SpotPriceNote
        const xc = user.exchanges[n.host]
        // Spots can come before the user is fetched after login and before/while the
        // markets page reload when it receives a dex conn note.
        if (!xc || !xc.markets) break
        for (const [mktName, spot] of Object.entries(n.spots)) xc.markets[mktName].spot = spot
        break
      }
      case 'fiatrateupdate': {
        this.fiatRatesMap = (note as RateNote).fiatRates
        break
      }
      case 'bot': {
        const n = note as BotNote
        const [r, bots] = [n.report, this.user.bots]
        const idx = bots.findIndex((report: BotReport) => report.programID === r.programID)
        switch (n.topic) {
          case 'BotRetired':
            if (idx >= 0) bots.splice(idx, 1)
            break
          default:
            if (idx >= 0) bots[idx] = n.report
            else bots.push(n.report)
        }
      }
    }
  }

  /*
   * notify is the top-level handler for notifications received from the client.
   * Notifications are propagated to the loadedPage.
   */
  notify (note: CoreNote) {
    // Handle type-specific updates.
    this.log('notes', 'notify', note)
    this.updateUser(note)
    // Inform the page.
    for (const feeder of this.noteReceivers) {
      const f = feeder[note.type]
      if (!f) continue
      try {
        f(note)
      } catch (error) {
        console.error('note feeder error:', error.message ? error.message : error)
        console.log(note)
        console.log(error.stack)
      }
    }
    // Discard data notifications.
    if (note.severity < ntfn.POKE) return
    // Poke notifications have their own display.
    const { popupTmpl, popupNotes, showPopups } = this
    if (showPopups) {
      const span = popupTmpl.cloneNode(true) as HTMLElement
      Doc.tmplElement(span, 'text').textContent = `${note.subject}: ${note.details}`
      const indicator = Doc.tmplElement(span, 'indicator')
      if (note.severity === ntfn.POKE) {
        Doc.hide(indicator)
      } else setSeverityClass(indicator, note.severity)
      popupNotes.appendChild(span)
      // These take up screen space. Only show max 5 at a time.
      while (popupNotes.children.length > 5) popupNotes.removeChild(popupNotes.firstChild as Node)
      setTimeout(async () => {
        await Doc.animate(500, (progress: number) => {
          span.style.opacity = String(1 - progress)
        })
        span.remove()
      }, 6000)
    }
    // Success and higher severity go to the bell dropdown.
    if (note.severity === ntfn.POKE) this.prependPokeElement(note)
    else this.prependNoteElement(note)
  }

  /*
   * registerNoteFeeder registers a feeder for core notifications. The feeder
   * will be de-registered when a new page is loaded.
   */
  registerNoteFeeder (receivers: Record<string, (n: CoreNote) => void>) {
    this.noteReceivers.push(receivers)
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
  log (loggerID: string, ...msg: any) {
    if (this.loggers[loggerID]) console.log(`${nowString()}[${loggerID}]:`, ...msg)
    if (this.recorders[loggerID]) {
      this.recorders[loggerID].push({
        time: nowString(),
        msg: msg
      })
    }
  }

  prependPokeElement (cn: CoreNote) {
    const [el, note] = this.makePoke(cn)
    this.pokes.push(note)
    while (this.pokes.length > noteCacheSize) this.pokes.shift()
    this.prependListElement(this.page.pokeList, note, el)
  }

  prependNoteElement (cn: CoreNote, skipSave?: boolean) {
    const [el, note] = this.makeNote(cn)
    this.notes.push(note)
    while (this.notes.length > noteCacheSize) this.notes.shift()
    const noteList = this.page.noteList
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
      ni.textContent = String((unacked > noteCacheSize - 1) ? `${noteCacheSize - 1}+` : unacked)
      Doc.show(ni)
    } else Doc.hide(ni)
  }

  prependListElement (noteList: HTMLElement, note: CoreNotePlus, el: NoteElement) {
    el.note = note
    noteList.prepend(el)
    while (noteList.children.length > noteCacheSize) noteList.removeChild(noteList.lastChild as Node)
    this.setNoteTimes(noteList)
  }

  /*
   * makeNote constructs a single notification element for the drop-down
   * notification list.
   */
  makeNote (note: CoreNote): [NoteElement, CoreNotePlus] {
    const el = this.page.noteTmpl.cloneNode(true) as NoteElement
    if (note.severity > ntfn.POKE) {
      const cls = note.severity === ntfn.SUCCESS ? 'good' : note.severity === ntfn.WARNING ? 'warn' : 'bad'
      Doc.safeSelector(el, 'div.note-indicator').classList.add(cls)
    }

    Doc.safeSelector(el, 'div.note-subject').textContent = note.subject
    Doc.safeSelector(el, 'div.note-details').textContent = note.details
    const np: CoreNotePlus = { el, ...note }
    return [el, np]
  }

  makePoke (note: CoreNote): [NoteElement, CoreNotePlus] {
    const el = this.page.pokeTmpl.cloneNode(true) as NoteElement
    const d = new Date(note.stamp)
    Doc.tmplElement(el, 'dateTime').textContent = `${d.toLocaleDateString()}, ${d.toLocaleTimeString()}`
    Doc.tmplElement(el, 'details').textContent = `${note.subject}: ${note.details}`
    const np: CoreNotePlus = { el, ...note }
    return [el, np]
  }

  /*
   * loading appends the loader to the specified element and displays the
   * loading icon. The loader will block all interaction with the specified
   * element until Application.loaded is called.
   */
  loading (el: HTMLElement): () => void {
    const loader = this.page.loader.cloneNode(true) as HTMLElement
    el.appendChild(loader)
    return () => { loader.remove() }
  }

  /* orders retrieves a list of orders for the specified dex and market
   * including inflight orders.
   */
  orders (host: string, mktID: string): Order[] {
    let orders: Order[] = []
    const mkt = this.user.exchanges[host].markets[mktID]
    if (mkt.orders) orders = orders.concat(mkt.orders)
    if (mkt.inflight) orders = orders.concat(mkt.inflight)
    return orders
  }

  /*
   * haveActiveOrders returns whether or not there are active orders involving a
   * certain asset.
   */
  haveActiveOrders (assetID: number): boolean {
    for (const xc of Object.values(this.user.exchanges)) {
      if (!xc.markets) continue
      for (const market of Object.values(xc.markets)) {
        if (!market.orders) continue
        for (const ord of market.orders) {
          if ((ord.baseID === assetID || ord.quoteID === assetID) &&
            (ord.status < StatusExecuted || hasActiveMatches(ord))) return true
        }
      }
    }
    return false
  }

  /* order attempts to locate an order by order ID. */
  order (oid: string): Order | null {
    for (const xc of Object.values(this.user.exchanges)) {
      if (!xc || !xc.markets) continue
      for (const market of Object.values(xc.markets)) {
        if (!market.orders) continue
        for (const ord of market.orders) {
          if (ord.id === oid) return ord
        }
      }
    }
    return null
  }

  /*
   * canAccelerateOrder returns true if the "from" wallet of the order
   * supports acceleration, and if the order has unconfirmed swap
   * transactions.
   */
  canAccelerateOrder (order: Order): boolean {
    const walletTraitAccelerator = 1 << 4
    let fromAssetID
    if (order.sell) fromAssetID = order.baseID
    else fromAssetID = order.quoteID
    const wallet = this.walletMap[fromAssetID]
    if (!wallet || !(wallet.traits & walletTraitAccelerator)) return false
    if (order.matches) {
      for (let i = 0; i < order.matches.length; i++) {
        const match = order.matches[i]
        if (match.swap && match.swap.confs && match.swap.confs.count === 0 && !match.revoked) {
          return true
        }
      }
    }
    return false
  }

  /*
   * unitInfo fetches unit info [dex.UnitInfo] for the asset. If xc
   * [core.Exchange] is provided, and this is not a SupportedAsset, the UnitInfo
   * sent from the exchange's assets map [dex.Asset] will be used.
   */
  unitInfo (assetID: number, xc?: Exchange): UnitInfo {
    const supportedAsset = this.assets[assetID]
    if (supportedAsset) return supportedAsset.unitInfo
    if (!xc || !xc.assets) {
      throw Error(intl.prep(intl.ID_UNSUPPORTED_ASSET_INFO_ERR_MSG, { assetID: `${assetID}` }))
    }
    return xc.assets[assetID].unitInfo
  }

  /* conventionalRate converts the encoded atomic rate to a conventional rate */
  conventionalRate (baseID: number, quoteID: number, encRate: number, xc?: Exchange): number {
    const [b, q] = [this.unitInfo(baseID, xc), this.unitInfo(quoteID, xc)]

    const r = b.conventional.conversionFactor / q.conventional.conversionFactor
    return encRate * r / RateEncodingFactor
  }

  walletDefinition (assetID: number, walletType: string): WalletDefinition {
    const asset = this.assets[assetID]
    if (asset.token) return asset.token.definition
    if (!asset.info) throw Error('where\'s the wallet info?')
    if (walletType === '') return asset.info.availablewallets[asset.info.emptyidx]
    return asset.info.availablewallets.filter(def => def.type === walletType)[0]
  }

  currentWalletDefinition (assetID: number): WalletDefinition {
    const asset = this.assets[assetID]
    if (asset.token) {
      return asset.token.definition
    }
    return this.walletDefinition(assetID, this.assets[assetID].wallet.type)
  }

  /*
   * fetchBalance requests a balance update from the API. The API response does
   * include the balance, but we're ignoring it, since a balance update
   * notification is received via the Application anyways.
   */
  async fetchBalance (assetID: number): Promise<WalletBalance> {
    const res: BalanceResponse = await postJSON('/api/balance', { assetID: assetID })
    if (!this.checkResponse(res)) {
      throw new Error(`failed to fetch balance for asset ID ${assetID}`)
    }
    return res.balance
  }

  /*
   * checkResponse checks the response object as returned from the functions in
   * the http module. If the response indicates that the request failed, it
   * returns false, otherwise, true.
   */
  checkResponse (resp: APIResponse): boolean {
    return (resp.requestSuccessful && resp.ok)
  }

  /**
   * signOut call to /api/logout, if response with no errors occurred remove auth
   * and other privacy-critical cookies/locals and reload the page, otherwise
   * show a notification.
   */
  async signOut () {
    const res = await postJSON('/api/logout')
    if (!this.checkResponse(res)) {
      if (res.code === Errors.activeOrdersErr) {
        this.page.logoutErr.textContent = intl.prep(intl.ID_ACTIVE_ORDERS_LOGOUT_ERR_MSG)
      } else {
        this.page.logoutErr.textContent = res.msg
      }
      Doc.show(this.page.logoutErr)
      return
    }
    State.removeCookie(State.authCK)
    State.removeCookie(State.pwKeyCK)
    State.removeLocal(State.notificationsLK)
    window.location.href = '/login'
  }
}

/* getSocketURI returns the websocket URI for the client. */
function getSocketURI (): string {
  const protocol = (window.location.protocol === 'https:') ? 'wss' : 'ws'
  return `${protocol}://${window.location.host}/ws`
}

/*
 * severityClassMap maps a notification severity level to a CSS class that
 * assigns a background color.
 */
const severityClassMap: Record<number, string> = {
  [ntfn.SUCCESS]: 'good',
  [ntfn.ERROR]: 'bad',
  [ntfn.WARNING]: 'warn'
}

/* handlerFromPath parses the handler name from the path. */
function handlerFromPath (path: string): string {
  return path.replace(/^\//, '').split('/')[0].split('?')[0].split('#')[0]
}

/* nowString creates a string formatted like HH:MM:SS.xxx */
function nowString (): string {
  const stamp = new Date()
  const h = stamp.getHours().toString().padStart(2, '0')
  const m = stamp.getMinutes().toString().padStart(2, '0')
  const s = stamp.getSeconds().toString().padStart(2, '0')
  const ms = stamp.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}

function setSeverityClass (el: HTMLElement, severity: number) {
  el.classList.remove('bad', 'warn', 'good')
  el.classList.add(severityClassMap[severity])
}

/* updateMatch updates the match in or adds the match to the order. */
function updateMatch (order: Order, match: Match) {
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
