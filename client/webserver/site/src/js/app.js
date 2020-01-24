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
    window.history.replaceState({ page: this.main.dataset.handler }, '', window.location.href)
    this.attachHeader(idel(document, 'header'))
    this.attachCommon(this.header)
    this.attach()
    ws.connect(getSocketURI())
    ws.registerRoute(updateWalletRoute, wallet => {
      this.user.wallets[wallet.symbol] = wallet
    })
    ws.registerRoute(errorMsgRoute, msg => {
      this.notify(ERROR, msg)
    })
    ws.registerRoute(successMsgRoute, msg => {
      this.notify(SUCCESS, msg)
    })
    this.fetchUser()
  }

  async fetchUser () {
    const user = await getJSON('/api/user')
    if (!user.isOK) {
      console.error('error fetching user status', user.errMsg)
      return
    }
    this.user = user
  }

  // Load the page from the server. Insert and bind to the HTML.
  async loadPage (page) {
    const response = await window.fetch(`/${page}`)
    if (!response.ok) return false
    const html = await response.text()
    const doc = Doc.noderize(html)
    const main = idel(doc, 'main')
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
    var hide
    hide = e => {
      if (!Doc.mouseInElement(e, this.noteBox)) {
        this.noteBox.style.display = 'none'
        unbind(document, hide)
      }
    }
    bind(idel(header, 'noteMenuEntry'), 'click', e => {
      bind(document, 'click', hide)
      this.noteBox.style.display = 'block'
      this.noteIndicator.style.display = 'none'
    })
    if (this.notes.length === 0) {
      this.noteList.textContent = 'no notifications'
    }
  }

  // attachCommon scans the provided node and handles some common bindings.
  attachCommon (node) {
    node.querySelectorAll('[data-pagelink]').forEach(link => {
      const page = link.dataset.pagelink
      bind(link, 'click', async () => {
        var res = await this.loadPage(page)
        if (res) {
          window.history.pushState({ page: page }, page, `/${page}`)
        }
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
    obj.isOK = true
    return obj
  } catch (response) {
    response.isOK = false
    response.errMsg = await response.text()
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
  const submitBttn = idel(main, 'submit')
  const dexAddr = idel(main, 'dex')
  bind(submitBttn, 'click', async () => {
    const login = {
      dex: dexAddr.value,
      pass: idel(main, 'pw').value
    }
    var res = await postJSON('/api/login', login)
    if (res.isOK) app.loadPage('markets')
  })
}

// handleRegister is the 'register' page main element handler.
function handleRegister (main) {
  bind(idel(main, 'submit'), 'click', async () => {
    const dex = idel(main, 'dex').value
    const registration = {
      dex: dex,
      password: idel(main, 'dexPass').value,
      walletpass: idel(main, 'walletPass').value,
      account: idel(main, 'acct').value,
      inipath: idel(main, 'iniPath').value
    }
    var res = await postJSON('/api/register', registration)
    if (!res.isOK) {
      app.notify(ERROR, res.errMsg)
      return
    }
    // server responded, but response may still indicate an error.
    if (!res.ok) {
      app.notify(ERROR, res.msg)
    }
    app.notify(SUCCESS, `Account registered for ${dex}. Waiting for confirmations.`)
    app.loadPage('markets')
  })
}

// handleLogin is the 'markets' page main element handler.
function handleMarkets (main) {
  var market
  const get = s => idel(main, s)
  const marketLoader = get('marketloader')
  const priceBox = get('priceBox')
  const priceField = priceBox.querySelector('input[type=number]')
  const quoteUnits = main.querySelectorAll('[data-unit=quote]')
  const baseUnits = main.querySelectorAll('[data-unit=base]')
  const buyBttn = get('buyBttn')
  const sellBttn = get('sellBttn')
  const swapBttns = (before, now) => {
    before.classList.remove('selected')
    now.classList.add('selected')
  }
  bind(buyBttn, 'click', () => swapBttns(sellBttn, buyBttn))
  bind(sellBttn, 'click', () => swapBttns(buyBttn, sellBttn))
  const limitBttn = get('limitBttn')
  const marketBttn = get('marketBttn')
  const tifDiv = get('tifBox')
  // const tifCheck = tifDiv.querySelector('input[type=checkbox]')
  bind(limitBttn, 'click', () => {
    priceBox.classList.remove('d-hide')
    tifDiv.classList.remove('d-hide')
    swapBttns(marketBttn, limitBttn)
  })
  bind(marketBttn, 'click', () => {
    priceBox.classList.add('d-hide')
    tifDiv.classList.add('d-hide')
    swapBttns(limitBttn, marketBttn)
  })
  const baseImg = get('baseImg')
  const quoteImg = get('quoteImg')
  const baseBalance = get('baseBalance')
  const quoteBalance = get('quoteBalance')
  const reportPrice = p => { priceField.value = p.toFixed(8) }
  const reporters = {
    price: reportPrice
  }
  const chart = new DepthChart(get('marketchart'), reporters)
  const marketList = get('marketList') // includes dex headers
  const marketRows = marketList.querySelectorAll('.marketrow')
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
      marketLoader.classList.remove('d-none')
      setMarket(main, dex, base, quote)
    })
    markets.push({
      base: base,
      quote: quote,
      dex: dex
    })
  })

  // Get the order row to use as a template.
  const rowTemplate = get('orderRow')
  rowTemplate.remove()
  delete rowTemplate.id
  const tableBuilder = {
    row: rowTemplate,
    buys: get('buyRows'),
    sells: get('sellRows'),
    reporters: reporters
  }

  ws.registerRoute('book', data => {
    handleBook(main, chart, data, tableBuilder)
    market = data.market
    marketLoader.classList.add('d-none')
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
    baseImg.src = `/img/coins/${data.baseSymbol.toLowerCase()}.png`
    quoteImg.src = `/img/coins/${data.quoteSymbol.toLowerCase()}.png`
    baseBalance.textContent = formatCoinValue(data.baseBalance / 1e8)
    quoteBalance.textContent = formatCoinValue(data.quoteBalance / 1e8)
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
