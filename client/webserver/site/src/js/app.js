import Doc from './doc'
import State from './state'
import { DepthChart } from './charts'
import ws from './ws'

const idel = Doc.idel // = element by id
const bind = Doc.bind // = addEventHandler
var app

// Application is the main javascript web application for the Decred DEX client.
export default class Application {
  start () {
    app = this
    bind(window, 'popstate', (e) => {
      const page = e.state.page
      if (!page && page !== '') return
      this.loadPage(page)
    })
    this.main = idel(document, 'main')
    window.history.replaceState({ page: this.main.dataset.handler }, '', window.location.href)
    this.attachCommon(idel(document, 'header'))
    this.attach()
    ws.connect(`ws://${window.location.host}/ws`)
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

  // attachCommon scans the provided node and handles some common bindings.
  attachCommon (node) {
    node.querySelectorAll('[data-pagelink]').forEach(link => {
      let page = link.dataset.pagelink
      bind(link, 'click', async () => {
        var res = await this.loadPage(page)
        if (res) {
          window.history.pushState({ page: page }, page, `/${page}`)
        }
      })
    })
  }
}

// postJSON encodes the object and sends the JSON to the specified address.
async function postJSON (addr, data) {
  const response = await window.fetch(addr, {
    method: 'POST',
    headers: new window.Headers({ 'content-type': 'application/json' }),
    body: JSON.stringify(data)
  })
  return response.json()
}

// unattachers are handlers to be run when a page is unloaded.
var unattachers = []

// unnatach adds an unnatacher to the array.
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
    if (res.ok) app.loadPage('markets')
  })
}

// handleRegister is the 'register' page main element handler.
function handleRegister (main) {
  const submitBttn = idel(main, 'submit')
  const dexAddr = idel(main, 'dex')
  const wallet = idel(main, 'feeWallet')
  bind(submitBttn, 'click', async () => {
    const registration = {
      dex: dexAddr.value,
      wallet: wallet.value,
      rpcaddr: idel(main, 'rpcAddr').value,
      rpcuser: idel(main, 'rpcUser').value,
      rpcpass: idel(main, 'rpcPw').value,
      walletpass: idel(main, 'walletPw').value
    }
    var res = await postJSON('/api/register', registration)
    console.log(res)
  })
}

// handleLogin is the 'markets' page main element handler.
function handleMarkets (main) {
  var market = ''
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
    let mkt = div.dataset.mkt
    let dex = div.dataset.dex
    mktFound = mktFound || (lastMarket && lastMarket.dex === dex &&
      lastMarket.market === mkt)
    bind(div, 'click', () => {
      marketLoader.classList.remove('d-none')
      setMarket(main, dex, mkt)
    })
    markets.push({
      market: mkt,
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

  ws.registerEvtHandler('book', data => {
    // if (e.market !== market && e.market !== '') return
    handleBook(main, chart, data, tableBuilder)
    market = data.market
    marketLoader.classList.add('d-none')
    marketRows.forEach(row => {
      if (row.dataset.dex === data.dex && row.dataset.mkt === market) {
        row.classList.add('selected')
      } else {
        row.classList.remove('selected')
      }
    })
    State.store('selectedMarket', {
      dex: data.dex,
      market: market
    })
    baseUnits.forEach(el => { el.textContent = data.base })
    quoteUnits.forEach(el => { el.textContent = data.quote })
    baseImg.src = `/img/coins/${data.base.toLowerCase()}.png`
    quoteImg.src = `/img/coins/${data.quote.toLowerCase()}.png`
    baseBalance.textContent = formatCoinValue(data.baseBalance)
    quoteBalance.textContent = formatCoinValue(data.quoteBalance)
  })
  ws.registerEvtHandler('bookupdate', e => {
    if (e.market !== market && e.market !== '') return
    handleBookUpdate(main, e)
  })

  // Fetch the first market in the list, or the users last selected market, if
  // it was found.
  const firstEntry = mktFound ? lastMarket : markets[0]
  setMarket(main, firstEntry.dex, firstEntry.market)

  unattach(() => {
    ws.request('unmarket', {})
    ws.deregisterEvtHandlers('book')
    chart.unattach()
  })
}

// setMarkets sets a new market by sending the 'loadmarket' API request.
function setMarket (main, dex, mkt) {
  ws.request('loadmarket', {
    dex: dex,
    market: mkt
  })
}

// handleBook is the handler for the 'book' notification from the server.
// Updates the charts, order tables, etc.
function handleBook (main, chart, data, builder) {
  chart.set(data)
  const book = data.book
  loadTable(book.buys, builder.buys, builder)
  loadTable(book.sells, builder.sells, builder)
}

// loadTables loads the order book side into the specified table.
function loadTable (bookSide, table, builder) {
  while (table.firstChild) table.removeChild(table.firstChild)
  const check = document.createElement('span')
  check.classList.add('ico-check')
  bookSide.forEach(order => {
    const tr = builder.row.cloneNode(true)
    let rate = order.rate
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
  console.log('settings loaded')
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
