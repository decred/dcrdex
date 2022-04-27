import Doc from './doc'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { bind as bindForm } from './forms'
import { postJSON } from './http'
import * as intl from './locales'
import {
  app,
  Order,
  PageElement,
  OrderNote,
  MatchNote,
  Match,
  Coin,
  XYRange
} from './registry'

const Mainnet = 0
const Testnet = 1
// const Regtest = 3

const animationLength = 500

let net: number

interface EarlyAcceleration {
  timePast: number,
  wasAcceleration: boolean
}

interface PreAccelerate {
  swapRate: number
  suggestedRate: number
  suggestedRange: XYRange
  earlyAcceleration?: EarlyAcceleration
}

export default class OrderPage extends BasePage {
  orderID: string
  order: Order
  page: Record<string, PageElement>
  currentForm: HTMLElement
  secondTicker: number
  acceleratedRate: number
  refreshOnPopupClose: boolean
  earlyAcceleration?: EarlyAcceleration
  earlyAccelerationAlreadyDisplayed: boolean

  constructor (main: HTMLElement) {
    super()
    const stampers = Doc.applySelector(main, '[data-stamp]')
    net = parseInt(main.dataset.net || '')
    // Find the order
    this.orderID = main.dataset.oid || ''
    const ord = app().order(this.orderID)
    // app().order can only access active orders. If the order is not active,
    // we'll need to get the data from the database.
    if (ord) this.order = ord
    else this.fetchOrder()

    const page = this.page = Doc.idDescendants(main)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => {
        if (this.refreshOnPopupClose) {
          location.replace(location.href)
          return
        }
        Doc.hide(page.forms)
      })
    })

    if (page.cancelBttn) {
      Doc.bind(page.cancelBttn, 'click', () => {
        this.showForm(page.cancelForm)
      })
    }

    Doc.cleanTemplates(page.rangeOptTmpl)
    Doc.bind(page.accelerateBttn, 'click', () => {
      this.showAccelerateForm()
    })
    Doc.bind(page.accelerateSubmit, 'click', () => {
      this.submitAccelerate()
    })
    Doc.bind(page.submitEarlyConfirm, 'click', () => {
      this.submitAccelerate()
    })
    this.showAccelerationDiv()

    // If the user clicks outside of a form, it should close the page overlay.
    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) {
        if (this.refreshOnPopupClose) {
          location.reload()
          return
        }
        Doc.hide(page.forms)
        page.cancelPass.value = ''
      }
    })

    // Cancel order form
    bindForm(page.cancelForm, page.cancelSubmit, async () => { this.submitCancel() })

    main.querySelectorAll('[data-explorer-id]').forEach((link: PageElement) => {
      setCoinHref(link)
    })

    const setStamp = () => {
      for (const span of stampers) {
        span.textContent = Doc.timeSince(parseInt(span.dataset.stamp || ''))
      }
    }
    setStamp()

    this.secondTicker = window.setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds

    this.notifiers = {
      order: (note: OrderNote) => { this.handleOrderNote(note) },
      match: (note: MatchNote) => { this.handleMatchNote(note) }
    }
  }

  unload () {
    clearInterval(this.secondTicker)
  }

  /* fetchOrder fetches the order from the client. */
  async fetchOrder () {
    const res = await postJSON('/api/order', this.orderID)
    if (!app().checkResponse(res)) return
    this.order = res.order
  }

  /* showCancel shows a form to confirm submission of a cancel order. */
  showCancel () {
    const order = this.order
    const page = this.page
    const remaining = order.qty - order.filled
    const asset = OrderUtil.isMarketBuy(order) ? app().assets[order.quoteID] : app().assets[order.baseID]
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining, asset.info.unitinfo)
    page.cancelUnit.textContent = asset.info.unitinfo.conventional.unit.toUpperCase()
    this.showForm(page.cancelForm)
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement) {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.cancelForm, page.accelerateForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0px'
  }

  /*
   * showAccelerationDiv shows the acceleration button if the "from" asset's
   * wallet supports acceleration and the order has unconfirmed swap transactions
   */
  showAccelerationDiv () {
    const order = this.order
    if (!order) return
    const page = this.page
    const canAccelerateOrder: () => boolean = () => {
      const walletTraitAccelerator = 1 << 4
      let fromAssetID
      if (order.sell) fromAssetID = order.baseID
      else fromAssetID = order.quoteID
      const wallet = app().walletMap[fromAssetID]
      if (!wallet || !(wallet.traits & walletTraitAccelerator)) return false
      if (order.matches) {
        for (let i = 0; i < order.matches.length; i++) {
          const match = order.matches[i]
          if (match.swap && match.swap.confs && match.swap.confs.count === 0) {
            return true
          }
        }
      }
      return false
    }
    if (canAccelerateOrder()) Doc.show(page.accelerateDiv)
    else Doc.hide(page.accelerateDiv)
  }

  /* showAccelerateForm shows a form to accelerate an order */
  async showAccelerateForm () {
    const page = this.page
    const order = this.order
    while (page.sliderContainer.firstChild) {
      page.sliderContainer.removeChild(page.sliderContainer.firstChild)
    }
    const loaded = app().loading(page.accelerateDiv)
    const res = await postJSON('/api/preaccelerate', order.id)
    loaded()
    if (!app().checkResponse(res)) {
      page.preAccelerateErr.textContent = `Error accelerating order: ${res.msg}`
      Doc.hide(page.accelerateMainDiv, page.accelerateSuccess)
      Doc.show(page.accelerateMsgDiv, page.preAccelerateErr)
      this.showForm(page.accelerateForm)
      return
    }
    Doc.hide(page.accelerateMsgDiv, page.preAccelerateErr, page.accelerateErr, page.feeEstimateDiv, page.earlyAccelerationDiv)
    Doc.show(page.accelerateMainDiv, page.accelerateSuccess, page.configureAccelerationDiv)
    const preAccelerate: PreAccelerate = res.preAccelerate
    this.earlyAcceleration = preAccelerate.earlyAcceleration
    this.earlyAccelerationAlreadyDisplayed = false
    page.accelerateAvgFeeRate.textContent = `${preAccelerate.swapRate} ${preAccelerate.suggestedRange.yUnit}`
    page.accelerateCurrentFeeRate.textContent = `${preAccelerate.suggestedRate} ${preAccelerate.suggestedRange.yUnit}`
    OrderUtil.setOptionTemplates(page)
    this.acceleratedRate = preAccelerate.suggestedRange.start.y
    const updated = (_: number, newY: number) => { this.acceleratedRate = newY }
    const changed = async () => {
      const req = {
        orderID: order.id,
        newRate: this.acceleratedRate
      }
      const loaded = app().loading(page.sliderContainer)
      const res = await postJSON('/api/accelerationestimate', req)
      loaded()
      if (!app().checkResponse(res)) {
        page.accelerateErr.textContent = `Error estimating acceleration fee: ${res.msg}`
        Doc.show(page.accelerateErr)
        return
      }
      page.feeRateEstimate.textContent = `${this.acceleratedRate} ${preAccelerate.suggestedRange.yUnit}`
      let assetID
      let assetSymbol
      if (order.sell) {
        assetID = order.baseID
        assetSymbol = order.baseSymbol
      } else {
        assetID = order.quoteID
        assetSymbol = order.quoteSymbol
      }
      const unitInfo = app().unitInfo(assetID)
      page.feeEstimate.textContent = `${res.fee / unitInfo.conventional.conversionFactor} ${assetSymbol}`
      Doc.show(page.feeEstimateDiv)
    }
    const selected = () => { /* do nothing */ }
    const roundY = true
    const rangeHandler = new OrderUtil.XYRangeHandler(preAccelerate.suggestedRange,
      preAccelerate.suggestedRange.start.x, updated, changed, selected, roundY)
    page.sliderContainer.appendChild(rangeHandler.control)
    changed()
    this.showForm(page.accelerateForm)
  }

  /* submitAccelerate sends a request to accelerate an order */
  async submitAccelerate () {
    const order = this.order
    const page = this.page
    const req = {
      pw: page.acceleratePass.value,
      orderID: order.id,
      newRate: this.acceleratedRate
    }
    if (this.earlyAcceleration && !this.earlyAccelerationAlreadyDisplayed) {
      page.recentAccelerationTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
      page.recentSwapTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
      if (this.earlyAcceleration.wasAcceleration) {
        Doc.show(page.recentAccelerationMsg)
        Doc.hide(page.recentSwapMsg)
        page.recentAccelerationTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
      } else {
        Doc.show(page.recentSwapMsg)
        Doc.hide(page.recentAccelerationMsg)
        page.recentSwapTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
      }
      this.earlyAccelerationAlreadyDisplayed = true
      Doc.hide(page.configureAccelerationDiv)
      Doc.show(page.earlyAccelerationDiv)
      this.earlyAccelerationAlreadyDisplayed = true
      return
    }
    page.acceleratePass.value = ''
    const loaded = app().loading(page.accelerateForm)
    const res = await postJSON('/api/accelerateorder', req)
    loaded()
    if (app().checkResponse(res)) {
      this.refreshOnPopupClose = true
      page.accelerateTxID.textContent = res.txID
      Doc.hide(page.accelerateMainDiv, page.preAccelerateErr, page.accelerateErr)
      Doc.show(page.accelerateMsgDiv, page.accelerateSuccess)
    } else {
      page.accelerateErr.textContent = `Error accelerating order: ${res.msg}`
      Doc.hide(page.earlyAccelerationDiv)
      Doc.show(page.accelerateErr, page.configureAccelerationDiv)
    }
  }

  /* submitCancel submits a cancellation for the order. */
  async submitCancel () {
    // this will be the page.cancelSubmit button (evt.currentTarget)
    const page = this.page
    const order = this.order
    const req = {
      orderID: order.id,
      pw: page.cancelPass.value
    }
    page.cancelPass.value = ''
    const loaded = app().loading(page.cancelForm)
    const res = await postJSON('/api/cancel', req)
    loaded()
    if (!app().checkResponse(res)) return
    page.status.textContent = intl.prep(intl.ID_CANCELING)
    Doc.hide(page.forms)
    order.cancelling = true
  }

  /*
   * handleOrderNote is the handler for the 'order'-type notification, which are
   * used to update an order's status.
   */
  handleOrderNote (note: OrderNote) {
    const page = this.page
    const order = note.order
    this.order = order
    const bttn = page.cancelBttn
    if (bttn && order.id === this.orderID) {
      if (bttn && order.status > OrderUtil.StatusBooked) Doc.hide(bttn)
      page.status.textContent = OrderUtil.statusString(order)
    }
    for (const m of order.matches || []) this.processMatch(m)
    this.showAccelerationDiv()
  }

  /* handleMatchNote handles a 'match' notification. */
  handleMatchNote (note: MatchNote) {
    if (note.orderID !== this.orderID) return
    this.processMatch(note.match)
  }

  /*
   * processMatch synchronizes a match's card with a match received in a
   * 'order' or 'match' notification.
   */
  processMatch (m: Match) {
    let card: HTMLElement | null = null
    for (const div of Doc.applySelector(this.page.matchBox, '.match-card')) {
      if (div.dataset.matchID === m.matchID) {
        card = div
        break
      }
    }
    if (!card) {
      // TO DO: Create a new card from template.
      return
    }

    const setCoin = (divName: string, linkName: string, coin: Coin) => {
      if (!card) return // Ugh
      if (!coin) return
      Doc.show(Doc.tmplElement(card, divName))
      const coinLink = Doc.tmplElement(card, linkName)
      coinLink.textContent = coin.stringID
      coinLink.dataset.explorerCoin = coin.stringID
      setCoinHref(coinLink)
    }

    setCoin('swap', 'swapCoin', m.swap)
    setCoin('counterSwap', 'counterSwapCoin', m.counterSwap)
    setCoin('redeem', 'redeemCoin', m.redeem)
    setCoin('counterRedeem', 'counterRedeemCoin', m.counterRedeem)
    setCoin('refund', 'refundCoin', m.refund)

    const swapSpan = Doc.tmplElement(card, 'swapMsg')
    const cSwapSpan = Doc.tmplElement(card, 'counterSwapMsg')

    if (inCounterSwapCast(m)) {
      cSwapSpan.textContent = confirmationString(m.counterSwap)
      Doc.hide(Doc.tmplElement(card, 'swapMsg'))
      Doc.show(cSwapSpan)
    } else if (inSwapCast(m)) {
      swapSpan.textContent = confirmationString(m.swap)
      Doc.hide(Doc.tmplElement(card, 'counterSwapMsg'))
      Doc.show(swapSpan)
    } else {
      Doc.hide(swapSpan, cSwapSpan)
    }

    Doc.tmplElement(card, 'status').textContent = OrderUtil.matchStatusString(m.status, m.side)
  }
}

/*
 * confirmationString is a string describing the state of confirmations for a
 * coin
 * */
function confirmationString (coin: Coin) {
  if (!coin.confs) return ''
  return `${coin.confs.count} / ${coin.confs.required} confirmations`
}

/*
 * inCounterSwapCast will be true if we are waiting on confirmations for the
 * counterparty's swap.
 */
function inCounterSwapCast (m: Match) {
  return (m.side === OrderUtil.Taker && m.status === OrderUtil.MakerSwapCast) || (m.side === OrderUtil.Maker && m.status === OrderUtil.TakerSwapCast)
}

/*
 * inCounterSwapCast will be true if we are waiting on confirmations for our own
 * swap.
 */
function inSwapCast (m: Match) {
  return (m.side === OrderUtil.Maker && m.status === OrderUtil.MakerSwapCast) || (m.side === OrderUtil.Taker && m.status === OrderUtil.TakerSwapCast)
}

/*
 * setCoinHref sets the hyperlink element's href attribute based on its
 * data-explorer-id and data-explorer-coin values.
 */
function setCoinHref (link: PageElement) {
  const assetExplorer = CoinExplorers[parseInt(link.dataset.explorerId || '')]
  if (!assetExplorer) return
  const formatter = assetExplorer[net]
  if (!formatter) return
  link.classList.remove('plainlink')
  link.classList.add('subtlelink')
  link.href = formatter(link.dataset.explorerCoin || '')
}

const CoinExplorers: Record<number, Record<number, (cid: string) => string>> = {
  42: { // dcr
    [Mainnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      return `https://explorer.dcrdata.org/tx/${txid}/out/${vout}`
    },
    [Testnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      return `https://testnet.dcrdata.org/tx/${txid}/out/${vout}`
    }
  },
  0: { // btc
    [Mainnet]: (cid: string) => `https://mempool.space/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://mempool.space/testnet/tx/${cid.split(':')[0]}`
  },
  2: { // ltc
    [Mainnet]: (cid: string) => `https://ltc.bitaps.com/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://tltc.bitaps.com/${cid.split(':')[0]}`
  },
  60: { // eth
    [Mainnet]: (cid: string) => {
      if (cid.length === 42) {
        return `https://etherscan.io/address/${cid}`
      }
      return `https://etherscan.io/tx/${cid}`
    },
    [Testnet]: (cid: string) => {
      if (cid.length === 42) {
        return `https://goerli.etherscan.io/address/${cid}`
      }
      return `https://goerli.etherscan.io/tx/${cid}`
    }
  },
  3: { // doge
    [Mainnet]: (cid: string) => `https://dogeblocks.com/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/dogecoin/testnet/tx/${cid.split(':')[0]}`
  },
  145: { // bch
    [Mainnet]: (cid: string) => `https://bch.loping.net/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://tbch4.loping.net/tx/${cid.split(':')[0]}`
  }
}
