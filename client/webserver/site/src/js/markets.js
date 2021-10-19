import Doc, { WalletIcons } from './doc'
import State from './state'
import BasePage from './basepage'
import OrderBook from './orderbook'
import { CandleChart, DepthChart } from './charts'
import { postJSON } from './http'
import { NewWalletForm, UnlockWalletForm, bind as bindForm } from './forms'
import * as Order from './orderutil'
import ws from './ws'
import * as intl from './locales'

let app
const bind = Doc.bind

const bookRoute = 'book'
const bookOrderRoute = 'book_order'
const unbookOrderRoute = 'unbook_order'
const updateRemainingRoute = 'update_remaining'
const epochOrderRoute = 'epoch_order'
const candlesRoute = 'candles'
const candleUpdateRoute = 'candle_update'
const unmarketRoute = 'unmarket'

const lastMarketKey = 'selectedMarket'
const chartRatioKey = 'chartRatio'
const depthZoomKey = 'depthZoom'

const animationLength = 500

const anHour = 60 * 60 * 1000 // milliseconds

const depthChart = 'depth_chart'
const candleChart = 'candle_chart'

const check = document.createElement('span')
check.classList.add('ico-check')

export default class MarketsPage extends BasePage {
  constructor (application, main, data) {
    super()
    app = application
    const page = this.page = Doc.idDescendants(main)
    this.main = main
    this.loaded = app.loading(this.main.parentElement)
    this.maxLoaded = null
    // There may be multiple pending updates to the max order. This makes sure
    // that the screen is updated with the most recent one.
    this.maxOrderUpdateCounter = 0
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
    // store original title so we can re-append it when updating market value.
    this.ogTitle = document.title

    const depthReporters = {
      click: p => { this.reportDepthClick(p) },
      volume: d => { this.reportDepthVolume(d) },
      mouse: d => { this.reportDepthMouse(d) },
      zoom: z => { this.reportDepthZoom(z) }
    }
    this.depthChart = new DepthChart(page.marketChart, depthReporters, State.fetch(depthZoomKey))

    const candleReporters = {
      mouse: c => { this.reportMouseCandle(c) }
    }
    this.candleChart = new CandleChart(page.marketChart, candleReporters)

    // TODO: Store user's state and reload last known configuration.
    this.candleChart.hide()
    this.currentChart = depthChart
    this.candleDur = ''

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
    cleanTemplates(page.rowTemplate, page.liveTemplate, page.durBttnTemplate)

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
      page.maxLbl.textContent = intl.prep(intl.ID_BUY)
      this.setOrderBttnText()
      this.setOrderVisibility()
      this.drawChartLines()
    })
    bind(page.sellBttn, 'click', () => {
      swapBttns(page.buyBttn, page.sellBttn)
      page.submitBttn.classList.add('sellred')
      page.submitBttn.classList.remove('buygreen')
      page.maxLbl.textContent = intl.prep(intl.ID_SELL)
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
        color: this.isSell() ? this.depthChart.theme.sellLine : this.depthChart.theme.buyLine
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
    bind(page.depthBttn, 'click', () => {
      this.depthChartSelected()
    })
    bind(page.candlestickBttn, 'click', () => {
      this.candleChartSelected()
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
    // Handle the intial candlestick data on the 'candles' route.
    ws.registerRoute(candleUpdateRoute, data => { this.handleCandleUpdateRoute(data) })
    // Handle the candles update on the 'candles' route.
    ws.registerRoute(candlesRoute, data => { this.handleCandlesRoute(data) })
    // Bind the wallet unlock form.
    this.unlockForm = new UnlockWalletForm(app, page.openForm, async () => { this.openFunc() })
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
      this.setDepthMarkers()
    })

    // Load the user's layout preferences.
    const setChartRatio = r => {
      if (r > 0.7) r = 0.7
      else if (r < 0.25) r = 0.25

      const h = r * (this.main.clientHeight - app.header.offsetHeight)
      page.marketChart.style.height = `${h}px`

      this.depthChart.resize(h)
      this.candleChart.resize(h)
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
      conn: note => { this.handleConnNote(note) },
      balance: note => { this.handleBalanceNote(note) },
      feepayment: note => { this.handleFeePayment(note) },
      walletstate: note => { this.handleWalletStateNote(note) }
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
    return !!this.market.dex.pendingFee
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
    // By default the order form should be hidden, and only if market is set
    // and ready for trading the form should show up.
    Doc.hide(page.orderForm)
    const feePaid = !this.hasFeePending()
    const assetsAreSupported = this.assetsAreSupported()
    const base = this.market.base
    const quote = this.market.quote
    const hasWallets = base && app.assets[base.id].wallet && quote && app.assets[quote.id].wallet

    if (feePaid && assetsAreSupported && hasWallets) {
      Doc.show(page.orderForm)
    }
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

    page.loaderMsg.textContent = intl.prep(intl.ID_NOT_SUPPORTED, { asset: symbol.toUpperCase() })
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
  updateRegistrationStatusView () {
    const { page, market: { dex } } = this
    page.regStatusDex.textContent = dex.host

    const pending = dex.pendingFee
    if (!pending) {
      this.setRegistrationStatusView(intl.prep(intl.ID_REGISTRATION_FEE_SUCCESS), '', 'completed')
      return
    }

    const confirmationsRequired = dex.regFees[pending.symbol].confs
    page.confReq.textContent = confirmationsRequired
    const confStatusMsg = `${pending.confs} / ${confirmationsRequired}`
    this.setRegistrationStatusView(intl.prep(intl.ID_WAITING_FOR_CONFS), confStatusMsg, 'waiting')
  }

  /*
   * setRegistrationStatusVisibility toggles the registration status view based
   * on the dex data.
   */
  setRegistrationStatusVisibility () {
    const { page, market: { dex } } = this

    // If dex is not connected to server, is not possible to know fee
    // registration status.
    if (!dex.connected) return

    this.updateRegistrationStatusView()

    if (dex.pendingFee) {
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
      this.page.submitBttn.textContent = intl.prep(intl.ID_SET_BUTTON_SELL, { asset: this.market.base.symbol.toUpperCase() })
    } else this.page.submitBttn.textContent = intl.prep(intl.ID_SET_BUTTON_BUY, { asset: this.market.base.symbol.toUpperCase() })
  }

  setCandleDurBttns () {
    const { page, market } = this
    Doc.empty(page.durBttnBox)
    for (const dur of market.dex.candleDurs) {
      const bttn = page.durBttnTemplate.cloneNode(true)
      bttn.textContent = dur
      Doc.bind(bttn, 'click', () => this.candleDurationSelected(dur))
      page.durBttnBox.appendChild(bttn)
    }
  }

  /* setMarket sets the currently displayed market. */
  async setMarket (host, base, quote) {
    const dex = app.user.exchanges[host]
    const page = this.page
    // If we have not yet connected, there is no dex.assets or any other
    // exchange data, so just put up a message and wait for the connection to be
    // established, at which time handleConnNote will refresh and reload.
    if (!dex.connected) {
      this.market = { dex: dex }
      page.chartErrMsg.textContent = intl.prep(intl.ID_CONNECTION_FAILED)
      Doc.show(page.chartErrMsg)
      this.loaded()
      this.main.style.opacity = 1
      Doc.hide(page.marketLoader)
      return
    }

    const baseCfg = dex.assets[base]
    const quoteCfg = dex.assets[quote]
    const [baseAsset, quoteAsset] = [app.assets[base], app.assets[quote]]
    const rateConversionFactor = Order.RateEncodingFactor / baseAsset.info.unitinfo.conventional.conversionFactor * quoteAsset.info.unitinfo.conventional.conversionFactor
    Doc.hide(page.maxOrd, page.chartErrMsg)
    if (this.preorderTimer) {
      window.clearTimeout(this.preorderTimer)
      this.preorderTimer = null
    }
    const mktId = marketID(baseCfg.symbol, quoteCfg.symbol)
    this.market = {
      dex: dex,
      sid: mktId, // A string market identifier used by the DEX.
      cfg: dex.markets[mktId],
      // app.assets is a map of core.SupportedAsset type, which can be found at
      // client/core/types.go.
      base: baseAsset,
      quote: quoteAsset,
      maxSell: null,
      maxBuys: {},
      candleCaches: {},
      baseCfg,
      quoteCfg,
      rateConversionFactor
    }

    page.marketLoader.classList.remove('d-none')
    if (!dex.candleDurs || dex.candleDurs.length === 0) this.currentChart = depthChart
    if (this.currentChart === depthChart) ws.request('loadmarket', makeMarket(host, base, quote))
    else {
      if (dex.candleDurs.indexOf(this.candleDur) === -1) this.candleDur = dex.candleDurs[0]
      this.loadCandles()
    }
    this.setLoaderMsgVisibility()
    this.setRegistrationStatusVisibility()
    this.resolveOrderFormVisibility()
    this.setOrderBttnText()
    this.setCandleDurBttns()
  }

  /*
   * reportDepthClick is a callback used by the DepthChart when the user clicks
   * on the chart area. The rate field is set to the x-value of the click.
   */
  reportDepthClick (r) {
    this.page.rateField.value = Doc.formatCoinValue(r)
    this.rateFieldChanged()
  }

  /*
   * reportDepthVolume accepts a volume report from the DepthChart and sets the
   * values in the chart legend.
   */
  reportDepthVolume (d) {
    const page = this.page
    const { base, quote } = this.market
    const [b, q] = [base.info.unitinfo, quote.info.unitinfo]
    // DepthChart reports volumes in conventional units. We'll still use
    // formatCoinValue for formatting though.
    page.sellBookedBase.textContent = Doc.formatCoinValue(d.sellBase * b.conventional.conversionFactor, b)
    page.sellBookedQuote.textContent = Doc.formatCoinValue(d.sellQuote * q.conventional.conversionFactor, q)
    page.buyBookedBase.textContent = Doc.formatCoinValue(d.buyBase * b.conventional.conversionFactor, b)
    page.buyBookedQuote.textContent = Doc.formatCoinValue(d.buyQuote * q.conventional.conversionFactor, q)
  }

  /*
   * reportDepthMouse accepts informations about the mouse position on the
   * chart area.
   */
  reportDepthMouse (d) {
    while (this.hovers.length) this.hovers.shift().classList.remove('hover')
    const page = this.page
    if (!d) {
      Doc.hide(page.hoverData)
      return
    }

    // If the user is hovered to within a small percent (based on chart width)
    // of a user order, highlight that order's row.
    for (const metaOrd of Object.values(this.metaOrders)) {
      const [row, ord] = [metaOrd.row, metaOrd.order]
      if (ord.status !== Order.StatusBooked) continue
      if (d.hoverMarkers.indexOf(ord.rate) > -1) {
        row.classList.add('hover')
        this.hovers.push(row)
      }
    }

    page.hoverPrice.textContent = Doc.formatCoinValue(d.rate / this.market.rateConversionFactor)
    page.hoverVolume.textContent = Doc.formatCoinValue(d.depth, this.market.base.info.unitinfo)
    page.hoverVolume.style.color = d.dotColor
    Doc.show(page.hoverData)
  }

  /*
   * reportDepthZoom accepts informations about the current depth chart zoom level.
   * This information is saved to disk so that the zoom level can be maintained
   * across reloads.
   */
  reportDepthZoom (zoom) {
    State.store(depthZoomKey, zoom)
  }

  reportMouseCandle (candle) {
    const page = this.page
    if (!candle) {
      Doc.hide(page.hoverData)
      return
    }

    page.candleStart.textContent = Doc.formatCoinValue(candle.startRate / this.market.rateConversionFactor)
    page.candleEnd.textContent = Doc.formatCoinValue(candle.endRate / this.market.rateConversionFactor)
    page.candleHigh.textContent = Doc.formatCoinValue(candle.highRate / this.market.rateConversionFactor)
    page.candleLow.textContent = Doc.formatCoinValue(candle.lowRate / this.market.rateConversionFactor)
    page.candleVol.textContent = Doc.formatCoinValue(candle.matchVolume, this.market.base.unitInfo)
    Doc.show(page.hoverData)
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
      qty: convertConventional(qtyField.value, conventionalFactor(market.base)),
      rate: convertConventional(page.rateField.value, market.rateConversionFactor), // message-rate
      tifnow: page.tifNow.checked
    }
  }

  /**
   * previewQuoteAmt shows quote amount when rate or quantity input are changed
   */
  previewQuoteAmt (show) {
    const page = this.page
    const order = this.parseOrder()
    const adjusted = this.adjustedRate()
    page.orderErr.textContent = ''
    if (adjusted) {
      if (order.sell) this.preSell()
      else this.preBuy()
    }
    this.depthLines.input = []
    if (adjusted && this.isLimit()) {
      this.depthLines.input = [{
        rate: order.rate / this.market.rateConversionFactor,
        color: order.sell ? this.depthChart.theme.sellLine : this.depthChart.theme.buyLine
      }]
    }
    this.drawChartLines()
    if (!show || !adjusted || !order.qty) {
      page.orderPreview.textContent = ''
      this.drawChartLines()
      return
    }
    const quoteAsset = app.assets[order.quote]
    const total = order.rate * order.qty
    page.orderPreview.textContent = intl.prep(intl.ID_ORDER_PREVIEW, { total, asset: quoteAsset.symbol.toUpperCase() })
    if (this.isSell()) this.preSell()
    else this.preBuy()
  }

  /**
   * preSell populates the max order message for the largest available sell.
   */
  preSell () {
    this.maxOrderUpdateCounter++
    const mkt = this.market
    const baseWallet = app.assets[mkt.base.id].wallet
    if (baseWallet.available < mkt.cfg.lotsize) {
      this.setMaxOrder({ lots: 0 })
      return
    }
    if (mkt.maxSell) {
      this.setMaxOrder(mkt.maxSell.swap)
      return
    }
    // We only fetch pre-sell once per balance update, so don't delay.
    this.schedulePreOrder('/api/maxsell', {}, 0, res => {
      mkt.maxSell = res.maxSell
      mkt.sellBalance = baseWallet.balance.available
      this.setMaxOrder(res.maxSell.swap)
    })
  }

  /**
   * preBuy populates the max order message for the largest available buy.
   */
  preBuy () {
    this.maxOrderUpdateCounter++
    const mkt = this.market
    const rate = this.adjustedRate()
    const quoteWallet = app.assets[mkt.quote.id].wallet
    const aLot = mkt.cfg.lotsize * (rate / Order.RateEncodingFactor)
    if (quoteWallet.balance.available < aLot) {
      this.setMaxOrder({ lots: 0 })
      return
    }
    if (mkt.maxBuys[rate]) {
      this.setMaxOrder(mkt.maxBuys[rate].swap)
      return
    }
    // 0 delay for first fetch after balance update or market change, otherwise
    // meter these at 1 / sec.
    const delay = mkt.maxBuys ? 1000 : 0
    this.schedulePreOrder('/api/maxbuy', { rate: rate }, delay, res => {
      mkt.maxBuys[rate] = res.maxBuy
      mkt.buyBalance = app.assets[mkt.quote.id].wallet.balance.available
      this.setMaxOrder(res.maxBuy.swap)
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
    page.maxFromLots.textContent = intl.prep(intl.ID_CALCULATING)
    page.maxFromLotsLbl.textContent = ''
    const counter = this.maxOrderUpdateCounter
    this.preorderTimer = window.setTimeout(async () => {
      this.preorderTimer = null
      if (counter !== this.maxOrderUpdateCounter) return
      const res = await postJSON(path, {
        host: this.market.dex.host,
        base: bid,
        quote: qid,
        ...args
      })
      if (counter !== this.maxOrderUpdateCounter) return
      if (!app.checkResponse(res, true)) {
        console.warn('max order estimate not available:', res)
        page.maxFromLots.textContent = intl.prep(intl.ID_ESTIMATE_UNAVAILABLE)
        if (this.maxLoaded) {
          this.maxLoaded()
          this.maxLoaded = null
        }
        return
      }
      success(res)
    }, delay)
  }

  /* setMaxOrder sets the max order text. */
  setMaxOrder (maxOrder, toConverter) {
    const page = this.page
    if (this.maxLoaded) {
      this.maxLoaded()
      this.maxLoaded = null
    }
    Doc.show(page.maxOrd, page.maxLotBox, page.maxAboveZero)
    const sell = this.isSell()
    page.maxFromLots.textContent = maxOrder.lots.toString()
    // XXX add plural into format details, so we don't need this
    page.maxFromLotsLbl.textContent = maxOrder.lots === 1 ? 'lot' : 'lots'
    if (maxOrder.lots === 0) {
      Doc.hide(page.maxAboveZero)
      return
    }
    // Could add the maxOrder.estimatedFees here, but that might also be
    // confusing.
    const [fromAsset, toAsset] = sell ? [this.market.base, this.market.quote] : [this.market.quote, this.market.base]
    page.maxFromAmt.textContent = Doc.formatCoinValue(maxOrder.value, fromAsset.info.unitinfo)
    page.maxFromTicker.textContent = fromAsset.symbol.toUpperCase()
    // Could subtract the maxOrder.redemptionFees here.
    const toConversion = sell ? this.adjustedRate() / Order.RateEncodingFactor : Order.RateEncodingFactor / this.adjustedRate()
    page.maxToAmt.textContent = Doc.formatCoinValue(maxOrder.value * toConversion, toAsset.info.unitinfo)
    page.maxToTicker.textContent = toAsset.symbol.toUpperCase()
  }

  /*
   * validateOrder performs some basic order sanity checks, returning boolean
   * true if the order appears valid.
   */
  validateOrder (order) {
    const page = this.page
    if (order.isLimit && !order.rate) {
      Doc.show(page.orderErr)
      page.orderErr.textContent = intl.prep(intl.ID_NO_ZERO_RATE)
      return false
    }
    if (!order.qty) {
      Doc.show(page.orderErr)
      page.orderErr.textContent = intl.prep(intl.ID_NO_ZERO_QUANTITY)
      return false
    }
    return true
  }

  /* handleBook accepts the data sent in the 'book' notification. */
  handleBook (data) {
    const { cfg, base, quote, baseCfg, quoteCfg } = this.market
    this.book = new OrderBook(data, baseCfg.symbol, quoteCfg.symbol)
    this.loadTable()
    for (const order of (data.book.epoch || [])) {
      if (order.rate > 0) this.book.add(order)
      this.addTableOrder(order)
    }
    if (!this.book) {
      this.depthChart.clear()
      Doc.empty(this.page.buyRows)
      Doc.empty(this.page.sellRows)
      return
    }
    this.depthChart.set(this.book, cfg.lotsize, cfg.ratestep, base.info.unitinfo, quote.info.unitinfo)
  }

  /*
   * midGapConventional is the same as midGap, but returns the mid-gap rate as
   * the conventional ratio. This is used to convert from a conventional
   * quantity from base to quote or vice-versa.
   */
  midGapConventional () {
    const gap = this.midGap()
    if (!gap) return gap
    const { base, quote } = this.market
    return gap * base.info.unitinfo.conventional.conversionFactor / quote.info.unitinfo.conventional.conversionFactor
  }

  /*
   * midGap returns the value in the middle of the best buy and best sell. If
   * either one of the buy or sell sides are empty, midGap returns the best rate
   * from the other side. If both sides are empty, midGap returns the value
   * null. The rate returned is the atomic ratio.
   */
  midGap () {
    const book = this.book
    if (!book) return
    if (book.buys && book.buys.length) {
      if (book.sells && book.sells.length) {
        return (book.buys[0].msgRate + book.sells[0].msgRate) / 2 / Order.RateEncodingFactor
      }
      return book.buys[0].msgRate / Order.RateEncodingFactor
    }
    if (book.sells && book.sells.length) {
      return book.sells[0].msgRate / Order.RateEncodingFactor
    }
    return null
  }

  /*
   * setMarketBuyOrderEstimate sets the "min. buy" display for the current
   * market.
   */
  setMarketBuyOrderEstimate () {
    const market = this.market
    const lotSize = market.cfg.lotsize
    const xc = app.user.exchanges[market.dex.host]
    const buffer = xc.markets[market.sid].buybuffer
    const gap = this.midGapConventional()
    if (gap) {
      this.page.minMktBuy.textContent = Doc.formatCoinValue(lotSize * buffer * gap, market.base.info.unitinfo)
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
        this.setDepthMarkers()
      })
      this.updateUserOrderRow(row, ord)
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
    this.setDepthMarkers()
  }

  /*
  * updateUserOrderRow sets the td contents of the user's order table row.
  */
  updateUserOrderRow (tr, ord) {
    updateDataCol(tr, 'type', Order.typeString(ord))
    updateDataCol(tr, 'side', Order.sellString(ord))
    updateDataCol(tr, 'age', Doc.timeSince(ord.stamp))
    updateDataCol(tr, 'rate', Doc.formatCoinValue(ord.rate / this.market.rateConversionFactor))
    updateDataCol(tr, 'qty', Doc.formatCoinValue(ord.qty, this.market.base.info.unitinfo))
    updateDataCol(tr, 'filled', `${(ord.filled / ord.qty * 100).toFixed(1)}%`)
    updateDataCol(tr, 'settled', `${(Order.settled(ord) / ord.qty * 100).toFixed(1)}%`)
    updateDataCol(tr, 'status', Order.statusString(ord))
  }

  /* setMarkers sets the depth chart markers for booked orders. */
  setDepthMarkers () {
    const markers = {
      buys: [],
      sells: []
    }
    const rateFactor = this.market.rateConversionFactor
    for (const mo of Object.values(this.metaOrders)) {
      const ord = mo.order
      if (ord.rate && ord.status === Order.StatusBooked) {
        if (ord.sell) {
          markers.sells.push({
            rate: ord.rate / rateFactor,
            active: ord.rate === this.activeMarkerRate
          })
        } else {
          markers.buys.push({
            rate: ord.rate / rateFactor,
            active: ord.rate === this.activeMarkerRate
          })
        }
      }
    }
    this.depthChart.setMarkers(markers)
    if (this.book) this.depthChart.draw()
  }

  /* updateTitle update the browser title based on the midgap value and the
   * selected assets.
   */
  updateTitle () {
    // gets first price value from buy or from sell, so we can show it on
    // title.
    const midGapValue = this.midGapConventional()
    if (!midGapValue) return

    const market = this.market
    const [b, q] = [market.baseCfg, market.quoteCfg]
    const baseSymb = b.symbol.toUpperCase()
    const quoteSymb = q.symbol.toUpperCase()
    // more than 6 numbers it gets too big for the title.
    document.title = `${Doc.formatCoinValue(midGapValue)} | ${baseSymb}${quoteSymb} | ${this.ogTitle}`
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
    this.updateTitle()
    page.marketLoader.classList.add('d-none')
    this.marketList.select(host, b.id, q.id)

    State.store(lastMarketKey, {
      host: note.host,
      base: mktBook.base,
      quote: mktBook.quote
    })

    page.lotSize.textContent = Doc.formatCoinValue(market.cfg.lotsize, market.base.info.unitinfo)
    page.rateStep.textContent = Doc.formatCoinValue(market.cfg.ratestep / market.rateConversionFactor)
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
    this.updateTitle()
    this.depthChart.draw()
  }

  /* handleUnbookOrderRoute is the handler for 'unbook_order' notifications. */
  handleUnbookOrderRoute (data) {
    app.log('book', 'handleUnbookOrderRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload
    this.book.remove(order.token)
    this.removeTableOrder(order)
    this.updateTitle()
    this.depthChart.draw()
  }

  /*
   * handleUpdateRemainingRoute is the handler for 'update_remaining'
   * notifications.
   */
  handleUpdateRemainingRoute (data) {
    app.log('book', 'handleUpdateRemainingRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const update = data.payload
    this.book.updateRemaining(update.token, update.qty, update.qtyAtomic)
    this.updateTableOrder(update)
    this.depthChart.draw()
  }

  /* handleEpochOrderRoute is the handler for 'epoch_order' notifications. */
  handleEpochOrderRoute (data) {
    app.log('book', 'handleEpochOrderRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload
    if (order.msgRate > 0) this.book.add(order) // No cancels or market orders
    if (order.qtyAtomic > 0) this.addTableOrder(order) // No cancel orders
    this.depthChart.draw()
  }

  /* handleCandlesRoute is the handler for 'candles' notifications. */
  handleCandlesRoute (data) {
    if (this.candlesLoading) {
      clearTimeout(this.candlesLoading.timer)
      this.candlesLoading.loaded()
      this.candlesLoading = null
    }
    this.depthChart.hide()
    this.candleChart.show()
    if (data.host !== this.market.dex.host) return
    const dur = data.payload.dur
    this.market.candleCaches[dur] = data.payload
    if (this.currentChart !== candleChart || this.candleDur !== dur) return
    this.candleChart.setCandles(data.payload, this.market.cfg, this.market.base.info.unitinfo, this.market.quote.info.unitinfo)
  }

  /* handleCandleUpdateRoute is the handler for 'candle_update' notifications. */
  handleCandleUpdateRoute (data) {
    if (data.host !== this.market.dex.host) return
    const { dur, candle } = data.payload
    const cache = this.market.candleCaches[dur]
    if (!cache) return // must not have seen the 'candles' notification yet?
    const candles = cache.candles
    if (candles.length === 0) candles.push(candle)
    else {
      const last = candles[candles.length - 1]
      if (last.startStamp === candle.startStamp) candles[candles.length - 1] = candle
      else candles.push(candle)
    }
    if (this.currentChart !== candleChart || this.candleDur !== dur) return
    this.candleChart.draw()
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
    this.unlockForm.setAsset(app.assets[asset.id])
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

    page.vQty.textContent = Doc.formatCoinValue(order.qty, baseAsset.info.unitinfo)
    page.vSideHeader.textContent = isSell ? intl.prep(intl.ID_SELL) : intl.prep(intl.ID_BUY)
    page.vSideSubmit.textContent = page.vSideHeader.textContent
    page.vBaseSubmit.textContent = baseAsset.symbol.toUpperCase()
    if (order.isLimit) {
      Doc.show(page.verifyLimit)
      Doc.hide(page.verifyMarket)
      page.vRate.textContent = Doc.formatCoinValue(order.rate / this.market.rateConversionFactor)
      page.vQuote.textContent = quoteAsset.symbol.toUpperCase()
      page.vTotal.textContent = Doc.formatCoinValue(order.rate / Order.RateEncodingFactor * order.qty, quoteAsset.info.unitinfo)
      page.vBase.textContent = baseAsset.symbol.toUpperCase()
      page.vSide.textContent = isSell ? intl.prep(intl.ID_SELL).toLowerCase() : intl.prep(intl.ID_BUY).toLowerCase()
    } else {
      Doc.hide(page.verifyLimit)
      Doc.show(page.verifyMarket)
      page.vSide.textContent = intl.prep(intl.ID_TRADE)
      page.vBase.textContent = fromAsset.symbol.toUpperCase()
      const gap = this.midGap()
      if (gap) {
        const received = order.sell ? order.qty * gap : order.qty / gap
        const lotSize = this.market.cfg.lotsize
        const lots = order.sell ? order.qty / lotSize : received / lotSize
        // TODO: Some kind of adjustment to align with lot sizes for market buy?
        page.vmTotal.textContent = Doc.formatCoinValue(received, toAsset.info.unitinfo)
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
    const asset = Order.isMarketBuy(order) ? this.market.quote : this.market.base
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining, asset.info.unitinfo)
    page.cancelUnit.textContent = asset.symbol.toUpperCase()
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
    this.walletForm.setAsset(asset.id)
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
      page.orderErr.textContent = intl.prep(intl.ID_NO_ASSET_WALLET, { asset: market.base.symbol })
      Doc.show(page.orderErr)
      return
    }
    if (!quoteWallet) {
      page.orderErr.textContent = intl.prep(intl.ID_NO_ASSET_WALLET, { asset: market.quote.symbol })
      Doc.show(page.orderErr)
      return
    }
    this.showVerify()
  }

  /*
   * handleWalletStateNote is the handler for the 'walletstate' notification
   * type.
   */
  handleWalletStateNote (note) {
    this.balanceWgt.updateAsset(note.wallet.assetID)
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
    if (note.topic === 'MissedCancel') {
      Doc.show(bttn)
    }
    if (order.filled === order.qty) {
      // Remove the cancellation button.
      Doc.hide(bttn)
    }
    this.updateUserOrderRow(metaOrder.row, order)
    // Only reset markers if there is a change, since the chart is redrawn.
    if ((oldStatus === Order.StatusEpoch && order.status === Order.StatusBooked) ||
      (oldStatus === Order.StatusBooked && order.status > Order.StatusBooked)) this.setDepthMarkers()
  }

  /*
   * handleEpochNote handles notifications signalling the start of a new epoch.
   */
  handleEpochNote (note) {
    app.log('book', 'handleEpochNote:', note)
    if (note.host !== this.market.dex.host || note.marketID !== this.market.sid) return
    if (this.book) {
      this.book.setEpoch(note.epoch)
      this.depthChart.draw()
    }

    this.clearOrderTableEpochs(note.epoch)
    for (const metaOrder of Object.values(this.metaOrders)) {
      const order = metaOrder.order
      const alreadyMatched = note.epoch > order.epoch
      const statusTD = Doc.tmplElement(metaOrder.row, 'status')
      switch (true) {
        case order.type === Order.Limit && order.status === Order.StatusEpoch && alreadyMatched:
          statusTD.textContent = order.tif === Order.ImmediateTiF ? intl.prep(intl.ID_EXECUTED) : intl.prep(intl.ID_BOOKED)
          order.status = order.tif === Order.ImmediateTiF ? Order.StatusExecuted : Order.StatusBooked
          break
        case order.type === Order.Market && order.status === Order.StatusEpoch:
          // Technically don't know if this should be 'executed' or 'settling'.
          statusTD.textContent = intl.prep(intl.ID_EXECUTED)
          order.status = Order.StatusExecuted
          break
      }
    }
  }

  setBalanceVisibility () {
    if (this.market.dex.connected) {
      Doc.show(this.page.balanceTable)
    } else {
      Doc.hide(this.page.balanceTable)
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
    Doc.hide(page.orderErr, page.vErr)
    const order = this.parseOrder()
    const pw = page.vPass.value
    page.vPass.value = ''
    const req = {
      order: order,
      pw: pw
    }
    if (!this.validateOrder(order)) return
    // Show loader and hide submit button.
    page.vSubmit.classList.add('d-hide')
    page.vLoader.classList.remove('d-hide')
    const res = await postJSON('/api/trade', req)
    // Hide loader and show submit button.
    page.vSubmit.classList.remove('d-hide')
    page.vLoader.classList.add('d-hide')
    // If errors display error on confirmation modal.
    if (!app.checkResponse(res, true)) {
      page.vErr.textContent = res.msg
      Doc.show(page.vErr)
      return
    }
    // Hide confirmation modal only on success.
    Doc.hide(page.forms)
    this.refreshActiveOrders()
    this.depthChart.draw()
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
    const lotSize = this.market.cfg.lotsize
    page.lotField.value = lots
    page.qtyField.value = Doc.formatCoinValue(lots * lotSize, this.market.base.info.unitinfo)
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
    const lotSize = this.market.cfg.lotsize
    const lots = Math.floor(order.qty / lotSize)
    const adjusted = lots * lotSize
    page.lotField.value = lots
    if (!order.isLimit && !order.sell) return
    if (finalize) page.qtyField.value = Doc.formatCoinValue(adjusted, this.market.base.info.unitinfo)
    this.previewQuoteAmt(true)
  }

  /*
   * marketBuyChanged is attached to the keyup and change events of the quantity
   * input for the market-buy form.
   */
  marketBuyChanged () {
    const page = this.page
    const qty = convertConventional(page.mktBuyField.value, conventionalFactor(this.market.quote))
    const gap = this.midGap()
    if (!gap || !qty) {
      page.mktBuyLots.textContent = '0'
      page.mktBuyScore.textContent = '0'
      return
    }
    const lotSize = this.market.cfg.lotsize
    const received = qty / gap
    page.mktBuyLots.textContent = (received / lotSize).toFixed(1)
    page.mktBuyScore.textContent = Doc.formatCoinValue(received, this.market.base.info.unitinfo)
  }

  /*
   * rateFieldChanged is attached to the keyup and change events of the rate
   * input.
   */
  rateFieldChanged () {
    // Truncate to rate step. If it is a market buy order, do not adjust.
    const adjusted = this.adjustedRate()
    if (adjusted <= 0) {
      this.depthLines.input = []
      this.drawChartLines()
      this.page.rateField.value = 0
      return
    }
    const order = this.parseOrder()
    const r = adjusted / this.market.rateConversionFactor
    this.page.rateField.value = r
    this.depthLines.input = [{
      rate: r,
      color: order.sell ? this.depthChart.theme.sellLine : this.depthChart.theme.buyLine
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
    const rate = convertConventional(v, this.market.rateConversionFactor)
    const rateStep = this.market.cfg.ratestep
    return rate - (rate % rateStep)
  }

  /* loadTable reloads the table from the current order book information. */
  loadTable () {
    this.loadTableSide(true)
    this.loadTableSide(false)
  }

  /* binOrdersByRateAndEpoch takes a list of sorted orders and returns the
     same orders grouped into arrays. The orders are grouped by their rate
     and whether or not they are epoch queue orders. Epoch queue orders
     will come after non epoch queue orders with the same rate. */
  binOrdersByRateAndEpoch (orders) {
    if (!orders || !orders.length) return []
    const bins = []
    let currEpochBin = []
    let currNonEpochBin = []
    let currRate = orders[0].msgRate
    if (orders[0].epoch) currEpochBin.push(orders[0])
    else currNonEpochBin.push(orders[0])
    for (let i = 1; i < orders.length; i++) {
      if (orders[i].msgRate !== currRate) {
        bins.push(currNonEpochBin)
        bins.push(currEpochBin)
        currEpochBin = []
        currNonEpochBin = []
        currRate = orders[i].msgRate
      }
      if (orders[i].epoch) currEpochBin.push(orders[i])
      else currNonEpochBin.push(orders[i])
    }
    bins.push(currNonEpochBin)
    bins.push(currEpochBin)
    return bins.filter(bin => bin.length > 0)
  }

  /* loadTables loads the order book side into its table. */
  loadTableSide (sell) {
    const bookSide = sell ? this.book.sells : this.book.buys
    const tbody = sell ? this.page.sellRows : this.page.buyRows
    Doc.empty(tbody)
    if (!bookSide || !bookSide.length) return
    const orderBins = this.binOrdersByRateAndEpoch(bookSide)
    orderBins.forEach(bin => { tbody.appendChild(this.orderTableRow(bin)) })
  }

  /* addTableOrder adds a single order to the appropriate table. */
  addTableOrder (order) {
    const tbody = order.sell ? this.page.sellRows : this.page.buyRows
    let row = tbody.firstChild
    // Handle market order differently.
    if (order.rate === 0) {
      // This is a market order.
      if (row && row.manager.getRate() === 0) {
        row.manager.insertOrder(order)
      } else {
        row = this.orderTableRow([order])
        tbody.insertBefore(row, tbody.firstChild)
      }
      return
    }
    // Must be a limit order. Sort by rate. Skip the market order row.
    if (row && row.manager.getRate() === 0) row = row.nextSibling
    while (row) {
      if (row.manager.compare(order) === 0) {
        row.manager.insertOrder(order)
        return
      } else if (row.manager.compare(order) > 0) {
        const tr = this.orderTableRow([order])
        tbody.insertBefore(tr, row)
        return
      }
      row = row.nextSibling
    }
    const tr = this.orderTableRow([order])
    tbody.appendChild(tr)
  }

  /* removeTableOrder removes a single order from its table. */
  removeTableOrder (order) {
    const token = order.token
    for (const tbody of [this.page.sellRows, this.page.buyRows]) {
      for (const tr of Array.from(tbody.children)) {
        if (tr.manager.removeOrder(token)) {
          return
        }
      }
    }
  }

  /* updateTableOrder looks for the order in the table and updates the qty */
  updateTableOrder (update) {
    for (const tbody of [this.page.sellRows, this.page.buyRows]) {
      for (const tr of Array.from(tbody.children)) {
        if (tr.manager.updateOrderQty(update)) {
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
      tr.manager.removeEpochOrders()
    }
  }

  /*
   * orderTableRow creates a new <tr> element to insert into an order table.
     Takes a bin of orders with the same rate, and displays the total quantity.
   */
  orderTableRow (orderBin) {
    const tr = this.page.rowTemplate.cloneNode(true)
    const { base, rateConversionFactor } = this.market
    const manager = new OrderTableRowManager(tr, orderBin, base.info.unitinfo, rateConversionFactor)
    tr.manager = manager
    bind(tr, 'click', () => {
      this.reportDepthClick(tr.manager.getRate())
    })
    if (tr.manager.getRate() !== 0) {
      Doc.bind(tr, 'mouseenter', e => {
        const chart = this.depthChart
        this.depthLines.hover = [{
          rate: tr.manager.getRate() / rateConversionFactor,
          color: tr.manager.isSell() ? chart.theme.sellLine : chart.theme.buyLine
        }]
        this.drawChartLines()
      })
    }
    return tr
  }

  /* handleConnNote handles the 'conn' notification.
   */
  async handleConnNote (note) {
    this.marketList.setConnectionStatus(note)
    if (note.connected) {
      // Having been disconnected from a DEX server, anything may have changed,
      // or this may be the first opportunity to get the server's config, so
      // fetch it all before reloading the markets page.
      await app.fetchUser()
      app.loadPage('markets')
    }
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
    this.depthChart.setLines([...this.depthLines.hover, ...this.depthLines.input])
    this.depthChart.draw()
  }

  /*
   * depthChartSelected is called when the user clicks a button to show the
   * depth chart.
  */
  depthChartSelected () {
    const page = this.page
    Doc.hide(page.depthBttn, page.durBttnBox, page.candleHoverData)
    Doc.show(page.candlestickBttn, page.epochLine, page.depthHoverData, page.depthSummary)
    this.currentChart = depthChart
    this.depthChart.show()
    this.candleChart.hide()
  }

  /*
   * candleChartSelected is called when the user clicks a button to show the
   * historical market data (candlestick) chart.
  */
  candleChartSelected () {
    const page = this.page
    const dex = this.market.dex
    this.currentChart = candleChart
    Doc.hide(page.candlestickBttn, page.epochLine, page.depthHoverData, page.depthSummary)
    Doc.show(page.depthBttn, page.durBttnBox, page.candleHoverData)
    if (dex.candleDurs.indexOf(this.candleDur) === -1) this.candleDur = dex.candleDurs[0]
    this.loadCandles()
  }

  /* candleDurationSelected sets the candleDur and loads the candles. */
  candleDurationSelected (dur) {
    this.candleDur = dur
    this.loadCandles()
  }

  /*
   * loadCandles loads the candles for the current candleDur. If a cache is already
   * active, the cache will be used without a loadcandles request.
   */
  loadCandles () {
    for (const bttn of this.page.durBttnBox.children) {
      if (bttn.textContent === this.candleDur) bttn.classList.add('selected')
      else bttn.classList.remove('selected')
    }
    const { candleCaches, cfg } = this.market
    const cache = candleCaches[this.candleDur]
    if (cache) {
      this.depthChart.hide()
      this.candleChart.show()
      this.candleChart.setCandles(cache, cfg)
      return
    }
    this.requestCandles()
  }

  /* requestCandles sends the loadcandles request. */
  requestCandles () {
    const loaded = app.loading(this.page.marketChart)
    this.candlesLoading = {
      loaded: loaded,
      timer: setTimeout(() => {
        if (this.candlesLoading) {
          this.candlesLoading = null
          loaded()
          console.error('candles not received')
        }
      }, 10000)
    }
    const { dex, base, quote } = this.market
    ws.request('loadcandles', { host: dex.host, base: base.id, quote: quote.id, dur: this.candleDur })
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /markets page.
   */
  unload () {
    ws.request(unmarketRoute, {})
    ws.deregisterRoute(bookRoute)
    ws.deregisterRoute(epochOrderRoute)
    ws.deregisterRoute(bookOrderRoute)
    ws.deregisterRoute(unbookOrderRoute)
    ws.deregisterRoute(candlesRoute)
    ws.deregisterRoute(candleUpdateRoute)
    this.depthChart.unattach()
    this.candleChart.unattach()
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
    this.lotSize = mkt.lotsize
    this.rateStep = mkt.ratestep
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
    const els = Doc.idDescendants(table)
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
    side.avail.textContent = Doc.formatCoinValue(bal.available, asset.info.unitinfo)
    side.locked.textContent = Doc.formatCoinValue((bal.locked + bal.contractlocked), asset.info.unitinfo)
    side.immature.textContent = Doc.formatCoinValue(bal.immature, asset.info.unitinfo)
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

/*
 * conventionalFactor picks out the conversion factor for conventional units for
 * the asset [core.SupportedAsset].
 */
function conventionalFactor (asset) { return asset.info.unitinfo.conventional.conversionFactor }

/* convertConventional converts the float string to atoms. */
function convertConventional (s, conversionFactor) {
  return Math.round(parseFloat(s) * conversionFactor)
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
 * cleanTemplates removes the elements from the DOM and deletes the id
 * attribute.
 */
function cleanTemplates (...tmpls) {
  tmpls.forEach(tmpl => {
    tmpl.remove()
    tmpl.removeAttribute('id')
  })
}

// OrderTableRowManager manages the data within a row in an order table. Each row
// represents all the orders in the order book with the same rate, but orders that
// are booked or still in the epoch queue are displayed in separate rows.
class OrderTableRowManager {
  constructor (tableRow, orderBin, baseUnitInfo, rateConversionFactor) {
    this.tableRow = tableRow
    this.orderBin = orderBin
    this.sell = orderBin[0].sell
    this.msgRate = orderBin[0].msgRate
    this.epoch = !!orderBin[0].epoch
    this.baseUnitInfo = baseUnitInfo
    this.rateConversionFactor = rateConversionFactor
    this.setRateEl()
    this.setEpochEl()
    this.updateQtyNumOrdersEl()
  }

  // setEpochEl displays a checkmark in the row if the orders represented by
  // this row are in the epoch queue.
  setEpochEl () {
    const epochEl = Doc.tmplElement(this.tableRow, 'epoch')
    if (this.isEpoch()) epochEl.appendChild(check.cloneNode())
  }

  // setRateEl popuplates the rate element in the row.
  setRateEl () {
    const rateEl = Doc.tmplElement(this.tableRow, 'rate')
    if (this.msgRate === 0) {
      rateEl.innerText = 'market'
    } else {
      const cssClass = this.isSell() ? 'sellcolor' : 'buycolor'
      rateEl.innerText = Doc.formatFullPrecision(this.msgRate / this.rateConversionFactor)
      rateEl.classList.add(cssClass)
    }
  }

  // updateQtyNumOrdersEl populates the quantity element in the row, and also
  // displays the number of orders if there is more than one order in the order
  // bin.
  updateQtyNumOrdersEl () {
    const qty = this.orderBin.reduce((total, curr) => total + curr.qtyAtomic, 0)
    const numOrders = this.orderBin.length
    const qtyEl = Doc.tmplElement(this.tableRow, 'qty')
    const numOrdersEl = Doc.tmplElement(this.tableRow, 'numorders')
    qtyEl.innerText = Doc.formatFullPrecision(qty, this.baseUnitInfo)
    if (numOrders > 1) {
      numOrdersEl.removeAttribute('hidden')
      numOrdersEl.innerText = numOrders
      numOrdersEl.title = `quantity is comprised of ${numOrders} orders`
    } else {
      numOrdersEl.setAttribute('hidden', true)
    }
  }

  // insertOrder adds an order to the order bin and updates the row elements
  // accordingly.
  insertOrder (order) {
    this.orderBin.push(order)
    this.updateQtyNumOrdersEl()
  }

  // updateOrderQuantity updates the quantity of the order identified by a token,
  // if it exists in the row, and updates the row elements accordingly. The function
  // returns true if the order is in the bin, and false otherwise.
  updateOrderQty (update) {
    const { token, qty, qtyAtomic } = update
    for (let i = 0; i < this.orderBin.length; i++) {
      if (this.orderBin[i].token === token) {
        this.orderBin[i].qty = qty
        this.orderBin[i].qtyAtomic = qtyAtomic
        this.updateQtyNumOrdersEl()
        return true
      }
    }
    return false
  }

  // removeOrder removes the order identified by the token, if it exists in the row,
  // and updates the row elements accordingly. If the order bin is empty, the row is
  // removed from the screen. The function returns true if an order was removed, and
  // false otherwise.
  removeOrder (token) {
    const index = this.orderBin.findIndex(order => order.token === token)
    if (index < 0) return false
    this.orderBin.splice(index, 1)
    if (!this.orderBin.length) this.tableRow.remove()
    else this.updateQtyNumOrdersEl()
    return true
  }

  // removeEpochOrders removes all the orders from the row that are not in the
  // new epoch's epoch queue and updates the elements accordingly.
  removeEpochOrders (newEpoch) {
    this.orderBin = this.orderBin.filter((order) => {
      return !(order.epoch && order.epoch !== newEpoch)
    })
    if (!this.orderBin.length) this.tableRow.remove()
    else this.updateQtyNumOrdersEl()
  }

  // getRate returns the rate of the orders in the row.
  getRate () {
    return this.msgRate
  }

  // isEpoch returns whether the orders in this row are in the epoch queue.
  isEpoch () {
    return this.epoch
  }

  // isSell returns whether the orders in this row are sell orders.
  isSell () {
    return this.sell
  }

  // compare takes an order and returns 0 if the order belongs in this row,
  // 1 if the order should go after this row in the table, and -1 if it should
  // be before this row in the table. Sell orders are displayed in ascending order,
  // buy orders are displayed in descending order, and epoch orders always come
  // after booked orders.
  compare (order) {
    if (this.getRate() === order.msgRate && this.isEpoch() === !!order.epoch) {
      return 0
    } else if (this.getRate() !== order.msgRate) {
      return (this.getRate() > order.msgRate) === order.sell ? 1 : -1
    } else {
      return this.isEpoch() ? 1 : -1
    }
  }
}
