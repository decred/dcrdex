import Doc from './doc'
import State from './state'
import { DepthChart } from './charts'
import ws from './ws'

const idel = Doc.idel // = element by id
const bind = Doc.bind
const unbind = Doc.unbind
var app

const SUCCESS = 'success'
const ERROR = 'error'
const DCR_ID = 42
const LIMIT = 1
// const MARKET = 2
// const CANCEL = 3

const updateWalletRoute = 'update_wallet'
const errorMsgRoute = 'error_message'
const successMsgRoute = 'success_message'

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

  async start () {
    app = this
    await this.fetchUser()
    bind(window, 'popstate', (e) => {
      const page = e.state.page
      if (!page && page !== '') return
      this.loadPage(page)
    })
    this.main = idel(document, 'main')
    const handler = this.main.dataset.handler
    window.history.replaceState({ page: handler }, '', `/${handler}`)
    this.attachHeader(idel(document, 'header'))
    this.attachCommon(this.header)
    this.attach()
    ws.connect(getSocketURI(), this.reconnected)
    ws.registerRoute(updateWalletRoute, wallet => {
      this.assets[wallet.assetID].wallet = wallet
      this.walletMap[wallet.assetID] = wallet
      const balances = this.main.querySelectorAll(`[data-balance-target="${wallet.assetID}"]`)
      balances.forEach(el => { el.textContent = (wallet.balance / 1e8).toFixed(8) })
    })
    ws.registerRoute(errorMsgRoute, msg => {
      this.notify(ERROR, msg)
    })
    ws.registerRoute(successMsgRoute, msg => {
      this.notify(SUCCESS, msg)
    })
  }

  reconnected () {
    window.location.reload()
  }

  async fetchUser () {
    const user = await getJSON('/api/user')
    if (!checkResponse(user)) return
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

  // Load the page from the server. Insert and bind to the HTML.
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

  // attach binds the common and specific handlers to the current main element.
  attach (data) {
    var handlerID = this.main.dataset.handler
    if (!handlerID) {
      console.error('cannot attach to content with no specified handler')
      return
    }
    unattachers.forEach(f => f())
    unattachers = []
    this.attachCommon(this.main)
    var handler = handlers[handlerID]
    if (!handler) {
      console.error(`no handler for ${handlerID}`)
    }
    handler(this.main, data)
  }

  attachHeader (header) {
    this.header = header
    const pg = this.page = parsePage(header, [
      'noteIndicator', 'noteBox', 'noteList', 'noteTemplate',
      'walletsMenuEntry', 'noteMenuEntry', 'settingsIcon', 'loginLink', 'loader'
    ])
    pg.noteIndicator.style.display = 'none'
    pg.noteTemplate.id = undefined
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
    bind(pg.noteMenuEntry, 'click', e => {
      bind(document, 'click', hide)
      pg.noteBox.style.display = 'block'
      pg.noteIndicator.style.display = 'none'
    })
    if (this.notes.length === 0) {
      pg.noteList.textContent = 'no notifications'
    }
  }

  // Use setLogged when user has signed in or out. For logging out, it may be
  // better to trigger a hard reload.
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

  // attachCommon scans the provided node and handles some common bindings.
  attachCommon (node) {
    node.querySelectorAll('[data-pagelink]').forEach(link => {
      const page = link.dataset.pagelink
      bind(link, 'click', async () => {
        await this.loadPage(page)
      })
    })
  }

  notify (level, msg) {
    var found = false
    for (let i = 0; i < this.notes.length; i++) {
      if (this.notes[i].msg === msg) {
        this.notes[i].count++
        found = true
        break
      }
    }
    if (!found) {
      this.notes.push({
        level: level,
        msg: msg,
        count: 1
      })
    }
    const notes = this.page.noteList
    Doc.empty(notes)
    for (let i = this.notes.length - 1; i >= 0; i--) {
      if (i < this.notes.length - 10) return
      const note = this.notes[i]
      const noteEl = this.makeNote(note.level, note.msg)
      notes.appendChild(noteEl)
    }
    this.notifyUI()
  }

  notifyUI () {
    const ni = this.page.noteIndicator
    ni.style.display = 'block'
    ni.classList.remove('bad')
    ni.classList.remove('good')
    const noteLevel = this.notes.reduce((a, v) => {
      if (v.level === ERROR) return ERROR
      if (v.level === SUCCESS && a !== ERROR) return SUCCESS
      return a
    }, 0)
    switch (noteLevel) {
      case ERROR:
        ni.classList.add('bad')
        break
      case SUCCESS:
        ni.classList.add('good')
        break
    }
  }

  makeNote (level, msg) {
    const note = this.page.noteTemplate.cloneNode(true)
    note.querySelector('div').classList.add(level === ERROR ? 'bad' : 'good')
    note.querySelector('span').textContent = msg
    return note
  }

  loading (el) {
    el.appendChild(this.page.loader)
  }

  loaded () {
    this.page.loader.remove()
  }

  orders (dex, bid, qid) {
    var o = this.user.exchanges[dex].markets[sid(bid, qid)].orders
    if (!o) {
      o = []
      this.user.exchanges[dex].markets[sid(bid, qid)].orders = o
    }
    return o
  }
}

function getSocketURI () {
  var protocol = (window.location.protocol === 'https:') ? 'wss' : 'ws'
  return `${protocol}://${window.location.host}/ws`
}

// requestJSON encodes the object and sends the JSON to the specified address.
async function requestJSON (method, addr, reqBody) {
  try {
    const response = await window.fetch(addr, {
      method: method,
      headers: new window.Headers({ 'content-type': 'application/json' }),
      body: reqBody
    })
    if (response.status !== 200) { throw response }
    const obj = await response.json()
    obj.requestSuccessful = true
    return obj
  } catch (response) {
    response.requestSuccessful = false
    response.msg = await response.text()
    return response
  }
}

async function postJSON (addr, data) {
  return requestJSON('POST', addr, JSON.stringify(data))
}

async function getJSON (addr) {
  return requestJSON('GET', addr)
}

// unattachers are handlers to be run when a page is unloaded.
var unattachers = []

// unattach adds an unattacher to the array.
function unattach (f) {
  unattachers.push(f)
}

// handlers are handlers for binding to main elements.
var handlers = {
  login: handleLogin,
  register: handleRegister,
  markets: handleMarkets,
  wallets: handleWallets,
  settings: handleSettings
}

// handleLogin is the 'login' page main element handler.
function handleLogin (main) {
  const page = parsePage(main, [
    'submit', 'errMsg', 'loginForm', 'pw'
  ])
  bindForm(page.loginForm, page.submit, async (e) => {
    app.loading(page.loginForm)
    Doc.hide(page.errMsg)
    const pw = page.pw.value
    page.pw.value = ''
    if (pw === '') {
      page.errMsg.textContent = 'password cannot be empty'
      Doc.show(page.errMsg)
      return
    }
    app.loaded()
    var res = await postJSON('/api/login', { pass: pw })
    if (!checkResponse(res)) return
    await app.fetchUser()
    app.setLogged(true)
    app.loadPage('markets')
  })
  page.pw.focus()
}

// handleRegister is the 'register' page main element handler.
function handleRegister (main) {
  const page = parsePage(main, [
    // Form 1: Set the application password
    'appPWForm', 'appPWSubmit', 'appErrMsg', 'appPW', 'appPWAgain',
    // Form 2: Create Decred wallet
    'walletForm',
    // Form 3: Open Decred wallet
    'openForm',
    // Form 4: DEX address
    'urlForm', 'addrInput', 'submitAddr', 'feeDisplay', 'addrErr',
    // Form 5: Final form to initiate registration. Client app password.
    'pwForm', 'clientPass', 'submitPW', 'regErr'
  ])

  const animationLength = 300

  const changeForm = async (form1, form2) => {
    const shift = main.offsetWidth / 2
    await Doc.animate(animationLength, progress => {
      form1.style.right = `${progress * shift}px`
    }, 'easeInHard')
    Doc.hide(form1)
    form1.style.right = '0px'
    form2.style.right = -shift
    Doc.show(form2)
    form2.querySelector('input').focus()
    await Doc.animate(animationLength, progress => {
      form2.style.right = `${-shift + progress * shift}px`
    }, 'easeOutHard')
    form2.style.right = '0px'
  }

  // SET APP PASSWORD
  // This form is only shown the first time the user visits the /register page.
  bindForm(page.appPWForm, page.appPWSubmit, async () => {
    Doc.hide(page.appErrMsg)
    const pw = page.appPW.value
    const pwAgain = page.appPWAgain.value
    if (pw === '') {
      page.appErrMsg.textContent = 'password cannot be empty'
      Doc.show(page.appErrMsg)
      return
    }
    if (pw !== pwAgain) {
      page.appErrMsg.textContent = 'passwords do not match'
      Doc.show(page.appErrMsg)
      return
    }
    page.appPW.value = ''
    page.appPWAgain.value = ''
    app.loading(page.appPWForm)
    var res = await postJSON('/api/init', { pass: pw })
    app.loaded()
    if (!checkResponse(res)) {
      page.appErrMsg.textContent = res.msg
      Doc.show(page.appErrMsg)
      return
    }
    app.setLogged(true)
    const dcrWallet = app.walletMap[DCR_ID]
    if (!dcrWallet) {
      changeForm(page.appPWForm, page.walletForm)
      return
    }
    // Not really sure if these other cases are possible if the user hasn't
    // even set their password yet.
    if (!dcrWallet.open) {
      changeForm(page.appPWForm, page.openForm)
      return
    }
    changeForm(page.appPWForm, page.urlForm)
  })

  bindNewWalletForm(page.walletForm, () => {
    changeForm(page.walletForm, page.urlForm)
  })
  page.walletForm.setAsset(app.assets[DCR_ID])

  // OPEN DCR WALLET
  // This form is only show if the wallet is not already open.
  bindOpenWalletForm(page.openForm, () => {
    changeForm(page.openForm, page.urlForm)
  })
  page.openForm.setAsset(app.assets[DCR_ID])

  // ENTER NEW DEX URL
  var fee
  bindForm(page.urlForm, page.submitAddr, async () => {
    Doc.hide(page.addrErr)
    const dex = page.addrInput.value
    if (dex === '') {
      page.addrErr.textContent = 'URL cannot be empty'
      Doc.show(page.addrErr)
      return
    }
    app.loading(page.urlForm)
    var res = await postJSON('/api/preregister', { dex: dex })
    app.loaded()
    if (!checkResponse(res)) {
      page.addrErr.textContent = res.msg
      Doc.show(page.addrErr)
      return
    }
    page.feeDisplay.textContent = formatCoinValue(res.fee / 1e8)
    fee = res.fee
    const dcrWallet = app.walletMap[DCR_ID]
    if (!dcrWallet) {
      // There is no known Decred wallet, show the wallet form
      await changeForm(page.urlForm, page.walletForm)
      return
    }
    // The Decred wallet is known, check if it is open.
    if (!dcrWallet.open) {
      await changeForm(page.urlForm, page.openForm)
      return
    }
    // The Decred wallet is known and open, collect the main client password.
    await changeForm(page.urlForm, page.pwForm)
  })

  // SUBMIT DEX REGISTRATION
  bindForm(page.pwForm, page.submitPW, async () => {
    Doc.hide(page.regErr)
    const dex = page.addrInput.value
    const registration = {
      dex: page.addrInput.value,
      pass: page.clientPass.value,
      fee: fee
    }
    page.clientPass.value = ''
    app.loading(page.pwForm)
    var res = await postJSON('/api/register', registration)
    app.loaded()
    if (!checkResponse(res)) {
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      return
    }
    // Need to get a fresh market list. May consider handling this with a
    // websocket update instead.
    await app.fetchUser()
    app.notify(SUCCESS, `Account registered for ${dex}. Waiting for confirmations.`)
    app.loadPage('markets')
  })
}

// handleMarkets is the 'markets' page main element handler.
async function handleMarkets (main, data) {
  var market
  const page = parsePage(main, [
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
      page.minMktBuy.textContent = formatCoinValue(lotSize * buffer * gap / 1e8)
    }
  }

  const setBalance = (a, row, img, button, bal) => {
    img.src = `/img/coins/${a.symbol.toLowerCase()}.png`
    if (a.wallet) {
      Doc.hide(button)
      Doc.show(row)
      bal.textContent = formatCoinValue(a.wallet.balance / 1e8)
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

  const refreshActiveOrders = () => {
    const orders = app.orders(selected.dex.url, selected.base.id, selected.quote.id)
    Doc.empty(page.liveList)
    for (const order of orders) {
      const row = page.liveTemplate.cloneNode(true)
      const set = (col, s) => { row.querySelector(`[data-col=${col}]`).textContent = s }
      set('side', order.sell ? 'sell' : 'buy')
      set('age', timeSince(order.stamp))
      set('rate', formatCoinValue(order.rate / 1e8))
      set('qty', formatCoinValue(order.qty / 1e8))
      set('filled', `${(order.filled / order.qty * 100).toFixed(1)}%`)
      if (order.type === LIMIT) {
        if (order.cancelling) {
          set('cancel', 'cancelling')
        } else {
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
    page.lotSize.textContent = formatCoinValue(selected.baseCfg.lotSize / 1e8)
    page.rateStep.textContent = formatCoinValue(selected.quoteCfg.rateStep / 1e8)
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
      page.vRate.textContent = formatCoinValue(order.rate / 1e8)
      page.vQty.textContent = formatCoinValue(order.qty / 1e8)
      page.vBase.textContent = baseAsset.symbol.toUpperCase()
      page.vQuote.textContent = quoteAsset.symbol.toUpperCase()
      page.vSide.textContent = order.sell ? 'sell' : 'buy'
      page.vTotal.textContent = formatCoinValue(order.rate / 1e8 * order.qty / 1e8)
    } else {
      Doc.hide(page.verifyLimit)
      Doc.show(page.verifyMarket)
      page.vSide.textContent = 'trade'
      page.vQty.textContent = formatCoinValue(order.qty / 1e8)
      page.vBase.textContent = fromAsset.symbol.toUpperCase()
      const gap = midGap()
      if (gap) {
        const received = order.sell ? order.qty * gap : order.qty / gap
        const lotSize = selected.baseCfg.lotSize
        const lots = order.sell ? order.qty / lotSize : received / lotSize
        // TODO: Some kind of adjustment to align with lot sizes for market buy?
        page.vmTotal.textContent = formatCoinValue(received / 1e8)
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
    page.cancelRemain.textContent = formatCoinValue(remaining / 1e8)
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
      if (!checkResponse(res)) return
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
  bindOpenWalletForm(page.openForm, async () => {
    openFunc()
  })

  // Create a wallet
  bindNewWalletForm(page.walletForm, async () => {
    const user = await app.fetchUser()
    const asset = user.assets[currentCreate.id]
    Doc.hide(page.forms)
    updateWallet(asset.id)
  })

  // Main order form
  bindForm(page.orderForm, page.submitBttn, async () => {
    stepSubmit()
  })

  // Order verification form
  bindForm(page.verifyForm, page.vSubmit, async () => {
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
    if (!checkResponse(res)) return
    console.log("--success: order =", res.order)
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
    page.mktBuyScore.textContent = formatCoinValue(received / 1e8)
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

// handleWallets is the 'wallets' page main element handler.
function handleWallets (main) {
  const page = parsePage(main, [
    'rightBox',
    // Table Rows
    'assetArrow', 'balanceArrow', 'statusArrow', 'walletTable', 'txtStatus',
    // Available markets
    'markets', 'dexTitle', 'marketsBox', 'oneMarket', 'marketsFor',
    'marketsCard',
    // New wallet form
    'walletForm', 'acctName', 'newWalletPass', 'iniPath', 'walletErr',
    'newWalletLogo', 'newWalletName',
    // Unlock wallet form
    'openForm',
    // Deposit
    'deposit', 'depositName', 'depositAddress',
    // Withdraw
    'withdrawForm', 'withdrawLogo', 'withdrawName', 'withdrawAddr',
    'withdrawAmt', 'withdrawAvail', 'submitWithdraw', 'withdrawFee',
    'withdrawUnit', 'withdrawPW', 'withdrawErr'
  ])

  // Read the document.
  const getAction = (row, name) => row.querySelector(`[data-action=${name}]`)
  const rowInfos = {}
  const rows = page.walletTable.querySelectorAll('tr')
  var firstRow
  for (const tr of rows) {
    const assetID = parseInt(tr.dataset.assetID)
    const rowInfo = rowInfos[assetID] = {}
    if (!firstRow) firstRow = rowInfo
    rowInfo.ID = assetID
    rowInfo.tr = tr
    rowInfo.symbol = tr.dataset.symbol
    rowInfo.name = tr.dataset.name
    rowInfo.stateIcons = new StateIcons(tr)
    rowInfo.actions = {
      connect: getAction(tr, 'connect'),
      unlock: getAction(tr, 'unlock'),
      withdraw: getAction(tr, 'withdraw'),
      deposit: getAction(tr, 'deposit'),
      create: getAction(tr, 'create'),
      lock: getAction(tr, 'lock')
    }
  }

  // Prepare templates
  page.dexTitle.removeAttribute('id')
  page.dexTitle.remove()
  page.oneMarket.removeAttribute('id')
  page.oneMarket.remove()
  page.markets.removeAttribute('id')
  page.markets.remove()

  // Methods to switch the item displayed on the right side, with a little
  // fade-in animation.
  var displayed, animation
  const animationLength = 300

  const hideBox = async () => {
    if (animation) await animation
    if (!displayed) return
    Doc.hide(displayed)
  }

  const showBox = async (box, focuser) => {
    box.style.opacity = '0'
    Doc.show(box)
    if (focuser) focuser.focus()
    await Doc.animate(animationLength, progress => {
      box.style.opacity = `${progress}`
    }, 'easeOut')
    box.style.opacity = '1'
    displayed = box
  }

  // Show the markets box, which lists the markets available for a selected
  // asset.
  const showMarkets = async assetID => {
    const box = page.marketsBox
    const card = page.marketsCard
    const rowInfo = rowInfos[assetID]
    await hideBox()
    Doc.empty(card)
    page.marketsFor.textContent = rowInfo.name
    for (const [url, xc] of Object.entries(app.user.exchanges)) {
      let count = 0
      for (const market of Object.values(xc.markets)) {
        if (market.baseid === assetID || market.quoteid === assetID) count++
      }
      if (count === 0) continue
      const header = page.dexTitle.cloneNode(true)
      header.textContent = new URL(url).host
      card.appendChild(header)
      const marketsBox = page.markets.cloneNode(true)
      card.appendChild(marketsBox)
      for (const market of Object.values(xc.markets)) {
        // Only show markets where this is the base or quote asset.
        if (market.baseid !== assetID && market.quoteid !== assetID) continue
        const mBox = page.oneMarket.cloneNode(true)
        mBox.querySelector('span').textContent = prettyMarketName(market)
        let counterSymbol = market.basesymbol
        if (market.baseid === assetID) counterSymbol = market.quotesymbol
        mBox.querySelector('img').src = logoPath(counterSymbol)
        // Bind the click to a load of the markets page.
        const pageData = { market: makeMarket(url, market.baseid, market.quoteid) }
        bind(mBox, 'click', () => { app.loadPage('markets', pageData) })
        marketsBox.appendChild(mBox)
      }
    }
    animation = showBox(box)
  }

  // Show the new wallet form.
  const showNewWallet = async assetID => {
    const box = page.walletForm
    const asset = app.assets[assetID]
    await hideBox()
    if (assetID !== walletAsset) {
      page.acctName.value = ''
      page.newWalletPass.value = ''
      page.iniPath.value = ''
      page.iniPath.placeholder = asset.info.configpath
      Doc.hide(page.walletErr)
      page.newWalletName.textContent = asset.info.name
    }
    walletAsset = assetID
    page.walletForm.setAsset(asset)
    animation = showBox(box, page.acctName)
  }

  // Show the form used to unlock a wallet.
  var openAsset
  const showOpen = async assetID => {
    openAsset = assetID
    await hideBox()
    page.openForm.setAsset(app.assets[assetID])
    animation = showBox(page.openForm, page.walletPass)
  }

  // Display a deposit address.
  const showDeposit = async assetID => {
    const box = page.deposit
    const asset = app.assets[assetID]
    const wallet = app.walletMap[assetID]
    if (!wallet) {
      app.notify(ERROR, `No wallet found for ${asset.info.name}. Cannot retrieve deposit address.`)
      return
    }
    await hideBox()
    page.depositName.textContent = asset.info.name
    page.depositAddress.textContent = wallet.address
    animation = showBox(box, page.walletPass)
  }

  // Show the form to withdraw funds.
  const showWithdraw = async assetID => {
    const box = page.withdrawForm
    const asset = app.assets[assetID]
    const wallet = app.walletMap[assetID]
    if (!wallet) {
      app.notify(ERROR, `No wallet found for ${asset.info.name}. Cannot withdraw.`)
    }
    await hideBox()
    page.withdrawAddr.value = ''
    page.withdrawAmt.value = ''
    page.withdrawAvail.textContent = (wallet.balance / 1e8).toFixed(8)
    page.withdrawLogo.src = logoPath(asset.symbol)
    page.withdrawName.textContent = asset.info.name
    page.withdrawFee.textContent = wallet.feerate
    page.withdrawUnit.textContent = wallet.units
    box.dataset.assetID = assetID
    animation = showBox(box, page.walletPass)
  }

  const doConnect = async assetID => {
    app.loading(main)
    var res = await postJSON('/api/connectwallet', {
      assetID: assetID
    })
    app.loaded()
    if (!checkResponse(res)) return
    const rowInfo = rowInfos[assetID]
    Doc.hide(rowInfo.actions.connect)
    rowInfo.stateIcons.locked()
  }

  // Bind the new wallet form.
  var walletAsset
  bindNewWalletForm(page.walletForm, () => {
    const rowInfo = rowInfos[walletAsset]
    showMarkets(rowInfo.ID)
    const a = rowInfo.actions
    Doc.hide(a.create)
    Doc.show(a.withdraw, a.deposit, a.lock)
    rowInfo.stateIcons.unlocked()
  })

  // Bind the wallet unlock form.
  bindOpenWalletForm(page.openForm, async () => {
    const rowInfo = rowInfos[openAsset]
    const a = rowInfo.actions
    Doc.show(a.lock, a.withdraw, a.deposit)
    Doc.hide(a.unlock, a.connect)
    rowInfo.stateIcons.unlocked()
    showMarkets(openAsset)
  })

  // Bind the withdraw form.
  const wForm = page.withdrawForm
  bindForm(wForm, page.submitWithdraw, async () => {
    Doc.hide(page.withdrawErr)
    const assetID = parseInt(wForm.dataset.assetID)
    const open = {
      assetID: assetID,
      address: page.withdrawAddr.value,
      value: parseInt(page.withdrawAmt.value * 1e8),
      pw: page.withdrawPW.value
    }
    app.loading(page.withdrawForm)
    var res = await postJSON('/api/withdraw', open)
    app.loaded()
    if (!checkResponse(res)) {
      page.withdrawErr.textContent = res.msg
      Doc.show(page.withdrawErr)
      return
    }
    app.notify(SUCCESS, `Withdraw initiated. Coin ID ${res.coin}.`)
    showMarkets(assetID)
  })

  // Bind the row clicks, which shows the available markets for the asset.
  for (const asset of Object.values(rowInfos)) {
    bind(asset.tr, 'click', () => {
      showMarkets(asset.ID)
    })
  }

  // Bind buttons
  for (const [k, asset] of Object.entries(rowInfos)) {
    const assetID = parseInt(k) // keys are string asset ID.
    const rowInfo = rowInfos[assetID]
    const a = asset.actions
    const show = (e, f) => {
      e.stopPropagation()
      f(assetID)
    }
    bind(a.connect, 'click', () => {
      doConnect(assetID)
    })
    bind(a.withdraw, 'click', e => {
      show(e, showWithdraw)
    })
    bind(a.deposit, 'click', e => {
      show(e, showDeposit)
    })
    bind(a.create, 'click', e => {
      show(e, showNewWallet)
    })
    bind(a.unlock, 'click', e => {
      show(e, showOpen)
    })
    bind(a.lock, 'click', async e => {
      e.stopPropagation()
      app.loading(page.walletForm)
      var res = await postJSON('/api/closewallet', { assetID: assetID })
      app.loaded()
      if (!checkResponse(res)) return
      const a = asset.actions
      Doc.hide(a.lock, a.withdraw, a.deposit)
      Doc.show(a.unlock)
      rowInfo.stateIcons.locked()
    })
  }

  // Clicking on the avaailable amount on the withdraw form populates the
  // amount field.
  bind(page.withdrawAvail, 'click', () => {
    page.withdrawAmt.value = page.withdrawAvail.textContent
  })

  if (!firstRow) return
  showMarkets(firstRow.ID)
}

// handleSettings is the 'settings' page main element handler.
function handleSettings (main) {
  const darkMode = idel(main, 'darkMode')
  bind(darkMode, 'click', () => {
    State.dark(darkMode.checked)
    if (darkMode.checked) {
      document.body.classList.add('dark')
    } else {
      document.body.classList.remove('dark')
    }
  })
}

// Parameters for printing asset values.
const coinValueSpecs = {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 6,
  maximumFractionDigits: 8
}

// formatCoinValue formats the asset value to a string.
function formatCoinValue (x) {
  return x.toLocaleString('en-us', coinValueSpecs)
}

// parsePage finds the child elements with the IDs specified in ids. An object
// with the elements as properties with names matching the IDs is returned.
function parsePage (main, ids) {
  const get = s => idel(main, s)
  const page = {}
  ids.forEach(id => { page[id] = get(id) })
  return page
}

function checkResponse (resp) {
  if (!resp.requestSuccessful || !resp.ok) {
    if (app.user.inited) app.notify(ERROR, resp.msg)
    return false
  }
  return true
}

// bindForm binds the click and submit events and prevents page reloading on
// submission.
function bindForm (form, submitBttn, handler) {
  const wrapper = e => {
    if (e.preventDefault) e.preventDefault()
    handler(e)
  }
  bind(submitBttn, 'click', wrapper)
  bind(form, 'submit', wrapper)
}

function prettyMarketName (market) {
  return `${market.basesymbol.toUpperCase()}-${market.quotesymbol.toUpperCase()}`
}

function logoPath (symbol) {
  return `/img/coins/${symbol}.png`
}

function bindNewWalletForm (form, success) {
  // CREATE DCR WALLET
  // This form is only shown the first time the user visits the /register page.
  const fields = parsePage(form, [
    'iniPath', 'acctName', 'newWalletPass', 'submitCreate', 'walletErr',
    'newWalletLogo', 'newWalletName', 'wClientPass'
  ])
  var currentAsset
  form.setAsset = asset => {
    currentAsset = asset
    fields.newWalletLogo.src = logoPath(asset.symbol)
    fields.newWalletName.textContent = asset.info.name
  }
  bindForm(form, fields.submitCreate, async () => {
    Doc.hide(fields.walletErr)
    const create = {
      assetID: parseInt(currentAsset.id),
      pass: fields.newWalletPass.value,
      account: fields.acctName.value,
      inipath: fields.iniPath.value,
      appPass: fields.wClientPass.value
    }
    fields.wClientPass.value = ''
    app.loading(form)
    var res = await postJSON('/api/newwallet', create)
    app.loaded()
    if (!checkResponse(res)) {
      fields.walletErr.textContent = res.msg
      Doc.show(fields.walletErr)
      return
    }
    fields.newWalletPass.value = ''
    success()
  })
}

function bindOpenWalletForm (form, success) {
  const fields = parsePage(form, [
    'submitOpen', 'openErr', 'walletPass', 'unlockLogo', 'unlockName'
  ])
  var currentAsset
  form.setAsset = asset => {
    currentAsset = asset
    fields.unlockLogo.src = logoPath(asset.symbol)
    fields.unlockName.textContent = asset.name
    fields.walletPass.value = ''
  }
  bindForm(form, fields.submitOpen, async () => {
    Doc.hide(fields.openErr)
    const open = {
      assetID: parseInt(currentAsset.id),
      pass: fields.walletPass.value
    }
    fields.walletPass.value = ''
    app.loading(form)
    var res = await postJSON('/api/openwallet', open)
    app.loaded()
    if (!checkResponse(res)) {
      fields.openErr.textContent = res.msg
      Doc.show(fields.openErr)
      return
    }
    success()
  })
}

class StateIcons {
  constructor (box) {
    const stateIcon = (row, name) => row.querySelector(`[data-state=${name}]`)
    this.icons = {}
    this.icons.sleeping = stateIcon(box, 'sleeping')
    this.icons.locked = stateIcon(box, 'locked')
    this.icons.unlocked = stateIcon(box, 'unlocked')
    this.icons.nowallet = stateIcon(box, 'nowallet')
  }

  sleeping () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.nowallet)
    Doc.show(i.sleeping)
  }

  locked () {
    const i = this.icons
    Doc.hide(i.unlocked, i.nowallet, i.sleeping)
    Doc.show(i.locked)
  }

  unlocked () {
    const i = this.icons
    Doc.hide(i.locked, i.nowallet, i.sleeping)
    Doc.show(i.unlocked)
  }

  nowallet () {
    const i = this.icons
    Doc.hide(i.locked, i.unlocked, i.sleeping)
    Doc.show(i.nowallet)
  }

  readWallet (wallet) {
    switch (true) {
      case (!wallet):
        this.nowallet()
        break
      case (!wallet.running):
        this.sleeping()
        break
      case (!wallet.open):
        this.locked()
        break
      case (wallet.open):
        this.unlocked()
        break
      default:
        console.error('wallet in unknown state', wallet)
    }
  }
}

function sid (b, q) { return `${b}-${q}` }

const aYear = 31536000000
const aMonth = 2592000000
const aDay = 86400000
const anHour = 3600000
const aMinute = 60000

function timeMod (t, dur) {
  const n = Math.floor(t / dur)
  const mod = t - n * dur
  return [n, mod]
}

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
