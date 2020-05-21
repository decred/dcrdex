import Doc, { WalletIcons, BipIDs } from './doc'
import State from './state'
import BasePage from './basepage'
import OrderBook from './orderbook'
import { DepthChart } from './charts'
import { postJSON } from './http'
import * as forms from './forms'
import ws from './ws'

var app
const bind = Doc.bind

const LIMIT = 1
// const MARKET = 2
// const CANCEL = 3

const bookRoute = 'book'
const bookOrderRoute = 'book_order'
const unbookOrderRoute = 'unbook_order'
const updateRemainingRoute = 'update_remaining'
const epochOrderRoute = 'epoch_order'
const bookUpdateRoute = 'bookupdate'
const unmarketRoute = 'unmarket'

const animationLength = 500

const check = document.createElement('span')
check.classList.add('ico-check')

export default class MarketsPage extends BasePage {
  constructor (application, main, data) {
    super()
    app = application
    const page = this.page = Doc.parsePage(main, [
      // Templates, loaders, chart div...
      'marketLoader', 'marketChart', 'marketList', 'rowTemplate', 'buyRows',
      'sellRows',
      // Order form.
      'orderForm', 'priceBox', 'buyBttn', 'sellBttn', 'baseBalance',
      'quoteBalance', 'limitBttn', 'marketBttn', 'tifBox', 'submitBttn',
      'qtyField', 'rateField', 'orderErr', 'baseBox', 'quoteBox', 'baseImg',
      'quoteImg', 'baseNewButton', 'quoteNewButton', 'baseBalSpan',
      'quoteBalSpan', 'lotSize', 'rateStep', 'lotField', 'tifNow', 'mktBuyBox',
      'mktBuyLots', 'mktBuyField', 'minMktBuy', 'qtyBox', 'loaderMsg',
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
    this.market = null
    this.currentForm = null
    this.openAsset = null
    this.openFunc = null
    this.currentCreate = null
    this.book = null
    this.orderRows = {}
    this.marketRows = page.marketList.querySelectorAll('.marketrow')
    this.markets = []
    const reporters = {
      price: p => { this.reportPrice(p) }
    }
    this.chart = new DepthChart(page.marketChart, reporters)

    // Prepare templates for the buy and sell tables and the user's order table.
    page.rowTemplate.remove()
    page.rowTemplate.removeAttribute('id')
    page.liveTemplate.removeAttribute('id')
    page.liveTemplate.remove()

    // Store the elements that need their ticker changed when the market
    // changes.
    this.quoteUnits = main.querySelectorAll('[data-unit=quote]')
    this.baseUnits = main.querySelectorAll('[data-unit=base]')

    // The wallet-control icons.
    this.walletIcons = {
      base: new WalletIcons(page.baseBox),
      quote: new WalletIcons(page.quoteBox)
    }

    // Buttons to set order type and side.
    bind(page.buyBttn, 'click', () => {
      swapBttns(page.sellBttn, page.buyBttn)
      this.setOrderVisibility()
    })
    bind(page.sellBttn, 'click', () => {
      swapBttns(page.buyBttn, page.sellBttn)
      this.setOrderVisibility()
    })
    // const tifCheck = tifDiv.querySelector('input[type=checkbox]')
    bind(page.limitBttn, 'click', () => {
      swapBttns(page.marketBttn, page.limitBttn)
      this.setOrderVisibility()
    })
    bind(page.marketBttn, 'click', () => {
      swapBttns(page.limitBttn, page.marketBttn)
      this.setOrderVisibility()
    })

    document.getElementById('rateField').addEventListener('wheel', myFunction);

    function disableNumberWheel() {
      this.style.fontSize = "35px";
    }

    // Scan the rows in the market table and pull some basic info.
    var lastMarket = (data && data.market) ? data.market : State.fetch('selectedMarket')
    var mktFound = false
    this.marketRows.forEach(div => {
      const base = parseInt(div.dataset.base)
      const quote = parseInt(div.dataset.quote)
      const dex = div.dataset.dex
      mktFound = mktFound || (lastMarket && lastMarket.dex === dex &&
        lastMarket.base === base && lastMarket.quote === quote)
      // Clicking on the row will load a new market.
      bind(div, 'click', () => {
        page.marketLoader.classList.remove('d-none')
        this.setMarket(dex, base, quote)
      })
      this.markets.push({
        base: base,
        quote: quote,
        dex: dex
      })
    })

    // Handle the full orderbook sent on the 'book' route.
    ws.registerRoute(bookRoute, data => { this.handleBookRoute(data) })
    // Handle the new order for the order book on the 'book_order' route.
    ws.registerRoute(bookOrderRoute, data => { this.handleBookOrderRoute(data) })
    // Remove the order sent on the 'unbook_order' route from the orderbook.
    ws.registerRoute(unbookOrderRoute, data => { this.handleUnbookOrderRoute(data) })
    // Update the remaining quantity on a booked order.
    ws.registerRoute(updateRemainingRoute, data => { this.handleUpdateRemainingRoute(data) })
    // Handle the new order for the order book on the 'epoch_order' route.
    ws.registerRoute(epochOrderRoute, data => { this.handleEpochOrderRoute(data) })
    // Bind the wallet unlock form.
    forms.bindOpenWallet(app, page.openForm, async () => { this.openFunc() })
    // Create a wallet
    forms.bindNewWallet(app, page.walletForm, async () => { this.createWallet() })
    // Main order form
    forms.bind(page.orderForm, page.submitBttn, async () => { this.stepSubmit() })
    // Order verification form
    forms.bind(page.verifyForm, page.vSubmit, async () => { this.submitOrder() })

    // If the user clicks outside of a form, it should close the page overlay.
    bind(page.forms, 'mousedown', e => {
      if (!Doc.mouseInElement(e, this.currentForm)) Doc.hide(page.forms)
    })

    // Wallet button callbacks.
    bind(page.baseNewButton, 'click', () => { this.showCreate(this.market.base) })
    bind(page.quoteNewButton, 'click', () => { this.showCreate(this.market.quote) })
    const i = this.walletIcons
    bind(i.base.icons.locked, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
    bind(i.quote.icons.locked, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })
    bind(i.base.icons.sleeping, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
    bind(i.quote.icons.sleeping, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })

    // Event listeners for interactions with the various input fields.
    bind(page.lotField, 'change', () => { this.lotChanged() })
    bind(page.lotField, 'keyup', () => { this.lotChanged() })
    bind(page.qtyField, 'change', () => { this.quantityChanged(true) })
    bind(page.qtyField, 'keyup', () => { this.quantityChanged(false) })
    bind(page.mktBuyField, 'change', () => { this.marketBuyChanged() })
    bind(page.mktBuyField, 'keyup', () => { this.marketBuyChanged() })
    bind(page.rateField, 'change', () => { this.rateFieldChanged() })

    // Notification filters.
    this.notifiers = {
      order: note => { this.handleOrderNote(note) },
      epoch: note => { this.handleEpochNote(note) }
    }

    // Fetch the first market in the list, or the users last selected market, if
    // it was found.
    const firstEntry = mktFound ? lastMarket : this.markets[0]
    this.setMarket(firstEntry.dex, firstEntry.base, firstEntry.quote)
  }

  /* isSell is true if the user has selected sell in the order options. */
  isSell () {
    return this.page.sellBttn.classList.contains('selected')
  }

  /* isLimit is true if the user has selected the "limit order" tab. */
  isLimit () {
    return this.page.limitBttn.classList.contains('selected')
  }

  /*
   * setOrderVisibility sets which form is visible based on the specified
   * options.
   */
  setOrderVisibility () {
    const page = this.page
    if (this.isLimit()) {
      Doc.show(page.priceBox, page.tifBox, page.qtyBox)
      Doc.hide(page.mktBuyBox)
    } else {
      Doc.hide(page.priceBox)
      if (this.isSell()) {
        Doc.hide(page.mktBuyBox)
        Doc.show(page.qtyBox)
      } else {
        Doc.show(page.mktBuyBox)
        Doc.hide(page.qtyBox)
      }
    }
  }

  /* showLoaderMsg shows a message at loaderMsg and hides the orderForm in case
  * dexc does not support an asset.
  */
  showLoaderMsg (symbol) {
    const page = this.page
    Doc.hide(page.orderForm)
    Doc.show(page.loaderMsg)
    page.loaderMsg.textContent = `${symbol.toUpperCase()} is not supported`
  }

  /* hideLoaderMsg hides the loaderMsg and shows the orderForm back again */
  hideLoaderMsg () {
    const page = this.page
    Doc.show(page.orderForm)
    Doc.hide(page.loaderMsg)
  }

  /* setMarket sets the currently displayed market. */
  async setMarket (url, base, quote) {
    const dex = app.user.exchanges[url]
    this.market = {
      dex: dex,
      sid: marketID(base, quote), // A string market identifier used by the DEX.
      // app.assets is a map of core.SupportedAsset type, which can be found at
      // client/core/types.go.
      base: app.assets[base],
      quote: app.assets[quote],
      // dex.assets is a map of dex.Asset type, which is defined at
      // dex/asset.go.
      baseCfg: dex.assets[base],
      quoteCfg: dex.assets[quote]
    }
    ws.request('loadmarket', makeMarket(url, base, quote))

    const [b, q] = [this.market.base, this.market.quote]
    if (b && q) {
      return this.hideLoaderMsg()
    }
    if (!b) {
      this.showLoaderMsg(this.market.baseCfg.symbol)
    }
    if (!q) {
      this.showLoaderMsg(this.market.quoteCfg.symbol)
    }
  }

  /*
   * reportPrice is a callback used by the DepthChart when the user clicks
   * on the chart area. The rate field is set to the x-value of the click.
   */
  reportPrice (p) {
    this.page.rateField.value = p.toFixed(8)
    this.rateFieldChanged()
  }

  /*
   * parseOrder pulls the order information from the form fields. Data is not
   * validated in any way.
   */
  parseOrder () {
    const page = this.page
    let qtyField = page.qtyField
    const limit = this.isLimit()
    const sell = this.isSell()
    const market = this.market
    if (!limit && !sell) {
      qtyField = page.mktBuyField
    }
    return {
      dex: market.dex.url,
      isLimit: limit,
      sell: sell,
      base: market.base.id,
      quote: market.quote.id,
      qty: asAtoms(qtyField.value),
      rate: asAtoms(page.rateField.value), // message-rate
      tifnow: page.tifNow.checked
    }
  }

  /*
   * validateOrder performs some basic order sanity checks, returning boolean
   * true if the order appears valid.
   */
  validateOrder (order) {
    const page = this.page
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

  /* handleBook accepts the data sent in the 'book' notification. */
  handleBook (data) {
    this.book = new OrderBook(data)
    this.loadTable()
    for (const order of (data.book.epoch || [])) {
      if (order.rate > 0) this.book.add(order)
      this.addTableOrder(order)
    }
    if (!this.book) {
      this.chart.clear()
      Doc.empty(this.page.buyRows)
      Doc.empty(this.page.sellRows)
      return
    }
    this.chart.set(this.book)
  }

  /*
   * midGap returns the value in the middle of the best buy and best sell. If
   * either one of the buy or sell sides are empty, midGap returns the best
   * rate from the other side. If both sides are empty, midGap returns the
   * value null.
   */
  midGap () {
    const book = this.book
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

  /*
   * setMarketBuyOrderEstimate sets the "min. buy" display for the current
   * market.
   */
  setMarketBuyOrderEstimate () {
    const market = this.market
    const lotSize = market.baseCfg.lotSize
    const xc = app.user.exchanges[market.dex.url]
    const buffer = xc.markets[market.sid].buybuffer
    const gap = this.midGap()
    if (gap) {
      this.page.minMktBuy.textContent = Doc.formatCoinValue(lotSize * buffer * gap / 1e8)
    }
  }

  /* setBalance sets the balance display. */
  setBalance (a, row, img, button, bal) {
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

  /*
   * updateWallet updates the displayed wallet information based on the
   * core.Wallet state.
   */
  updateWallet (assetID) {
    const page = this.page
    const market = this.market
    const asset = app.assets[assetID]
    const [b, q] = [market.base, market.quote]
    switch (assetID) {
      case (market.baseCfg.id):
        if (!b) {
          Doc.hide(page.baseBox)
          return
        }
        Doc.show(page.baseBox)
        this.setBalance(asset, page.baseBalSpan, page.baseImg, page.baseNewButton, page.baseBalance)
        this.walletIcons.base.readWallet(asset.wallet)
        break
      case (market.quoteCfg.id):
        if (!q) {
          Doc.hide(page.quoteBox)
          return
        }
        Doc.show(page.quoteBox)
        this.setBalance(asset, page.quoteBalSpan, page.quoteImg, page.quoteNewButton, page.quoteBalance)
        this.walletIcons.quote.readWallet(asset.wallet)
    }
  }

  /* refreshActiveOrders refreshes the user's active order list. */
  refreshActiveOrders () {
    const page = this.page
    const orderRows = this.orderRows
    const market = this.market
    for (const oid in orderRows) delete orderRows[oid]
    const orders = app.orders(market.dex.url, market.baseCfg.id, market.quoteCfg.id)
    Doc.empty(page.liveList)
    for (const order of orders) {
      const row = page.liveTemplate.cloneNode(true)
      orderRows[order.id] = row
      const set = (col, s) => { row.querySelector(`[data-col=${col}]`).textContent = s }
      set('side', order.sell ? 'sell' : 'buy')
      set('age', Doc.timeSince(order.stamp))
      set('rate', Doc.formatCoinValue(order.rate / 1e8))
      set('qty', Doc.formatCoinValue(order.qty / 1e8))
      set('filled', `${(order.filled / order.qty * 100).toFixed(1)}%`)
      if (order.type === LIMIT) {
        if (order.cancelling) {
          set('cancel', order.canceled ? 'canceled' : 'cancelling')
        } else if (order.filled !== order.qty) {
          const icon = row.querySelector('[data-col=cancel] > span')
          Doc.show(icon)
          bind(icon, 'click', e => {
            e.stopPropagation()
            this.showCancel(icon, order)
          })
        }
      }
      page.liveList.appendChild(row)
    }
  }

  /* handleBookRoute is the handler for the 'book' notification, which is sent
   * in response to a new market subscription. The data received will contain
   * the entire order book.
   */
  handleBookRoute (data) {
    const market = this.market
    const page = this.page
    const url = market.dex.url
    const [b, q] = [market.baseCfg, market.quoteCfg]
    if (data.base !== b.id || data.quote !== q.id) return
    this.handleBook(data)
    page.marketLoader.classList.add('d-none')
    this.marketRows.forEach(row => {
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
    page.lotSize.textContent = Doc.formatCoinValue(market.baseCfg.lotSize / 1e8)
    page.rateStep.textContent = Doc.formatCoinValue(market.quoteCfg.rateStep / 1e8)
    this.baseUnits.forEach(el => { el.textContent = b.symbol.toUpperCase() })
    this.quoteUnits.forEach(el => { el.textContent = q.symbol.toUpperCase() })
    this.updateWallet(b.id)
    this.updateWallet(q.id)
    this.setMarketBuyOrderEstimate()
    this.refreshActiveOrders()
  }

  /* handleBookOrderRoute is the handler for 'book_order' notifications. */
  handleBookOrderRoute (data) {
    const order = data.payload
    if (order.rate > 0) this.book.add(order)
    this.addTableOrder(order)
    this.chart.draw()
  }

  /* handleUnbookOrderRoute is the handler for 'unbook_order' notifications. */
  handleUnbookOrderRoute (data) {
    const order = data.payload
    this.book.remove(order.token)
    this.removeTableOrder(order)
    this.chart.draw()
  }

  /*
   * handleUpdateRemainingRoute is the handler for 'update_remaining'
   * notifications.
   */
  handleUpdateRemainingRoute (data) {
    const update = data.payload
    this.book.updateRemaining(update.token, update.qty)
    this.updateTableOrder(update)
    this.chart.draw()
  }

  /* handleEpochOrderRoute is the handler for 'epoch_order' notifications. */
  handleEpochOrderRoute (data) {
    const order = data.payload
    if (order.rate > 0) this.book.add(order)
    this.addTableOrder(order)
    this.chart.draw()
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form) {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.openForm, page.verifyForm, page.walletForm, page.cancelForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0px'
  }

  /* showOpen shows the form to unlock a wallet. */
  async showOpen (asset, f) {
    const page = this.page
    this.openAsset = asset
    this.openFunc = f
    page.openForm.setAsset(app.assets[asset.id])
    this.showForm(page.openForm)
    page.walletPass.focus()
  }

  /* showVerify shows the form to accept the currently parsed order information
   * and confirm submission of the order to the dex.
   */
  showVerify () {
    const page = this.page
    const order = this.parseOrder()
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
      const gap = this.midGap()
      if (gap) {
        const received = order.sell ? order.qty * gap : order.qty / gap
        const lotSize = this.market.baseCfg.lotSize
        const lots = order.sell ? order.qty / lotSize : received / lotSize
        // TODO: Some kind of adjustment to align with lot sizes for market buy?
        page.vmTotal.textContent = Doc.formatCoinValue(received / 1e8)
        page.vmAsset.textContent = toAsset.symbol.toUpperCase()
        page.vmLots.textContent = lots.toFixed(1)
      } else {
        Doc.hide(page.verifyMarket)
      }
    }
    this.showForm(page.verifyForm)
  }

  /* showCancel shows a form to confirm submission of a cancel order. */
  showCancel (bttn, order) {
    const page = this.page
    const remaining = order.qty - order.filled
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining / 1e8)
    const isMarketBuy = !order.isLimit && !order.sell
    const symbol = isMarketBuy ? this.market.quote.symbol : this.market.base.symbol
    page.cancelUnit.textContent = symbol.toUpperCase()
    this.showForm(page.cancelForm)
    bind(page.cancelSubmit, 'click', async () => {
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

  /* showCreate shows the new wallet creation form. */
  showCreate (asset) {
    const page = this.page
    this.currentCreate = asset
    page.walletForm.setAsset(asset)
    this.showForm(page.walletForm)
    page.acctName.focus()
  }

  /*
   * stepSubmit will examine the current state of wallets and step the user
   * through the process of order submission.
   * NOTE: I expect this process will be streamlined soon such that the wallets
   * will attempt to be unlocked in the order submission process, negating the
   * need to unlock ahead of time.
   */
  stepSubmit () {
    const page = this.page
    const market = this.market
    Doc.hide(page.orderErr)
    if (!this.validateOrder(this.parseOrder())) return
    const baseWallet = app.walletMap[market.base.id]
    const quoteWallet = app.walletMap[market.quote.id]
    if (!baseWallet) {
      page.orderErr.textContent = `No ${market.base.symbol} wallet`
      Doc.show(page.orderErr)
      return
    }
    if (!quoteWallet) {
      page.orderErr.textContent = `No ${market.quote.symbol} wallet`
      Doc.show(page.orderErr)
      return
    }
    this.showVerify()
  }

  /*
   * handleOrderNote is the handler for the 'order'-type notification, which are
   * used to update an order's status.
   */
  handleOrderNote (note) {
    const order = note.order
    if (order.targetID && note.subject === 'cancel') {
      this.orderRows[order.targetID].querySelector('[data-col=cancel]').textContent = 'canceled'
    } else {
      const row = this.orderRows[order.id]
      if (!row) return
      const td = row.querySelector('[data-col=filled]')
      td.textContent = `${(order.filled / order.qty * 100).toFixed(1)}%`
      if (order.filled === order.qty) {
        // Remove the cancellation button.
        row.querySelector('[data-col=cancel]').textContent = ''
      }
    }
  }

  /*
   * handleEpochNote handles notifications signalling the start of a new epoch.
   */
  handleEpochNote (note) {
    if (this.book) {
      this.book.setEpoch(note.epoch)
      this.chart.draw()
    }
    this.clearOrderTableEpochs(note.epoch)
    this.chart.draw()
  }

  /*
   * submitOrder is attached to the affirmative button on the order validation
   * form. Clicking the button is the last step in the order submission process.
   */
  async submitOrder () {
    const page = this.page
    const market = this.market
    Doc.hide(page.forms)
    const order = this.parseOrder()
    const pw = page.vPass.value
    page.vPass.textContent = ''
    const req = {
      order: order,
      pw: pw
    }
    if (!this.validateOrder(order)) return
    var res = await postJSON('/api/trade', req)
    app.loaded()
    if (!app.checkResponse(res)) return
    // If the wallets are not open locally, they must have been opened during
    // ordering. Grab updated info.
    const baseWallet = app.walletMap[market.base.id]
    const quoteWallet = app.walletMap[market.quote.id]
    if (!baseWallet.open || !quoteWallet.open) {
      await app.fetchUser()
      this.updateWallet(market.base.id)
      this.updateWallet(market.quote.id)
    }
    app.orders(order.dex, order.base, order.quote).push(res.order)
    this.refreshActiveOrders()
  }

  /*
   * createWallet is attached to successful submission of the wallet creation
   * form. createWallet is only called once the form is submitted and a success
   * response is received from the client.
   */
  async createWallet () {
    const user = await app.fetchUser()
    const asset = user.assets[this.currentCreate.id]
    Doc.hide(this.page.forms)
    this.updateWallet(asset.id)
  }

  /*
   * walletUnlocked is attached to successful submission of the wallet unlock
   * form. walletUnlocked is only called once the form is submitted and a
   * success response is received from the client.
   */
  async walletUnlocked () {
    Doc.hide(this.page.forms)
    await app.fetchUser()
    this.updateWallet(this.openAsset.id)
  }

  /* lotChanged is attached to the keyup and change events of the lots input. */
  lotChanged () {
    const page = this.page
    const lots = parseInt(page.lotField.value)
    if (lots <= 0) {
      page.lotField.value = 0
      page.qtyField.value = ''
      return
    }
    const lotSize = this.market.baseCfg.lotSize
    page.lotField.value = lots
    page.qtyField.value = (lots * lotSize / 1e8)
  }

  /*
   * quantityChanged is attached to the keyup and change events of the quantity
   * input.
   */
  quantityChanged (finalize) {
    const page = this.page
    const order = this.parseOrder()
    if (order.qty <= 0) {
      page.lotField.value = 0
      page.qtyField.value = ''
      return
    }
    const lotSize = this.market.baseCfg.lotSize
    const lots = Math.floor(order.qty / lotSize)
    const adjusted = lots * lotSize
    page.lotField.value = lots
    if (!order.isLimit && !order.sell) return
    if (finalize) page.qtyField.value = (adjusted / 1e8)
  }

  /*
   * marketBuyChanged is attached to the keyup and change events of the quantity
   * input for the market-buy form.
   */
  marketBuyChanged () {
    const page = this.page
    const qty = asAtoms(page.mktBuyField.value)
    const gap = this.midGap()
    if (!gap || !qty) {
      page.mktBuyLots.textContent = '0'
      page.mktBuyScore.textContent = '0'
      return
    }
    const lotSize = this.market.baseCfg.lotSize
    const received = qty / gap
    page.mktBuyLots.textContent = (received / lotSize).toFixed(1)
    page.mktBuyScore.textContent = Doc.formatCoinValue(received / 1e8)
  }

  /*
   * rateFieldChanged is attached to the keyup and change events of the rate
   * input.
   */
  rateFieldChanged () {
    const order = this.parseOrder()
    if (order.rate <= 0) {
      this.page.rateField.value = 0
      return
    }
    // Truncate to rate step. If it is a market buy order, do not adjust.
    const rateStep = this.market.quoteCfg.rateStep
    const adjusted = order.rate - (order.rate % rateStep)
    this.page.rateField.value = (adjusted / 1e8)
  }

  /* loadTable reloads the table from the current order book information. */
  loadTable () {
    this.loadTableSide(true)
    this.loadTableSide(false)
  }

  /* loadTables loads the order book side into its table. */
  loadTableSide (sell) {
    const bookSide = sell ? this.book.sells : this.book.buys
    const tbody = sell ? this.page.sellRows : this.page.buyRows
    const cssClass = sell ? 'sellcolor' : 'buycolor'
    Doc.empty(tbody)
    if (!bookSide || !bookSide.length) return
    bookSide.forEach(order => { tbody.appendChild(this.orderTableRow(order, cssClass)) })
  }

  /* addTableOrder adds a single order to the appropriate table. */
  addTableOrder (order) {
    const tbody = order.sell ? this.page.sellRows : this.page.buyRows
    const cssClass = order.sell ? 'sellcolor' : 'buycolor'
    var row = tbody.firstChild
    // Handle market order differently.
    if (order.rate === 0) {
      // This is a market order.
      if (!row || row.order.rate !== 0) {
        row = this.orderTableRow(order, cssClass)
        tbody.insertBefore(row, tbody.firstChild)
      }
      row.addQty(order.qty)
      return
    }
    // Must be a limit order. Sort by rate. Skip the market order row.
    if (row && row.order.rate === 0) row = row.nextSibling
    const tr = this.orderTableRow(order, cssClass)
    while (row) {
      if ((order.rate < row.order.rate) === order.sell) {
        tbody.insertBefore(tr, row)
        return
      }
      row = row.nextSibling
    }
    tbody.appendChild(tr)
  }

  /* removeTableOrder removes a single order from its table. */
  removeTableOrder (order) {
    const tbody = order.sell ? this.page.sellRows : this.page.buyRows
    const token = order.token
    for (const tr of Array.from(tbody.children)) {
      if (tr.order.token === token) {
        tr.remove()
        return
      }
    }
  }

  /* updateTableOrder looks for the order in the table and updates the qty */
  updateTableOrder (update) {
    const token = update.token
    for (const tbody of [this.page.sellRows, this.page.buyRows]) {
      for (const tr of Array.from(tbody.children)) {
        if (tr.order.token === token) {
          const td = tr.querySelector('[data-type=qty]')
          td.innerText = update.qty.toFixed(8)
          return
        }
      }
    }
  }

  /*
   * clearOrderTableEpochs removes immediate-tif orders whose epoch has expired.
   */
  clearOrderTableEpochs (newEpoch) {
    this.clearOrderTableEpochSide(this.page.sellRows)
    this.clearOrderTableEpochSide(this.page.buyRows)
  }

  /*
   * clearOrderTableEpochs removes immediate-tif orders whose epoch has expired
   * for a single side.
   */
  clearOrderTableEpochSide (tbody, newEpoch) {
    for (const tr of Array.from(tbody.children)) {
      if (tr.order.epoch && tr.order.epoch !== newEpoch) tr.remove()
    }
  }

  /*
   * orderTableRow creates a new <tr> element to insert into an order table.
   */
  orderTableRow (order, cssClass) {
    const tr = this.page.rowTemplate.cloneNode(true)
    tr.qty = order.qty
    tr.order = order
    const rate = order.rate
    bind(tr, 'click', () => {
      this.reportPrice(rate)
    })
    var qtyTD
    tr.querySelectorAll('td').forEach(td => {
      switch (td.dataset.type) {
        case 'qty':
          qtyTD = td
          td.innerText = order.qty.toFixed(8)
          break
        case 'rate':
          if (order.rate === 0) {
            td.innerText = 'market'
          } else {
            td.innerText = order.rate.toFixed(8)
            td.classList.add(cssClass)
          }
          break
        case 'epoch':
          if (order.epoch) td.appendChild(check.cloneNode())
          break
      }
    })
    tr.addQty = (qty) => {
      tr.qty += qty
      qtyTD.innerText = tr.qty.toFixed(8)
    }
    return tr
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /markets page.
   */
  unload () {
    ws.request(unmarketRoute, {})
    ws.deregisterRoute(bookRoute)
    ws.deregisterRoute(bookUpdateRoute)
    ws.deregisterRoute(epochOrderRoute)
    ws.deregisterRoute(bookOrderRoute)
    ws.deregisterRoute(unbookOrderRoute)
    this.chart.unattach()
  }
}

/* makeMarket creates a market object that specifies basic market details. */
function makeMarket (dex, base, quote) {
  return {
    dex: dex,
    base: base,
    quote: quote
  }
}

/* marketID creates a DEX-compatible market name from the BIP IDs. */
export function marketID (b, q) { return `${BipIDs[b]}_${BipIDs[q]}` }

/* asAtoms converts the float string to atoms. */
function asAtoms (s) {
  return Math.round(parseFloat(s) * 1e8)
}

/* swapBttns changes the 'selected' class of the buttons. */
function swapBttns (before, now) {
  before.classList.remove('selected')
  now.classList.add('selected')
}
