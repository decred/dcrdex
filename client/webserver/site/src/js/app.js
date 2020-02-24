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

  start () {
    app = this
    this.userPromise = this.fetchUser()
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
    page.errMsg.classList.add('d-hide')
    const pw = page.pw.value
    page.pw.value = ''
    if (pw === '') {
      page.errMsg.textContent = 'password cannot be empty'
      page.errMsg.classList.remove('d-hide')
      return
    }
    app.loaded()
    var res = await postJSON('/api/login', { pass: pw })
    if (!checkResponse(res)) return
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
    'openForm', 'walletPass', 'submitOpen', 'openErr',
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
    page.appPW.value = ''
    const pwAgain = page.appPWAgain.value
    if (pw === '') {
      page.errMsg.textContent = 'password cannot be empty'
      Doc.show(page.errMsg)
      return
    }
    if (pw !== pwAgain) {
      page.appErrMsg.textContent = 'passwords do not match'
      Doc.show(page.appErrMsg)
      return
    }
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

  page.walletForm.dataset.assetID = DCR_ID
  bindNewWalletForm(page.walletForm, () => {
    changeForm(page.walletForm, page.urlForm)
  })

  // OPEN DCR WALLET
  // This form is only show if the wallet is not already open.
  page.openForm.dataset.assetID = DCR_ID
  bindOpenWalletForm(page.openForm, () => {
    changeForm(page.openForm, page.urlForm)
  })

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
    await app.userPromise
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
function handleMarkets (main, data) {
  var market
  const page = parsePage(main, [
    'marketLoader', 'priceBox', 'buyBttn', 'sellBttn', 'baseImg', 'quoteImg',
    'baseBalance', 'quoteBalance', 'limitBttn', 'marketBttn', 'tifBox',
    'marketChart', 'marketList', 'rowTemplate', 'buyRows', 'sellRows'
  ])

  page.rowTemplate.remove()
  page.rowTemplate.removeAttribute('id')
  const priceField = page.priceBox.querySelector('input[type=number]')
  const quoteUnits = main.querySelectorAll('[data-unit=quote]')
  const baseUnits = main.querySelectorAll('[data-unit=base]')
  const swapBttns = (before, now) => {
    before.classList.remove('selected')
    now.classList.add('selected')
  }
  bind(page.buyBttn, 'click', () => swapBttns(page.sellBttn, page.buyBttn))
  bind(page.sellBttn, 'click', () => swapBttns(page.buyBttn, page.sellBttn))
  // const tifCheck = tifDiv.querySelector('input[type=checkbox]')
  bind(page.limitBttn, 'click', () => {
    page.priceBox.classList.remove('d-hide')
    page.tifBox.classList.remove('d-hide')
    swapBttns(page.marketBttn, page.limitBttn)
  })
  bind(page.marketBttn, 'click', () => {
    page.priceBox.classList.add('d-hide')
    page.tifBox.classList.add('d-hide')
    swapBttns(page.limitBttn, page.marketBttn)
  })

  var selected = {}
  const reqMarket = async (dex, base, quote) => {
    await app.userPromise
    selected = {
      base: app.assets[base],
      quote: app.assets[quote]
    }
    ws.request('loadmarket', makeMarket(dex, base, quote))
  }

  const reportPrice = p => { priceField.value = p.toFixed(8) }
  const reporters = {
    price: reportPrice
  }
  const chart = new DepthChart(page.marketChart, reporters)
  const marketRows = page.marketList.querySelectorAll('.marketrow')
  const markets = []

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

  const tableBuilder = {
    row: page.rowTemplate,
    buys: page.buyRows,
    sells: page.sellRows,
    reporters: reporters
  }

  // handleBook is the handler for the 'book' notification from the server.
  // Updates the charts, order tables, etc.
  const handleBook = (data) => {
    const book = data.book
    const b = tableBuilder
    if (!book) {
      chart.clear()
      Doc.empty(b.buys)
      Doc.empty(b.sells)
      return
    }
    chart.set({
      book: data.book,
      quoteSymbol: selected.quote.symbol,
      baseSymbol: selected.base.symbol
    })
    loadTable(book.buys, b.buys, b, 'buycolor')
    loadTable(book.sells, b.sells, b, 'sellcolor')
  }

  ws.registerRoute('book', data => {
    const [b, q] = [selected.base, selected.quote]
    if (data.base !== b.id || data.quote !== q.id) return
    handleBook(data)
    market = data.market
    page.marketLoader.classList.add('d-none')
    marketRows.forEach(row => {
      if (row.dataset.dex === data.dex && parseInt(row.dataset.base) === data.base && parseInt(row.dataset.quote) === data.quote) {
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
    baseUnits.forEach(el => { el.textContent = b.symbol.toUpperCase() })
    quoteUnits.forEach(el => { el.textContent = q.symbol.toUpperCase() })
    page.baseImg.src = `/img/coins/${b.symbol.toLowerCase()}.png`
    page.quoteImg.src = `/img/coins/${q.symbol.toLowerCase()}.png`
    var bal = b.wallet ? b.wallet.balance / 1e8 : 0
    page.baseBalance.textContent = formatCoinValue(bal)
    bal = q.wallet ? q.wallet.balance / 1e8 : 0
    page.quoteBalance.textContent = formatCoinValue(bal)
  })
  ws.registerRoute('bookupdate', e => {
    if (market && (e.market.dex !== market.dex || e.market.base !== market.base || e.market.quote !== market.quote)) return
    handleBookUpdate(main, e)
  })

  // Fetch the first market in the list, or the users last selected market, if
  // it was found.
  const firstEntry = mktFound ? lastMarket : markets[0]
  reqMarket(firstEntry.dex, firstEntry.base, firstEntry.quote)

  unattach(() => {
    ws.request('unmarket', {})
    ws.deregisterRoute('book')
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
    'openForm', 'unlockLogo', 'unlockName', 'walletPass', 'submitOpen',
    'openErr',
    // Deposit
    'deposit', 'depositName', 'depositAddress',
    // Withdraw
    'withdrawForm', 'withdrawLogo', 'withdrawName', 'withdrawAddr',
    'withdrawAmt', 'withdrawAvail', 'submitWithdraw', 'withdrawFee',
    'withdrawUnit', 'withdrawPW', 'withdrawErr'
  ])

  // Read the document.
  const stateIcon = (row, name) => row.querySelector(`[data-state=${name}]`)
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
    rowInfo.stateIcons = {
      sleeping: stateIcon(tr, 'sleeping'),
      locked: stateIcon(tr, 'locked'),
      unlocked: stateIcon(tr, 'unlocked'),
      nowallet: stateIcon(tr, 'nowallet')
    }
    rowInfo.actions = {
      connect: getAction(tr, 'connect'),
      unlock: getAction(tr, 'unlock'),
      withdraw: getAction(tr, 'withdraw'),
      deposit: getAction(tr, 'deposit'),
      create: getAction(tr, 'create'),
      lock: getAction(tr, 'lock')
    }
  }

  // Prepare asset markets template
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
    displayed.classList.add('d-hide')
  }

  const showBox = async (box, focuser) => {
    box.style.opacity = '0'
    box.classList.remove('d-hide')
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
    await app.userPromise
    const box = page.marketsBox
    const card = page.marketsCard
    const rowInfo = rowInfos[assetID]
    await hideBox()
    Doc.empty(card)
    page.marketsFor.textContent = rowInfo.name
    for (const [url, markets] of Object.entries(app.user.markets)) {
      const count = markets.reduce((a, market) => {
        if (market.baseid === assetID || market.quoteid === assetID) a++
        return a
      }, 0)
      if (count === 0) continue
      const header = page.dexTitle.cloneNode(true)
      header.textContent = new URL(url).host
      card.appendChild(header)
      const marketsBox = page.markets.cloneNode(true)
      card.appendChild(marketsBox)
      for (const market of markets) {
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
      page.walletErr.classList.add('d-hide')
      page.newWalletName.textContent = asset.info.name
    }
    walletAsset = assetID
    page.walletForm.dataset.assetID = assetID
    page.newWalletLogo.src = logoPath(asset.symbol)
    page.newWalletName.textContent = asset.info.name
    animation = showBox(box, page.acctName)
  }

  // Show the form used to unlock a wallet.
  var openAsset
  const showOpen = async assetID => {
    const box = page.openForm
    const asset = app.assets[assetID]
    await hideBox()
    page.openErr.classList.add('d-hide')
    page.unlockLogo.src = logoPath(asset.symbol)
    page.unlockName.textContent = asset.info.name
    page.walletPass.value = ''
    openAsset = assetID
    box.dataset.assetID = assetID
    animation = showBox(box, page.walletPass)
  }

  // Display a deposit address.
  const showDeposit = async assetID => {
    const box = page.deposit
    const asset = app.assets[assetID]
    await app.userPromise
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
    await app.userPromise
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
    const [a, i] = [rowInfo.actions, rowInfo.stateIcons]
    Doc.hide(a.connect, i.sleeping)
    Doc.show(i.locked)
  }

  // Bind the new wallet form.
  var walletAsset
  bindNewWalletForm(page.walletForm, () => {
    const rowInfo = rowInfos[walletAsset]
    showMarkets(rowInfo.ID)
    const [a, i] = [rowInfo.actions, rowInfo.stateIcons]
    Doc.hide(a.create, i.nowallet)
    Doc.show(a.withdraw, a.deposit, a.lock, i.unlocked)
  })

  // Bind the wallet unlock form.
  bindOpenWalletForm(page.openForm, async () => {
    const rowInfo = rowInfos[openAsset]
    const [a, i] = [rowInfo.actions, rowInfo.stateIcons]
    Doc.show(i.unlocked, a.lock, a.withdraw, a.deposit)
    Doc.hide(i.sleeping, i.locked, a.unlock, a.connect)
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
      const [a, i] = [asset.actions, asset.stateIcons]
      Doc.hide(i.unlocked, a.lock, a.withdraw, a.deposit)
      Doc.show(i.locked, a.unlock)
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
  minimumSignificantDigits: 3,
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
    'wClientPass'
  ])
  bindForm(form, fields.submitCreate, async () => {
    Doc.hide(fields.walletErr)
    const create = {
      assetID: parseInt(form.dataset.assetID),
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
    'submitOpen', 'openErr', 'walletPass'
  ])
  bindForm(form, fields.submitOpen, async () => {
    Doc.hide(fields.openErr)
    const open = {
      assetID: parseInt(form.dataset.assetID),
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
