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
    ws.connect(getSocketURI())
    ws.registerRoute(updateWalletRoute, wallet => {
      var i = -1
      var walletList = this.user.wallets
      walletList.forEach((w, j) => {
        if (w.symbol === wallet.symbol) i = j
      })
      if (i > -1) walletList[i] = wallet
      else walletList.push(wallet)
      this.walletMap[wallet.assetID] = wallet
    })
    ws.registerRoute(errorMsgRoute, msg => {
      this.notify(ERROR, msg)
    })
    ws.registerRoute(successMsgRoute, msg => {
      this.notify(SUCCESS, msg)
    })
    this.userPromise = this.fetchUser()
  }

  async fetchUser () {
    const user = await getJSON('/api/user')
    if (!checkResponse(user)) return
    this.user = user
    user.wallets = user.wallets || []
    this.walletMap = user.wallets.reduce((a, v) => {
      a[v.assetID] = v
      return a
    }, {}) || {}
    return user
  }

  // Load the page from the server. Insert and bind to the HTML.
  async loadPage (page) {
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
    this.attach()
    return true
  }

  // attach binds the common and specific handlers to the current main element.
  attach () {
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
    handler(this.main)
  }

  attachHeader (header) {
    this.header = header
    this.noteIndicator = idel(header, 'noteIndicator')
    this.noteIndicator.style.display = 'none'
    this.noteBox = idel(header, 'noteBox')
    this.noteList = idel(header, 'noteList')
    this.noteTemplate = idel(header, 'noteTemplate')
    this.noteTemplate.id = undefined
    this.noteTemplate.remove()
    this.noteMenuEntry = idel(header, 'noteMenuEntry')
    this.settingsIcon = idel(header, 'settingsIcon')
    this.loginLink = idel(header, 'loginLink')
    this.loader = idel(header, 'loader')
    this.loader.remove()
    this.loader.style.backgroundColor = 'rgba(0, 0, 0, 0.5)'
    Doc.show(this.loader)
    var hide
    hide = e => {
      if (!Doc.mouseInElement(e, this.noteBox)) {
        this.noteBox.style.display = 'none'
        unbind(document, hide)
      }
    }
    bind(this.noteMenuEntry, 'click', e => {
      bind(document, 'click', hide)
      this.noteBox.style.display = 'block'
      this.noteIndicator.style.display = 'none'
    })
    if (this.notes.length === 0) {
      this.noteList.textContent = 'no notifications'
    }
  }

  // Use setLogged when user has signed in or out. For logging out, it may be
  // better to trigger a hard reload.
  setLogged (logged) {
    if (logged) {
      Doc.show(this.noteMenuEntry)
      Doc.show(this.settingsIcon)
      Doc.hide(this.loginLink)
      return
    }
    Doc.hide(this.noteMenuEntry)
    Doc.hide(this.settingsIcon)
    Doc.show(this.loginLink)
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
    Doc.empty(this.noteList)
    for (let i = this.notes.length - 1; i >= 0; i--) {
      if (i < this.notes.length - 10) return
      const note = this.notes[i]
      const noteEl = this.makeNote(note.level, note.msg)
      this.noteList.appendChild(noteEl)
    }
    this.notifyUI()
  }

  notifyUI () {
    const ni = this.noteIndicator
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
    const note = this.noteTemplate.cloneNode(true)
    note.querySelector('div').classList.add(level === ERROR ? 'bad' : 'good')
    note.querySelector('span').textContent = msg
    return note
  }

  loading (el) {
    el.appendChild(this.loader)
  }

  loaded () {
    this.loader.remove()
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
    if (e.preventDefault) e.preventDefault()
    page.errMsg.classList.add('d-hide')
    const pw = page.pw.value
    if (pw === '') {
      page.errMsg.textContent = 'password cannot be empty'
      page.errMsg.classList.remove('d-hide')
      return
    }
    var res = await postJSON('/api/login', { pass: pw })
    if (!checkResponse(res)) return
    app.setLogged(true)
    app.loadPage('markets')
  })
}

// handleRegister is the 'register' page main element handler.
function handleRegister (main) {
  const page = parsePage(main, [
    // Form 1: Set the application password
    'appPWForm', 'appPWSubmit', 'appErrMsg', 'appPW', 'appPWAgain',
    // Form 2: Create Decred wallet
    'walletForm', 'iniPath', 'acctName', 'newWalletPass', 'submitCreate',
    'walletErr',
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

  // CREATE DCR WALLET
  // This form is only shown the first time the user visits the /register page.
  bindForm(page.walletForm, page.submitCreate, async () => {
    Doc.hide(page.walletErr)
    const create = {
      assetID: DCR_ID,
      pass: page.newWalletPass.value,
      account: page.acctName.value,
      inipath: page.iniPath.value
    }
    app.loading(page.walletForm)
    var res = await postJSON('/api/newwallet', create)
    app.loaded()
    if (!checkResponse(res)) {
      page.walletErr.textContent = res.msg
      Doc.show(page.walletErr)
      return
    }
    await changeForm(page.walletForm, page.urlForm)
  })

  // OPEN DCR WALLET
  // This form is only show if the wallet is not already open.
  bindForm(page.openForm, page.submitOpen, async () => {
    Doc.hide(page.openErr)
    const open = {
      assetID: DCR_ID,
      pass: page.walletPass.value
    }
    app.loading(page.openForm)
    var res = await postJSON('/api/openwallet', open)
    app.loaded()
    if (!checkResponse(res)) {
      page.openErr.textContent = res.msg
      Doc.show(page.openErr)
      return
    }
    await changeForm(page.openForm, page.urlForm)
  })

  // ENTER NEW DEX URL
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
      pass: page.clientPass.value
    }
    app.loading(page.pwForm)
    var res = await postJSON('/api/register', registration)
    app.loaded()
    if (!checkResponse(res)) {
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      return
    }
    app.notify(SUCCESS, `Account registered for ${dex}. Waiting for confirmations.`)
    app.loadPage('markets')
  })
}

// handleMarkets is the 'markets' page main element handler.
function handleMarkets (main) {
  var market
  const page = parsePage(main, [
    'marketLoader', 'priceBox', 'buyBttn', 'sellBttn', 'baseImg', 'quoteImg',
    'baseBalance', 'quoteBalance', 'limitBttn', 'marketBttn', 'tifBox',
    'marketChart', 'marketList', 'rowTemplate', 'buyRows', 'sellRows'
  ])

  page.rowTemplate.remove()
  delete page.rowTemplate.id
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
  const reportPrice = p => { priceField.value = p.toFixed(8) }
  const reporters = {
    price: reportPrice
  }
  const chart = new DepthChart(page.marketChart, reporters)
  const marketRows = page.marketList.querySelectorAll('.marketrow')
  const markets = []

  // Check if there is a market saved for the user.
  var lastMarket = State.fetch('selectedMarket')
  var mktFound = false
  marketRows.forEach(div => {
    const base = parseInt(div.dataset.base)
    const quote = parseInt(div.dataset.quote)
    const dex = div.dataset.dex
    mktFound = mktFound || (lastMarket && lastMarket.dex === dex &&
      lastMarket.base === base && lastMarket.quote === quote)
    bind(div, 'click', () => {
      page.marketLoader.classList.remove('d-none')
      setMarket(main, dex, base, quote)
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

  ws.registerRoute('book', data => {
    handleBook(main, chart, data, tableBuilder)
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
    baseUnits.forEach(el => { el.textContent = data.baseSymbol.toUpperCase() })
    quoteUnits.forEach(el => { el.textContent = data.quoteSymbol.toUpperCase() })
    page.baseImg.src = `/img/coins/${data.baseSymbol.toLowerCase()}.png`
    page.quoteImg.src = `/img/coins/${data.quoteSymbol.toLowerCase()}.png`
    page.baseBalance.textContent = formatCoinValue(data.baseBalance / 1e8)
    page.quoteBalance.textContent = formatCoinValue(data.quoteBalance / 1e8)
  })
  ws.registerRoute('bookupdate', e => {
    if (market && (e.market.dex !== market.dex || e.market.base !== market.base || e.market.quote !== market.quote)) return
    handleBookUpdate(main, e)
  })

  // Fetch the first market in the list, or the users last selected market, if
  // it was found.
  const firstEntry = mktFound ? lastMarket : markets[0]
  setMarket(main, firstEntry.dex, firstEntry.base, firstEntry.quote)

  unattach(() => {
    ws.request('unmarket', {})
    ws.deregisterRoute('book')
    chart.unattach()
  })
}

// setMarkets sets a new market by sending the 'loadmarket' API request.
function setMarket (main, dex, base, quote) {
  ws.request('loadmarket', {
    dex: dex,
    base: base,
    quote: quote
  })
}

// handleBook is the handler for the 'book' notification from the server.
// Updates the charts, order tables, etc.
function handleBook (main, chart, data, builder) {
  const book = data.book
  if (!book) {
    chart.clear()
    Doc.empty(builder.buys)
    Doc.empty(builder.sells)
    return
  }
  chart.set(data)
  loadTable(book.buys, builder.buys, builder, 'buycolor')
  loadTable(book.sells, builder.sells, builder, 'sellcolor')
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
  console.log('wallets loaded')
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
