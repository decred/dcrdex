import Doc, { WalletIcons } from './doc'
import State from './state'
import BasePage from './basepage'
import OrderBook from './orderbook'
import {
  CandleChart,
  DepthChart,
  DepthLine,
  CandleReporters,
  MouseReport,
  VolumeReport,
  DepthMarker
} from './charts'
import { postJSON } from './http'
import { NewWalletForm, UnlockWalletForm, AccelerateOrderForm, bind as bindForm } from './forms'
import * as OrderUtil from './orderutil'
import ws from './ws'
import * as intl from './locales'
import {
  app,
  SupportedAsset,
  PageElement,
  Order,
  Market,
  OrderEstimate,
  MaxOrderEstimate,
  Exchange,
  UnitInfo,
  Asset,
  Candle,
  CandlesPayload,
  TradeForm,
  BookUpdate,
  MaxSell,
  MaxBuy,
  SwapEstimate,
  MarketOrderBook,
  APIResponse,
  PreSwap,
  PreRedeem,
  WalletStateNote,
  WalletCreationNote,
  SpotPriceNote,
  FeePaymentNote,
  OrderNote,
  EpochNote,
  BalanceNote,
  MiniOrder,
  RemainderUpdate,
  ConnEventNote,
  Spot,
  OrderOption,
  ConnectionStatus
} from './registry'

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

const percentFormatter = new Intl.NumberFormat(document.documentElement.lang, {
  minimumFractionDigits: 1,
  maximumFractionDigits: 2
})

const parentIDNone = 0xFFFFFFFF

interface MetaOrder {
  row: HTMLElement
  order: Order
  cancelling?: boolean
  status?: number
}

interface CancelData {
  bttn: PageElement
  order: Order
}

interface CurrentMarket {
  dex: Exchange
  sid: string // A string market identifier used by the DEX.
  cfg: Market
  base: SupportedAsset
  quote: SupportedAsset
  baseUnitInfo: UnitInfo
  quoteUnitInfo: UnitInfo
  maxSellRequested: boolean
  maxSell: MaxOrderEstimate | null
  sellBalance: number
  buyBalance: number
  maxBuys: Record<number, MaxOrderEstimate>
  candleCaches: Record<string, CandlesPayload>
  baseCfg: Asset
  quoteCfg: Asset
  rateConversionFactor: number
}

interface BalanceWidgetElement {
  id: number
  parentID: number
  cfg: Asset | null
  logo: PageElement
  symbol: PageElement
  avail: PageElement
  newWalletRow: PageElement
  newWalletBttn: PageElement
  walletPendingRow: PageElement
  locked: PageElement
  immature: PageElement
  parentBal: PageElement
  parentIcon: PageElement
  unsupported: PageElement
  expired: PageElement
  connect: PageElement
  spinner: PageElement
  iconBox: PageElement
  stateIcons: WalletIcons
}

interface LoadTracker {
  loaded: () => void
  timer: number
}

interface OrderRow extends HTMLElement {
  manager: OrderTableRowManager
}

export default class MarketsPage extends BasePage {
  page: Record<string, PageElement>
  main: HTMLElement
  loaded: (() => void) | null
  maxLoaded: (() => void) | null
  maxOrderUpdateCounter: number
  market: CurrentMarket
  currentForm: HTMLElement
  openAsset: SupportedAsset
  openFunc: () => void
  currentCreate: SupportedAsset
  maxEstimateTimer: number | null
  book: OrderBook
  cancelData: CancelData
  metaOrders: Record<string, MetaOrder>
  preorderCache: Record<string, OrderEstimate>
  currentOrder: TradeForm
  depthLines: Record<string, DepthLine[]>
  activeMarkerRate: number | null
  hovers: HTMLElement[]
  ordersSortKey: string
  ordersSortDirection: 1 | -1
  ogTitle: string
  depthChart: DepthChart
  candleChart: CandleChart
  currentChart: string
  candleDur: string
  balanceWgt: BalanceWidget
  marketList: MarketList
  quoteUnits: NodeListOf<HTMLElement>
  baseUnits: NodeListOf<HTMLElement>
  unlockForm: UnlockWalletForm
  newWalletForm: NewWalletForm
  keyup: (e: KeyboardEvent) => void
  secondTicker: number
  candlesLoading: LoadTracker | null
  accelerateOrderForm: AccelerateOrderForm

  constructor (main: HTMLElement, data: any) {
    super()
    const page = this.page = Doc.idDescendants(main)
    this.main = main
    if (!this.main.parentElement) return // Not gonna happen, but TypeScript cares.
    this.loaded = app().loading(this.main.parentElement)
    // There may be multiple pending updates to the max order. This makes sure
    // that the screen is updated with the most recent one.
    this.maxOrderUpdateCounter = 0
    this.metaOrders = {}
    this.preorderCache = {}
    this.depthLines = {
      hover: [],
      input: []
    }
    this.hovers = []
    // 'Your Orders' list sort key and direction.
    this.ordersSortKey = 'submitTime'
    // 1 if sorting ascendingly, -1 if sorting descendingly.
    this.ordersSortDirection = 1
    // store original title so we can re-append it when updating market value.
    this.ogTitle = document.title

    const depthReporters = {
      click: (x: number) => { this.reportDepthClick(x) },
      volume: (r: VolumeReport) => { this.reportDepthVolume(r) },
      mouse: (r: MouseReport) => { this.reportDepthMouse(r) },
      zoom: (z: number) => { this.reportDepthZoom(z) }
    }
    this.depthChart = new DepthChart(page.marketChart, depthReporters, State.fetch(depthZoomKey))

    const candleReporters: CandleReporters = {
      mouse: c => { this.reportMouseCandle(c) }
    }
    this.candleChart = new CandleChart(page.marketChart, candleReporters)

    const success = () => { /* do nothing */ }
    // Do not call cleanTemplates before creating the AccelerateOrderForm
    this.accelerateOrderForm = new AccelerateOrderForm(page.accelerateForm, success)

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
    OrderUtil.setOptionTemplates(page)
    Doc.cleanTemplates(page.rowTemplate, page.liveTemplate, page.durBttnTemplate, page.booleanOptTmpl, page.rangeOptTmpl, page.orderOptTmpl)

    // Prepare the list of markets.
    this.marketList = new MarketList(page.marketList)
    for (const xc of this.marketList.xcSections) {
      if (!xc.marketRows) continue
      for (const row of xc.marketRows) {
        bind(row.node, 'click', () => {
          this.setMarket(xc.host, row.mkt.baseid, row.mkt.quoteid)
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
        rate: parseFloat(page.rateField.value || '0'),
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
      if (this.isSell()) {
        const maxSell = this.market.maxSell
        if (!maxSell) return
        page.lotField.value = String(maxSell.swap.lots)
      } else page.lotField.value = String(this.market.maxBuys[this.adjustedRate()].swap.lots)
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
    ws.registerRoute(bookRoute, (data: BookUpdate) => { this.handleBookRoute(data) })
    // Handle the new order for the order book on the 'book_order' route.
    ws.registerRoute(bookOrderRoute, (data: BookUpdate) => { this.handleBookOrderRoute(data) })
    // Remove the order sent on the 'unbook_order' route from the orderbook.
    ws.registerRoute(unbookOrderRoute, (data: BookUpdate) => { this.handleUnbookOrderRoute(data) })
    // Update the remaining quantity on a booked order.
    ws.registerRoute(updateRemainingRoute, (data: BookUpdate) => { this.handleUpdateRemainingRoute(data) })
    // Handle the new order for the order book on the 'epoch_order' route.
    ws.registerRoute(epochOrderRoute, (data: BookUpdate) => { this.handleEpochOrderRoute(data) })
    // Handle the intial candlestick data on the 'candles' route.
    ws.registerRoute(candleUpdateRoute, (data: BookUpdate) => { this.handleCandleUpdateRoute(data) })
    // Handle the candles update on the 'candles' route.
    ws.registerRoute(candlesRoute, (data: BookUpdate) => { this.handleCandlesRoute(data) })
    // Bind the wallet unlock form.
    this.unlockForm = new UnlockWalletForm(page.unlockWalletForm, async () => { this.openFunc() })
    // Create a wallet
    this.newWalletForm = new NewWalletForm(page.newWalletForm, async () => { this.createWallet() })
    // Main order form.
    bindForm(page.orderForm, page.submitBttn, async () => { this.stepSubmit() })
    // Order verification form.
    bindForm(page.verifyForm, page.vSubmit, async () => { this.submitOrder() })
    // Unlock for order estimation
    Doc.bind(page.vUnlockSubmit, 'click', async () => { this.submitEstimateUnlock() })
    // Cancel order form.
    bindForm(page.cancelForm, page.cancelSubmit, async () => { this.submitCancel() })
    // Order detail view
    Doc.bind(page.vFeeDetails, 'click', () => this.showForm(page.vDetailPane))
    Doc.bind(page.closeDetailPane, 'click', () => this.showVerifyForm())
    // Bind active orders list's header sort events.
    page.liveTable.querySelectorAll('[data-ordercol]')
      .forEach((th: HTMLElement) => bind(th, 'click', () => setOrdersSortCol(th.dataset.ordercol || '')))

    const setOrdersSortCol = (key: string) => {
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
      Doc.safeSelector(page.liveTable, `[data-ordercol=${key}]`).classList.add(sortCls)
    }

    // Set default's sorted col header classes.
    setOrdersSortColClasses()

    const closePopups = () => {
      Doc.hide(page.forms)
      page.vPass.value = ''
      page.cancelPass.value = ''
    }

    // If the user clicks outside of a form, it should close the page overlay.
    bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (Doc.isDisplayed(page.vDetailPane) && !Doc.mouseInElement(e, page.vDetailPane)) return this.showVerifyForm()
      if (!Doc.mouseInElement(e, this.currentForm)) {
        closePopups()
      }
    })

    this.keyup = (e: KeyboardEvent) => {
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
    const setChartRatio = (r: number) => {
      if (r > 0.7) r = 0.7
      else if (r < 0.25) r = 0.25

      const h = r * (this.main.clientHeight - app().header.offsetHeight)
      page.marketChart.style.height = `${h}px`

      this.depthChart.resize(h)
      this.candleChart.resize(h)
    }
    const chartDivRatio = State.fetch(chartRatioKey)
    if (chartDivRatio) {
      setChartRatio(chartDivRatio)
    }
    // Bind chart resizing.
    bind(page.chartResizer, 'mousedown', (e: MouseEvent) => {
      if (e.button !== 0) return
      e.preventDefault()
      let chartRatio: number
      const trackMouse = (ee: MouseEvent) => {
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
    app().registerNoteFeeder({
      order: (note: OrderNote) => { this.handleOrderNote(note) },
      epoch: (note: EpochNote) => { this.handleEpochNote(note) },
      conn: (note: ConnEventNote) => { this.handleConnNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      feepayment: (note: FeePaymentNote) => { this.handleFeePayment(note) },
      spots: (note: SpotPriceNote) => { this.handlePriceUpdate(note) }
    })

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
    this.secondTicker = window.setInterval(() => {
      for (const metaOrder of Object.values(this.metaOrders)) {
        const td = Doc.tmplElement(metaOrder.row, 'age')
        td.textContent = Doc.timeSince(metaOrder.order.submitTime)
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
    const { base, quote } = this.market
    const hasWallets = base && app().assets[base.id].wallet && quote && app().assets[quote.id].wallet

    if (feePaid && assetsAreSupported && hasWallets) {
      Doc.show(page.orderForm)
    }
  }

  /* setLoaderMsgVisibility displays a message in case a dex asset is not
   * supported
   */
  setLoaderMsgVisibility () {
    const { page, market } = this

    if (this.assetsAreSupported()) {
      // make sure to hide the loader msg
      Doc.hide(page.loaderMsg)
      return
    }
    const symbol = market.base ? market.quoteCfg.symbol : market.baseCfg.symbol
    page.loaderMsg.textContent = intl.prep(intl.ID_NOT_SUPPORTED, { asset: symbol.toUpperCase() })
    Doc.show(page.loaderMsg)
  }

  /* setRegistrationStatusView sets the text content and class for the
   * registration status view
   */
  setRegistrationStatusView (titleContent: string, confStatusMsg: string, titleClass: string) {
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
    page.confReq.textContent = String(confirmationsRequired)
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
    if (dex.connectionStatus !== ConnectionStatus.Connected) return

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
      this.page.submitBttn.textContent = intl.prep(intl.ID_SET_BUTTON_SELL, { asset: Doc.shortSymbol(this.market.baseCfg.symbol) })
    } else this.page.submitBttn.textContent = intl.prep(intl.ID_SET_BUTTON_BUY, { asset: Doc.shortSymbol(this.market.baseCfg.symbol) })
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
  async setMarket (host: string, base: number, quote: number) {
    const dex = app().user.exchanges[host]
    const page = this.page

    // reset form inputs
    page.lotField.value = ''
    page.qtyField.value = ''
    page.rateField.value = ''

    // If we have not yet connected, there is no dex.assets or any other
    // exchange data, so just put up a message and wait for the connection to be
    // established, at which time handleConnNote will refresh and reload.
    if (dex.connectionStatus !== ConnectionStatus.Connected) {
      // TODO: Figure out why this was like this.
      // this.market = { dex: dex }

      page.chartErrMsg.textContent = intl.prep(intl.ID_CONNECTION_FAILED)
      Doc.show(page.chartErrMsg)
      if (this.loaded) this.loaded()
      this.main.style.opacity = '1'
      Doc.hide(page.marketLoader)
      return
    }

    const baseCfg = dex.assets[base]
    const quoteCfg = dex.assets[quote]

    const [bui, qui] = [app().unitInfo(base, dex), app().unitInfo(quote, dex)]

    const rateConversionFactor = OrderUtil.RateEncodingFactor / bui.conventional.conversionFactor * qui.conventional.conversionFactor
    Doc.hide(page.maxOrd, page.chartErrMsg)
    if (this.maxEstimateTimer) {
      window.clearTimeout(this.maxEstimateTimer)
      this.maxEstimateTimer = null
    }
    const mktId = marketID(baseCfg.symbol, quoteCfg.symbol)
    this.market = {
      dex: dex,
      sid: mktId, // A string market identifier used by the DEX.
      cfg: dex.markets[mktId],
      // app().assets is a map of core.SupportedAsset type, which can be found at
      // client/core/types.go.
      base: app().assets[base],
      quote: app().assets[quote],
      baseUnitInfo: bui,
      quoteUnitInfo: qui,
      maxSell: null,
      maxBuys: {},
      maxSellRequested: false,
      candleCaches: {},
      baseCfg,
      quoteCfg,
      rateConversionFactor,
      sellBalance: 0,
      buyBalance: 0
    }

    Doc.show(page.marketLoader)
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
    this.previewQuoteAmt(false)
  }

  /*
   * reportDepthClick is a callback used by the DepthChart when the user clicks
   * on the chart area. The rate field is set to the x-value of the click.
   */
  reportDepthClick (r: number) {
    this.page.rateField.value = String(r)
    this.rateFieldChanged()
  }

  /*
   * reportDepthVolume accepts a volume report from the DepthChart and sets the
   * values in the chart legend.
   */
  reportDepthVolume (r: VolumeReport) {
    const page = this.page
    const { baseUnitInfo: b, quoteUnitInfo: q } = this.market
    // DepthChart reports volumes in conventional units. We'll still use
    // formatCoinValue for formatting though.
    page.sellBookedBase.textContent = Doc.formatCoinValue(r.sellBase * b.conventional.conversionFactor, b)
    page.sellBookedQuote.textContent = Doc.formatCoinValue(r.sellQuote * q.conventional.conversionFactor, q)
    page.buyBookedBase.textContent = Doc.formatCoinValue(r.buyBase * b.conventional.conversionFactor, b)
    page.buyBookedQuote.textContent = Doc.formatCoinValue(r.buyQuote * q.conventional.conversionFactor, q)
  }

  /*
   * reportDepthMouse accepts informations about the mouse position on the
   * chart area.
   */
  reportDepthMouse (r: MouseReport) {
    while (this.hovers.length) (this.hovers.shift() as HTMLElement).classList.remove('hover')
    const page = this.page
    if (!r) {
      Doc.hide(page.hoverData)
      return
    }

    // If the user is hovered to within a small percent (based on chart width)
    // of a user order, highlight that order's row.
    for (const metaOrd of Object.values(this.metaOrders)) {
      const [row, ord] = [metaOrd.row, metaOrd.order]
      if (ord.status !== OrderUtil.StatusBooked) continue
      if (r.hoverMarkers.indexOf(ord.rate) > -1) {
        row.classList.add('hover')
        this.hovers.push(row)
      }
    }

    page.hoverPrice.textContent = Doc.formatCoinValue(r.rate)
    page.hoverVolume.textContent = Doc.formatCoinValue(r.depth)
    page.hoverVolume.style.color = r.dotColor
    Doc.show(page.hoverData)
  }

  /*
   * reportDepthZoom accepts informations about the current depth chart zoom level.
   * This information is saved to disk so that the zoom level can be maintained
   * across reloads.
   */
  reportDepthZoom (zoom: number) {
    State.store(depthZoomKey, zoom)
  }

  reportMouseCandle (candle: Candle | null) {
    const page = this.page
    if (!candle) {
      Doc.hide(page.hoverData)
      return
    }

    page.candleStart.textContent = Doc.formatCoinValue(candle.startRate / this.market.rateConversionFactor)
    page.candleEnd.textContent = Doc.formatCoinValue(candle.endRate / this.market.rateConversionFactor)
    page.candleHigh.textContent = Doc.formatCoinValue(candle.highRate / this.market.rateConversionFactor)
    page.candleLow.textContent = Doc.formatCoinValue(candle.lowRate / this.market.rateConversionFactor)
    page.candleVol.textContent = Doc.formatCoinValue(candle.matchVolume, this.market.baseUnitInfo)
    Doc.show(page.hoverData)
  }

  /*
   * parseOrder pulls the order information from the form fields. Data is not
   * validated in any way.
   */
  parseOrder (): TradeForm {
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
      qty: convertToAtoms(qtyField.value || '', market.baseUnitInfo.conventional.conversionFactor),
      rate: convertToAtoms(page.rateField.value || '', market.rateConversionFactor), // message-rate
      tifnow: page.tifNow.checked || false,
      options: {}
    }
  }

  /**
   * previewQuoteAmt shows quote amount when rate or quantity input are changed
   */
  previewQuoteAmt (show: boolean) {
    const page = this.page
    if (!this.market.base || !this.market.quote) return // Not a supported asset
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
    const quoteAsset = app().assets[order.quote]
    const quoteQty = order.qty * order.rate / OrderUtil.RateEncodingFactor
    const total = Doc.formatCoinValue(quoteQty, this.market.quoteUnitInfo)

    page.orderPreview.textContent = intl.prep(intl.ID_ORDER_PREVIEW, { total, asset: quoteAsset.symbol.toUpperCase() })
    if (this.isSell()) this.preSell()
    else this.preBuy()
  }

  /**
   * preSell populates the max order message for the largest available sell.
   */
  preSell () {
    const mkt = this.market
    const baseWallet = app().assets[mkt.base.id].wallet
    if (baseWallet.balance.available < mkt.cfg.lotsize) {
      this.setMaxOrder(null)
      return
    }
    if (mkt.maxSell) {
      this.setMaxOrder(mkt.maxSell.swap)
      return
    }
    if (mkt.maxSellRequested) return
    mkt.maxSellRequested = true
    // We only fetch pre-sell once per balance update, so don't delay.
    this.scheduleMaxEstimate('/api/maxsell', {}, 0, (res: MaxSell) => {
      mkt.maxSellRequested = false
      mkt.maxSell = res.maxSell
      mkt.sellBalance = baseWallet.balance.available
      this.setMaxOrder(res.maxSell.swap)
    })
  }

  /**
   * preBuy populates the max order message for the largest available buy.
   */
  preBuy () {
    const mkt = this.market
    const rate = this.adjustedRate()
    const quoteWallet = app().assets[mkt.quote.id].wallet
    if (!quoteWallet) return
    const aLot = mkt.cfg.lotsize * (rate / OrderUtil.RateEncodingFactor)
    if (quoteWallet.balance.available < aLot) {
      this.setMaxOrder(null)
      return
    }
    if (mkt.maxBuys[rate]) {
      this.setMaxOrder(mkt.maxBuys[rate].swap)
      return
    }
    // 0 delay for first fetch after balance update or market change, otherwise
    // meter these at 1 / sec.
    const delay = Object.keys(mkt.maxBuys).length ? 350 : 0
    this.scheduleMaxEstimate('/api/maxbuy', { rate }, delay, (res: MaxBuy) => {
      mkt.maxBuys[rate] = res.maxBuy
      mkt.buyBalance = app().assets[mkt.quote.id].wallet.balance.available
      this.setMaxOrder(res.maxBuy.swap)
    })
  }

  /**
   * scheduleMaxEstimate shows the loading icon and schedules a call to an order
   * estimate api endpoint. If another call to scheduleMaxEstimate is made before
   * this one is fired (after delay), this call will be canceled.
   */
  scheduleMaxEstimate (path: string, args: any, delay: number, success: (res: any) => void) {
    const page = this.page
    if (!this.maxLoaded) this.maxLoaded = app().loading(page.maxOrd)
    const [bid, qid] = [this.market.base.id, this.market.quote.id]
    const [bWallet, qWallet] = [app().assets[bid].wallet, app().assets[qid].wallet]
    if (!bWallet || !bWallet.running || !qWallet || !qWallet.running) return
    if (this.maxEstimateTimer) window.clearTimeout(this.maxEstimateTimer)

    Doc.show(page.maxOrd, page.maxLotBox)
    Doc.hide(page.maxAboveZero)
    page.maxFromLots.textContent = intl.prep(intl.ID_CALCULATING)
    page.maxFromLotsLbl.textContent = ''
    this.maxOrderUpdateCounter++
    const counter = this.maxOrderUpdateCounter
    this.maxEstimateTimer = window.setTimeout(async () => {
      this.maxEstimateTimer = null
      if (counter !== this.maxOrderUpdateCounter) return
      const res = await postJSON(path, {
        host: this.market.dex.host,
        base: bid,
        quote: qid,
        ...args
      })
      if (counter !== this.maxOrderUpdateCounter) return
      if (!app().checkResponse(res, true)) {
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
  setMaxOrder (maxOrder: SwapEstimate | null) {
    const page = this.page
    if (this.maxLoaded) {
      this.maxLoaded()
      this.maxLoaded = null
    }
    Doc.show(page.maxOrd, page.maxLotBox, page.maxAboveZero)
    const sell = this.isSell()

    let lots = 0
    if (maxOrder) lots = maxOrder.lots

    page.maxFromLots.textContent = lots.toString()
    // XXX add plural into format details, so we don't need this
    page.maxFromLotsLbl.textContent = lots === 1 ? 'lot' : 'lots'
    if (!maxOrder) {
      Doc.hide(page.maxAboveZero)
      return
    }
    // Could add the estimatedFees here, but that might also be
    // confusing.
    const [fromAsset, toAsset] = sell ? [this.market.base, this.market.quote] : [this.market.quote, this.market.base]
    page.maxFromAmt.textContent = Doc.formatCoinValue(maxOrder.value || 0, fromAsset.unitInfo)
    page.maxFromTicker.textContent = fromAsset.symbol.toUpperCase()
    // Could subtract the maxOrder.redemptionFees here.
    const toConversion = sell ? this.adjustedRate() / OrderUtil.RateEncodingFactor : OrderUtil.RateEncodingFactor / this.adjustedRate()
    page.maxToAmt.textContent = Doc.formatCoinValue((maxOrder.value || 0) * toConversion, toAsset.unitInfo)
    page.maxToTicker.textContent = toAsset.symbol.toUpperCase()
  }

  /*
   * validateOrder performs some basic order sanity checks, returning boolean
   * true if the order appears valid.
   */
  validateOrder (order: TradeForm) {
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
  handleBook (data: MarketOrderBook) {
    const { cfg, baseUnitInfo, quoteUnitInfo, baseCfg, quoteCfg } = this.market
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
    this.depthChart.set(this.book, cfg.lotsize, cfg.ratestep, baseUnitInfo, quoteUnitInfo)
  }

  /*
   * midGapConventional is the same as midGap, but returns the mid-gap rate as
   * the conventional ratio. This is used to convert from a conventional
   * quantity from base to quote or vice-versa.
   */
  midGapConventional () {
    const gap = this.midGap()
    if (!gap) return gap
    const { baseUnitInfo: b, quoteUnitInfo: q } = this.market
    return gap * b.conventional.conversionFactor / q.conventional.conversionFactor
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
        return (book.buys[0].msgRate + book.sells[0].msgRate) / 2 / OrderUtil.RateEncodingFactor
      }
      return book.buys[0].msgRate / OrderUtil.RateEncodingFactor
    }
    if (book.sells && book.sells.length) {
      return book.sells[0].msgRate / OrderUtil.RateEncodingFactor
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
    const xc = app().user.exchanges[market.dex.host]
    const buffer = xc.markets[market.sid].buybuffer
    const gap = this.midGapConventional()
    if (gap) {
      this.page.minMktBuy.textContent = Doc.formatCoinValue(lotSize * buffer * gap, market.baseUnitInfo)
    }
  }

  /*
   * ordersSortCompare returns sort compare function according to the active
   * sort key and direction.
   */
  ordersSortCompare () {
    switch (this.ordersSortKey) {
      case 'submitTime':
        return (a: Order, b: Order) => this.ordersSortDirection * (b.submitTime - a.submitTime)
      case 'rate':
        return (a: Order, b: Order) => this.ordersSortDirection * (a.rate - b.rate)
      case 'qty':
        return (a: Order, b: Order) => this.ordersSortDirection * (a.qty - b.qty)
      case 'type':
        return (a: Order, b: Order) => this.ordersSortDirection *
        OrderUtil.typeString(a).localeCompare(OrderUtil.typeString(b))
      case 'sell':
        return (a: Order, b: Order) => this.ordersSortDirection *
          (OrderUtil.sellString(a)).localeCompare(OrderUtil.sellString(b))
      case 'status':
        return (a: Order, b: Order) => this.ordersSortDirection *
          (OrderUtil.statusString(a)).localeCompare(OrderUtil.statusString(b))
      case 'settled':
        return (a: Order, b: Order) => this.ordersSortDirection *
          ((OrderUtil.settled(a) * 100 / a.qty) - (OrderUtil.settled(b) * 100 / b.qty))
      case 'filled':
        return (a: Order, b: Order) => this.ordersSortDirection *
          ((OrderUtil.filled(a) * 100 / a.qty) - (OrderUtil.filled(b) * 100 / b.qty))
    }
  }

  /* refreshActiveOrders refreshes the user's active order list. */
  refreshActiveOrders () {
    const page = this.page
    const metaOrders = this.metaOrders
    const market = this.market
    for (const oid in metaOrders) delete metaOrders[oid]
    const orders = app().orders(market.dex.host, marketID(market.baseCfg.symbol, market.quoteCfg.symbol))
    // Sort orders by sort key.
    const compare = this.ordersSortCompare()
    orders.sort(compare)

    Doc.empty(page.liveList)
    for (const ord of orders) {
      const row = page.liveTemplate.cloneNode(true) as HTMLElement
      metaOrders[ord.id] = {
        row: row,
        order: ord
      }
      Doc.bind(row, 'mouseenter', () => {
        this.activeMarkerRate = ord.rate
        this.setDepthMarkers()
      })
      this.updateUserOrderRow(row, ord)
      if (ord.type === OrderUtil.Limit && (ord.tif === OrderUtil.StandingTiF && ord.status < OrderUtil.StatusExecuted)) {
        const icon = Doc.tmplElement(row, 'cancelBttn')
        Doc.show(icon)
        bind(icon, 'click', e => {
          e.stopPropagation()
          this.showCancel(row, ord.id)
        })
      }

      const accelerateBttn = Doc.tmplElement(row, 'accelerateBttn')
      bind(accelerateBttn, 'click', e => {
        e.stopPropagation()
        this.showAccelerate(ord)
      })
      if (app().canAccelerateOrder(ord)) {
        Doc.show(accelerateBttn)
      }

      const side = Doc.tmplElement(row, 'side')
      side.classList.add(ord.sell ? 'sellcolor' : 'buycolor')
      const link = Doc.tmplElement(row, 'link')
      link.href = `order/${ord.id}`
      app().bindInternalNavigation(row)
      page.liveList.appendChild(row)
      app().bindTooltips(row)
    }
    this.setDepthMarkers()
  }

  /*
  * updateUserOrderRow sets the td contents of the user's order table row.
  */
  updateUserOrderRow (tr: HTMLElement, ord: Order) {
    updateDataCol(tr, 'type', OrderUtil.typeString(ord))
    updateDataCol(tr, 'side', OrderUtil.sellString(ord))
    updateDataCol(tr, 'age', Doc.timeSince(ord.submitTime))
    updateDataCol(tr, 'rate', Doc.formatCoinValue(ord.rate / this.market.rateConversionFactor))
    updateDataCol(tr, 'qty', Doc.formatCoinValue(ord.qty, this.market.baseUnitInfo))
    updateDataCol(tr, 'filled', `${(OrderUtil.filled(ord) / ord.qty * 100).toFixed(1)}%`)
    updateDataCol(tr, 'settled', `${(OrderUtil.settled(ord) / ord.qty * 100).toFixed(1)}%`)
    updateDataCol(tr, 'status', OrderUtil.statusString(ord))
  }

  /* setMarkers sets the depth chart markers for booked orders. */
  setDepthMarkers () {
    const markers: Record<string, DepthMarker[]> = {
      buys: [],
      sells: []
    }
    const rateFactor = this.market.rateConversionFactor
    for (const mo of Object.values(this.metaOrders)) {
      const ord = mo.order
      if (ord.rate && ord.status === OrderUtil.StatusBooked) {
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

    const { baseCfg: b, quoteCfg: q } = this.market
    const baseSymb = b.symbol.toUpperCase()
    const quoteSymb = q.symbol.toUpperCase()
    // more than 6 numbers it gets too big for the title.
    document.title = `${Doc.formatCoinValue(midGapValue)} | ${baseSymb}${quoteSymb} | ${this.ogTitle}`
  }

  /* handleBookRoute is the handler for the 'book' notification, which is sent
   * in response to a new market subscription. The data received will contain
   * the entire order book.
   */
  handleBookRoute (note: BookUpdate) {
    app().log('book', 'handleBookRoute:', note)
    const mktBook = note.payload
    const market = this.market
    const page = this.page
    const host = market.dex.host
    const [b, q] = [market.baseCfg, market.quoteCfg]
    if (mktBook.base !== b.id || mktBook.quote !== q.id) return
    this.refreshActiveOrders()
    this.handleBook(mktBook)
    this.updateTitle()
    Doc.hide(page.marketLoader)
    this.marketList.select(host, b.id, q.id)

    State.store(lastMarketKey, {
      host: note.host,
      base: mktBook.base,
      quote: mktBook.quote
    })

    page.lotSize.textContent = Doc.formatCoinValue(market.cfg.lotsize, market.baseUnitInfo)
    page.rateStep.textContent = Doc.formatCoinValue(market.cfg.ratestep / market.rateConversionFactor)
    this.baseUnits.forEach(el => {
      Doc.empty(el)
      el.appendChild(Doc.symbolize(b.symbol))
    })
    this.quoteUnits.forEach(el => {
      Doc.empty(el)
      el.appendChild(Doc.symbolize(q.symbol))
    })
    this.balanceWgt.setWallets(host, b.id, q.id)
    this.setMarketBuyOrderEstimate()
    this.refreshActiveOrders()
    if (this.loaded) {
      this.loaded()
      this.loaded = null
      Doc.animate(250, progress => {
        this.main.style.opacity = String(progress)
      })
    }
  }

  /* handleBookOrderRoute is the handler for 'book_order' notifications. */
  handleBookOrderRoute (data: BookUpdate) {
    app().log('book', 'handleBookOrderRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload as MiniOrder
    if (order.rate > 0) this.book.add(order)
    this.addTableOrder(order)
    this.updateTitle()
    this.depthChart.draw()
  }

  /* handleUnbookOrderRoute is the handler for 'unbook_order' notifications. */
  handleUnbookOrderRoute (data: BookUpdate) {
    app().log('book', 'handleUnbookOrderRoute:', data)
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
  handleUpdateRemainingRoute (data: BookUpdate) {
    app().log('book', 'handleUpdateRemainingRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const update = data.payload
    this.book.updateRemaining(update.token, update.qty, update.qtyAtomic)
    this.updateTableOrder(update)
    this.depthChart.draw()
  }

  /* handleEpochOrderRoute is the handler for 'epoch_order' notifications. */
  handleEpochOrderRoute (data: BookUpdate) {
    app().log('book', 'handleEpochOrderRoute:', data)
    if (data.host !== this.market.dex.host || data.marketID !== this.market.sid) return
    const order = data.payload
    if (order.msgRate > 0) this.book.add(order) // No cancels or market orders
    if (order.qtyAtomic > 0) this.addTableOrder(order) // No cancel orders
    this.depthChart.draw()
  }

  /* handleCandlesRoute is the handler for 'candles' notifications. */
  handleCandlesRoute (data: BookUpdate) {
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
    this.candleChart.setCandles(data.payload, this.market.cfg, this.market.baseUnitInfo, this.market.quoteUnitInfo)
  }

  /* handleCandleUpdateRoute is the handler for 'candle_update' notifications. */
  handleCandleUpdateRoute (data: BookUpdate) {
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
  async showForm (form: HTMLElement) {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.unlockWalletForm, page.verifyForm, page.newWalletForm,
      page.cancelForm, page.vDetailPane, page.accelerateForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  /* showOpen shows the form to unlock a wallet. */
  async showOpen (asset: SupportedAsset, f: () => void) {
    const page = this.page
    this.openAsset = asset
    this.openFunc = f
    this.unlockForm.refresh(app().assets[asset.id])
    this.showForm(page.unlockWalletForm)
    page.uwAppPass.focus()
  }

  /* showVerify shows the form to accept the currently parsed order information
   * and confirm submission of the order to the dex.
   */
  showVerify () {
    this.preorderCache = {}
    const page = this.page
    const order = this.currentOrder = this.parseOrder()
    const isSell = order.sell
    const baseAsset = app().assets[order.base]
    const quoteAsset = app().assets[order.quote]
    const toAsset = isSell ? quoteAsset : baseAsset
    const fromAsset = isSell ? baseAsset : quoteAsset

    // Set the to and from icons in the fee details pane.
    for (const icon of Doc.applySelector(page.vDetailPane, '[data-icon]')) {
      switch (icon.dataset.icon) {
        case 'from':
          icon.src = Doc.logoPath(fromAsset.symbol)
          break
        case 'to':
          icon.src = Doc.logoPath(toAsset.symbol)
      }
    }

    Doc.hide(page.vUnlockPreorder, page.vPreorderErr)
    Doc.show(page.vPreorder)

    page.vBuySell.textContent = isSell ? 'Selling' : 'Buying'
    const buySellStr = isSell ? intl.prep(intl.ID_SELL) : intl.prep(intl.ID_BUY)
    page.vSideSubmit.textContent = buySellStr
    page.vOrderHost.textContent = order.host
    if (order.isLimit) {
      Doc.show(page.verifyLimit)
      Doc.hide(page.verifyMarket)
      const orderDesc = `Limit ${buySellStr} Order`
      page.vOrderType.textContent = order.tifnow ? orderDesc + ' (immediate)' : orderDesc
      page.vRate.textContent = Doc.formatCoinValue(order.rate / this.market.rateConversionFactor)
      page.vQty.textContent = Doc.formatCoinValue(order.qty, baseAsset.unitInfo)
      const total = order.rate / OrderUtil.RateEncodingFactor * order.qty
      page.vTotal.textContent = Doc.formatCoinValue(total, quoteAsset.unitInfo)
      // Format total fiat value.
      this.showFiatValue(quoteAsset.id, total, page.vFiatTotal)
    } else {
      Doc.hide(page.verifyLimit)
      Doc.show(page.verifyMarket)
      page.vOrderType.textContent = `Market ${buySellStr} Order`
      const ui = order.sell ? this.market.baseUnitInfo : this.market.quoteUnitInfo
      page.vmFromTotal.textContent = Doc.formatCoinValue(order.qty, ui)
      page.vmFromAsset.textContent = fromAsset.symbol.toUpperCase()
      // Format fromAsset fiat value.
      this.showFiatValue(fromAsset.id, order.qty, page.vmFromTotalFiat)
      const gap = this.midGap()
      if (gap) {
        Doc.show(page.vMarketEstimate)
        const received = order.sell ? order.qty * gap : order.qty / gap
        page.vmToTotal.textContent = Doc.formatCoinValue(received, toAsset.unitInfo)
        page.vmToAsset.textContent = toAsset.symbol.toUpperCase()
        // Format recieved value to fiat equivalent.
        this.showFiatValue(toAsset.id, received, page.vmTotalFiat)
      } else {
        Doc.hide(page.vMarketEstimate)
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
    this.showVerifyForm()
    page.vPass.focus()

    if (baseAsset.wallet.open && quoteAsset.wallet.open) this.preOrder(order)
    else {
      Doc.hide(page.vPreorder)
      if (State.passwordIsCached()) this.unlockWalletsForEstimates('')
      else Doc.show(page.vUnlockPreorder)
    }
  }

  // showFiatValue displays the fiat equivalent for an order quantity.
  showFiatValue (assetID: number, qty: number, display: PageElement) {
    if (display) {
      const rate = app().fiatRatesMap[assetID]
      display.textContent = Doc.formatFiatConversion(qty, rate, app().unitInfo(assetID))
      if (rate) Doc.show(display.parentElement as Element)
      else Doc.hide(display.parentElement as Element)
    }
  }

  /* showVerifyForm displays form to verify an order */
  async showVerifyForm () {
    const page = this.page
    Doc.hide(page.vErr)
    this.showForm(page.verifyForm)
  }

  /*
   * submitEstimateUnlock reads the current vUnlockPass and unlocks any locked
   * wallets.
   */
  async submitEstimateUnlock () {
    const pw = this.page.vUnlockPass.value || ''
    return await this.unlockWalletsForEstimates(pw)
  }

  /*
   * unlockWalletsForEstimates unlocks any locked wallets with the provided
   * password.
   */
  async unlockWalletsForEstimates (pw: string) {
    const page = this.page
    const loaded = app().loading(page.verifyForm)
    const err = await this.attemptWalletUnlock(pw)
    loaded()
    if (err) return this.setPreorderErr(err)
    Doc.show(page.vPreorder)
    Doc.hide(page.vUnlockPreorder)
    this.preOrder(this.parseOrder())
  }

  /*
   * attemptWalletUnlock unlocks both the base and quote wallets for the current
   * market, if locked.
   */
  async attemptWalletUnlock (pw: string) {
    const { base, quote } = this.market
    const assetIDs = []
    if (!base.wallet.open) assetIDs.push(base.id)
    if (!quote.wallet.open) assetIDs.push(quote.id)
    const req = {
      pass: pw,
      assetID: -1
    }
    for (const assetID of assetIDs) {
      req.assetID = assetID
      const res = await postJSON('/api/openwallet', req)
      if (!app().checkResponse(res, true)) {
        return res.msg
      }
    }
  }

  /* fetchPreorder fetches the pre-order estimates and options. */
  async fetchPreorder (order: TradeForm) {
    const page = this.page
    const cacheKey = JSON.stringify(order.options)
    const cached = this.preorderCache[cacheKey]
    if (cached) return cached

    Doc.hide(page.vPreorderErr)
    const loaded = app().loading(page.verifyForm)
    const res = await postJSON('/api/preorder', wireOrder(order))
    loaded()
    if (!app().checkResponse(res, true)) return { err: res.msg }
    this.preorderCache[cacheKey] = res.estimate
    return res.estimate
  }

  /*
   * setPreorderErr sets and displays the pre-order error message and hides the
   * pre-order details box.
   */
  setPreorderErr (msg: string) {
    const page = this.page
    Doc.hide(page.vPreorder)
    Doc.show(page.vPreorderErr)
    page.vPreorderErrTip.dataset.tooltip = msg
  }

  /* preOrder loads the options and fetches pre-order estimates */
  async preOrder (order: TradeForm) {
    // if (!this.validateOrder(order)) return
    const page = this.page

    // Add swap options.
    const refreshPreorder = async () => {
      const res: APIResponse = await this.fetchPreorder(order)
      if (res.err) return this.setPreorderErr(res.err)
      const est = (res as any) as OrderEstimate
      Doc.hide(page.vPreorderErr)
      Doc.show(page.vPreorder)
      const { swap, redeem } = est
      Doc.empty(page.vOrderOpts)
      swap.options = swap.options || []
      redeem.options = redeem.options || []
      this.setFeeEstimates(swap, redeem, order)

      const changed = async () => {
        await refreshPreorder()
        Doc.animate(400, progress => {
          page.vFeeSummary.style.backgroundColor = `rgba(128, 128, 128, ${0.5 - 0.5 * progress})`
        })
      }
      const addOption = (opt: OrderOption, isSwap: boolean) => page.vOrderOpts.appendChild(OrderUtil.optionElement(opt, order, changed, isSwap))
      for (const opt of swap.options || []) addOption(opt, true)
      for (const opt of redeem.options || []) addOption(opt, false)
      app().bindTooltips(page.vOrderOpts)
    }

    refreshPreorder()
  }

  /* setFeeEstimates sets all of the pre-order estimate fields */
  setFeeEstimates (swap: PreSwap, redeem: PreRedeem, order: TradeForm) {
    const { page, market } = this
    const { baseUnitInfo, quoteUnitInfo, rateConversionFactor } = market
    const swapped = swap.estimate.value || 0
    const fmtPct = percentFormatter.format

    let [toUI, fromUI] = [baseUnitInfo, quoteUnitInfo]
    if (this.currentOrder.sell) {
      [fromUI, toUI] = [toUI, fromUI]
    }

    // Set swap fee estimates in the details pane.
    const bestSwapPct = swap.estimate.realisticBestCase / swapped * 100
    page.vSwapFeesLowPct.textContent = `${fmtPct(bestSwapPct)}%`
    page.vSwapFeesLow.textContent = Doc.formatCoinValue(swap.estimate.realisticBestCase, fromUI)
    const worstSwapPct = swap.estimate.realisticWorstCase / swapped * 100
    page.vSwapFeesHighPct.textContent = `${fmtPct(worstSwapPct)}%`
    page.vSwapFeesHigh.textContent = Doc.formatCoinValue(swap.estimate.realisticWorstCase, fromUI)
    page.vSwapFeesMaxPct.textContent = `${fmtPct(swap.estimate.maxFees / swapped * 100)}%`
    page.vSwapFeesMax.textContent = Doc.formatCoinValue(swap.estimate.maxFees, fromUI)

    // Set redemption fee estimates in the details pane.
    const midGap = this.midGap()
    const estRate = midGap || order.rate / rateConversionFactor
    const received = order.sell ? swapped * estRate : swapped / estRate
    const bestRedeemPct = redeem.estimate.realisticBestCase / received * 100
    page.vRedeemFeesLowPct.textContent = `${fmtPct(bestRedeemPct)}%`
    page.vRedeemFeesLow.textContent = Doc.formatCoinValue(redeem.estimate.realisticBestCase, toUI)
    const worstRedeemPct = redeem.estimate.realisticWorstCase / received * 100
    page.vRedeemFeesHighPct.textContent = `${fmtPct(worstRedeemPct)}%`
    page.vRedeemFeesHigh.textContent = Doc.formatCoinValue(redeem.estimate.realisticWorstCase, toUI)

    // Set the summary percent, which is a simple addition of swap and redeem
    // loss percents.
    page.vFeeSummaryLow.textContent = fmtPct(bestSwapPct + bestRedeemPct)
    page.vFeeSummaryHigh.textContent = fmtPct(worstSwapPct + worstRedeemPct)
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
    // Toggle the loader and submit button.
    const loaded = app().loading(page.cancelSubmit)
    const res = await postJSON('/api/cancel', req)
    loaded()
    // Display error on confirmation modal.
    if (!app().checkResponse(res, true)) {
      page.cancelErr.textContent = res.msg
      Doc.show(page.cancelErr)
      return
    }
    // Hide confirmation modal only on success.
    Doc.hide(cancelData.bttn, page.forms)
    order.cancelling = true
  }

  /* showCancel shows a form to confirm submission of a cancel order. */
  showCancel (row: HTMLElement, orderID: string) {
    const order = this.metaOrders[orderID].order
    const page = this.page
    const remaining = order.qty - order.filled
    const asset = OrderUtil.isMarketBuy(order) ? this.market.quote : this.market.base
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining, asset.unitInfo)
    page.cancelUnit.textContent = asset.symbol.toUpperCase()
    Doc.hide(page.cancelErr)
    this.showForm(page.cancelForm)
    page.cancelPass.focus()
    this.cancelData = {
      bttn: Doc.tmplElement(row, 'cancelBttn'),
      order: order
    }
  }

  /* showAccelerate shows the accelerate order form. */
  showAccelerate (order: Order) {
    const loaded = app().loading(this.main)
    this.accelerateOrderForm.refresh(order)
    loaded()
    this.showForm(this.page.accelerateForm)
  }

  /* showCreate shows the new wallet creation form. */
  showCreate (asset: SupportedAsset) {
    const page = this.page
    this.currentCreate = asset
    this.newWalletForm.setAsset(asset.id)
    this.showForm(page.newWalletForm)
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
    const baseWallet = app().walletMap[market.base.id]
    const quoteWallet = app().walletMap[market.quote.id]
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
   * handlePriceUpdate is the handler for the 'spots' notification.
   */
  handlePriceUpdate (note: SpotPriceNote) {
    const xcSection = this.marketList.xcSection(note.host)
    if (!xcSection) return
    for (const spot of Object.values(note.spots)) {
      const marketRow = xcSection.marketRow(spot.baseID, spot.quoteID)
      if (marketRow) marketRow.setSpot(spot)
    }
  }

  /*
   * handleFeePayment is the handler for the 'feepayment' notification type.
   * This is used to update the registration status of the current exchange.
   */
  handleFeePayment (note: FeePaymentNote) {
    const dexAddr = note.dex
    if (dexAddr !== this.market.dex.host) return
    // update local dex
    this.market.dex = app().exchanges[dexAddr]
    this.setRegistrationStatusVisibility()
  }

  /*
   * handleOrderNote is the handler for the 'order'-type notification, which are
   * used to update a user's order's status.
   */
  handleOrderNote (note: OrderNote) {
    const order = note.order
    const metaOrder = this.metaOrders[order.id]
    // If metaOrder doesn't exist for the given order it means it was
    // created via dexcctl and the GUI isn't aware of it.
    // Call refreshActiveOrders to grab the order.
    if (!metaOrder) return this.refreshActiveOrders()
    const oldStatus = metaOrder.status
    metaOrder.order = order
    const cancelBttn = Doc.tmplElement(metaOrder.row, 'cancelBttn')
    if (note.topic === 'MissedCancel') Doc.show(cancelBttn)
    if (order.filled === order.qty) Doc.hide(cancelBttn)
    const accelerateBttn = Doc.tmplElement(metaOrder.row, 'accelerateBttn')
    if (app().canAccelerateOrder(order)) Doc.show(accelerateBttn)
    else Doc.hide(accelerateBttn)
    this.updateUserOrderRow(metaOrder.row, order)
    // Only reset markers if there is a change, since the chart is redrawn.
    if ((oldStatus === OrderUtil.StatusEpoch && order.status === OrderUtil.StatusBooked) ||
      (oldStatus === OrderUtil.StatusBooked && order.status > OrderUtil.StatusBooked)) this.setDepthMarkers()
  }

  /*
   * handleEpochNote handles notifications signalling the start of a new epoch.
   */
  handleEpochNote (note: EpochNote) {
    app().log('book', 'handleEpochNote:', note)
    if (note.host !== this.market.dex.host || note.marketID !== this.market.sid) return
    if (this.book) {
      this.book.setEpoch(note.epoch)
      this.depthChart.draw()
    }

    this.clearOrderTableEpochs()
    for (const metaOrder of Object.values(this.metaOrders)) {
      const order = metaOrder.order
      const alreadyMatched = note.epoch > order.epoch
      const statusTD = Doc.tmplElement(metaOrder.row, 'status')
      switch (true) {
        case order.type === OrderUtil.Limit && order.status === OrderUtil.StatusEpoch && alreadyMatched:
          statusTD.textContent = order.tif === OrderUtil.ImmediateTiF ? intl.prep(intl.ID_EXECUTED) : intl.prep(intl.ID_BOOKED)
          order.status = order.tif === OrderUtil.ImmediateTiF ? OrderUtil.StatusExecuted : OrderUtil.StatusBooked
          break
        case order.type === OrderUtil.Market && order.status === OrderUtil.StatusEpoch:
          // Technically don't know if this should be 'executed' or 'settling'.
          statusTD.textContent = intl.prep(intl.ID_EXECUTED)
          order.status = OrderUtil.StatusExecuted
          break
      }
    }
  }

  setBalanceVisibility () {
    if (this.market.dex.connectionStatus === ConnectionStatus.Connected) {
      Doc.show(this.page.balanceTable)
    } else {
      Doc.hide(this.page.balanceTable)
    }
  }

  /* handleBalanceNote handles notifications updating a wallet's balance. */
  handleBalanceNote (note: BalanceNote) {
    this.setBalanceVisibility()
    // if connection to dex server fails, it is not possible to retrieve
    // markets.
    if (this.market.dex.connectionStatus !== ConnectionStatus.Connected) return
    // If there's a balance update, refresh the max order section.
    const mkt = this.market
    const avail = note.balance.available
    switch (note.assetID) {
      case mkt.baseCfg.id:
        // If we're not showing the max order panel yet, don't do anything.
        if (!mkt.maxSell) break
        if (typeof mkt.sellBalance === 'number' && mkt.sellBalance !== avail) mkt.maxSell = null
        if (this.isSell()) this.preSell()
        break
      case mkt.quoteCfg.id:
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
    const order = this.currentOrder
    const pw = page.vPass.value
    page.vPass.value = ''
    const req = {
      order: wireOrder(order),
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
    if (!app().checkResponse(res, true)) {
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
    const user = await app().fetchUser()
    if (!user) return
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
    const lots = parseInt(page.lotField.value || '0')
    if (lots <= 0) {
      page.lotField.value = '0'
      page.qtyField.value = ''
      this.previewQuoteAmt(false)
      return
    }
    const lotSize = this.market.cfg.lotsize
    page.lotField.value = String(lots)
    // Conversion factor must be a multiple of 10.
    page.qtyField.value = String(lots * lotSize / this.market.baseUnitInfo.conventional.conversionFactor)
    this.previewQuoteAmt(true)
  }

  /*
   * quantityChanged is attached to the keyup and change events of the quantity
   * input.
   */
  quantityChanged (finalize: boolean) {
    const page = this.page
    const order = this.parseOrder()
    if (order.qty < 0) {
      page.lotField.value = '0'
      page.qtyField.value = ''
      this.previewQuoteAmt(false)
      return
    }
    const lotSize = this.market.cfg.lotsize
    const lots = Math.floor(order.qty / lotSize)
    const adjusted = lots * lotSize
    page.lotField.value = String(lots)
    if (!order.isLimit && !order.sell) return
    // Conversion factor must be a multiple of 10.
    if (finalize) page.qtyField.value = String(adjusted / this.market.baseUnitInfo.conventional.conversionFactor)
    this.previewQuoteAmt(true)
  }

  /*
   * marketBuyChanged is attached to the keyup and change events of the quantity
   * input for the market-buy form.
   */
  marketBuyChanged () {
    const page = this.page
    const qty = convertToAtoms(page.mktBuyField.value || '', this.market.quoteUnitInfo.conventional.conversionFactor)
    const gap = this.midGap()
    if (!gap || !qty) {
      page.mktBuyLots.textContent = '0'
      page.mktBuyScore.textContent = '0'
      return
    }
    const lotSize = this.market.cfg.lotsize
    const received = qty / gap
    page.mktBuyLots.textContent = (received / lotSize).toFixed(1)
    page.mktBuyScore.textContent = Doc.formatCoinValue(received, this.market.baseUnitInfo)
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
      this.page.rateField.value = '0'
      return
    }
    const order = this.parseOrder()
    const r = adjusted / this.market.rateConversionFactor
    this.page.rateField.value = String(r)
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
  adjustedRate (): number {
    const v = this.page.rateField.value
    if (!v) return NaN
    const rate = convertToAtoms(v, this.market.rateConversionFactor)
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
  binOrdersByRateAndEpoch (orders: MiniOrder[]) {
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
  loadTableSide (sell: boolean) {
    const bookSide = sell ? this.book.sells : this.book.buys
    const tbody = sell ? this.page.sellRows : this.page.buyRows
    Doc.empty(tbody)
    if (!bookSide || !bookSide.length) return
    const orderBins = this.binOrdersByRateAndEpoch(bookSide)
    orderBins.forEach(bin => { tbody.appendChild(this.orderTableRow(bin)) })
  }

  /* addTableOrder adds a single order to the appropriate table. */
  addTableOrder (order: MiniOrder) {
    const tbody = order.sell ? this.page.sellRows : this.page.buyRows
    let row = tbody.firstChild as OrderRow
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
    if (row && row.manager.getRate() === 0) row = row.nextSibling as OrderRow
    while (row) {
      if (row.manager.compare(order) === 0) {
        row.manager.insertOrder(order)
        return
      } else if (row.manager.compare(order) > 0) {
        const tr = this.orderTableRow([order])
        tbody.insertBefore(tr, row)
        return
      }
      row = row.nextSibling as OrderRow
    }
    const tr = this.orderTableRow([order])
    tbody.appendChild(tr)
  }

  /* removeTableOrder removes a single order from its table. */
  removeTableOrder (order: MiniOrder) {
    const token = order.token
    for (const tbody of [this.page.sellRows, this.page.buyRows]) {
      for (const tr of (Array.from(tbody.children) as OrderRow[])) {
        if (tr.manager.removeOrder(token)) {
          return
        }
      }
    }
  }

  /* updateTableOrder looks for the order in the table and updates the qty */
  updateTableOrder (u: RemainderUpdate) {
    for (const tbody of [this.page.sellRows, this.page.buyRows]) {
      for (const tr of (Array.from(tbody.children) as OrderRow[])) {
        if (tr.manager.updateOrderQty(u)) {
          return
        }
      }
    }
  }

  /*
   * clearOrderTableEpochs removes immediate-tif orders whose epoch has expired.
   */
  clearOrderTableEpochs () {
    this.clearOrderTableEpochSide(this.page.sellRows)
    this.clearOrderTableEpochSide(this.page.buyRows)
  }

  /*
   * clearOrderTableEpochs removes immediate-tif orders whose epoch has expired
   * for a single side.
   */
  clearOrderTableEpochSide (tbody: HTMLElement) {
    for (const tr of (Array.from(tbody.children)) as OrderRow[]) {
      tr.manager.removeEpochOrders()
    }
  }

  /*
   * orderTableRow creates a new <tr> element to insert into an order table.
     Takes a bin of orders with the same rate, and displays the total quantity.
   */
  orderTableRow (orderBin: MiniOrder[]): OrderRow {
    const tr = this.page.rowTemplate.cloneNode(true) as OrderRow
    const { baseUnitInfo, rateConversionFactor } = this.market
    const manager = new OrderTableRowManager(tr, orderBin, baseUnitInfo, rateConversionFactor)
    tr.manager = manager
    bind(tr, 'click', () => {
      this.reportDepthClick(tr.manager.getRate() / rateConversionFactor)
    })
    if (tr.manager.getRate() !== 0) {
      Doc.bind(tr, 'mouseenter', () => {
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
  async handleConnNote (note: ConnEventNote) {
    this.marketList.setConnectionStatus(note)
    if (note.connectionStatus === ConnectionStatus.Connected) {
      // Having been disconnected from a DEX server, anything may have changed,
      // or this may be the first opportunity to get the server's config, so
      // fetch it all before reloading the markets page.
      await app().fetchUser()
      app().loadPage('markets')
    }
  }

  /*
   * filterMarkets sets the display of markets in the markets list based on the
   * value of the search input.
   */
  filterMarkets () {
    const filterTxt = this.page.marketSearch.value
    const filter = filterTxt ? (mkt: MarketRow) => mkt.name.includes(filterTxt) : () => true
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
  candleDurationSelected (dur: string) {
    this.candleDur = dur
    this.loadCandles()
  }

  /*
   * loadCandles loads the candles for the current candleDur. If a cache is already
   * active, the cache will be used without a loadcandles request.
   */
  loadCandles () {
    for (const bttn of Doc.kids(this.page.durBttnBox)) {
      if (bttn.textContent === this.candleDur) bttn.classList.add('selected')
      else bttn.classList.remove('selected')
    }
    const { candleCaches, cfg, baseUnitInfo, quoteUnitInfo } = this.market
    const cache = candleCaches[this.candleDur]
    if (cache) {
      this.depthChart.hide()
      this.candleChart.show()
      this.candleChart.setCandles(cache, cfg, baseUnitInfo, quoteUnitInfo)
      return
    }
    this.requestCandles()
  }

  /* requestCandles sends the loadcandles request. */
  requestCandles () {
    this.candlesLoading = {
      loaded: () => { Doc.hide(this.page.marketLoader) },
      timer: window.setTimeout(() => {
        if (this.candlesLoading) {
          this.candlesLoading = null
          Doc.hide(this.page.marketLoader)
          console.error('candles not received')
        }
      }, 10000)
    }
    const { dex, baseCfg, quoteCfg } = this.market
    ws.request('loadcandles', { host: dex.host, base: baseCfg.id, quote: quoteCfg.id, dur: this.candleDur })
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
  xcSections: ExchangeSection[]
  selected: MarketRow

  constructor (div: HTMLElement) {
    const xcTmpl = Doc.tmplElement(div, 'xc')
    Doc.cleanTemplates(xcTmpl)
    this.xcSections = []
    for (const dex of Object.values(app().user.exchanges)) {
      this.xcSections.push(new ExchangeSection(xcTmpl, dex))
    }
    // Initial sort is alphabetical.
    for (const xc of this.sortedSections()) {
      div.appendChild(xc.node)
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
  xcSection (host: string) {
    for (const xc of this.xcSections) {
      if (xc.host === host) return xc
    }
    return null
  }

  /* exists will be true if the specified market exists. */
  exists (host: string, baseID: number, quoteID: number) {
    const xc = this.xcSection(host)
    // If connecting to an offline server the client is not able to get xc.marketRows.
    // Therefore we must check for it.
    if (!xc || !xc.marketRows) return false
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
  select (host: string, baseID: number, quoteID: number) {
    if (this.selected) this.selected.node.classList.remove('selected')
    const xcSection = this.xcSection(host)
    if (!xcSection) return console.error(`select: no exchange section for ${host}`)
    const marketRow = xcSection.marketRow(baseID, quoteID)
    if (!marketRow) return console.error(`select: no market row for ${host}, ${baseID}-${quoteID}`)
    this.selected = marketRow
    this.selected.node.classList.add('selected')
  }

  /* setConnectionStatus sets the visibility of the disconnected icon based
   * on the core.ConnEventNote.
   */
  setConnectionStatus (note: ConnEventNote) {
    const xcSection = this.xcSection(note.host)
    if (!xcSection) return console.error(`setConnectionStatus: no exchange section for ${note.host}`)
    xcSection.setConnected(note.connectionStatus === ConnectionStatus.Connected)
  }

  /*
   * setFilter sets the visibility of market rows based on the provided filter.
   */
  setFilter (filter: (mkt: MarketRow) => boolean) {
    for (const xc of this.xcSections) {
      xc.setFilter(filter)
    }
  }
}

/*
 * ExchangeSection is a top level section of the MarketList.
 */
class ExchangeSection {
  marketRows: MarketRow[]
  host: string
  dex: Exchange
  node: HTMLElement
  disconnectedIco: PageElement

  constructor (template: HTMLElement, dex: Exchange) {
    this.dex = dex
    this.host = dex.host
    this.node = template.cloneNode(true) as HTMLElement
    const tmpl = Doc.parseTemplate(this.node)
    tmpl.header.textContent = dex.host

    this.disconnectedIco = tmpl.disconnected
    if (dex.connectionStatus === ConnectionStatus.Connected) Doc.hide(tmpl.disconnected)

    tmpl.mkts.removeChild(tmpl.mktrow)
    // If disconnected is not possible to get the markets from the server.
    if (!dex.markets) return

    this.marketRows = Object.values(dex.markets).map(mkt => {
      const bui = app().unitInfo(mkt.baseid, dex)
      const qui = app().unitInfo(mkt.quoteid, dex)
      const rateConversionFactor = OrderUtil.RateEncodingFactor / bui.conventional.conversionFactor * qui.conventional.conversionFactor
      return new MarketRow(tmpl.mktrow, mkt, rateConversionFactor)
    })

    // for (const mkt of Object.values(dex.markets)) {
    //   this.marketRows.push(new MarketRow(tmpl.mktrow, mkt))
    // }
    for (const market of this.sortedMarkets()) {
      tmpl.mkts.appendChild(market.node)
    }
  }

  /*
   * sortedMarkets is the list of MarketRow sorted alphabetically by the base
   * symbol first, quote symbol second.
   */
  sortedMarkets () {
    if (!this.marketRows) return []
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
  marketRow (baseID: number, quoteID: number) {
    for (const mkt of this.marketRows) {
      if (mkt.baseID === baseID && mkt.quoteID === quoteID) return mkt
    }
  }

  /* setConnected sets the visiblity of the disconnected icon. */
  setConnected (isConnected: boolean) {
    if (isConnected) Doc.hide(this.disconnectedIco)
    else Doc.show(this.disconnectedIco)
  }

  /*
   * setFilter sets the visibility of market rows based on the provided filter.
   */
  setFilter (filter: (mkt: MarketRow) => boolean) {
    for (const mkt of this.marketRows) {
      if (filter(mkt)) Doc.show(mkt.node)
      else Doc.hide(mkt.node)
    }
  }
}

/*
 * MarketRow represents one row in the MarketList. A MarketRow is a subsection
 * of the ExchangeSection.
 */
class MarketRow {
  node: HTMLElement
  mkt: Market
  name: string
  baseID: number
  quoteID: number
  lotSize: number
  rateStep: number
  tmpl: Record<string, PageElement>
  rateConversionFactor: number

  constructor (template: HTMLElement, mkt: Market, rateConversionFactor: number) {
    this.mkt = mkt
    this.name = mkt.name
    this.baseID = mkt.baseid
    this.quoteID = mkt.quoteid
    this.lotSize = mkt.lotsize
    this.rateStep = mkt.ratestep
    this.rateConversionFactor = rateConversionFactor
    this.node = template.cloneNode(true) as HTMLElement
    const tmpl = this.tmpl = Doc.parseTemplate(this.node)
    tmpl.baseIcon.src = Doc.logoPath(mkt.basesymbol)
    tmpl.quoteIcon.src = Doc.logoPath(mkt.quotesymbol)
    tmpl.baseSymbol.appendChild(Doc.symbolize(mkt.basesymbol))
    tmpl.quoteSymbol.appendChild(Doc.symbolize(mkt.quotesymbol))
    this.setSpot(mkt.spot)
  }

  setSpot (spot: Spot) {
    if (!spot) return
    const { tmpl, mkt } = this

    Doc.show(tmpl.pctChange)
    const pct = spot.change24 * 100
    const num = percentFormatter.format(pct)
    const sign = pct > 0 ? '+' : ''
    tmpl.pctChange.textContent = `${sign}${num}%`
    tmpl.pctChange.classList.remove('upgreen', 'downred', 'grey')
    tmpl.pctChange.classList.add(pct === 0 ? 'grey' : pct > 0 ? 'upgreen' : 'downred')
    const baseAsset = app().assets[mkt.baseid]
    if (baseAsset) {
      Doc.show(tmpl.bottomRow)
      tmpl.assetName.textContent = baseAsset.name
      tmpl.price.textContent = Doc.formatCoinValue(spot.rate / this.rateConversionFactor)
    }
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
  base: BalanceWidgetElement
  quote: BalanceWidgetElement
  parentRow: PageElement
  dex: Exchange

  constructor (table: HTMLElement) {
    const els = Doc.idDescendants(table)
    this.base = {
      id: 0,
      parentID: parentIDNone,
      cfg: null,
      logo: els.baseImg,
      symbol: els.balBaseSymbol,
      avail: els.baseAvail,
      newWalletRow: els.baseNewWalletRow,
      newWalletBttn: els.baseNewButton,
      walletPendingRow: els.baseWalletPendingRow,
      locked: els.baseLocked,
      immature: els.baseImmature,
      parentBal: els.baseParent,
      parentIcon: els.baseParentLogo,
      unsupported: els.baseUnsupported,
      expired: els.baseExpired,
      connect: els.baseConnect,
      spinner: els.baseSpinner,
      iconBox: els.baseWalletState,
      stateIcons: new WalletIcons(els.baseWalletState)
    }
    this.quote = {
      id: 0,
      parentID: parentIDNone,
      cfg: null,
      logo: els.quoteImg,
      symbol: els.balQuoteSymbol,
      avail: els.quoteAvail,
      newWalletRow: els.quoteNewWalletRow,
      newWalletBttn: els.quoteNewButton,
      walletPendingRow: els.quoteWalletPendingRow,
      locked: els.quoteLocked,
      immature: els.quoteImmature,
      parentBal: els.quoteParent,
      parentIcon: els.quoteParentLogo,
      unsupported: els.quoteUnsupported,
      expired: els.quoteExpired,
      connect: els.quoteConnect,
      spinner: els.quoteSpinner,
      iconBox: els.quoteWalletState,
      stateIcons: new WalletIcons(els.quoteWalletState)
    }
    this.parentRow = els.parentBalance

    app().registerNoteFeeder({
      balance: (note: BalanceNote) => { this.updateAsset(note.assetID) },
      walletstate: (note: WalletStateNote) => { this.updateAsset(note.wallet.assetID) },
      createwallet: (note: WalletCreationNote) => { this.updateAsset(note.assetID) }
    })
  }

  /*
   * setWallet sets the balance widget to display data for specified market.
   */
  setWallets (host: string, baseID: number, quoteID: number) {
    const parentID = (assetID: number) => {
      const asset = app().assets[assetID]
      if (asset?.token) return asset.token.parentID
      return parentIDNone
    }
    this.dex = app().user.exchanges[host]
    this.base.id = baseID
    this.base.parentID = parentID(baseID)
    this.base.cfg = this.dex.assets[baseID]
    this.quote.id = quoteID
    this.quote.parentID = parentID(quoteID)
    this.quote.cfg = this.dex.assets[quoteID]
    Doc.hide(this.parentRow)
    this.updateWallet(this.base)
    this.updateWallet(this.quote)
  }

  /*
   * updateWallet updates the displayed wallet information based on the
   * core.Wallet state.
   */
  updateWallet (side: BalanceWidgetElement) {
    if (!side.cfg) return // no wallet set yet
    const asset = app().assets[side.id]
    // Just hide everything to start.
    Doc.hide(
      side.newWalletRow, side.avail, side.immature, side.locked,
      side.expired, side.unsupported, side.connect, side.spinner, side.iconBox,
      side.walletPendingRow, side.parentIcon
    )
    side.logo.src = Doc.logoPath(side.cfg.symbol)
    Doc.empty(side.symbol)
    side.symbol.appendChild(Doc.symbolize(side.cfg.symbol))
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
      if (asset.walletCreationPending) {
        Doc.show(side.walletPendingRow)
        return
      }
      Doc.show(side.newWalletRow)
      return
    }
    // Parent asset
    side.parentBal.textContent = '—'
    if (asset.token) {
      Doc.show(this.parentRow, side.parentIcon)
      const { wallet: { balance }, unitInfo, symbol } = app().assets[asset.token.parentID]
      side.parentBal.textContent = Doc.formatCoinValue(balance.available, unitInfo)
      side.parentIcon.src = Doc.logoPath(symbol)
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
      app().fetchBalance(side.id)
      Doc.show(side.spinner)
      return
    }
    // We have a wallet and a DEX-specific balance. Set all of the fields.
    Doc.show(side.avail, side.immature, side.locked)
    side.avail.textContent = Doc.formatCoinValue(bal.available, asset.unitInfo)
    side.locked.textContent = Doc.formatCoinValue((bal.locked + bal.contractlocked), asset.unitInfo)
    side.immature.textContent = Doc.formatCoinValue(bal.immature, asset.unitInfo)
    // If the current balance update time is older than an hour, show the
    // expiration icon. Request a balance update, if possible.
    const expired = new Date().getTime() - new Date(bal.stamp).getTime() > anHour
    if (expired) {
      Doc.show(side.expired)
      if (wallet.running) app().fetchBalance(side.id)
    } else Doc.hide(side.expired)
  }

  /* updateParent updates the side's parent asset balance. */
  updateParent (side: BalanceWidgetElement) {
    const { wallet: { balance }, unitInfo, symbol } = app().assets[side.parentID]
    side.parentBal.textContent = Doc.formatCoinValue(balance.available, unitInfo)
    side.parentIcon.src = Doc.logoPath(symbol)
  }

  /*
   * updateAsset updates the info for one side of the existing market. If the
   * specified asset ID is not one of the current market's base or quote assets,
   * it is silently ignored.
   */
  updateAsset (assetID: number) {
    if (assetID === this.base.id) this.updateWallet(this.base)
    else if (assetID === this.quote.id) this.updateWallet(this.quote)
    if (assetID === this.base.parentID) this.updateParent(this.base)
    if (assetID === this.quote.parentID) this.updateParent(this.quote)
  }
}

/* makeMarket creates a market object that specifies basic market details. */
function makeMarket (host: string, base?: number, quote?: number) {
  return {
    host: host,
    base: base,
    quote: quote
  }
}

/* marketID creates a DEX-compatible market name from the ticker symbols. */
export function marketID (b: string, q: string) { return `${b}_${q}` }

/* convertToAtoms converts the float string to the basic unit of a coin. */
function convertToAtoms (s: string, conversionFactor: number) {
  if (!s) return 0
  return Math.round(parseFloat(s) * conversionFactor)
}

/* swapBttns changes the 'selected' class of the buttons. */
function swapBttns (before: HTMLElement, now: HTMLElement) {
  before.classList.remove('selected')
  now.classList.add('selected')
}

/*
 * updateDataCol sets the textContent of descendent template element.
 */
function updateDataCol (tr: HTMLElement, col: string, s: string) {
  Doc.tmplElement(tr, col).textContent = s
}

/*
 * wireOrder prepares a copy of the order with the options field converted to a
 * string -> string map.
 */
function wireOrder (order: TradeForm) {
  const stringyOptions: Record<string, string> = {}
  for (const [k, v] of Object.entries(order.options)) stringyOptions[k] = JSON.stringify(v)
  return Object.assign({}, order, { options: stringyOptions })
}

// OrderTableRowManager manages the data within a row in an order table. Each row
// represents all the orders in the order book with the same rate, but orders that
// are booked or still in the epoch queue are displayed in separate rows.
class OrderTableRowManager {
  tableRow: HTMLElement
  orderBin: MiniOrder[]
  sell: boolean
  msgRate: number
  epoch: boolean
  baseUnitInfo: UnitInfo
  rateConversionFactor: number

  constructor (tableRow: HTMLElement, orderBin: MiniOrder[], baseUnitInfo: UnitInfo, rateConversionFactor: number) {
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
      numOrdersEl.innerText = String(numOrders)
      numOrdersEl.title = `quantity is comprised of ${numOrders} orders`
    } else {
      numOrdersEl.setAttribute('hidden', 'true')
    }
  }

  // insertOrder adds an order to the order bin and updates the row elements
  // accordingly.
  insertOrder (order: MiniOrder) {
    this.orderBin.push(order)
    this.updateQtyNumOrdersEl()
  }

  // updateOrderQuantity updates the quantity of the order identified by a token,
  // if it exists in the row, and updates the row elements accordingly. The function
  // returns true if the order is in the bin, and false otherwise.
  updateOrderQty (update: RemainderUpdate) {
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
  removeOrder (token: string) {
    const index = this.orderBin.findIndex(order => order.token === token)
    if (index < 0) return false
    this.orderBin.splice(index, 1)
    if (!this.orderBin.length) this.tableRow.remove()
    else this.updateQtyNumOrdersEl()
    return true
  }

  // removeEpochOrders removes all the orders from the row that are not in the
  // new epoch's epoch queue and updates the elements accordingly.
  removeEpochOrders (newEpoch?: number) {
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
  compare (order: MiniOrder) {
    if (this.getRate() === order.msgRate && this.isEpoch() === !!order.epoch) {
      return 0
    } else if (this.getRate() !== order.msgRate) {
      return (this.getRate() > order.msgRate) === order.sell ? 1 : -1
    } else {
      return this.isEpoch() ? 1 : -1
    }
  }
}
