import Doc from './doc'
import State from './state'
import RegistrationPage from './register'
import LoginPage from './login'
import WalletsPage, { txTypeString } from './wallets'
import SettingsPage from './settings'
import MarketsPage from './markets'
import OrdersPage from './orders'
import OrderPage from './order'
import MarketMakerPage from './mm'
import MarketMakerSettingsPage from './mmsettings'
import DexSettingsPage from './dexsettings'
import MarketMakerArchivesPage from './mmarchives'
import MarketMakerLogsPage from './mmlogs'
import InitPage from './init'
import { MM } from './mmutil'
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
  ReputationNote,
  CoreNote,
  OrderNote,
  Market,
  Order,
  Match,
  BalanceNote,
  WalletConfigNote,
  WalletSyncNote,
  MatchNote,
  ConnEventNote,
  SpotPriceNote,
  UnitInfo,
  WalletDefinition,
  WalletBalance,
  LogMessage,
  NoteElement,
  BalanceResponse,
  APIResponse,
  RateNote,
  InFlightOrder,
  WalletTransaction,
  TxHistoryResult,
  WalletNote,
  TransactionNote,
  PageElement,
  ActionRequiredNote,
  ActionResolvedNote,
  TransactionActionNote,
  CoreActionRequiredNote,
  RejectedTxData,
  MarketMakingStatus,
  RunStatsNote,
  MMBotStatus,
  CEXNotification,
  CEXBalanceUpdate,
  EpochReportNote,
  CEXProblemsNote
} from './registry'
import { setCoinHref } from './coinexplorers'

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

interface UserResponse extends APIResponse {
  user?: User
  lang: string
  langs: string[]
  inited: boolean
  onionUrl: string
  mmStatus: MarketMakingStatus
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
  init: InitPage,
  mm: MarketMakerPage,
  mmsettings: MarketMakerSettingsPage,
  mmarchives: MarketMakerArchivesPage,
  mmlogs: MarketMakerLogsPage
}

interface LangData {
  name: string
  flag: string
}

const languageData: Record<string, LangData> = {
  'en-US': {
    name: 'English',
    flag: '🇺🇸' // Not 🇬🇧. MURICA!
  },
  'pt-BR': {
    name: 'Portugese',
    flag: '🇧🇷'
  },
  'zh-CN': {
    name: 'Chinese',
    flag: '🇨🇳'
  },
  'pl-PL': {
    name: 'Polish',
    flag: '🇵🇱'
  },
  'de-DE': {
    name: 'German',
    flag: '🇩🇪'
  },
  'ar': {
    name: 'Arabic',
    flag: '🇪🇬' // Egypt I guess
  }
}

interface requiredAction {
  div: PageElement
  stamp: number
  uniqueID: string
  actionID: string
  selected: boolean
}

// Application is the main javascript web application for Bison Wallet.
export default class Application {
  notes: CoreNotePlus[]
  pokes: CoreNotePlus[]
  langs: string[]
  lang: string
  mmStatus: MarketMakingStatus
  inited: boolean
  authed: boolean
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
  txHistoryMap: Record<number, TxHistoryResult>
  requiredActions: Record<string, requiredAction>
  onionUrl: string

  constructor () {
    this.notes = []
    this.pokes = []
    this.seedGenTime = 0
    this.commitHash = process.env.COMMITHASH || ''
    this.noteReceivers = []
    this.fiatRatesMap = {}
    this.showPopups = State.fetchLocal(State.popupsLK) === '1'
    this.txHistoryMap = {}
    this.requiredActions = {}

    console.log('Bison Wallet, Build', this.commitHash.substring(0, 7))

    // Set dark theme.
    document.body.classList.toggle('dark', State.isDark())

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
    window.mmStatus = () => this.mmStatus

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

    window.user = () => this.user
  }

  /**
   * Start the application. This is the only thing done from the index.js entry
   * point. Read the id = main element and attach handlers.
   */
  async start () {
    // Handle back navigation from the browser.
    bind(window, 'popstate', (e: PopStateEvent) => {
      const page = e.state?.page
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
    const ignoreCachedLocale = process.env.NODE_ENV === 'development'
    await intl.loadLocale(this.lang, this.commitHash, ignoreCachedLocale)
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
    this.attachActions()
    this.attachCommon(this.header)
    this.attach({})

    // If we are authed, populate notes, otherwise get we'll them from the login
    // response.
    if (this.authed) await this.fetchNotes()
    this.updateMenuItemsDisplay()
    // initialize desktop notifications
    ntfn.fetchDesktopNtfnSettings()
    // Connect the websocket and register the notification route.
    ws.connect(getSocketURI(), () => this.reconnected())
    ws.registerRoute(notificationRoute, (note: CoreNote) => {
      this.notify(note)
    })
  }

  /*
   * reconnected is called by the websocket client when a reconnection is made.
   */
  reconnected () {
    if (this.main?.dataset.handler === 'settings') window.location.assign('/')
    else window.location.reload() // This triggers another websocket disconnect/connect (!)
    // a fetchUser() and loadPage(window.history.state.page) might work
  }

  /*
   * Fetch and save the user, which is the primary core state that must be
   * maintained by the Application.
   */
  async fetchUser (): Promise<User | void> {
    const resp: UserResponse = await getJSON('/api/user')
    if (!this.checkResponse(resp)) return
    this.inited = resp.inited
    this.authed = Boolean(resp.user)
    this.onionUrl = resp.onionUrl
    this.lang = resp.lang
    this.langs = resp.langs
    this.mmStatus = resp.mmStatus
    if (!resp.user) return
    const user = resp.user
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

  async fetchMMStatus () {
    this.mmStatus = await MM.status()
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

    if (window.isWebview) {
      // Bind webview URL handlers
      this.bindUrlHandlers(this.main)
    }

    this.bindUnits(this.main)
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

  /*
   * bindUnits binds a hovering unit selection menu to the value or rate
   * display elements. The menu gives users an option to convert the value
   * to their preferred units.
   */
  bindUnits (main: PageElement) {
    const div = document.createElement('div') as PageElement
    div.classList.add('position-absolute', 'p-3')
    // div.style.backgroundColor = 'yellow'
    const rows = document.createElement('div') as PageElement
    div.appendChild(rows)
    rows.classList.add('body-bg', 'border')
    const addRow = (el: PageElement, unit: string, cFactor: number) => {
      const box = Doc.safeSelector(el, '[data-unit-box]')
      const atoms = parseInt(box.dataset.atoms as string)
      const row = document.createElement('div')
      row.textContent = unit
      rows.appendChild(row)
      row.classList.add('p-2', 'hoverbg', 'pointer')
      Doc.bind(row, 'click', () => {
        Doc.setText(el, '[data-value]', Doc.formatFourSigFigs(atoms / cFactor, Math.round(Math.log10(cFactor))))
        Doc.setText(el, '[data-unit]', unit)
      })
    }
    for (const el of Doc.applySelector(main, '[data-conversion-value]')) {
      const box = Doc.safeSelector(el, '[data-unit-box]')
      Doc.bind(box, 'mouseenter', () => {
        Doc.empty(rows)
        box.appendChild(div)
        const lyt = Doc.layoutMetrics(box)
        const assetID = parseInt(box.dataset.assetID as string)
        const { unitInfo: ui } = this.assets[assetID]
        addRow(el, ui.conventional.unit, ui.conventional.conversionFactor)
        for (const { unit, conversionFactor } of ui.denominations) addRow(el, unit, conversionFactor)
        addRow(el, ui.atomicUnit, 1)
        if (lyt.bodyTop > (div.offsetHeight + this.header.offsetHeight)) {
          div.style.bottom = 'calc(100% - 1rem)'
          div.style.top = 'auto'
        } else {
          div.style.top = 'calc(100% - 1rem)'
          div.style.bottom = 'auto'
        }
      })
      Doc.bind(box, 'mouseleave', () => div.remove())
    }
  }

  bindUrlHandlers (ancestor: HTMLElement) {
    if (!window.openUrl) return
    for (const link of Doc.applySelector(ancestor, 'a[target=_blank]')) {
      Doc.bind(link, 'click', (e: MouseEvent) => {
        e.preventDefault()
        window.openUrl(link.href ?? '')
      })
    }
  }

  /* attachHeader attaches the header element, which unlike the main element,
   * isn't replaced during page navigation.
   */
  attachHeader () {
    this.header = idel(document.body, 'header')
    const page = this.page = Doc.idDescendants(this.header)
    this.headerSpace = page.headerSpace
    this.popupNotes = idel(document.body, 'popupNotes')
    this.popupTmpl = Doc.tmplElement(this.popupNotes, 'note')
    if (this.popupTmpl) this.popupTmpl.remove()
    else console.error('popupTmpl element not found')
    this.tooltip = idel(document.body, 'tooltip')
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

    Doc.cleanTemplates(page.langBttnTmpl)
    const { name, flag } = languageData[this.lang]
    page.langFlag.textContent = flag
    page.langName.textContent = name

    for (const lang of this.langs) {
      if (lang === this.lang) continue
      const div = page.langBttnTmpl.cloneNode(true) as PageElement
      const { name, flag } = languageData[lang]
      div.textContent = flag
      div.title = name
      Doc.bind(div, 'click', () => this.setLanguage(lang))
      page.langBttns.appendChild(div)
    }
  }

  attachActions () {
    const { page } = this
    Object.assign(page, Doc.idDescendants(Doc.idel(document.body, 'requiredActions')))
    Doc.cleanTemplates(page.missingNoncesTmpl, page.actionTxTableTmpl, page.tooCheapTmpl, page.lostNonceTmpl)
    Doc.bind(page.actionsCollapse, 'click', () => {
      Doc.hide(page.actionDialog)
      Doc.show(page.actionDialogCollapsed)
    })
    Doc.bind(page.actionDialogCollapsed, 'click', () => {
      Doc.hide(page.actionDialogCollapsed)
      Doc.show(page.actionDialog)
      if (page.actionDialogContent.children.length === 0) this.showOldestAction()
    })
    const showAdjacentAction = (dir: number) => {
      const selected = Object.values(this.requiredActions).filter((r: requiredAction) => r.selected)[0]
      const actions = this.sortedActions()
      const idx = actions.indexOf(selected)
      this.showRequestedAction(actions[idx + dir].uniqueID)
    }
    Doc.bind(page.prevAction, 'click', () => showAdjacentAction(-1))
    Doc.bind(page.nextAction, 'click', () => showAdjacentAction(1))
  }

  setRequiredActions () {
    const { user: { actions }, requiredActions } = this
    if (!actions) return
    for (const a of actions) this.addAction(a)
    if (Object.keys(requiredActions).length) {
      this.showOldestAction()
      this.blinkAction()
    }
  }

  sortedActions () {
    const actions = Object.values(this.requiredActions)
    actions.sort((a: requiredAction, b: requiredAction) => a.stamp - b.stamp)
    return actions
  }

  showOldestAction () {
    this.showRequestedAction(this.sortedActions()[0].uniqueID)
  }

  addAction (req: ActionRequiredNote) {
    const { page, requiredActions } = this
    const existingAction = requiredActions[req.uniqueID]
    if (existingAction && existingAction.actionID === req.actionID) return
    const div = this.actionForm(req)
    if (existingAction) {
      if (existingAction.selected) existingAction.div.replaceWith(div)
      existingAction.div = div
    } else {
      requiredActions[req.uniqueID] = {
        div,
        stamp: (new Date()).getTime(),
        uniqueID: req.uniqueID,
        actionID: req.actionID,
        selected: false
      }
      const n = Object.keys(requiredActions).length
      page.actionDialogCount.textContent = String(n)
      page.actionCount.textContent = String(n)
      if (Doc.isHidden(page.actionDialog)) {
        this.showRequestedAction(req.uniqueID)
      }
    }
  }

  blinkAction () {
    Doc.blink(this.page.actionDialog)
    Doc.blink(this.page.actionDialogCollapsed)
  }

  resolveAction (req: ActionResolvedNote) {
    this.resolveActionWithID(req.uniqueID)
  }

  resolveActionWithID (uniqueID: string) {
    const { page, requiredActions } = this
    const existingAction = requiredActions[uniqueID]
    if (!existingAction) return
    delete requiredActions[uniqueID]
    const rem = Object.keys(requiredActions).length
    existingAction.div.remove()
    if (rem === 0) {
      Doc.hide(page.actionDialog, page.actionDialogCollapsed)
      return
    }
    page.actionDialogCount.textContent = String(rem)
    page.actionCount.textContent = String(rem)
    if (existingAction.selected) this.showOldestAction()
  }

  actionForm (req: ActionRequiredNote) {
    switch (req.actionID) {
      case 'tooCheap':
        return this.tooCheapAction(req)
      case 'missingNonces':
        return this.missingNoncesAction(req)
      case 'lostNonce':
        return this.lostNonceAction(req)
      case 'redeemRejected':
      case 'refundRejected':
        return this.txRejectedAction(req)
    }
    throw Error('unknown required action ID ' + req.actionID)
  }

  actionTxTable (req: ActionRequiredNote) {
    const { assetID, payload } = req
    const n = payload as TransactionActionNote
    const { unitInfo: ui, token } = this.assets[assetID]
    const table = this.page.actionTxTableTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(table)
    tmpl.lostTxID.textContent = n.tx.id
    tmpl.lostTxID.dataset.explorerCoin = n.tx.id
    setCoinHref(token ? token.parentID : assetID, tmpl.lostTxID)
    tmpl.txAmt.textContent = Doc.formatCoinValue(n.tx.amount, ui)
    tmpl.amtUnit.textContent = ui.conventional.unit
    const parentUI = token ? this.unitInfo(token.parentID) : ui
    tmpl.type.textContent = txTypeString(n.tx.type)
    tmpl.feeAmount.textContent = Doc.formatCoinValue(n.tx.fees, parentUI)
    tmpl.feeUnit.textContent = parentUI.conventional.unit
    switch (req.actionID) {
      case 'tooCheap': {
        Doc.show(tmpl.newFeesRow)
        tmpl.newFees.textContent = Doc.formatCoinValue(n.tx.fees, parentUI)
        tmpl.newFeesUnit.textContent = parentUI.conventional.unit
        break
      }
    }
    return table
  }

  async submitAction (req: ActionRequiredNote, action: any, errMsg: PageElement) {
    Doc.hide(errMsg)
    const loading = this.loading(this.page.actionDialog)
    const res = await postJSON('/api/takeaction', {
      assetID: req.assetID,
      actionID: req.actionID,
      action
    })
    loading()
    if (!this.checkResponse(res)) {
      errMsg.textContent = res.msg
      Doc.show(errMsg)
      return
    }
    this.resolveActionWithID(req.uniqueID)
  }

  missingNoncesAction (req: ActionRequiredNote) {
    const { assetID } = req
    const div = this.page.missingNoncesTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    const { name } = this.assets[assetID]
    tmpl.assetName.textContent = name
    Doc.bind(tmpl.doNothingBttn, 'click', () => {
      this.submitAction(req, { recover: false }, tmpl.errMsg)
    })
    Doc.bind(tmpl.recoverBttn, 'click', () => {
      this.submitAction(req, { recover: true }, tmpl.errMsg)
    })
    return div
  }

  tooCheapAction (req: ActionRequiredNote) {
    const { assetID, payload } = req
    const n = payload as TransactionActionNote
    const div = this.page.tooCheapTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    const { name } = this.assets[assetID]
    tmpl.assetName.textContent = name
    tmpl.txTable.appendChild(this.actionTxTable(req))
    const act = (bump: boolean) => {
      this.submitAction(req, {
        txID: n.tx.id,
        bump
      }, tmpl.errMsg)
    }
    Doc.bind(tmpl.keepWaitingBttn, 'click', () => act(false))
    Doc.bind(tmpl.addFeesBttn, 'click', () => act(true))
    return div
  }

  lostNonceAction (req: ActionRequiredNote) {
    const { assetID, payload } = req
    const n = payload as TransactionActionNote
    const div = this.page.lostNonceTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    const { name } = this.assets[assetID]
    tmpl.assetName.textContent = name
    tmpl.nonce.textContent = String(n.nonce)
    tmpl.txTable.appendChild(this.actionTxTable(req))
    Doc.bind(tmpl.abandonBttn, 'click', () => {
      this.submitAction(req, { txID: n.tx.id, abandon: true }, tmpl.errMsg)
    })
    Doc.bind(tmpl.keepWaitingBttn, 'click', () => {
      this.submitAction(req, { txID: n.tx.id, abandon: false }, tmpl.errMsg)
    })
    Doc.bind(tmpl.replaceBttn, 'click', () => {
      const replacementID = tmpl.idInput.value
      if (!replacementID) {
        tmpl.idInput.focus()
        Doc.blink(tmpl.idInput)
        return
      }
      this.submitAction(req, { txID: n.tx.id, abandon: false, replacementID }, tmpl.errMsg)
    })
    return div
  }

  txRejectedAction (req: ActionRequiredNote) {
    const { orderID, coinID, coinFmt, assetID, txType } = req.payload as RejectedTxData
    const div = this.page.rejectedTxTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    const { name, token } = this.assets[assetID]
    tmpl.assetName.textContent = name
    tmpl.txid.textContent = coinFmt
    tmpl.txid.dataset.explorerCoin = coinID
    tmpl.txType.textContent = txType
    setCoinHref(token ? token.parentID : assetID, tmpl.txid)
    Doc.bind(tmpl.doNothingBttn, 'click', () => {
      this.submitAction(req, { orderID, coinID, retry: false }, tmpl.errMsg)
    })
    Doc.bind(tmpl.tryAgainBttn, 'click', () => {
      this.submitAction(req, { orderID, coinID, retry: true }, tmpl.errMsg)
    })
    return div
  }

  showRequestedAction (uniqueID: string) {
    const { page, requiredActions } = this
    Doc.hide(page.actionDialogCollapsed)
    for (const r of Object.values(requiredActions)) r.selected = r.uniqueID === uniqueID
    Doc.empty(page.actionDialogContent)
    const action = requiredActions[uniqueID]
    page.actionDialogContent.appendChild(action.div)
    Doc.show(page.actionDialog)
    const actions = this.sortedActions()
    if (actions.length === 1) {
      Doc.hide(page.actionsNavigator)
      return
    }
    Doc.show(page.actionsNavigator)
    const idx = actions.indexOf(action)
    page.currentAction.textContent = String(idx + 1)
    page.prevAction.classList.toggle('invisible', idx === 0)
    page.nextAction.classList.toggle('invisible', idx === actions.length - 1)
  }

  /*
   * updateMarketElements sets the textContent for any ticker or asset name
   * elements or any asset logo src attributes for descendents of ancestor.
   */
  updateMarketElements (ancestor: PageElement, baseID: number, quoteID: number, xc?: Exchange) {
    const getAsset = (assetID: number) => {
      const a = this.assets[assetID]
      if (a) return a
      if (!xc) throw Error(`no asset found for asset ID ${assetID}`)
      const xcAsset = xc.assets[assetID]
      return { unitInfo: xcAsset.unitInfo, name: xcAsset.symbol, symbol: xcAsset.symbol }
    }
    const { unitInfo: bui, name: baseName, symbol: baseSymbol } = getAsset(baseID)
    for (const el of Doc.applySelector(ancestor, '[data-base-name')) el.textContent = baseName
    for (const img of Doc.applySelector(ancestor, '[data-base-logo]')) img.src = Doc.logoPath(baseSymbol)
    for (const el of Doc.applySelector(ancestor, '[data-base-ticker]')) el.textContent = bui.conventional.unit
    const { unitInfo: qui, name: quoteName, symbol: quoteSymbol } = getAsset(quoteID)
    for (const el of Doc.applySelector(ancestor, '[data-quote-name')) el.textContent = quoteName
    for (const img of Doc.applySelector(ancestor, '[data-quote-logo]')) img.src = Doc.logoPath(quoteSymbol)
    for (const el of Doc.applySelector(ancestor, '[data-quote-ticker]')) el.textContent = qui.conventional.unit
  }

  async setLanguage (lang: string) {
    await postJSON('/api/setlocale', lang)
    window.location.reload()
  }

  /*
   * showDropdown sets the position and visibility of the specified dropdown
   * dialog according to the position of its icon button.
   */
  showDropdown (icon: HTMLElement, dialog: HTMLElement) {
    Doc.hide(this.page.noteBox, this.page.profileBox)
    Doc.show(dialog)
    if (window.innerWidth < 500) Object.assign(dialog.style, { left: '0', right: '0', top: '0' })
    else {
      const ico = icon.getBoundingClientRect()
      const right = `${window.innerWidth - ico.left - ico.width + 5}px`
      Object.assign(dialog.style, { left: 'auto', right, top: `${ico.top - 4}px` })
    }

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
   * updateMenuItemsDisplay should be called when the user has signed in or out,
   * and when the user registers a DEX.
   */
  updateMenuItemsDisplay () {
    const { page, authed, mmStatus } = this
    if (!page) {
      // initial page load, header elements not yet attached but menu items
      // would already be hidden/displayed as appropriate.
      return
    }
    if (!authed) {
      page.profileBox.classList.remove('authed')
      Doc.hide(page.noteBell, page.walletsMenuEntry, page.marketsMenuEntry)
      return
    }
    Doc.setVis(Object.keys(this.exchanges).length > 0, page.marketsMenuEntry, page.mmLink)

    page.profileBox.classList.add('authed')
    Doc.show(page.noteBell, page.walletsMenuEntry, page.marketsMenuEntry)
    Doc.setVis(mmStatus, page.mmLink)
  }

  async fetchNotes () {
    const res = await getJSON('/api/notes')
    if (!this.checkResponse(res)) return console.error('failed to fetch notes:', res?.msg || String(res))
    res.notes.reverse()
    this.setNotes(res.notes)
    this.setPokes(res.pokes)
    this.setRequiredActions()
  }

  /* attachCommon scans the provided node and handles some common bindings. */
  attachCommon (node: HTMLElement) {
    this.bindInternalNavigation(node)
  }

  /*
   * updateBondConfs updates the information for a pending bond.
   */
  updateBondConfs (dexAddr: string, coinID: string, confs: number) {
    const dex = this.exchanges[dexAddr]
    for (const bond of dex.auth.pendingBonds) if (bond.coinID === coinID) bond.confs = confs
  }

  updateTier (host: string, bondedTier: number) {
    this.exchanges[host].auth.rep.bondedTier = bondedTier
  }

  /*
   * handleBondNote is the handler for the 'bondpost'-type notification, which
   * is used to update the dex tier and registration status.
   */
  handleBondNote (note: BondNote) {
    if (note.auth) this.exchanges[note.dex].auth = note.auth
    switch (note.topic) {
      case 'RegUpdate':
        if (note.coinID !== null) { // should never be null for RegUpdate
          this.updateBondConfs(note.dex, note.coinID, note.confirmations)
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
   * handleTransaction either adds a new transaction to the transaction history
   * or updates an existing transaction.
   */
  handleTransactionNote (assetID: number, note: TransactionNote) {
    const txHistory = this.txHistoryMap[assetID]
    if (!txHistory) return

    if (note.new) {
      txHistory.txs.unshift(note.transaction)
      return
    }

    for (let i = 0; i < txHistory.txs.length; i++) {
      if (txHistory.txs[i].id === note.transaction.id) {
        txHistory.txs[i] = note.transaction
        break
      }
    }
  }

  handleTxHistorySyncedNote (assetID: number) {
    delete this.txHistoryMap[assetID]
  }

  loggedIn (notes: CoreNote[], pokes: CoreNote[]) {
    this.setNotes(notes)
    this.setPokes(pokes)
    this.setRequiredActions()
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
      this.prependNoteElement(notes[i])
    }
  }

  /*
   * setPokes sets the current poke cache and populates the pokes display.
   */
  setPokes (pokes: CoreNote[]) {
    this.log('pokes', 'setPokes', pokes)
    this.pokes = []
    Doc.empty(this.page.pokeList)
    for (let i = 0; i < pokes.length; i++) {
      this.prependPokeElement(pokes[i])
    }
  }

  botStatus (host: string, baseID: number, quoteID: number): MMBotStatus | undefined {
    for (const bot of (this.mmStatus?.bots ?? [])) {
      const { config: c } = bot
      if (host === c.host && baseID === c.baseID && quoteID === c.quoteID) {
        return bot
      }
    }
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
        mkt.orders = mkt.orders || []
        const updateOrder = (mkt: Market, ord: Order) => {
          const i = mkt.orders.findIndex((o: Order) => o.id === ord.id)
          if (i === -1) return false
          if (note.topic === 'OrderRetired') mkt.orders.splice(i, 1)
          else mkt.orders[i] = ord
          return true
        }
        // If the notification order already exists we update it.
        // In case market's orders list is empty or the notification order isn't
        // part of it we add it manually as this means the order was
        // just placed.
        if (!updateOrder(mkt, order)) mkt.orders.push(order)
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
      case 'reputation': {
        const n = note as ReputationNote
        this.exchanges[n.host].auth.rep = n.rep
        break
      }
      case 'walletstate':
      case 'walletconfig': {
        // assets can be null if failed to connect to dex server.
        if (!assets) return
        const wallet = (note as WalletConfigNote)?.wallet
        if (!wallet) return
        const asset = assets[wallet.assetID]
        asset.wallet = wallet
        walletMap[wallet.assetID] = wallet
        break
      }
      case 'walletsync': {
        const n = note as WalletSyncNote
        const w = this.walletMap[n.assetID]
        if (w) {
          w.syncStatus = n.syncStatus
          w.synced = w.syncStatus.synced
          w.syncProgress = n.syncProgress
        }
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
      case 'actionrequired': {
        const n = note as CoreActionRequiredNote
        this.addAction(n.payload)
        break
      }
      case 'walletnote': {
        const n = note as WalletNote
        switch (n.payload.route) {
          case 'transaction': {
            const txNote = n.payload as TransactionNote
            this.handleTransactionNote(n.payload.assetID, txNote)
            break
          }
          case 'actionRequired': {
            const req = n.payload as ActionRequiredNote
            this.addAction(req)
            this.blinkAction()
            break
          }
          case 'actionResolved': {
            this.resolveAction(n.payload as ActionResolvedNote)
          }
        }
        if (n.payload.route === 'transactionHistorySynced') {
          this.handleTxHistorySyncedNote(n.payload.assetID)
        }
        break
      }
      case 'runstats': {
        this.log('mm', { runstats: note })
        const n = note as RunStatsNote
        const bot = this.botStatus(n.host, n.baseID, n.quoteID)
        if (bot) {
          bot.runStats = n.stats
          bot.running = Boolean(n.stats)
          if (!n.stats) {
            bot.latestEpoch = undefined
            bot.cexProblems = undefined
          }
        }
        break
      }
      case 'cexnote': {
        const n = note as CEXNotification
        switch (n.topic) {
          case 'BalanceUpdate': {
            const u = n.note as CEXBalanceUpdate
            this.mmStatus.cexes[n.cexName].balances[u.assetID] = u.balance
          }
        }
        break
      }
      case 'epochreport': {
        const n = note as EpochReportNote
        const bot = this.botStatus(n.host, n.baseID, n.quoteID)
        if (bot) bot.latestEpoch = n.report
        break
      }
      case 'cexproblems': {
        const n = note as CEXProblemsNote
        const bot = this.botStatus(n.host, n.baseID, n.quoteID)
        if (bot) bot.cexProblems = n.problems
        break
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
      Doc.tmplElement(span, 'text').textContent = `${note.subject}: ${ntfn.plainNote(note.details)}`
      const indicator = Doc.tmplElement(span, 'indicator')
      if (note.severity === ntfn.POKE) {
        Doc.hide(indicator)
      } else setSeverityClass(indicator, note.severity)
      popupNotes.appendChild(span)
      Doc.show(popupNotes)
      // These take up screen space. Only show max 5 at a time.
      while (popupNotes.children.length > 5) popupNotes.removeChild(popupNotes.firstChild as Node)
      setTimeout(async () => {
        await Doc.animate(500, (progress: number) => {
          span.style.opacity = String(1 - progress)
        })
        span.remove()
        if (popupNotes.children.length === 0) Doc.hide(popupNotes)
      }, 6000)
    }
    // Success and higher severity go to the bell dropdown.
    if (note.severity === ntfn.POKE) this.prependPokeElement(note)
    else this.prependNoteElement(note)

    // show desktop notification
    ntfn.desktopNotify(note)
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

  prependNoteElement (cn: CoreNote) {
    const [el, note] = this.makeNote(cn)
    this.notes.push(note)
    while (this.notes.length > noteCacheSize) this.notes.shift()
    const noteList = this.page.noteList
    this.prependListElement(noteList, note, el)
    this.bindUrlHandlers(el)
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
    ntfn.insertRichNote(Doc.safeSelector(el, 'div.note-details'), note.details)
    const np: CoreNotePlus = { el, ...note }
    return [el, np]
  }

  makePoke (note: CoreNote): [NoteElement, CoreNotePlus] {
    const el = this.page.pokeTmpl.cloneNode(true) as NoteElement
    Doc.tmplElement(el, 'subject').textContent = `${note.subject}:`
    ntfn.insertRichNote(Doc.tmplElement(el, 'details'), note.details)
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
      for (let i = 0; i < order.matches?.length; i++) {
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

  parentAsset (assetID: number) : SupportedAsset {
    const asset = this.assets[assetID]
    if (!asset.token) return asset
    return this.assets[asset.token.parentID]
  }

  /*
  * baseChainSymbol returns the symbol for the asset's parent if the asset is a
  * token, otherwise the symbol for the asset itself.
  */
  baseChainSymbol (assetID: number) {
    const asset = this.user.assets[assetID]
    return asset.token ? this.user.assets[asset.token.parentID].symbol : asset.symbol
  }

  /*
   * extensionWallet returns the ExtensionConfiguredWallet for the asset, if
   * it exists.
   */
  extensionWallet (assetID: number) {
    return this.user.extensionModeConfig?.restrictedWallets[this.baseChainSymbol(assetID)]
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
    State.removeLocal(State.notificationsLK) // Notification storage was DEPRECATED pre-v1.
    window.location.href = '/login'
  }

  /*
   * txHistory loads the tx history for an asset. If the results are not
   * already cached, they are cached. If we have reached the oldest tx,
   * this fact is also cached. If the exact amount of transactions as have been
   * made are requested, we will not know if we have reached the last tx until
   * a subsequent call.
  */
  async txHistory (assetID: number, n: number, after?: string): Promise<TxHistoryResult> {
    const url = '/api/txhistory'
    const cachedTxHistory = this.txHistoryMap[assetID]
    if (!cachedTxHistory) {
      const res = await postJSON(url, {
        n: n,
        assetID: assetID
      })
      if (!this.checkResponse(res)) {
        throw new Error(res.msg)
      }
      let txs : WalletTransaction[] | null | undefined = res.txs
      if (!txs) {
        txs = []
      }
      this.txHistoryMap[assetID] = {
        txs: txs,
        lastTx: txs.length < n
      }
      return this.txHistoryMap[assetID]
    }
    const txs : WalletTransaction[] = []
    let lastTx = false
    const startIndex = after ? cachedTxHistory.txs.findIndex(tx => tx.id === after) + 1 : 0
    if (after && startIndex === -1) {
      throw new Error('invalid after tx ' + after)
    }
    let lastIndex = startIndex
    for (let i = startIndex; i < cachedTxHistory.txs.length && txs.length < n; i++) {
      txs.push(cachedTxHistory.txs[i])
      lastIndex = i
      after = cachedTxHistory.txs[i].id
    }
    if (cachedTxHistory.lastTx && lastIndex === cachedTxHistory.txs.length - 1) {
      lastTx = true
    }
    if (txs.length < n && !cachedTxHistory.lastTx) {
      const res = await postJSON(url, {
        n: n - txs.length + 1, // + 1 because first result will be refID
        assetID: assetID,
        refID: after,
        past: true
      })
      if (!this.checkResponse(res)) {
        throw new Error(res.msg)
      }
      let resTxs : WalletTransaction[] | null | undefined = res.txs
      if (!resTxs) {
        resTxs = []
      }
      if (resTxs.length > 0 && after) {
        if (resTxs[0].id === after) {
          resTxs.shift()
        } else {
          // Implies a bug in the client
          console.error('First tx history element != refID')
        }
      }
      cachedTxHistory.lastTx = resTxs.length < n - txs.length
      lastTx = cachedTxHistory.lastTx
      txs.push(...resTxs)
      cachedTxHistory.txs.push(...resTxs)
    }
    return { txs, lastTx }
  }

  getWalletTx (assetID: number, txID: string): WalletTransaction | undefined {
    const cachedTxHistory = this.txHistoryMap[assetID]
    if (!cachedTxHistory) return undefined
    return cachedTxHistory.txs.find(tx => tx.id === txID)
  }

  clearTxHistory (assetID: number) {
    delete this.txHistoryMap[assetID]
  }

  async needsCustomProvider (assetID: number): Promise<boolean> {
    const baseChainID = this.assets[assetID]?.token?.parentID ?? assetID
    if (!baseChainID) return false
    const w = this.walletMap[baseChainID]
    if (!w) return false
    const traitAccountLocker = 1 << 14
    if ((w.traits & traitAccountLocker) === 0) return false
    const res = await postJSON('/api/walletsettings', { assetID: baseChainID })
    if (!this.checkResponse(res)) {
      console.error(res.msg)
      return false
    }
    const settings = res.map as Record<string, string>
    return !settings.providers
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
