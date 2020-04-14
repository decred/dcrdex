import Doc, { StateIcons } from './doc'
import BasePage from './basepage'
import State from './state'
import RegistrationPage from './register'
import LoginPage from './login'
import WalletsPage from './wallets'
import SettingsPage from './settings'
import { DepthChart } from './charts'
import { postJSON, getJSON } from './http'
import * as forms from './forms'
import * as ntfn from './notifications'
import ws from './ws'

const idel = Doc.idel // = element by id
const bind = Doc.bind
const unbind = Doc.unbind
var app

const LIMIT = 1
// const MARKET = 2
// const CANCEL = 3

const updateWalletRoute = 'update_wallet'
const notificationRoute = 'notify'

// window.logit = function (lvl, msg) { app.notify(lvl, msg) }

// Application is the main javascript web application for the Decred DEX client.
export default class Application {
  constructor () {
    this.notes = []
    this.user = {
      accounts: {},
      wallets: {}
    }
  }

  /**
   * Start the application. This is the only thing done from the index.js entry
   * point. Read the id = main element and attach handlers.
   */
  async start () {
    app = this
    // The "user" is a large data structure that contains nearly all state
    // information, including exchanges, markets, wallets, and orders. It must
    // be loaded immediately.
    await this.fetchUser()
    // Handle back navigation from the browser.
    bind(window, 'popstate', (e) => {
      const page = e.state.page
      if (!page && page !== '') return
      this.loadPage(page)
    })
    // The main element is the interchangeable part of the page that doesn't
    // include the header. Main should define a data-handler attribute
    // associated with  one of the available constructors.
    this.main = idel(document, 'main')
    const handler = this.main.dataset.handler
    // The application is free to respond with a page that differs from the
    // one requested in the omnibox, e.g. routing though a login page. Set the
    // current URL state based on the actual page.
    window.history.replaceState({ page: handler }, '', `/${handler}`)
    // Attach stuff.
    this.attachHeader()
    this.attachCommon(this.header)
    this.attach()
    // Load recent notifications from Window.localStorage.
    const notes = State.fetch('notifications')
    this.setNotes(notes || [])
    // Connect the websocket and register for a couple of routes.
    ws.connect(getSocketURI(), this.reconnected)
    ws.registerRoute(updateWalletRoute, wallet => {
      this.assets[wallet.assetID].wallet = wallet
      this.walletMap[wallet.assetID] = wallet
      const balances = this.main.querySelectorAll(`[data-balance-target="${wallet.assetID}"]`)
      balances.forEach(el => { el.textContent = (wallet.balance / 1e8).toFixed(8) })
    })
    ws.registerRoute(notificationRoute, note => {
      this.notify(note)
    })
  }

  /*
   * reconnected is called by the websocket client when a reconnection is made.
   */
  reconnected () {
    window.location.reload()
  }

  /*
   * Fetch and save the user, which is the primary core state that must be
   * maintained by the Application.
   */
  async fetchUser () {
    const user = await getJSON('/api/user')
    if (!app.checkResponse(user)) return
    this.user = user
    this.assets = user.assets
    this.walletMap = {}
    for (const [assetID, asset] of Object.entries(user.assets)) {
      if (asset.wallet) {
        this.walletMap[assetID] = asset.wallet
      }
    }
    return user
  }

  /* Load the page from the server. Insert and bind the DOM. */
  async loadPage (page, data) {
    const response = await window.fetch(`/${page}`)
    if (!response.ok) return false
    const html = await response.text()
    const doc = Doc.noderize(html)
    const main = idel(doc, 'main')
    const delivered = main.dataset.handler
    window.history.pushState({ page: delivered }, delivered, `/${delivered}`)
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
    unattachers.forEach(f => f())
    unattachers = []
    this.attachCommon(this.main)
    var constructor = constructors[handlerID]
    if (!constructor) {
      console.error(`no constructor for ${handlerID}`)
    }
    if (this.loadedPage) this.loadedPage.unload()
    this.loadedPage = new constructor(this, this.main, data) || {}
  }

  /* attachHeader attaches the header element, which unlike the main element,
   * isn't replaced during page navigation.
   */
  attachHeader () {
    this.header = idel(document.body, 'header')
    this.pokeNote = idel(document.body, 'pokeNote')
    const pg = this.page = Doc.parsePage(this.header, [
      'noteIndicator', 'noteBox', 'noteList', 'noteTemplate',
      'walletsMenuEntry', 'noteMenuEntry', 'settingsIcon', 'loginLink', 'loader'
    ])
    pg.noteIndicator.style.display = 'none'
    delete pg.noteTemplate.id
    pg.noteTemplate.remove()
    pg.loader.remove()
    pg.loader.style.backgroundColor = 'rgba(0, 0, 0, 0.5)'
    Doc.show(pg.loader)
    var hide
    hide = e => {
      if (!Doc.mouseInElement(e, pg.noteBox)) {
        pg.noteBox.style.display = 'none'
        unbind(document, hide)
      }
    }
    bind(pg.noteMenuEntry, 'click', async e => {
      bind(document, 'click', hide)
      pg.noteBox.style.display = 'block'
      pg.noteIndicator.style.display = 'none'
      const acks = []
      for (const note of this.notes) {
        if (!note.acked) {
          note.acked = true
          if (note.id && note.severity > ntfn.POKE) acks.push(note.id)
        }
        note.el.querySelector('div.note-time').textContent = timeSince(note.stamp)
      }
      this.storeNotes()
      if (acks.length) ws.request('acknotes', acks)
    })
    if (this.notes.length === 0) {
      pg.noteList.textContent = 'no notifications'
    }
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
   * setLogged should be called when the user has signed in or out. For logging
   * out, it may be better to trigger a hard reload.
   */
  setLogged (logged) {
    const pg = this.page
    if (logged) {
      Doc.show(pg.noteMenuEntry, pg.settingsIcon, pg.walletsMenuEntry)
      Doc.hide(pg.loginLink)
      return
    }
    Doc.hide(pg.noteMenuEntry, pg.settingsIcon)
    Doc.show(pg.loginLink)
  }

  /* attachCommon scans the provided node and handles some common bindings. */
  attachCommon (node) {
    node.querySelectorAll('[data-pagelink]').forEach(link => {
      const page = link.dataset.pagelink
      bind(link, 'click', async () => {
        await this.loadPage(page)
      })
    })
  }

  /*
   * setNotes sets the current notification cache and populates the notification
   * display.
   */
  setNotes (notes) {
    this.notes = notes
    this.setNoteElements()
  }

  /*
   * notify is the top-level handler for notifications received from the client.
   * Notifications are propagated to the loadedPage.
   */
  notify (note) {
    // Handle type-specific updates.
    switch (note.type) {
      case 'order': {
        const order = note.order
        const mkt = this.user.exchanges[order.dex].markets[order.market]
        if (mkt.orders) {
          for (const i in mkt.orders) {
            if (mkt.orders[i].id === order.id) {
              mkt.orders[i] = order
              break
            }
          }
        }
      }
    }
    // Inform the page.
    this.loadedPage.notify(note)
    // Discard data notifications.
    if (note.severity < ntfn.POKE) return
    // Poke notifications have their own display.
    if (note.severity === ntfn.POKE) {
      this.pokeNote.firstChild.textContent = `${note.subject}: ${note.details}`
      this.pokeNote.classList.add('active')
      if (this.pokeNote.timer) {
        clearTimeout(this.pokeNote.timer)
      }
      this.pokeNote.timer = setTimeout(() => {
        this.pokeNote.classList.remove('active')
        delete this.pokeNote.timer
      }, 5000)
      return
    }
    // Success and higher severity go to the bell dropdown.
    this.notes.push(note)
    this.setNoteElements()
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

  /* orders retreives a list of orders for the specified dex and market. */
  orders (dex, bid, qid) {
    var o = this.user.exchanges[dex].markets[sid(bid, qid)].orders
    if (!o) {
      o = []
      this.user.exchanges[dex].markets[sid(bid, qid)].orders = o
    }
    return o
  }

  /*
   * checkResponse checks the response object as returned from the functions in
   * the http module. If the response indicates that the request failed, a
   * message will be displayed in the drop-down notifications and false will be
   * returned.
   */
  checkResponse (resp) {
    if (!resp.requestSuccessful || !resp.ok) {
      if (this.user.inited) this.notify(ntfn.make('API error', resp.msg, ntfn.ERROR))
      return false
    }
    return true
  }
}

/* getSocketURI returns the websocket URI for the client. */
function getSocketURI () {
  var protocol = (window.location.protocol === 'https:') ? 'wss' : 'ws'
  return `${protocol}://${window.location.host}/ws`
}

/* unattachers are handlers to be run when a page is unloaded. */
var unattachers = []

/* unattach adds an unattacher to the array. */
function unattach (f) {
  unattachers.push(f)
}

class MarketsPage extends BasePage {
  constructor (application, main, data) {
    super()
    this.notifiers = handleMarkets(main, data)
  }
}

// handleMarkets is the 'markets' page main element handler.
function handleMarkets (main, data) {
  var market
  const page = Doc.parsePage(main, [
    // Templates, loaders, chart div...
    'marketLoader', 'marketChart', 'marketList', 'rowTemplate', 'buyRows',
    'sellRows',
    // Order form.
    'orderForm', 'priceBox', 'buyBttn', 'sellBttn', 'baseBalance',
    'quoteBalance', 'limitBttn', 'marketBttn', 'tifBox', 'submitBttn',
    'qtyField', 'rateField', 'orderErr', 'baseBox', 'quoteBox', 'baseImg',
    'quoteImg', 'baseNewButton', 'quoteNewButton', 'baseBalSpan',
    'quoteBalSpan', 'lotSize', 'rateStep', 'lotField', 'tifNow', 'mktBuyBox',
    'mktBuyLots', 'mktBuyField', 'minMktBuy', 'qtyBox',
    // Wallet unlock form
    'forms', 'openForm', 'walletPass',
    // Order submission is verified with the user's password.
    'verifyForm', 'vSide', 'vQty', 'vBase', 'vRate',
    'vTotal', 'vQuote', 'vPass', 'vSubmit', 'verifyLimit', 'verifyMarket',
    'vmTotal', 'vmAsset', 'vmLots', 'mktBuyScore',
    // Create wallet form
    'walletForm', 'acctName',
    // Active orders
    'liveTemplate', 'liveList',
    // Cancel order form
    'cancelForm', 'cancelRemain', 'cancelUnit', 'cancelPass', 'cancelSubmit'
  ])

  page.rowTemplate.remove()
  page.rowTemplate.removeAttribute('id')
  page.liveTemplate.removeAttribute('id')
  page.liveTemplate.remove()
  const quoteUnits = main.querySelectorAll('[data-unit=quote]')
  const baseUnits = main.querySelectorAll('[data-unit=base]')
  const isSell = () => page.sellBttn.classList.contains('selected')
  const isLimit = () => page.limitBttn.classList.contains('selected')
  const asAtoms = s => Math.round(parseFloat(s) * 1e8)
  const swapBttns = (before, now) => {
    before.classList.remove('selected')
    now.classList.add('selected')
  }

  const setOrderVisibility = (limit, sell) => {
    if (limit) {
      Doc.show(page.priceBox, page.tifBox, page.qtyBox)
      Doc.hide(page.mktBuyBox)
    } else {
      Doc.hide(page.priceBox)
      if (sell) {
        Doc.hide(page.mktBuyBox)
        Doc.show(page.qtyBox)
      } else {
        Doc.show(page.mktBuyBox)
        Doc.hide(page.qtyBox)
      }
    }
  }

  bind(page.buyBttn, 'click', () => {
    setOrderVisibility(isLimit(), false)
    swapBttns(page.sellBttn, page.buyBttn)
  })
  bind(page.sellBttn, 'click', () => {
    setOrderVisibility(isLimit(), true)
    swapBttns(page.buyBttn, page.sellBttn)
  })
  // const tifCheck = tifDiv.querySelector('input[type=checkbox]')
  bind(page.limitBttn, 'click', () => {
    setOrderVisibility(true, isSell())
    swapBttns(page.marketBttn, page.limitBttn)
  })
  bind(page.marketBttn, 'click', () => {
    setOrderVisibility(false, isSell())
    swapBttns(page.limitBttn, page.marketBttn)
  })

  const walletIcons = {
    base: new StateIcons(page.baseBox),
    quote: new StateIcons(page.quoteBox)
  }

  var selected
  const reqMarket = async (url, base, quote) => {
    const dex = app.user.exchanges[url]
    selected = {
      dex: dex,
      sid: sid(base, quote), // A string market identifier used by the DEX.
      base: app.assets[base],
      baseCfg: dex.assets[base],
      quote: app.assets[quote],
      quoteCfg: dex.assets[quote]
    }
    ws.request('loadmarket', makeMarket(url, base, quote))
  }

  const reportPrice = p => { page.rateField.value = p.toFixed(8) }
  const reporters = {
    price: reportPrice
  }
  const chart = new DepthChart(page.marketChart, reporters)
  const marketRows = page.marketList.querySelectorAll('.marketrow')
  const markets = []

  const parseOrder = () => {
    let qtyField = page.qtyField
    const limit = isLimit()
    const sell = isSell()
    if (!limit && !sell) {
      qtyField = page.mktBuyField
    }
    return {
      dex: selected.dex.url,
      isLimit: limit,
      sell: sell,
      base: selected.base.id,
      quote: selected.quote.id,
      qty: asAtoms(qtyField.value),
      rate: asAtoms(page.rateField.value), // message-rate
      tifnow: page.tifNow.checked
    }
  }

  const validateOrder = order => {
    if (order.isLimit && !order.rate) {
      Doc.show(page.orderErr)
      page.orderErr.textContent = 'zero rate not allowed'
      return false
    }
    if (!order.qty) {
      Doc.show(page.orderErr)
      page.orderErr.textContent = 'zero quantity not allowed'
      return false
    }
    return true
  }

  // If no data was passed in, check if there is a market saved for the user.
  var lastMarket = (data && data.market) ? data.market : State.fetch('selectedMarket')
  var mktFound = false
  marketRows.forEach(div => {
    const base = parseInt(div.dataset.base)
    const quote = parseInt(div.dataset.quote)
    const dex = div.dataset.dex
    mktFound = mktFound || (lastMarket && lastMarket.dex === dex &&
      lastMarket.base === base && lastMarket.quote === quote)
    bind(div, 'click', () => {
      page.marketLoader.classList.remove('d-none')
      reqMarket(dex, base, quote)
    })
    markets.push({
      base: base,
      quote: quote,
      dex: dex
    })
  })

  // Template elements used to clone rows in the order tables.
  const tableBuilder = {
    row: page.rowTemplate,
    buys: page.buyRows,
    sells: page.sellRows,
    reporters: reporters
  }

  // handleBook is the handler for the 'book' notification from the server.
  // Updates the charts, order tables, etc.
  var book
  const handleBook = (data) => {
    book = data.book
    const b = tableBuilder
    if (!book) {
      chart.clear()
      Doc.empty(b.buys)
      Doc.empty(b.sells)
      return
    }
    chart.set({
      book: book,
      quoteSymbol: selected.quote.symbol,
      baseSymbol: selected.base.symbol
    })
    loadTable(book.buys, b.buys, b, 'buycolor')
    loadTable(book.sells, b.sells, b, 'sellcolor')
  }

  const midGap = () => {
    if (!book) return
    if (book.buys && book.buys.length) {
      if (book.sells && book.sells.length) {
        return (book.buys[0].rate + book.sells[0].rate) / 2
      }
      return book.buys[0].rate
    }
    if (book.sells && book.sells.length) {
      return book.sells[0].rate
    }
    return null
  }

  const setMktBuyEstimate = () => {
    const lotSize = selected.baseCfg.lotSize
    const xc = app.user.exchanges[selected.dex.url]
    const buffer = xc.markets[selected.sid].buybuffer
    const gap = midGap()
    if (gap) {
      page.minMktBuy.textContent = Doc.formatCoinValue(lotSize * buffer * gap / 1e8)
    }
  }

  const setBalance = (a, row, img, button, bal) => {
    img.src = `/img/coins/${a.symbol.toLowerCase()}.png`
    if (a.wallet) {
      Doc.hide(button)
      Doc.show(row)
      bal.textContent = Doc.formatCoinValue(a.wallet.balance / 1e8)
      return
    }
    Doc.show(button)
    Doc.hide(row)
  }

  const updateWallet = assetID => {
    const asset = app.assets[assetID]
    switch (assetID) {
      case (selected.base.id):
        setBalance(asset, page.baseBalSpan, page.baseImg, page.baseNewButton, page.baseBalance)
        walletIcons.base.readWallet(asset.wallet)
        break
      case (selected.quote.id):
        setBalance(asset, page.quoteBalSpan, page.quoteImg, page.quoteNewButton, page.quoteBalance)
        walletIcons.quote.readWallet(asset.wallet)
    }
  }

  const orderRows = {}
  const refreshActiveOrders = () => {
    for (const oid in orderRows) delete orderRows[oid]
    const orders = app.orders(selected.dex.url, selected.base.id, selected.quote.id)
    Doc.empty(page.liveList)
    for (const order of orders) {
      const row = page.liveTemplate.cloneNode(true)
      orderRows[order.id] = row
      const set = (col, s) => { row.querySelector(`[data-col=${col}]`).textContent = s }
      set('side', order.sell ? 'sell' : 'buy')
      set('age', timeSince(order.stamp))
      set('rate', Doc.formatCoinValue(order.rate / 1e8))
      set('qty', Doc.formatCoinValue(order.qty / 1e8))
      set('filled', `${(order.filled / order.qty * 100).toFixed(1)}%`)
      if (order.type === LIMIT) {
        if (order.cancelling) {
          set('cancel', order.canceled ? 'canceled' : 'cancelling')
        } else if (order.filled !== order.qty) {
          const icon = row.querySelector('[data-col=cancel] > span')
          Doc.show(icon)
          Doc.bind(icon, 'click', e => {
            e.stopPropagation()
            showCancel(icon, order)
          })
        }
      }
      page.liveList.appendChild(row)
    }
  }

  ws.registerRoute('book', data => {
    const [b, q] = [selected.base, selected.quote]
    if (data.base !== b.id || data.quote !== q.id) return
    handleBook(data)
    market = data.market
    page.marketLoader.classList.add('d-none')
    const url = selected.dex.url
    marketRows.forEach(row => {
      const d = row.dataset
      if (d.dex === url && parseInt(d.base) === data.base && parseInt(d.quote) === data.quote) {
        row.classList.add('selected')
      } else {
        row.classList.remove('selected')
      }
    })
    State.store('selectedMarket', {
      dex: data.dex,
      base: data.base,
      quote: data.quote
    })
    page.lotSize.textContent = Doc.formatCoinValue(selected.baseCfg.lotSize / 1e8)
    page.rateStep.textContent = Doc.formatCoinValue(selected.quoteCfg.rateStep / 1e8)
    baseUnits.forEach(el => { el.textContent = b.symbol.toUpperCase() })
    quoteUnits.forEach(el => { el.textContent = q.symbol.toUpperCase() })
    updateWallet(b.id)
    updateWallet(q.id)
    setMktBuyEstimate()
    refreshActiveOrders()
  })

  ws.registerRoute('bookupdate', e => {
    if (market && (e.market.dex !== market.dex || e.market.base !== market.base || e.market.quote !== market.quote)) return
    handleBookUpdate(main, e)
  })

  const animationLength = 500
  var currentForm
  const showForm = async form => {
    currentForm = form
    Doc.hide(page.openForm, page.verifyForm, page.walletForm, page.cancelForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0px'
  }

  // Show a form to connect/unlock a wallet.
  var openAsset
  var openFunc
  const showOpen = async (asset, f) => {
    openAsset = asset
    openFunc = f
    page.openForm.setAsset(app.assets[asset.id])
    showForm(page.openForm)
    page.walletPass.focus()
  }

  const showVerify = () => {
    const order = parseOrder()
    const baseAsset = app.assets[order.base]
    const quoteAsset = app.assets[order.quote]
    const fromAsset = order.sell ? baseAsset : quoteAsset
    const toAsset = order.sell ? quoteAsset : baseAsset
    if (order.isLimit) {
      Doc.show(page.verifyLimit)
      Doc.hide(page.verifyMarket)
      page.vRate.textContent = Doc.formatCoinValue(order.rate / 1e8)
      page.vQty.textContent = Doc.formatCoinValue(order.qty / 1e8)
      page.vBase.textContent = baseAsset.symbol.toUpperCase()
      page.vQuote.textContent = quoteAsset.symbol.toUpperCase()
      page.vSide.textContent = order.sell ? 'sell' : 'buy'
      page.vTotal.textContent = Doc.formatCoinValue(order.rate / 1e8 * order.qty / 1e8)
    } else {
      Doc.hide(page.verifyLimit)
      Doc.show(page.verifyMarket)
      page.vSide.textContent = 'trade'
      page.vQty.textContent = Doc.formatCoinValue(order.qty / 1e8)
      page.vBase.textContent = fromAsset.symbol.toUpperCase()
      const gap = midGap()
      if (gap) {
        const received = order.sell ? order.qty * gap : order.qty / gap
        const lotSize = selected.baseCfg.lotSize
        const lots = order.sell ? order.qty / lotSize : received / lotSize
        // TODO: Some kind of adjustment to align with lot sizes for market buy?
        page.vmTotal.textContent = Doc.formatCoinValue(received / 1e8)
        page.vmAsset.textContent = toAsset.symbol.toUpperCase()
        page.vmLots.textContent = lots.toFixed(1)
      } else {
        Doc.hide(page.verifyMarket)
      }
    }
    showForm(page.verifyForm)
  }

  const showCancel = (bttn, order) => {
    const remaining = order.qty - order.filled
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining / 1e8)
    const isMarketBuy = !order.isLimit && !order.sell
    const symbol = isMarketBuy ? selected.quote.symbol : selected.base.symbol
    page.cancelUnit.textContent = symbol.toUpperCase()
    showForm(page.cancelForm)
    Doc.bind(page.cancelSubmit, 'click', async () => {
      const pw = page.cancelPass.value
      page.cancelPass.value = ''
      const req = {
        orderID: order.id,
        pw: pw
      }
      var res = await postJSON('/api/cancel', req)
      app.loaded()
      Doc.hide(page.forms)
      if (!app.checkResponse(res)) return
      bttn.parentNode.textContent = 'cancelling'
      order.cancelling = true
    })
  }

  var currentCreate
  const showCreate = asset => {
    currentCreate = asset
    page.walletForm.setAsset(asset)
    showForm(page.walletForm)
    page.acctName.focus()
  }

  const stepSubmit = () => {
    Doc.hide(page.orderErr)
    if (!validateOrder(parseOrder())) return
    const baseWallet = app.walletMap[selected.base.id]
    const quoteWallet = app.walletMap[selected.quote.id]
    if (!baseWallet) {
      page.orderErr.textContent = `No ${selected.base.symbol} wallet`
      Doc.show(page.orderErr)
      return
    }
    if (!quoteWallet) {
      page.orderErr.textContent = `No ${selected.quote.symbol} wallet`
      Doc.show(page.orderErr)
      return
    }
    showVerify()
  }

  // Bind the wallet unlock form.
  forms.bindOpenWallet(app, page.openForm, async () => {
    openFunc()
  })

  // Create a wallet
  forms.bindNewWallet(app, page.walletForm, async () => {
    const user = await app.fetchUser()
    const asset = user.assets[currentCreate.id]
    Doc.hide(page.forms)
    updateWallet(asset.id)
  })

  // Main order form
  forms.bind(page.orderForm, page.submitBttn, async () => {
    stepSubmit()
  })

  // Order verification form
  forms.bind(page.verifyForm, page.vSubmit, async () => {
    Doc.hide(page.forms)
    const order = parseOrder()
    const pw = page.vPass.value
    page.vPass.textContent = ''
    const req = {
      order: order,
      pw: pw
    }
    if (!validateOrder(order)) return
    var res = await postJSON('/api/trade', req)
    app.loaded()
    if (!app.checkResponse(res)) return
    // If the wallets are not open locally, they must have been opened during
    // ordering. Grab updated info.
    const baseWallet = app.walletMap[selected.base.id]
    const quoteWallet = app.walletMap[selected.quote.id]
    if (!baseWallet.open || !quoteWallet.open) {
      await app.fetchUser()
      updateWallet(selected.base.id)
      updateWallet(selected.quote.id)
    }
    app.orders(order.dex, order.base, order.quote).push(res.order)
    refreshActiveOrders()
  })

  // If the user clicks outside of a form, it should close the page overlay.
  bind(page.forms, 'click', e => {
    if (!Doc.mouseInElement(e, currentForm)) Doc.hide(page.forms)
  })

  // Add wallet buttons
  bind(page.baseNewButton, 'click', () => { showCreate(selected.base) })
  bind(page.quoteNewButton, 'click', () => { showCreate(selected.quote) })
  const unlocked = async () => {
    Doc.hide(page.forms)
    await app.fetchUser()
    updateWallet(openAsset.id)
  }
  bind(walletIcons.base.icons.locked, 'click', () => { showOpen(selected.base, unlocked) })
  bind(walletIcons.quote.icons.locked, 'click', () => { showOpen(selected.quote, unlocked) })
  bind(walletIcons.base.icons.sleeping, 'click', () => { showOpen(selected.base, unlocked) })
  bind(walletIcons.quote.icons.sleeping, 'click', () => { showOpen(selected.quote, unlocked) })

  // Fetch the first market in the list, or the users last selected market, if
  // it was found.
  const firstEntry = mktFound ? lastMarket : markets[0]
  reqMarket(firstEntry.dex, firstEntry.base, firstEntry.quote)

  const lotChange = () => {
    const lots = parseInt(page.lotField.value)
    if (lots <= 0) {
      page.lotField.value = 0
      page.qtyField.value = ''
      return
    }
    const lotSize = selected.baseCfg.lotSize
    page.lotField.value = lots
    page.qtyField.value = (lots * lotSize / 1e8)
  }
  bind(page.lotField, 'change', lotChange)
  bind(page.lotField, 'keyup', lotChange)

  const qtyChange = (finalize) => {
    const order = parseOrder()
    if (order.qty <= 0) {
      page.lotField.value = 0
      page.qtyField.value = ''
      return
    }
    const lotSize = selected.baseCfg.lotSize
    const lots = Math.floor(order.qty / lotSize)
    const adjusted = lots * lotSize
    page.lotField.value = lots
    if (!order.isLimit && !order.sell) return
    if (finalize) page.qtyField.value = (adjusted / 1e8)
  }
  bind(page.qtyField, 'change', () => qtyChange(true))
  bind(page.qtyField, 'keyup', () => qtyChange(false))

  const mktBuyChange = () => {
    const qty = asAtoms(page.mktBuyField.value)
    const gap = midGap()
    if (!gap || !qty) {
      page.mktBuyLots.textContent = '0'
      page.mktBuyScore.textContent = '0'
      return
    }
    const lotSize = selected.baseCfg.lotSize
    const received = qty / gap
    page.mktBuyLots.textContent = (received / lotSize).toFixed(1)
    page.mktBuyScore.textContent = Doc.formatCoinValue(received / 1e8)
  }
  bind(page.mktBuyField, 'change', mktBuyChange)
  bind(page.mktBuyField, 'keyup', mktBuyChange)

  bind(page.rateField, 'change', () => {
    const order = parseOrder()
    if (order.rate <= 0) {
      page.rateField.value = 0
      return
    }
    // Truncate to rate step. If it is a market buy order, do not adjust.
    const rateStep = selected.quoteCfg.rateStep
    const adjusted = order.rate - (order.rate % rateStep)
    page.rateField.value = (adjusted / 1e8)
  })

  unattach(() => {
    ws.request('unmarket', {})
    ws.deregisterRoute('book')
    ws.deregisterRoute('bookupdate')
    chart.unattach()
  })

  return {
    order: note => {
      const order = note.order
      if (order.targetID && note.subject === 'cancel') {
        orderRows[order.targetID].querySelector('[data-col=cancel]').textContent = 'canceled'
      } else {
        const row = orderRows[order.id]
        if (!row) return
        const td = row.querySelector('[data-col=filled]')
        td.textContent = `${(order.filled / order.qty * 100).toFixed(1)}%`
        if (order.filled === order.qty) {
          // Remove the cancellation button.
          row.querySelector('[data-col=cancel]').textContent = ''
        }
      }
    }
  }
}

/* constructors is a map to page constructors. */
var constructors = {
  login: LoginPage,
  register: RegistrationPage,
  markets: MarketsPage,
  wallets: WalletsPage,
  settings: SettingsPage
}

function makeMarket (dex, base, quote) {
  return {
    dex: dex,
    base: base,
    quote: quote
  }
}

// loadTables loads the order book side into the specified table.
function loadTable (bookSide, table, builder, cssClass) {
  Doc.empty(table)
  const check = document.createElement('span')
  check.classList.add('ico-check')
  bookSide.forEach(order => {
    const tr = builder.row.cloneNode(true)
    const rate = order.rate
    bind(tr, 'click', () => {
      builder.reporters.price(rate)
    })
    tr.querySelectorAll('td').forEach(td => {
      switch (td.dataset.type) {
        case 'qty':
          td.innerText = order.qty.toFixed(8)
          break
        case 'rate':
          td.innerText = order.rate.toFixed(8)
          td.classList.add(cssClass)
          break
        case 'epoch':
          if (order.epoch) td.appendChild(check.cloneNode())
          break
      }
    })
    table.appendChild(tr)
  })
}

// handleBookUpdate handles a websocket order book update from the server.
function handleBookUpdate (main, update) {
  console.log('updating order book')
}

function sid (b, q) { return `${b}-${q}` }

const aYear = 31536000000
const aMonth = 2592000000
const aDay = 86400000
const anHour = 3600000
const aMinute = 60000

/* timeMod returns the quotient and remainder of t / dur. */
function timeMod (t, dur) {
  const n = Math.floor(t / dur)
  return [n, t - n * dur]
}

/*
 * timeSince returns a string representation of the duration since the specified
 * unix timestamp.
 */
function timeSince (t) {
  var seconds = Math.floor(((new Date().getTime()) - t))
  var result = ''
  var count = 0
  const add = (n, s) => {
    if (n > 0 || count > 0) count++
    if (n > 0) result += `${n} ${s} `
    return count >= 2
  }
  var y, mo, d, h, m, s
  [y, seconds] = timeMod(seconds, aYear)
  if (add(y, 'y')) { return result }
  [mo, seconds] = timeMod(seconds, aMonth)
  if (add(mo, 'mo')) { return result }
  [d, seconds] = timeMod(seconds, aDay)
  if (add(d, 'd')) { return result }
  [h, seconds] = timeMod(seconds, anHour)
  if (add(h, 'h')) { return result }
  [m, seconds] = timeMod(seconds, aMinute)
  if (add(m, 'm')) { return result }
  [s, seconds] = timeMod(seconds, 1000)
  add(s, 's')
  return result || '0 s'
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
