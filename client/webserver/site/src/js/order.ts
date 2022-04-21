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
  Coin
} from './registry'

const Mainnet = 0
const Testnet = 1
// const Regtest = 3

const animationLength = 500

let net: number

export default class OrderPage extends BasePage {
  orderID: string
  order: Order
  page: Record<string, PageElement>
  currentForm: HTMLElement
  secondTicker: number

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

    if (page.cancelBttn) {
      Doc.bind(page.cancelBttn, 'click', () => {
        this.showForm(page.cancelForm)
      })
    }

    // If the user clicks outside of a form, it should close the page overlay.
    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) {
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
    Doc.hide(page.cancelForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0px'
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
    const order = note.order
    const bttn = this.page.cancelBttn
    if (bttn && order.id === this.orderID) {
      if (bttn && order.status > OrderUtil.StatusBooked) Doc.hide(bttn)
      this.page.status.textContent = OrderUtil.statusString(order)
    }
    for (const m of order.matches || []) this.processMatch(m)
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
    [Mainnet]: (cid: string) => `https://bitaps.com/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://tbtc.bitaps.com/${cid.split(':')[0]}`
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
  }
}
