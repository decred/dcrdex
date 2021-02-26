import Doc, { WalletIcons } from './doc'
import State from './state'
import BasePage from './basepage'
import OrderBook from './orderbook'
import { DepthChart } from './charts'
import { postJSON } from './http'
import { NewWalletForm, bindOpenWallet, bind as bindForm } from './forms'
import * as Order from './orderutil'
import ws from './ws'

let app
const bind = Doc.bind

const bookRoute = 'book'
const bookOrderRoute = 'book_order'
const unbookOrderRoute = 'unbook_order'
const updateRemainingRoute = 'update_remaining'
const epochOrderRoute = 'epoch_order'
const bookUpdateRoute = 'bookupdate'
const unmarketRoute = 'unmarket'

const lastMarketKey = 'selectedMarket'
const chartRatioKey = 'chartRatio'
const depthZoomKey = 'depthZoom'

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
      'marketLoader', 'marketList', 'rowTemplate', 'buyRows', 'sellRows',
      'marketSearch', 'rightSide',
      // Registration status
      'registrationStatus', 'regStatusTitle', 'regStatusMessage', 'regStatusConfsDisplay',
      'regStatusDex', 'confReq',
      // Order form.
      'orderForm', 'priceBox', 'buyBttn', 'sellBttn', 'limitBttn', 'marketBttn',
      'tifBox', 'submitBttn', 'qtyField', 'rateField', 'orderErr',
      'baseWalletIcons', 'quoteWalletIcons', 'lotSize', 'rateStep', 'lotField',
      'tifNow', 'mktBuyBox', 'mktBuyLots', 'mktBuyField', 'minMktBuy', 'qtyBox',
      'loaderMsg', 'balanceTable', 'orderPreview',
      // Wallet unlock form
      'forms', 'openForm', 'uwAppPass',
      // Order submission is verified with the user's password.
      'verifyForm', 'vHeader', 'vSideHeader', 'vSide', 'vQty', 'vBase', 'vRate',
      'vTotal', 'vQuote', 'vPass', 'vSideSubmit', 'vBaseSubmit', 'vSubmit', 'verifyLimit', 'verifyMarket',
      'vmTotal', 'vmAsset', 'vmLots', 'mktBuyScore',
      // Create wallet form
      'walletForm',
      // Active orders
      'liveTemplate', 'liveList', 'liveTable',
      // Cancel order form
      'cancelForm', 'cancelRemain', 'cancelUnit', 'cancelPass', 'cancelSubmit',
      // Chart and legend
      'marketChart', 'chartResizer', 'sellBookedBase', 'sellBookedQuote',
      'buyBookedBase', 'buyBookedQuote', 'hoverData', 'hoverPrice',
      'hoverVolume', 'chartLegend', 'chartErrMsg',
      // Max order section
      'maxOrd', 'maxLbl', 'maxFromLots', 'maxFromAmt', 'maxFromTicker',
      'maxToAmt', 'maxToTicker', 'maxAboveZero', 'maxLotBox', 'maxFromLotsLbl',
      'maxBox'
    ])
    this.main = main
    this.loaded = app.loading(this.main.parentElement)
    this.maxLoaded = null
    this.market = null
    this.registrationStatus = {}
    this.currentForm = null
    this.openAsset = null
    this.openFunc = null
    this.currentCreate = null
    this.preorderTimer = null
    this.book = null
    this.cancelData = null
    this.metaOrders = {}
    this.depthLines = {
      hover: [],
      input: []
    }
    this.activeMarkerRate = null
    this.hovers = []
    // 'Your Orders' list sort key and direction.
    this.ordersSortKey = 'stamp'
    // 1 if sorting ascendingly, -1 if sorting descendingly.
    this.ordersSortDirection = 1

    const reporters = {
      click: p => { this.reportClick(p) },
      volume: d => { this.reportVolume(d) },
      mouse: d => { this.reportMousePosition(d) },
      zoom: z => { this.reportZoom(z) }
    }
    this.chart = new DepthChart(page.marketChart, reporters, State.fetch(depthZoomKey))

    // Set up the BalanceWidget.
    {
      const wgt = this.balanceWgt = new BalanceWidget(page.balanceTable)
      const baseIcons = wgt.base.stateIcons.icons
      const quoteIcons = wgt.quote.stateIcons.icons
      bind(wgt.base.connect, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
      bind(wgt.quote.connect, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })
      bind(wgt.base.expired, 'click', () => { this.showOpen(this.market.base, this.walletUnlocked) })
      bind(wgt.quote.expired, 'click', () => { this.showOpen(this.market.quote, this.walletUnlocked) })
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
      page.submitBttn.classList.remove('sellred')
      page.submitBttn.classList.add('buygreen')
      page.maxLbl.textContent = 'Buy'
      this.setOrderBttnText()
      this.setOrderVisibility()
      this.drawChartLines()
    })
    bind(page.sellBttn, 'click', () => {
      swapBttns(page.buyBttn, page.sellBttn)
      page.submitBttn.classList.add('sellred')
      page.submitBttn.classList.remove('buygreen')
      page.maxLbl.textContent = 'Sell'
      this.setOrderBttnText()
      this.setOrderVisibility()
      this.drawChartLines()
    })
    bind(page.limitBttn, 'click', () => {
      swapBttns(page.marketBttn, page.limitBttn)
      this.setOrderVisibility()
      if (!page.rateField.value) return
      this.depthLines.input = [{
        rate: page.rateField.value,
        color: this.isSell() ? this.chart.theme.sellLine : this.chart.theme.buyLine
      }]
      this.drawChartLines()
    })
    bind(page.marketBttn, 'click', () => {
      swapBttns(page.limitBttn, page.marketBttn)
      this.setOrderVisibility()
      this.setMarketBuyOrderEstimate()
      this.depthLines.input = []
      this.drawChartLines()
    })
    bind(page.maxOrd, 'click', () => {
      if (this.isSell()) page.lotField.value = this.market.maxSell.swap.lots
      else page.lotField.value = this.market.maxBuys[this.adjustedRate()].swap.lots
      this.lotChanged()
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
    bindOpenWallet(app, page.openForm, async () => { this.openFunc() })
    // Create a wallet
    this.walletForm = new NewWalletForm(app, page.walletForm, async () => { this.createWallet() })
    // Main order form.
    bindForm(page.orderForm, page.submitBttn, async () => { this.stepSubmit() })
    // Order verification form.
    bindForm(page.verifyForm, page.vSubmit, async () => { this.submitOrder() })
    // Cancel order form.
    bindForm(page.cancelForm, page.cancelSubmit, async () => { this.submitCancel() })
    // Bind active orders list's header sort events.
    page.liveTable.querySelectorAll('[data-ordercol]')
      .forEach(th => bind(th, 'click', () => setOrdersSortCol(th.dataset.ordercol)))

    const setOrdersSortCol = (key) => {
      // First unset header's current sorted col classes.
      unsetOrdersSortColClasses()
      // If already sorting by key change sort direction.
      if (this.ordersSortKey === key) {
        this.ordersSortDirection *= -1
      } else {
        // Otherwise update sort key and set default direction to ascending.
        this.ordersSortKey = key
        this.ordersSortDirection = 1
      }
      this.refreshActiveOrders()
      // Set header's new sorted col classes.
      setOrdersSortColClasses()
    }

    const sortClassByDirection = () => {
      if (this.ordersSortDirection === 1) return 'sorted-asc'
      return 'sorted-dsc'
    }

    const unsetOrdersSortColClasses = () => {
      page.liveTable.querySelectorAll('[data-ordercol]')
        .forEach(th => th.classList.remove('sorted-asc', 'sorted-dsc'))
    }

    const setOrdersSortColClasses = () => {
      const key = this.ordersSortKey
      const sortCls = sortClassByDirection()
      page.liveTable.querySelector(`[data-ordercol=${key}]`).classList.add(sortCls)
    }

    // Set default's sorted col header classes.
    setOrdersSortColClasses()

    const closePopups = () => {
      Doc.hide(page.forms)
      page.vPass.value = ''
      page.cancelPass.value = ''
    }

    // If the user clicks outside of a form, it should close the page overlay.
    bind(page.forms, 'mousedown', e => {
      if (!Doc.mouseInElement(e, this.currentForm)) {
        closePopups()
      }
    })

    this.keyup = e => {
      if (e.key === 'Escape') {
        closePopups()
      }
    }
    bind(document, 'keyup', this.keyup)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { closePopups() })
    })

    // Event listeners for interactions with the various input fields.
    bind(page.lotField, 'change', () => { this.lotChanged() })
    bind(page.lotField, 'keyup', () => { this.lotChanged() })
    bind(page.qtyField, 'change', () => { this.quantityChanged(true) })
    bind(page.qtyField, 'keyup', () => { this.quantityChanged(false) })
    bind(page.mktBuyField, 'change', () => { this.marketBuyChanged() })
    bind(page.mktBuyField, 'keyup', () => { this.marketBuyChanged() })
    bind(page.rateField, 'change', () => { this.rateFieldChanged() })
    bind(page.rateField, 'keyup', () => { this.previewQuoteAmt(true) })

    // Market search input bindings.
    bind(page.marketSearch, 'change', () => { this.filterMarkets() })
    bind(page.marketSearch, 'keyup', () => { this.filterMarkets() })

    const clearChartLines = () => {
      this.depthLines.hover = []
      this.drawChartLines()
    }
    bind(page.buyRows, 'mouseleave', clearChartLines)
    bind(page.sellRows, 'mouseleave', clearChartLines)
    bind(page.liveList, 'mouseleave', () => {
      this.activeMarkerRate = null
      this.setMarkers()
    })

    // Load the user's layout preferences.
    const setChartRatio = r => {
      if (r > 0.7) r = 0.7
      else if (r < 0.25) r = 0.25
      page.marketChart.style.height = `${r * 100}%`
      this.chart.resize()
    }
    const chartDivRatio = State.fetch(chartRatioKey)
    if (chartDivRatio) {
      setChartRatio(chartDivRatio)
    }
    // Bind chart resizing.
    bind(page.chartResizer, 'mousedown', e => {
      if (e.button !== 0) return
      e.preventDefault()
      let chartRatio
      const trackMouse = ee => {
        ee.preventDefault()
        const box = page.rightSide.getBoundingClientRect()
        const h = box.bottom - box.top
        chartRatio = (ee.pageY - box.top) / h
        setChartRatio(chartRatio)
      }
      bind(document, 'mousemove', trackMouse)
      bind(document, 'mouseup', () => {
        if (chartRatio) State.store(chartRatioKey, chartRatio)
        Doc.unbind(document, 'mousemove', trackMouse)
      })
    })

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
    let selected
    if (data && data.host && typeof data.base !== 'undefined' && typeof data.quote !== 'undefined') {
      selected = makeMarket(data.host, parseInt(data.base), parseInt(data.quote))
    } else {
      selected = State.fetch(lastMarketKey)
    }
    if (!selected || !this.marketList.exists(selected.host, selected.base, selected.quote)) {
      selected = this.marketList.first()
    }
    this.setMarket(selected.host, selected.base, selected.quote)

    // Start a ticker to update time-since values.
    this.secondTicker = setInterval(() => {
      for (const metaOrder of Object.values(this.metaOrders)) {
        const td = Doc.tmplElement(metaOrder.row, 'age')
        td.textContent = Doc.timeSince(metaOrder.order.stamp)
      }
    }, 1000)

    // set the initial state for the registration status
    this.setRegistrationStatusVisibility()
    this.setBalanceVisibility()
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
      Doc.show(page.priceBox, page.tifBox, page.qtyBox, page.maxBox)
      Doc.hide(page.mktBuyBox)
      this.previewQuoteAmt(true)
    } else {
      Doc.hide(page.tifBox, page.maxBox, page.priceBox)
      if (this.isSell()) {
        Doc.hide(page.mktBuyBox)
        Doc.show(page.qtyBox)
        this.previewQuoteAmt(true)
      } else {
        Doc.show(page.mktBuyBox)
        Doc.hide(page.qtyBox)
        this.previewQuoteAmt(false)
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

    // If dex is not connected to server, is not possible to know fee
    // registration status.
    if (!dex.connected) return

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

  setOrderBttnText () {
    if (this.isSell()) {
      this.page.submitBttn.textContent = `Place order to sell ${this.market.base.symbol.toUpperCase()}`
    } else this.page.submitBttn.textContent = `Place order to buy  ${this.market.base.symbol.toUpperCase()}`
  }

  /* setMarket sets the currently displayed market. */
  async setMarket (host, base, quote) {
    const dex = app.user.exchanges[host]
    if (!dex.connected) {
      this.market = { dex: dex }
      this.page.chartErrMsg.textContent = 'Connection to dex server failed. ' +
        'You can close dexc and try again later or wait for it to reconnect.'
      Doc.show(this.page.chartErrMsg)
      this.loaded()
      this.main.style.opacity = 1
      Doc.hide(this.page.marketLoader)
      return
    }

    const baseCfg = dex.assets[base]
    const quoteCfg = dex.assets[quote]
    Doc.hide(this.page.maxOrd)
    if (this.preorderTimer) {
      window.clearTimeout(this.preorderTimer)
      this.preorderTimer = null
    }
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
      quoteCfg: dex.assets[quote],
      maxSell: null,
      maxBuys: {}
    }
    this.page.marketLoader.classList.remove('d-none')
    ws.request('loadmarket', makeMarket(host, base, quote))
    this.setLoaderMsgVisibility()
    this.setRegistrationStatusVisibility()
    this.resolveOrderFormVisibility()
    this.setOrderBttnText()
  }

  /*
   * reportClick is a callback used by the DepthChart when the user clicks
   * on the chart area. The rate field is set to the x-value of the click.
   */
  reportClick (p) {
    this.page.rateField.value = p.toFixed(8)
    this.rateFieldChanged()
  }

  /*
   * reportVolume accepts a volume report from the DepthChart and sets the
   * values in the chart legend.
   */
  reportVolume (d) {
    const page = this.page
    page.sellBookedBase.textContent = Doc.formatCoinValue(d.sellBase)
    page.sellBookedQuote.textContent = Doc.formatCoinValue(d.sellQuote)
    page.buyBookedBase.textContent = Doc.formatCoinValue(d.buyBase)
    page.buyBookedQuote.textContent = Doc.formatCoinValue(d.buyQuote)
  }

  /*
   * reportMousePosition accepts informations about the mouse position on the
   * chart area.
   */
  reportMousePosition (d) {
    while (this.hovers.length) this.hovers.shift().classList.remove('hover')
    const page = this.page
    if (!d) {
      Doc.hide(page.hoverData)
      return
    }

    // If the user is hovered to within a small percent (based on chart width)
    // of a user order, highlight that order's row.
    const markers = d.hoverMarkers.map(v => Math.round(v * 1e8))
    for (const metaOrd of Object.values(this.metaOrders)) {
      const [row, ord] = [metaOrd.row, metaOrd.order]
      if (ord.status !== Order.StatusBooked) continue
      if (markers.indexOf(ord.rate) > -1) {
        row.classList.add('hover')
        this.hovers.push(row)
      }
    }

    page.hoverPrice.textContent = Doc.formatCoinValue(d.rate)
    page.hoverVolume.textContent = Doc.formatCoinValue(d.depth)
    page.hoverVolume.style.color = d.dotColor
    page.chartLegend.style.left = `${d.yAxisWidth}px`
    Doc.show(page.hoverData)
  }

  /*
   * reportZoom accepts informations about the current depth chart zoom level.
   * This information is saved to disk so that the zoom level can be maintained
   * across reloads.
   */
  reportZoom (zoom) {
    State.store(depthZoomKey, zoom)
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

  /**
   * previewQuoteAmt shows quote amount when rate or quantity input are changed
   */
  previewQuoteAmt (show) {
    const page = this.page
    const order = this.parseOrder()
    page.orderErr.textContent = ''
    if (order.rate) {
      if (order.sell) this.preSell()
      else this.preBuy()
    }
    this.depthLines.input = []
    if (order.rate && this.isLimit()) {
      this.depthLines.input = [{
        rate: order.rate / 1e8,
        color: order.sell ? this.chart.theme.sellLine : this.chart.theme.buyLine
      }]
    }
    this.drawChartLines()
    if (!show || !order.rate || !order.qty) {
      page.orderPreview.textContent = ''
      this.drawChartLines()
      return
    }
    const quoteAsset = app.assets[order.quote]
    const total = Doc.formatCoinValue(order.rate / 1e8 * order.qty / 1e8)
    page.orderPreview.textContent = `Total: ${total} ${quoteAsset.symbol.toUpperCase()}`
  }

  /**
   * preSell populates the max order message for the largest available sell.
   */
  preSell () {
    const mkt = this.market
    const baseWallet = app.assets[mkt.base.id].wallet
    if (baseWallet.available < mkt.baseCfg.lotSize) {
      this.setMaxOrder(0, this.adjustedRate() / 1e8)
      return
    }
    if (mkt.maxSell) {
      this.setMaxOrder(mkt.maxSell.swap, this.adjustedRate() / 1e8)
      return
    }
    // We only fetch pre-sell once per balance update, so don't delay.
    this.schedulePreOrder('/api/maxsell', {}, 0, res => {
      mkt.maxSell = res.maxSell
      mkt.sellBalance = baseWallet.balance.available
      this.setMaxOrder(res.maxSell.swap, this.adjustedRate() / 1e8)
    })
  }

  /**
   * preBuy populates the max order message for the largest available buy.
   */
  preBuy () {
    const mkt = this.market
    const rate = this.adjustedRate()
    const quoteWallet = app.assets[mkt.quote.id].wallet
    const aLot = mkt.baseCfg.lotSize * (rate / 1e8)
    if (quoteWallet.balance.available < aLot) {
      this.setMaxOrder(0, 1e8 / rate)
      return
    }
    if (mkt.maxBuys[rate]) {
      this.setMaxOrder(mkt.maxBuys[rate].swap, 1e8 / rate)
      return
    }
    // 0 delay for first fetch after balance update or market change, otherwise
    // meter these at 1 / sec.
    const delay = mkt.maxBuys ? 1000 : 0
    this.schedulePreOrder('/api/maxbuy', { rate: rate }, delay, res => {
      mkt.maxBuys[rate] = res.maxBuy
      mkt.buyBalance = app.assets[mkt.quote.id].wallet.balance.available
      this.setMaxOrder(res.maxBuy.swap, 1e8 / rate)
    })
  }

  /**
   * schedulePreorder shows the loading icon and schedules a call to an order
   * estimate api endpoint. If another call to schedulePreorder is made before
   * this one is fired (after delay), this call will be canceled.
   */
  schedulePreOrder (path, args, delay, success) {
    const page = this.page
    if (!this.maxLoaded) this.maxLoaded = app.loading(page.maxOrd)
    const [bid, qid] = [this.market.base.id, this.market.quote.id]
    const [bWallet, qWallet] = [app.assets[bid].wallet, app.assets[qid].wallet]
    if (!bWallet || !bWallet.running || !qWallet || !qWallet.running) return
    if (this.preorderTimer) window.clearTimeout(this.preorderTimer)

    Doc.show(page.maxOrd, page.maxLotBox)
    Doc.hide(page.maxAboveZero)
    page.maxFromLots.textContent = 'calculating...'
    page.maxFromLotsLbl.textContent = ''
    this.preorderTimer = window.setTimeout(async () => {
      this.preorderTimer = null
      const res = await postJSON(path, {
        host: this.market.dex.host,
        base: bid,
        quote: qid,
        ...args
      })
      this.maxLoaded()
      this.maxLoaded = null

      if (!app.checkResponse(res, true)) {
        console.warn('max order estimate not available:', res)
        page.maxFromLots.textContent = 'estimate unavailable'
        return
      }
      success(res)
    }, delay)
  }

  /* setMaxOrder sets the max order text. */
  setMaxOrder (maxOrder, rate) {
    const page = this.page
    Doc.show(page.maxOrd, page.maxLotBox, page.maxAboveZero)
    const sell = this.isSell()
    page.maxFromLots.textContent = maxOrder.lots.toString()
    page.maxFromLotsLbl.textContent = maxOrder.lots === 1 ? 'lot' : 'lots'
    if (maxOrder.lots === 0) {
      Doc.hide(page.maxAboveZero)
      return
    }
    // Could add the maxOrder.estimatedFees here, but that might also be
    // confusing.
    page.maxFromAmt.textContent = Doc.formatCoinValue(maxOrder.value / 1e8)
    page.maxFromTicker.textContent = sell ? this.market.base.symbol : this.market.quote.symbol.toUpperCase()
    // Could subtract the maxOrder.redemptionFees here.
    page.maxToAmt.textContent = Doc.formatCoinValue(maxOrder.value / 1e8 * rate)
    page.maxToTicker.textContent = sell ? this.market.quote.symbol : this.market.base.symbol.toUpperCase()
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
    const [b, q] = [this.market.baseCfg, this.market.quoteCfg]
    this.book = new OrderBook(data, b.symbol, q.symbol)
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
    this.chart.set(this.book, b.lotSize, q.rateStep)
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

  /*
   * ordersSortCompare returns sort compare function according to the active
   * sort key and direction.
   */
  ordersSortCompare () {
    switch (this.ordersSortKey) {
      case 'stamp':
        return (a, b) => this.ordersSortDirection * (b.stamp - a.stamp)
      case 'rate':
        return (a, b) => this.ordersSortDirection * (a.rate - b.rate)
      case 'qty':
        return (a, b) => this.ordersSortDirection * (a.qty - b.qty)
      case 'type':
        return (a, b) => this.ordersSortDirection *
          Order.typeString(a).localeCompare(Order.typeString(b))
      case 'sell':
        return (a, b) => this.ordersSortDirection *
          (Order.sellString(a)).localeCompare(Order.sellString(b))
      case 'status':
        return (a, b) => this.ordersSortDirection *
          (Order.statusString(a)).localeCompare(Order.statusString(b))
      case 'settled':
        return (a, b) => this.ordersSortDirection *
          ((Order.settled(a) * 100 / a.qty) - (Order.settled(b) * 100 / b.qty))
      case 'filled':
        return (a, b) => this.ordersSortDirection *
          ((a.filled * 100 / a.qty) - (b.filled * 100 / b.qty))
    }
  }

  /* refreshActiveOrders refreshes the user's active order list. */
  refreshActiveOrders () {
    const page = this.page
    const metaOrders = this.metaOrders
    const market = this.market
    for (const oid in metaOrders) delete metaOrders[oid]
    const orders = app.orders(market.dex.host, marketID(market.baseCfg.symbol, market.quoteCfg.symbol))
    // Sort orders by sort key.
    const compare = this.ordersSortCompare()
    orders.sort(compare)

    Doc.empty(page.liveList)
    for (const ord of orders) {
      const row = page.liveTemplate.cloneNode(true)
      metaOrders[ord.id] = {
        row: row,
        order: ord
      }
      Doc.bind(row, 'mouseenter', e => {
        this.activeMarkerRate = ord.rate
        this.setMarkers()
      })
      updateUserOrderRow(row, ord)
      if (ord.type === Order.Limit && (ord.tif === Order.StandingTiF && ord.status < Order.StatusExecuted)) {
        const icon = Doc.tmplElement(row, 'cancelBttn')
        Doc.show(icon)
        bind(icon, 'click', e => {
          e.stopPropagation()
          this.showCancel(row, ord.id)
        })
      }
      const side = Doc.tmplElement(row, 'side')
      side.classList.add(ord.sell ? 'sellcolor' : 'buycolor')
      const link = Doc.tmplElement(row, 'link')
      link.href = `order/${ord.id}`
      app.bindInternalNavigation(row)
      page.liveList.appendChild(row)
      app.bindTooltips(row)
    }
    this.setMarkers()
  }

  /* setMarkers sets the depth chart markers for booked orders. */
  setMarkers () {
    const markers = {
      buys: [],
      sells: []
    }
    for (const mo of Object.values(this.metaOrders)) {
      const ord = mo.order
      if (ord.rate && ord.status === Order.StatusBooked) {
        if (ord.sell) {
          markers.sells.push({
            rate: ord.rate / 1e8,
            active: ord.rate === this.activeMarkerRate
          })
        } else {
          markers.buys.push({
            rate: ord.rate / 1e8,
            active: ord.rate === this.activeMarkerRate
          })
        }
      }
    }
    this.chart.setMarkers(markers)
    if (this.book) this.chart.draw()
  }

  /* handleBookRoute is the handler for the 'book' notification, which is sent
   * in response to a new market subscription. The data received will contain
   * the entire order book.
   */
  handleBookRoute (note) {
    app.log('book', 'handleBookRoute:', note)
    const mktBook = note.payload
    const market = this.market
    const page = this.page
    const host = market.dex.host
    const [b, q] = [market.baseCfg, market.quoteCfg]
    if (mktBook.base !== b.id || mktBook.quote !== q.id) return
    this.refreshActiveOrders()
    this.handleBook(mktBook)
    page.marketLoader.classList.add('d-none')
    this.marketList.select(host, b.id, q.id)

    State.store(lastMarketKey, {
      host: note.host,
      base: mktBook.base,
      quote: mktBook.quote
    })

    page.lotSize.textContent = Doc.formatCoinValue(market.baseCfg.lotSize / 1e8)
    page.rateStep.textContent = market.quoteCfg.rateStep / 1e8
    this.baseUnits.forEach(el => { el.textContent = b.symbol.toUpperCase() })
    this.quoteUnits.forEach(el => { el.textContent = q.symbol.toUpperCase() })
    this.balanceWgt.setWallets(host, b.id, q.id)
    this.setMarketBuyOrderEstimate()
    this.refreshActiveOrders()
    if (this.loaded) {
      this.loaded()
      this.loaded = null
      Doc.animate(250, progress => {
        this.main.style.opacity = progress
      })
    }
  }

  /* handleBookOrderRoute is the handler for 'book_order' notifications. */
  handleBookOrderRoute (data) {
    app.log('book', 'handleBookOrderRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload
    if (order.rate > 0) this.book.add(order)
    this.addTableOrder(order)
    this.chart.draw()
  }

  /* handleUnbookOrderRoute is the handler for 'unbook_order' notifications. */
  handleUnbookOrderRoute (data) {
    app.log('book', 'handleUnbookOrderRoute:', data)
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
    app.log('book', 'handleUpdateRemainingRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const update = data.payload
    this.book.updateRemaining(update.token, update.qty)
    this.updateTableOrder(update)
    this.chart.draw()
  }

  /* handleEpochOrderRoute is the handler for 'epoch_order' notifications. */
  handleEpochOrderRoute (data) {
    app.log('book', 'handleEpochOrderRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload
    if (order.rate > 0) this.book.add(order) // No cancels or market orders
    if (order.qty > 0) this.addTableOrder(order) // No cancel orders
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
    form.style.right = '0'
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
    const isSell = order.sell
    const baseAsset = app.assets[order.base]
    const quoteAsset = app.assets[order.quote]
    const toAsset = isSell ? quoteAsset : baseAsset
    const fromAsset = isSell ? baseAsset : quoteAsset

    page.vQty.textContent = Doc.formatCoinValue(order.qty / 1e8)
    page.vSideHeader.textContent = isSell ? 'Sell' : 'Buy'
    page.vSideSubmit.textContent = page.vSideHeader.textContent
    page.vBaseSubmit.textContent = baseAsset.symbol.toUpperCase()
    if (order.isLimit) {
      Doc.show(page.verifyLimit)
      Doc.hide(page.verifyMarket)
      page.vRate.textContent = Doc.formatCoinValue(order.rate / 1e8)
      page.vQuote.textContent = quoteAsset.symbol.toUpperCase()
      page.vTotal.textContent = Doc.formatCoinValue(order.rate / 1e8 * order.qty / 1e8)
      page.vBase.textContent = baseAsset.symbol.toUpperCase()
      page.vSide.textContent = isSell ? 'sell' : 'buy'
    } else {
      Doc.hide(page.verifyLimit)
      Doc.show(page.verifyMarket)
      page.vSide.textContent = 'trade'
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
    // Visually differentiate between buy/sell orders.
    const buyBtnClass = 'buygreen'
    const sellBtnClass = 'sellred'
    if (isSell) {
      page.vHeader.classList.add(sellBtnClass)
      page.vHeader.classList.remove(buyBtnClass)
      page.vSubmit.classList.add(sellBtnClass)
      page.vSubmit.classList.remove(buyBtnClass)
    } else {
      page.vHeader.classList.add(buyBtnClass)
      page.vHeader.classList.remove(sellBtnClass)
      page.vSubmit.classList.add(buyBtnClass)
      page.vSubmit.classList.remove(sellBtnClass)
    }
    this.showForm(page.verifyForm)
    page.vPass.focus()
  }

  async submitCancel () {
    // this will be the page.cancelSubmit button (evt.currentTarget)
    const page = this.page
    const cancelData = this.cancelData
    const order = cancelData.order
    const req = {
      orderID: order.id,
      pw: page.cancelPass.value
    }
    page.cancelPass.value = ''
    const res = await postJSON('/api/cancel', req)
    if (!app.checkResponse(res)) return
    Doc.hide(cancelData.bttn, page.forms)
    order.cancelling = true
  }

  /* showCancel shows a form to confirm submission of a cancel order. */
  showCancel (row, orderID) {
    const order = this.metaOrders[orderID].order
    const page = this.page
    const remaining = order.qty - order.filled
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining / 1e8)
    const symbol = Order.isMarketBuy(order) ? this.market.quote.symbol : this.market.base.symbol
    page.cancelUnit.textContent = symbol.toUpperCase()
    this.showForm(page.cancelForm)
    page.cancelPass.focus()
    this.cancelData = {
      bttn: Doc.tmplElement(row, 'cancelBttn'),
      order: order
    }
  }

  /* showCreate shows the new wallet creation form. */
  showCreate (asset) {
    const page = this.page
    this.currentCreate = asset
    this.walletForm.setAsset(asset)
    this.showForm(page.walletForm)
    this.walletForm.loadDefaults()
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
   * This is used to update the registration status of the current exchange.
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
   * used to update a user's order's status.
   */
  handleOrderNote (note) {
    const order = note.order
    const metaOrder = this.metaOrders[order.id]
    // If metaOrder doesn't exist for the given order it means it was
    // created via dexcctl and the GUI isn't aware of it.
    // Call refreshActiveOrders to grab the order.
    if (!metaOrder) return this.refreshActiveOrders()
    const oldStatus = metaOrder.status
    metaOrder.order = order
    const bttn = Doc.tmplElement(metaOrder.row, 'cancelBttn')
    if (note.subject === 'Missed cancel') {
      Doc.show(bttn)
    }
    if (order.filled === order.qty) {
      // Remove the cancellation button.
      Doc.hide(bttn)
    }
    updateUserOrderRow(metaOrder.row, order)
    // Only reset markers if there is a change, since the chart is redrawn.
    if ((oldStatus === Order.StatusEpoch && order.status === Order.StatusBooked) ||
      (oldStatus === Order.StatusBooked && order.status > Order.StatusBooked)) this.setMarkers()
  }

  /*
   * handleEpochNote handles notifications signalling the start of a new epoch.
   */
  handleEpochNote (note) {
    app.log('book', 'handleEpochNote:', note)
    if (note.host !== this.market.dex.host || note.marketID !== this.market.sid) return
    if (this.book) {
      this.book.setEpoch(note.epoch)
      this.chart.draw()
    }
    this.clearOrderTableEpochs(note.epoch)
    for (const metaOrder of Object.values(this.metaOrders)) {
      const order = metaOrder.order
      const alreadyMatched = note.epoch > order.epoch
      const statusTD = Doc.tmplElement(metaOrder.row, 'status')
      switch (true) {
        case order.type === Order.Limit && order.status === Order.StatusEpoch && alreadyMatched:
          statusTD.textContent = order.tif === Order.ImmediateTiF ? 'executed' : 'booked'
          order.status = order.tif === Order.ImmediateTiF ? Order.StatusExecuted : Order.StatusBooked
          break
        case order.type === Order.Market && order.status === Order.StatusEpoch:
          // Technically don't know if this should be 'executed' or 'settling'.
          statusTD.textContent = 'executed'
          order.status = Order.StatusExecuted
          break
      }
    }
  }

  setBalanceVisibility () {
    if (this.market.dex.connected) {
      Doc.show(this.page.balanceTable)
      Doc.show(this.page.orderForm)
    } else {
      Doc.hide(this.page.balanceTable)
      Doc.hide(this.page.orderForm)
    }
  }

  /* handleBalanceNote handles notifications updating a wallet's balance. */
  handleBalanceNote (note) {
    this.setBalanceVisibility()
    // if connection to dex server fails, it is not possible to retrieve
    // markets.
    if (!this.market.dex.connected) return
    this.balanceWgt.updateAsset(note.assetID)
    // If there's a balance update, refresh the max order section.
    const mkt = this.market
    const avail = note.balance.available
    switch (note.assetID) {
      case mkt.base.id:
        // If we're not showing the max order panel yet, don't do anything.
        if (!mkt.maxSell) break
        if (typeof mkt.sellBalance === 'number' && mkt.sellBalance !== avail) mkt.maxSell = null
        if (this.isSell()) this.preSell()
        break
      case mkt.quote.id:
        if (!Object.keys(mkt.maxBuys).length) break
        if (typeof mkt.buyBalance === 'number' && mkt.buyBalance !== avail) mkt.maxBuys = {}
        if (!this.isSell()) this.preBuy()
    }
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
    const res = await postJSON('/api/trade', req)
    if (!app.checkResponse(res)) return
    // If the wallets are not open locally, they must have been opened during
    // ordering. Grab updated info.
    const baseWallet = app.walletMap[market.base.id]
    const quoteWallet = app.walletMap[market.quote.id]
    if (!baseWallet.open || !quoteWallet.open) {
      this.balanceWgt.updateAsset(market.base.id)
      this.balanceWgt.updateAsset(market.quote.id)
    }
    this.refreshActiveOrders()
    this.chart.draw()
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
    this.previewQuoteAmt(true)
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
    this.previewQuoteAmt(true)
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
      this.depthLines.input = []
      this.drawChartLines()
      this.page.rateField.value = 0
      return
    }
    // Truncate to rate step. If it is a market buy order, do not adjust.
    const adjusted = this.adjustedRate()
    const v = (adjusted / 1e8)
    this.page.rateField.value = v
    this.depthLines.input = [{
      rate: v,
      color: order.sell ? this.chart.theme.sellLine : this.chart.theme.buyLine
    }]
    this.drawChartLines()
    this.previewQuoteAmt(true)
  }

  /*
   * adjustedRate is the current rate field rate, rounded down to a
   * multiple of rateStep.
   */
  adjustedRate () {
    const v = this.page.rateField.value
    if (!v) return null
    const rate = asAtoms(v)
    const rateStep = this.market.quoteCfg.rateStep
    return rate - (rate % rateStep)
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
    let row = tbody.firstChild
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
    const token = order.token
    for (const tbody of [this.page.sellRows, this.page.buyRows]) {
      for (const tr of Array.from(tbody.children)) {
        if (tr.order.token === token) {
          tr.remove()
          return
        }
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
      this.reportClick(rate)
    })
    let qtyTD
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
            // Draw a line on the chart on hover.
            Doc.bind(tr, 'mouseenter', e => {
              const chart = this.chart
              this.depthLines.hover = [{
                rate: order.rate,
                color: order.sell ? chart.theme.sellLine : chart.theme.buyLine
              }]
              this.drawChartLines()
            })
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

  /* drawChartLines draws the hover and input lines on the chart. */
  drawChartLines () {
    this.chart.setLines([...this.depthLines.hover, ...this.depthLines.input])
    this.chart.draw()
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
    Doc.unbind(document, 'keyup', this.keyup)
    clearInterval(this.secondTicker)
  }
}

/*
 *  MarketList represents the list of exchanges and markets on the left side of
 * markets view. The MarketList provides utilities for adjusting the visibility
 * and sort order of markets.
 */
class MarketList {
  constructor (div) {
    this.selected = null
    const xcTmpl = Doc.tmplElement(div, 'xc')
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
    // Cannot find markets if server connection failed.
    if (!firstMkt) return makeMarket(firstXC.host)
    return makeMarket(firstXC.host, firstMkt.baseID, firstMkt.quoteID)
  }

  /* select sets the specified market as selected. */
  select (host, baseID, quoteID) {
    if (this.selected) this.selected.row.classList.remove('selected')
    this.selected = this.xcSection(host).marketRow(baseID, quoteID)
    this.selected.row.classList.add('selected')
  }

  /* setConnectionStatus sets the visibility of the disconnected icon based
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
    const header = Doc.tmplElement(box, 'header')
    this.disconnectedIcon = Doc.tmplElement(header, 'disconnected')
    if (dex.connected) Doc.hide(this.disconnectedIcon)
    header.append(dex.host)

    this.marketRows = []
    this.rows = Doc.tmplElement(box, 'mkts')
    const rowTmpl = Doc.tmplElement(this.rows, 'mktrow')
    this.rows.removeChild(rowTmpl)
    // If disconnected is not possible to get the markets from the server.
    if (!dex.markets) return

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
    Doc.tmplElement(row, 'baseicon').src = Doc.logoPath(mkt.basesymbol)
    Doc.tmplElement(row, 'quoteicon').src = Doc.logoPath(mkt.quotesymbol)
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
    side.locked.textContent = Doc.formatCoinValue((bal.locked + bal.contractlocked) / 1e8)
    side.immature.textContent = Doc.formatCoinValue(bal.immature / 1e8)
    // If the current balance update time is older than an hour, show the
    // expiration icon. Request a balance update, if possible.
    const expired = new Date().getTime() - new Date(bal.stamp).getTime() > anHour
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

/*
 * updateDataCol sets the textContent of descendent template element.
 */
function updateDataCol (tr, col, s) {
  Doc.tmplElement(tr, col).textContent = s
}

/*
 * updateUserOrderRow sets the td contents of the user's order table row.
 */
function updateUserOrderRow (tr, ord) {
  updateDataCol(tr, 'type', Order.typeString(ord))
  updateDataCol(tr, 'side', Order.sellString(ord))
  updateDataCol(tr, 'age', Doc.timeSince(ord.stamp))
  updateDataCol(tr, 'rate', Doc.formatCoinValue(ord.rate / 1e8))
  updateDataCol(tr, 'qty', Doc.formatCoinValue(ord.qty / 1e8))
  updateDataCol(tr, 'filled', `${(ord.filled / ord.qty * 100).toFixed(1)}%`)
  updateDataCol(tr, 'settled', `${(Order.settled(ord) / ord.qty * 100).toFixed(1)}%`)
  updateDataCol(tr, 'status', Order.statusString(ord))
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
