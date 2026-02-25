import { getJSON, postJSON } from "./http"
import {
  AppState,
  User,
  PageData,
  UnitInfo,
  TotalUSDBalance,
  FormData,
  CoreNote,
  LogMessage,
  OrderNote,
  InFlightOrder,
  Market,
  Order,
  BalanceNote,
  BondNote,
  MatchNote,
  ConnEventNote,
  SpotPriceNote,
  WalletNote,
  TransactionNote,
  ReputationNote,
  WalletConfigNote,
  WalletSyncNote,
  ActionRequiredNote,
  Match,
  RateNote,
  TipChangeNote,
  BookUpdate,
  MarketMakingStatus
} from "./registry"
import { TickerAsset, normalizedTicker } from "./assets"
import Doc from "./doc"
import State from "./state"
import ws from "./ws"
import { loadLocale } from "./intl"

export class Application {
  user: User | null
  commitHash: string
  langs: string[]
  lang: string
  mmStatus: MarketMakingStatus
  inited: boolean
  authed: boolean
  pageLoaded: (d: PageData) => void
  reRender: () => void
  pageData: PageData
  tickerList: TickerAsset[]
  tickerMap: Record<string, TickerAsset>
  balanceUpdaters: Record<string, (() => void)>
  fiatRateUpdaters: Record<string, (() => void)>
  setForm: (form: FormData) => void
  noteReceivers: Record<string, (n: CoreNote) => void>
  loggers: Record<string, boolean>
  recorders: Record<string, LogMessage[]>

  constructor () {
    this.user = null
    this.pageData = { pageParts: [''], pageRoot: '', data: {}, query: new URLSearchParams() }
    this.balanceUpdaters = {}
    this.fiatRateUpdaters = {}
    this.noteReceivers = {}
    this.setForm = () => { }

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
    // window.mmStatus = () => this.mmStatus

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

  async start (pageLoaded: (d: PageData) => void, reRender: () => void) {
    this.pageLoaded = pageLoaded
    this.reRender = reRender
    await this.fetchBuildInfo()
    console.log('Bison Wallet, Build', this.commitHash.substring(0, 8))

    ws.connect(getSocketURI(), () => window.location.reload())
    ws.registerRoute('notify', (note: CoreNote) => this.notify(note))
    // Dealing with some baggage from the old codebase.
    ws.registerRoute('book', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('book_order', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('unbook_order', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('update_remaining', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('epoch_order', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('epoch_order', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('candles', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('candle_update', (d: BookUpdate) => { this.notify(convertBookNote(d)) })
    ws.registerRoute('epoch_match_summary', (d: BookUpdate) => { this.notify(convertBookNote(d)) })

    // Handle back navigation from the browser.
    Doc.bind(window, 'popstate', (e: PopStateEvent) => {
      const page = e.state?.page
      if (!page && page !== '') return
      this.loadPage(page, e.state.data, true)
    })

    const page = pageFromURL()
    const lastState = window.history.state
    if (lastState?.page === page) { // probably a refresh
      this.loadPage(page, lastState.data, true)
    } else this.loadPage(page)
  }

  async loadPage (page: string, data?: any, skipPush?: boolean) {
    const url = new URL(`/${page}`, window.location.origin)
    if (!skipPush) {
      window.history.pushState({ page: page, data: data }, '', url.toString())
    }
    document.title = data?.title || 'Bison Wallet'
    this.storePageData({ pageParts: page.split('/'), pageRoot: pageRoot(page), data: data, query: url.searchParams })
  }

  storePageData (pd: PageData) {
    this.pageData = pd
    this.pageLoaded(pd)
  }

  async fetchBuildInfo () {
    const resp = await getJSON('/api/buildinfo')
    if (!resp.ok) return
    this.commitHash = resp.revision
  }

  async fetchAppState (): Promise<AppState> {
    const r = await getJSON('/api/user')
    const needsTickers = r.user && !this.user
    this.user = r.user
    this.inited = r.inited
    this.authed = Boolean(r.user)
    if (r.lang != this.lang) {
      await loadLocale(r.lang, this.commitHash, false)
    }
    this.lang = r.lang
    this.langs = r.langs
    this.mmStatus = r.mmStatus
    if (needsTickers) this.prepareTickerAssets()
    return r
  }

  async changeLocale (lang: string) {
    await postJSON('/api/setlocale', lang)
    await loadLocale(lang, this.commitHash, false)
    this.lang = lang
  }

  async fetchUser (): Promise<User | null> {
    const r = await this.fetchAppState()
    return r.user
  }

  registerNoteFeed (receivers: (n: CoreNote) => void): string {
    const k = randomKey()
    this.noteReceivers[k] = receivers
    return k
  }

  unregisterNoteFeed (k: string) {
    delete this.noteReceivers[k]
  }

  prepareTickerAssets () {
    const tickerList: TickerAsset[] = []
    const tickerMap: Record<string, TickerAsset> = {}
    for (const a of Object.values(this.user.assets)) {
      const normedTicker = normalizedTicker(a)
      let ta = tickerMap[normedTicker]
      if (ta) {
        ta.addNetworkAsset(a)
        continue
      }
      ta = new TickerAsset(a)
      tickerList.push(ta)
      tickerMap[normedTicker] = ta
    }
    this.tickerList = tickerList
    this.tickerMap = tickerMap
  }

  totalBalanceUSD (): TotalUSDBalance {
    if (!this.user) return { ok: false, total: 0, numContribs: 0 }
    if (Object.keys(this.user.tickerRates).length === 0) return { ok: false, total: 0, numContribs: 0 }
    let numContribs = 0
    const total = this.tickerList.reduce((acc: number, { ticker, total, xcRate, cFactor }: TickerAsset) => {
      if (!xcRate) {
        console.warn('no exchange rate info for', ticker)
        return acc
      }
      if (total === 0) return acc
      numContribs++
      return acc + (total / cFactor * acc)
    }, 0)
    return { ok: true, total, numContribs }
  }

  asset (assetID: number) {
    return this.user.assets[assetID]
  }

  unitInfo (assetID: number): UnitInfo {
    return this.user.assets[assetID].unitInfo
  }

  idRate (assetID: number) {
    return this.user.fiatRates[assetID]
  }

  wallet (assetID: number) {
    return this.user.assets[assetID].wallet
  }

  registerBalanceUpdater (updater: () => void): string {
    const key = randomKey()
    this.balanceUpdaters[key] = updater
    return key
  }

  unregisterBalanceUpdater (key: string) {
    delete this.balanceUpdaters[key]
  }

  registerFiatRateUpdater (updater: () => void): string {
    const key = randomKey()
    this.fiatRateUpdaters[key] = updater
    return key
  }

  unregisterFiatRateUpdater (key: string) {
    delete this.fiatRateUpdaters[key]
  }

  registerFormSetter (setForm: (form: FormData) => void) {
    this.setForm = setForm
  }

  unregisterFormSetter () {
    this.setForm = () => { }
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

  /*
 * notify is the top-level handler for notifications received from the client.
 * Notifications are propagated to the loadedPage.
 */
  notify (note: CoreNote) {
    // Handle type-specific updates.
    this.log('notes', 'notify', note)
    if (this.user) this.updateUser(note)
    // Inform the view.
    for (const f of Object.values(this.noteReceivers)) {
      try {
        f(note)
      } catch (error) {
        console.error('note feeder error:', error.message ? error.message : error)
        console.log(note)
        console.log(error.stack)
      }
    }
    // TODO: Popup notifications
  }

  updateUser (note: CoreNote) {
    const { user } = this
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
        user.assets[n.assetID].wallet.balance = n.balance
        Object.values(this.balanceUpdaters).forEach(u => u())
        break
      }
      case 'bondpost':
        this.handleBondNote(note as BondNote)
        break
      case 'reputation': {
        const n = note as ReputationNote
        user.exchanges[n.host].auth.rep = n.rep
        break
      }
      case 'walletstate':
      case 'walletconfig': {
        // assets can be null if failed to connect to dex server.
        const wallet = (note as WalletConfigNote)?.wallet
        if (!wallet) return
        const asset = user.assets[wallet.assetID]
        asset.wallet = wallet
        break
      }
      case 'walletsync': {
        const n = note as WalletSyncNote
        const w = this.wallet(n.assetID)
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
        this.user.fiatRates = (note as RateNote).fiatRates
        Object.values(this.fiatRateUpdaters).forEach(u => u())
        break
      }
      // TODO: implement required actions
      // case 'actionrequired': {
      //   const n = note as CoreActionRequiredNote
      //   this.addAction(n.payload)
      //   break
      // }
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
            // TODO: implement required actions
            // this.addAction(req)
            /// this.blinkAction()
            break
          }
          case 'actionResolved': {
            // TODO: implement
            // this.resolveAction(n.payload as ActionResolvedNote)
            break
          }
          case 'tipChange': {
            const note = n.payload as TipChangeNote
            const w = user.assets[note.assetID].wallet
            if (w) w.syncStatus.blocks = note.tip
          }
        }
        break
      }
      // TODO: implement market maker stuff
      // case 'runstats': {
      //     this.log('mm', { runstats: note })
      //     const n = note as RunStatsNote
      //     const bot = this.botStatus(n.host, n.baseID, n.quoteID)
      //     if (bot) {
      //       bot.runStats = n.stats
      //       bot.running = Boolean(n.stats)
      //       if (!n.stats) {
      //         bot.latestEpoch = undefined
      //         bot.cexProblems = undefined
      //       }
      //     }
      //     break
      //   }
      //   case 'cexnote': {
      //     const n = note as CEXNotification
      //     switch (n.topic) {
      //       case 'BalanceUpdate': {
      //         const u = n.note as CEXBalanceUpdate
      //         this.mmStatus.cexes[n.cexName].balances[u.assetID] = u.balance
      //       }
      //     }
      //     break
      //   }
      //   case 'epochreport': {
      //     const n = note as EpochReportNote
      //     const bot = this.botStatus(n.host, n.baseID, n.quoteID)
      //     if (bot) bot.latestEpoch = n.report
      //     break
      //   }
      //   case 'cexproblems': {
      //     const n = note as CEXProblemsNote
      //     const bot = this.botStatus(n.host, n.baseID, n.quoteID)
      //     if (bot) bot.cexProblems = n.problems
      //     break
      //   }
    }
  }

  /*
   * updateBondConfs updates the information for a pending bond.
   */
  updateBondConfs (dexAddr: string, coinID: string, confs: number) {
    const dex = this.user.exchanges[dexAddr]
    for (const bond of dex.auth.pendingBonds) if (bond.coinID === coinID) bond.confs = confs
  }

  updateTier (host: string, bondedTier: number) {
    this.user.exchanges[host].auth.rep.bondedTier = bondedTier
  }

  /*
   * handleBondNote is the handler for the 'bondpost'-type notification, which
   * is used to update the dex tier and registration status.
   */
  handleBondNote (note: BondNote) {
    if (note.auth) this.user.exchanges[note.dex].auth = note.auth
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

  handleTransactionNote (assetID: number, note: TransactionNote) {
    const w = this.wallet(assetID)
    if (!w) return
    const tx = note.transaction
    if (tx.confirmed) delete w.pendingTxs[tx.id]
    else w.pendingTxs[tx.id] = tx
  }
}

function randomKey (): string {
  return Math.random().toString(36).substring(2, 18)
}

function pageFromURL (): string {
  // Remove leading and trailing slashes
  return window.location.pathname.replace(/^\/|\/$/g, '')
}

function pageRoot (page: string): string {
  return page.split('/')[0]
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

/* getSocketURI returns the websocket URI for the client. */
function getSocketURI (): string {
  const protocol = (window.location.protocol === 'https:') ? 'wss' : 'ws'
  return `${protocol}://${window.location.host}/ws`
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

function convertBookNote (note: BookUpdate): CoreNote {
  return {
    type: 'bookupdate',
    topic: note.action,
    subject: '',
    details: '',
    severity: 0,
    stamp: 0,
    acked: false,
    id: '',
    data: {
      host: note.host,
      marketID: note.marketID,
      matchesSummary: note.matchesSummary,
      payload: note.payload
    }
  }
}

const app = new Application()

export default app