import Doc, { WalletIcons } from './doc'
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
const MARKET = 2
// const CANCEL = 3

const bookRoute = 'book'
const bookOrderRoute = 'book_order'
const unbookOrderRoute = 'unbook_order'
const updateRemainingRoute = 'update_remaining'
const epochOrderRoute = 'epoch_order'
const bookUpdateRoute = 'bookupdate'
const unmarketRoute = 'unmarket'

const lastMarketKey = 'selectedMarket'

/* The match statuses are a mirror of dex/order.MatchStatus. */
// const newlyMatched = 0
// const makerSwapCast = 1
// const takerSwapCast = 2
const makerRedeemed = 3
// const matchComplete = 4

/* The time-in-force specifiers are a mirror of dex/order.TimeInForce. */
const immediateTiF = 0
// const standingTiF = 1

/* The order statuses are a mirror of dex/order.OrderStatus. */
const statusUnknown = 0
const statusEpoch = 1
const statusBooked = 2
const statusExecuted = 3
const statusCanceled = 4
const statusRevoked = 5

const animationLength = 500

const anHour = 60 * 60 * 1000 // milliseconds

const check = document.createElement('span')
check.classList.add('ico-check')

export default class MarketsPage extends BasePage {
  constructor (application, main, data) {
    super()
    app = application
    const page = this.page = Doc.parsePage(main, [
      // Templates, loaders, chart div...
      'marketLoader', 'marketChart', 'marketList', 'rowTemplate', 'buyRows',
      'sellRows', 'marketSearch',
      // Registration status
      'registrationStatus', 'regStatusTitle', 'regStatusMessage', 'regStatusConfsDisplay',
      'regStatusDex', 'confReq',
      // Order form.
      'orderForm', 'priceBox', 'buyBttn', 'sellBttn', 'limitBttn', 'marketBttn',
      'tifBox', 'submitBttn', 'qtyField', 'rateField', 'orderErr',
      'baseWalletIcons', 'quoteWalletIcons', 'lotSize', 'rateStep', 'lotField',
      'tifNow', 'mktBuyBox', 'mktBuyLots', 'mktBuyField', 'minMktBuy', 'qtyBox',
      'loaderMsg', 'balanceTable',
      // Wallet unlock form
      'forms', 'openForm', 'uwAppPass',
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
    this.registrationStatus = {}
    this.currentForm = null
    this.openAsset = null
    this.openFunc = null
    this.currentCreate = null
    this.book = null
    this.orderRows = {}
    const reporters = {
      price: p => { this.reportPrice(p) }
    }
    this.chart = new DepthChart(page.marketChart, reporters)

    // Set up the BalanceWidget.
    {
      const wgt = this.balanceWgt = new BalanceWidget(page.balanceTable)
      const baseIcons = wgt.base.stateIcons.icons
      const quoteIcons = wgt.quote.stateIcons.icons
      bind(wgt.base.connect, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
      bind(wgt.quote.connect, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })
      bind(baseIcons.sleeping, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
      bind(quoteIcons.sleeping, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })
      bind(baseIcons.locked, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
      bind(quoteIcons.locked, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })
      bind(wgt.base.newWalletBttn, 'click', () => { this.showCreate(this.market.base) })
      bind(wgt.quote.newWalletBttn, 'click', () => { this.showCreate(this.market.quote) })
    }

    // Prepare templates for the buy and sell tables and the user's order table.
    cleanTemplates(page.rowTemplate, page.liveTemplate)

    // Prepare the list of markets.
    this.marketList = new MarketList(page.marketList)
    for (const xc of this.marketList.xcSections) {
      for (const mkt of xc.marketRows) {
        bind(mkt.row, 'click', () => {
          this.setMarket(xc.host, mkt.baseID, mkt.quoteID)
        })
      }
    }

    // Store the elements that need their ticker changed when the market
    // changes.
    this.quoteUnits = main.querySelectorAll('[data-unit=quote]')
    this.baseUnits = main.querySelectorAll('[data-unit=base]')

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
      this.setMarketBuyOrderEstimate()
    })

    Doc.disableMouseWheel(page.rateField, page.lotField, page.qtyField, page.mktBuyField)

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
      if (!Doc.mouseInElement(e, this.currentForm)) {
        Doc.hide(page.forms)
        if (this.currentForm.id === 'verifyForm') {
          page.vPass.value = ''
        }
      }
    })

    // Event listeners for interactions with the various input fields.
    bind(page.lotField, 'change', () => { this.lotChanged() })
    bind(page.lotField, 'keyup', () => { this.lotChanged() })
    bind(page.qtyField, 'change', () => { this.quantityChanged(true) })
    bind(page.qtyField, 'keyup', () => { this.quantityChanged(false) })
    bind(page.mktBuyField, 'change', () => { this.marketBuyChanged() })
    bind(page.mktBuyField, 'keyup', () => { this.marketBuyChanged() })
    bind(page.rateField, 'change', () => { this.rateFieldChanged() })

    // Market search input bindings.
    bind(page.marketSearch, 'change', () => { this.filterMarkets() })
    bind(page.marketSearch, 'keyup', () => { this.filterMarkets() })

    // Notification filters.
    this.notifiers = {
      order: note => { this.handleOrderNote(note) },
      epoch: note => { this.handleEpochNote(note) },
      conn: note => { this.marketList.setConnectionStatus(note) },
      balance: note => { this.handleBalanceNote(note) },
      feepayment: note => { this.handleFeePayment(note) }
    }

    // Fetch the first market in the list, or the users last selected market, if
    // it exists.
    var selected = (data && data.market) ? data.market : State.fetch(lastMarketKey)
    if (!selected || !this.marketList.exists(selected.host, selected.base, selected.quote)) {
      selected = this.marketList.first()
    }
    this.setMarket(selected.host, selected.base, selected.quote)

    // Start a ticker to update time-since values.
    this.secondTicker = setInterval(() => {
      this.page.liveList.querySelectorAll('[data-col=age]').forEach(td => {
        td.textContent = Doc.timeSince(td.parentNode.order.stamp)
      })
    }, 1000)

    // set the initial state for the registration status
    this.setRegistrationStatusVisibility()
  }

  /* isSell is true if the user has selected sell in the order options. */
  isSell () {
    return this.page.sellBttn.classList.contains('selected')
  }

  /* isLimit is true if the user has selected the "limit order" tab. */
  isLimit () {
    return this.page.limitBttn.classList.contains('selected')
  }

  /* hasFeePending is true if the fee payment is pending */
  hasFeePending () {
    const dex = this.market.dex
    return typeof dex.confs === 'number' && dex.confs < dex.confsrequired
  }

  /* assetsAreSupported is true if all the assets of the current market are
   * supported
   */
  assetsAreSupported () {
    const [b, q] = [this.market.base, this.market.quote]
    return b && q
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
      if (this.isSell()) {
        Doc.show(page.priceBox)
        Doc.hide(page.mktBuyBox)
        Doc.show(page.qtyBox)
      } else {
        Doc.hide(page.priceBox)
        Doc.show(page.mktBuyBox)
        Doc.hide(page.qtyBox)
      }
    }
  }

  /* resolveOrderFormVisibility displays or hides the 'orderForm' based on
   * a set of conditions to be met.
   */
  resolveOrderFormVisibility () {
    const page = this.page
    const feePaid = !this.hasFeePending()
    const assetsAreSupported = this.assetsAreSupported()
    const base = this.market.base
    const quote = this.market.quote
    const hasWallets = base && app.assets[base.id].wallet && quote && app.assets[quote.id].wallet

    if (feePaid && assetsAreSupported && hasWallets) {
      Doc.show(page.orderForm)
      return
    }

    Doc.hide(page.orderForm)
  }

  /* setLoaderMsgVisibility displays a message in case a dex asset is not
   * supported
   */
  setLoaderMsgVisibility () {
    const page = this.page
    const { base, quote } = this.market

    if (this.assetsAreSupported()) {
      // make sure to hide the loader msg
      Doc.hide(page.loaderMsg)
      return
    }
    const symbol = (!base && this.market.baseCfg.symbol) || (!quote && this.market.quoteCfg.symbol)

    page.loaderMsg.textContent = `${symbol.toUpperCase()} is not supported`
    Doc.show(page.loaderMsg)
  }

  /* setRegistrationStatusView sets the text content and class for the
   * registration status view
   */
  setRegistrationStatusView (titleContent, confStatusMsg, titleClass) {
    const page = this.page
    page.regStatusTitle.textContent = titleContent
    page.regStatusConfsDisplay.textContent = confStatusMsg
    page.registrationStatus.classList.remove('completed', 'error', 'waiting')
    page.registrationStatus.classList.add(titleClass)
  }

  /*
   * updateRegistrationStatusView updates the view based on the current
   * registration status
   */
  updateRegistrationStatusView (dexAddr, feePaid, confirmationsRequired, confirmations) {
    const page = this.page

    page.confReq.textContent = confirmationsRequired
    page.regStatusDex.textContent = dexAddr

    if (feePaid) {
      this.setRegistrationStatusView('Registration fee payment successful!', '', 'completed')
      return
    }

    const confStatusMsg = `${confirmations} / ${confirmationsRequired}`

    this.setRegistrationStatusView('Waiting for confirmations...', confStatusMsg, 'waiting')
  }

  /*
   * setRegistrationStatusVisibility toggles the registration status view based
   * on the dex data.
   */
  setRegistrationStatusVisibility () {
    const { page, market: { dex } } = this
    const { confs, confsrequired } = dex
    const feePending = this.hasFeePending()

    this.updateRegistrationStatusView(dex.host, !feePending, confsrequired, confs)

    if (feePending) {
      Doc.show(page.registrationStatus)
    } else {
      const toggle = () => {
        Doc.hide(page.registrationStatus)
        this.resolveOrderFormVisibility()
      }
      if (Doc.isHidden(page.orderForm)) {
        // wait a couple of seconds before showing the form so the success
        // message is shown to the user
        setTimeout(toggle, 5000)
        return
      }
      toggle()
    }
  }

  /* setMarket sets the currently displayed market. */
  async setMarket (host, base, quote) {
    const dex = app.user.exchanges[host]
    const baseCfg = dex.assets[base]
    const quoteCfg = dex.assets[quote]
    this.market = {
      dex: dex,
      sid: marketID(baseCfg.symbol, quoteCfg.symbol), // A string market identifier used by the DEX.
      // app.assets is a map of core.SupportedAsset type, which can be found at
      // client/core/types.go.
      base: app.assets[base],
      quote: app.assets[quote],
      // dex.assets is a map of dex.Asset type, which is defined at
      // dex/asset.go.
      baseCfg: dex.assets[base],
      quoteCfg: dex.assets[quote]
    }
    this.page.marketLoader.classList.remove('d-none')
    ws.request('loadmarket', makeMarket(host, base, quote))

    this.setLoaderMsgVisibility()
    this.setRegistrationStatusVisibility()
    this.resolveOrderFormVisibility()
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
      host: market.dex.host,
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
    this.book = new OrderBook(data, this.market.baseCfg.symbol, this.market.quoteCfg.symbol)
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
    const xc = app.user.exchanges[market.dex.host]
    const buffer = xc.markets[market.sid].buybuffer
    const gap = this.midGap()
    if (gap) {
      this.page.minMktBuy.textContent = Doc.formatCoinValue(lotSize * buffer * gap / 1e8)
    }
  }

  /* refreshActiveOrders refreshes the user's active order list. */
  refreshActiveOrders () {
    const page = this.page
    const orderRows = this.orderRows
    const market = this.market
    for (const oid in orderRows) delete orderRows[oid]
    const orders = app.orders(market.dex.host, marketID(market.baseCfg.symbol, market.quoteCfg.symbol))
    Doc.empty(page.liveList)
    for (const order of orders) {
      const row = page.liveTemplate.cloneNode(true)
      row.order = order
      orderRows[order.id] = row
      updateUserOrderRow(row, order)
      if (order.type === LIMIT) {
        if (!order.cancelling && order.filled !== order.qty) {
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
    const host = market.dex.host
    const [b, q] = [market.baseCfg, market.quoteCfg]
    if (data.base !== b.id || data.quote !== q.id) return
    this.handleBook(data)
    page.marketLoader.classList.add('d-none')
    this.marketList.select(host, b.id, q.id)

    State.store(lastMarketKey, {
      host: data.host,
      base: data.base,
      quote: data.quote
    })

    page.lotSize.textContent = Doc.formatCoinValue(market.baseCfg.lotSize / 1e8)
    page.rateStep.textContent = Doc.formatCoinValue(market.quoteCfg.rateStep / 1e8)
    this.baseUnits.forEach(el => { el.textContent = b.symbol.toUpperCase() })
    this.quoteUnits.forEach(el => { el.textContent = q.symbol.toUpperCase() })
    this.balanceWgt.setWallets(host, b.id, q.id)
    this.setMarketBuyOrderEstimate()
    this.refreshActiveOrders()
  }

  /* handleBookOrderRoute is the handler for 'book_order' notifications. */
  handleBookOrderRoute (data) {
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload
    if (order.rate > 0) this.book.add(order)
    this.addTableOrder(order)
    this.chart.draw()
  }

  /* handleUnbookOrderRoute is the handler for 'unbook_order' notifications. */
  handleUnbookOrderRoute (data) {
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
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
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const update = data.payload
    this.book.updateRemaining(update.token, update.qty)
    this.updateTableOrder(update)
    this.chart.draw()
  }

  /* handleEpochOrderRoute is the handler for 'epoch_order' notifications. */
  handleEpochOrderRoute (data) {
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
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
    page.uwAppPass.focus()
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
    const symbol = isMarketBuy(order) ? this.market.quote.symbol : this.market.base.symbol
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
   * handleFeePayment is the handler for the 'feepayment' notification type.
   * This is used to update the registration status of the currrent exchange.
   */
  handleFeePayment (note) {
    const dexAddr = note.dex
    if (dexAddr !== this.market.dex.host) return
    // update local dex
    this.market.dex = app.exchanges[dexAddr]
    this.setRegistrationStatusVisibility()
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
      updateUserOrderRow(row, order)
      if (order.filled === order.qty) {
        // Remove the cancellation button.
        updateDataCol(row, 'cancel', '')
      }
    }
  }

  /*
   * handleEpochNote handles notifications signalling the start of a new epoch.
   */
  handleEpochNote (note) {
    if (note.host !== this.market.dex.host || note.marketID !== this.market.sid) return
    if (this.book) {
      this.book.setEpoch(note.epoch)
      this.chart.draw()
    }
    this.clearOrderTableEpochs(note.epoch)
    for (const tr of Array.from(this.page.liveList.children)) {
      const order = tr.order
      const alreadyMatched = note.epoch > order.epoch
      const statusTD = tr.querySelector('[data-col=status]')
      switch (true) {
        case order.type === LIMIT && order.status === statusEpoch && alreadyMatched:
          statusTD.textContent = order.tif === immediateTiF ? 'executed' : 'booked'
          order.status = order.tif === immediateTiF ? statusExecuted : statusBooked
          break
        case order.type === MARKET && order.status === statusEpoch:
          // Technically don't know if this should be 'executed' or 'settling'.
          statusTD.textContent = 'executed'
          order.status = statusExecuted
          break
      }
    }
  }

  // handleBalanceNote handles notifications updating a wallet's balance.
  handleBalanceNote (note) {
    this.balanceWgt.updateAsset(note.assetID)
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
    page.vPass.value = ''
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
    await app.fetchUser()
    if (!baseWallet.open || !quoteWallet.open) {
      this.balanceWgt.updateAsset(market.base.id)
      this.balanceWgt.updateAsset(market.quote.id)
    }
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
    this.balanceWgt.updateAsset(asset.id)
    this.resolveOrderFormVisibility()
  }

  /*
   * walletUnlocked is attached to successful submission of the wallet unlock
   * form. walletUnlocked is only called once the form is submitted and a
   * success response is received from the client.
   */
  async walletUnlocked () {
    Doc.hide(this.page.forms)
    await app.fetchUser()
    this.balanceWgt.updateAsset(this.openAsset.id)
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
   * filterMarkets sets the display of markets in the markets list based on the
   * value of the search input.
   */
  filterMarkets () {
    const filterTxt = this.page.marketSearch.value
    const filter = filterTxt ? mkt => mkt.name.includes(filterTxt) : () => true
    this.marketList.setFilter(filter)
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
    clearInterval(this.secondTicker)
  }
}

/*
 * tmplElement is a helper function for grabbing sub-elements of the market list
 * template.
 */
function tmplElement (ancestor, s) {
  return ancestor.querySelector(`[data-tmpl="${s}"]`)
}

/*
 * MarketList respresents the list of exchanges and markets on the left side of
 * markets view. The MarketList provides utilities for adjusting the visibility
 * and sort order of markets.
 */
class MarketList {
  constructor (div) {
    this.selected = null
    const xcTmpl = tmplElement(div, 'xc')
    cleanTemplates(xcTmpl)
    this.xcSections = []
    for (const dex of Object.values(app.user.exchanges)) {
      this.xcSections.push(new ExchangeSection(xcTmpl, dex))
    }
    // Initial sort is alphabetical.
    for (const xc of this.sortedSections()) {
      div.appendChild(xc.box)
    }
  }

  /*
   * sortedSections returns a list of ExchangeSection sorted alphabetically by
   * host.
   */
  sortedSections () {
    return [...this.xcSections].sort((a, b) => a.host < b.host ? -1 : 1)
  }

  /*
   * xcSection is a getter for the ExchangeSection for a specified host.
   */
  xcSection (host) {
    for (const xc of this.xcSections) {
      if (xc.host === host) return xc
    }
    return null
  }

  /* exists will be true if the specified market exists. */
  exists (host, baseID, quoteID) {
    const xc = this.xcSection(host)
    if (!xc) return false
    for (const mkt of xc.marketRows) {
      if (mkt.baseID === baseID && mkt.quoteID === quoteID) return true
    }
    return false
  }

  /* first gets the first market from the first exchange, alphabetically. */
  first () {
    const firstXC = this.sortedSections()[0]
    const firstMkt = firstXC.first()
    return makeMarket(firstXC.host, firstMkt.baseID, firstMkt.quoteID)
  }

  /* select sets the specified market as selected. */
  select (host, baseID, quoteID) {
    if (this.selected) this.selected.row.classList.remove('selected')
    this.selected = this.xcSection(host).marketRow(baseID, quoteID)
    this.selected.row.classList.add('selected')
  }

  /* setConnectionStatus sets the visiblity of the disconnected icon based
   * on the core.ConnEventNote.
   */
  setConnectionStatus (note) {
    this.xcSection(note.host).setConnected(note.connected)
  }

  /*
   * setFilter sets the visibility of market rows based on the provided filter.
   */
  setFilter (filter) {
    for (const xc of this.xcSections) {
      xc.setFilter(filter)
    }
  }
}

/*
 * ExchangeSection is a top level section of the MarketList.
 */
class ExchangeSection {
  constructor (tmpl, dex) {
    this.dex = dex
    this.host = dex.host
    const box = tmpl.cloneNode(true)
    this.box = box
    const header = tmplElement(box, 'header')
    this.disconnectedIcon = tmplElement(header, 'disconnected')
    if (dex.connected) Doc.hide(this.disconnectedIcon)
    header.append(dex.host)

    this.marketRows = []
    this.rows = tmplElement(box, 'mkts')
    const rowTmpl = tmplElement(this.rows, 'mktrow')
    this.rows.removeChild(rowTmpl)
    for (const mkt of Object.values(dex.markets)) {
      this.marketRows.push(new MarketRow(rowTmpl, mkt))
    }

    for (const market of this.sortedMarkets()) {
      this.rows.appendChild(market.row)
    }
  }

  /*
   * sortedMarkets is the list of MarketRow sorted alphabetically by the base
   * symbol first, quote symbol second.
   */
  sortedMarkets () {
    return [...this.marketRows].sort((a, b) => a.name < b.name ? -1 : 1)
  }

  /*
   * first returns the first market in the alphabetically-sorted list of
   * markets.
   */
  first () {
    return this.sortedMarkets()[0]
  }

  /*
   * marketRow gets the MarketRow for the specified market.
   */
  marketRow (baseID, quoteID) {
    for (const mkt of this.marketRows) {
      if (mkt.baseID === baseID && mkt.quoteID === quoteID) return mkt
    }
    return null
  }

  /* setConnected sets the visiblity of the disconnected icon. */
  setConnected (isConnected) {
    if (isConnected) Doc.hide(this.disconnectedIcon)
    else Doc.show(this.disconnectedIcon)
  }

  /*
   * setFilter sets the visibility of market rows based on the provided filter.
   */
  setFilter (filter) {
    for (const mkt of this.marketRows) {
      if (filter(mkt)) Doc.show(mkt.row)
      else Doc.hide(mkt.row)
    }
  }
}

/*
 * MarketRow represents one row in the MarketList. A MarketRow is a subsection
 * of the ExchangeSection.
 */
class MarketRow {
  constructor (tmpl, mkt) {
    this.name = mkt.name
    this.baseID = mkt.baseid
    this.quoteID = mkt.quoteid
    const row = tmpl.cloneNode(true)
    this.row = row
    tmplElement(row, 'baseicon').src = Doc.logoPath(mkt.basesymbol)
    tmplElement(row, 'quoteicon').src = Doc.logoPath(mkt.quotesymbol)
    row.append(`${mkt.basesymbol.toUpperCase()}-${mkt.quotesymbol.toUpperCase()}`)
  }
}

/*
 * BalanceWidget is a display of balance information. Because the wallet can be
 * in any number of states, and because every exchange has different funding
 * coin confirmation requirements, the BalanceWidget displays a number of state
 * indicators and buttons, as well as tabulated balance data with rows for
 * locked and immature balance.
 */
class BalanceWidget {
  constructor (table) {
    const els = Doc.parsePage(table, [
      'baseAvail', 'quoteAvail', 'baseNewWalletRow', 'quoteNewWalletRow',
      'baseNewButton', 'quoteNewButton', 'baseLocked', 'quoteLocked',
      'baseImmature', 'quoteImmature', 'baseImg', 'quoteImg',
      'quoteUnsupported', 'baseUnsupported', 'baseExpired', 'quoteExpired',
      'baseConnect', 'quoteConnect', 'baseSpinner', 'quoteSpinner',
      'baseWalletState', 'quoteWalletState'
    ])
    this.base = {
      id: 0,
      cfg: null,
      logo: els.baseImg,
      avail: els.baseAvail,
      newWalletRow: els.baseNewWalletRow,
      newWalletBttn: els.baseNewButton,
      locked: els.baseLocked,
      immature: els.baseImmature,
      unsupported: els.baseUnsupported,
      expired: els.baseExpired,
      connect: els.baseConnect,
      spinner: els.baseSpinner,
      iconBox: els.baseWalletState,
      stateIcons: new WalletIcons(els.baseWalletState)
    }
    this.quote = {
      id: 0,
      cfg: null,
      logo: els.quoteImg,
      avail: els.quoteAvail,
      newWalletRow: els.quoteNewWalletRow,
      newWalletBttn: els.quoteNewButton,
      locked: els.quoteLocked,
      immature: els.quoteImmature,
      unsupported: els.quoteUnsupported,
      expired: els.quoteExpired,
      connect: els.quoteConnect,
      spinner: els.quoteSpinner,
      iconBox: els.quoteWalletState,
      stateIcons: new WalletIcons(els.quoteWalletState)
    }
    this.dex = null
  }

  /*
   * setWallet sets the balance widget to display data for specified market.
   */
  setWallets (host, baseID, quoteID) {
    this.dex = app.user.exchanges[host]
    this.base.id = baseID
    this.base.cfg = this.dex.assets[baseID]
    this.quote.id = quoteID
    this.quote.cfg = this.dex.assets[quoteID]
    this.updateWallet(this.base)
    this.updateWallet(this.quote)
  }

  /*
   * updateWallet updates the displayed wallet information based on the
   * core.Wallet state.
   */
  updateWallet (side) {
    const asset = app.assets[side.id]
    // Just hide everything to start.
    Doc.hide(
      side.newWalletRow, side.avail, side.immature, side.locked,
      side.expired, side.unsupported, side.connect, side.spinner, side.iconBox
    )
    side.logo.src = Doc.logoPath(side.cfg.symbol)
    // Handle an unsupported asset.
    if (!asset) {
      Doc.show(side.unsupported)
      return
    }
    Doc.show(side.iconBox)
    const wallet = asset.wallet
    side.stateIcons.readWallet(wallet)
    // Handle no wallet configured.
    if (!wallet) {
      Doc.show(side.newWalletRow)
      return
    }
    const bal = wallet.balance
    // Handle not connected and no balance known for the DEX.
    if (!bal && !wallet.running) {
      Doc.show(side.connect)
      return
    }
    // If there is no balance, but the wallet is connected, show the loading
    // icon while we fetch an update.
    if (!bal) {
      this.fetchBalance(side.id)
      Doc.show(side.spinner)
      return
    }
    // We have a wallet and a DEX-specific balance. Set all of the fields.
    Doc.show(side.avail, side.immature, side.locked)
    side.avail.textContent = Doc.formatCoinValue(bal.available / 1e8)
    side.locked.textContent = Doc.formatCoinValue(bal.locked / 1e8)
    side.immature.textContent = Doc.formatCoinValue(bal.immature / 1e8)
    // If the current balance update time is older than an hour, show the
    // expiration icon. Request a balance update, if possible.
    const expired = new Date().getTime() - new Date(wallet.balance.stamp).getTime() > anHour
    if (expired) {
      Doc.show(side.expired)
      if (wallet.running) this.fetchBalance(side.id)
    } else Doc.hide(side.expired)
  }

  /*
   * updateAsset updates the info for one side of the existing market. If the
   * specified asset ID is not one of the current market's base or quote assets,
   * it is silently ignored.
   */
  updateAsset (assetID) {
    if (assetID === this.base.id) this.updateWallet(this.base)
    else if (assetID === this.quote.id) this.updateWallet(this.quote)
  }

  /*
   * fetchBalance requests a balance update from the API. The API response does
   * include the balance, but we're ignoring it, since a balance update
   * notification is received via the Application anyways.
   */
  async fetchBalance (assetID) {
    const res = await postJSON('/api/balance', { assetID: assetID })
    if (!app.checkResponse(res)) {
      console.error('failed to fetch balance for asset ID', assetID)
    }
  }
}

/* makeMarket creates a market object that specifies basic market details. */
function makeMarket (host, base, quote) {
  return {
    host: host,
    base: base,
    quote: quote
  }
}

/* marketID creates a DEX-compatible market name from the ticker symbols. */
export function marketID (b, q) { return `${b}_${q}` }

/* asAtoms converts the float string to atoms. */
function asAtoms (s) {
  return Math.round(parseFloat(s) * 1e8)
}

/* swapBttns changes the 'selected' class of the buttons. */
function swapBttns (before, now) {
  before.classList.remove('selected')
  now.classList.add('selected')
}

/* sumSettled sums the quantities of the matches that have completed. */
function sumSettled (order) {
  if (!order.matches) return 0
  const qty = isMarketBuy(order) ? m => m.qty * m.rate * 1e-8 : m => m.qty
  return order.matches.reduce((settled, match) => {
    // >= makerRedeemed is used because the maker never actually goes to
    // matchComplete (once at makerRedeemed, nothing left to do), and the taker
    // never goes to makerRedeemed, since at that point, they just complete the
    // swap.
    return (match.status >= makerRedeemed) ? settled + qty(match) : settled
  }, 0)
}

/*
 * hasLiveMatches returns true if the order has matches that have not completed
 * settlement yet.
 */
function hasLiveMatches (order) {
  if (!order.matches) return false
  for (const match of order.matches) {
    if (match.status < makerRedeemed) return true
  }
  return false
}

/* statusString converts the order status to a string */
function statusString (order) {
  const isLive = hasLiveMatches(order)
  switch (order.status) {
    case statusUnknown: return 'unknown'
    case statusEpoch: return 'epoch'
    case statusBooked: return order.cancelling ? 'cancelling' : 'booked'
    case statusExecuted: return isLive ? 'settling' : 'executed'
    case statusCanceled: return isLive ? 'canceled/settling' : 'canceled'
    case statusRevoked: return isLive ? 'revoked/settling' : 'revoked'
  }
}

/*
 * updateDataCol sets the value of data-col=[col] <td> element that is a child
 * of the specified <tr>.
 */
function updateDataCol (tr, col, s) {
  tr.querySelector(`[data-col=${col}]`).textContent = s
}

/*
 * updateUserOrderRow sets the td contents of the user's order table row.
 */
function updateUserOrderRow (tr, order) {
  updateDataCol(tr, 'type', order.type === LIMIT ? 'limit' : 'market')
  updateDataCol(tr, 'side', order.sell ? 'sell' : 'buy')
  updateDataCol(tr, 'age', Doc.timeSince(order.stamp))
  updateDataCol(tr, 'rate', Doc.formatCoinValue(order.rate / 1e8))
  updateDataCol(tr, 'qty', Doc.formatCoinValue(order.qty / 1e8))
  updateDataCol(tr, 'filled', `${(order.filled / order.qty * 100).toFixed(1)}%`)
  updateDataCol(tr, 'settled', `${(sumSettled(order) / order.qty * 100).toFixed(1)}%`)
  updateDataCol(tr, 'status', statusString(order))
}

/* isMarketBuy will return true if the order is a market buy order. */
function isMarketBuy (order) {
  return order.type === MARKET && !order.sell
}

/*
 * cleanTemplates removes the elements from the DOM and deletes the id
 * attribute.
 */
function cleanTemplates (...tmpls) {
  tmpls.forEach(tmpl => {
    tmpl.remove()
    tmpl.removeAttribute('id')
  })
}
