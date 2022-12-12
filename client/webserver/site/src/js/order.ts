import Doc from './doc'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { bind as bindForm, AccelerateOrderForm } from './forms'
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
import { setOptionTemplates } from './opts'

const Mainnet = 0
const Testnet = 1
// const Regtest = 3

const coinIDTakerFoundMakerRedemption = 'TakerFoundMakerRedemption:'

const animationLength = 500

let net: number

export default class OrderPage extends BasePage {
  orderID: string
  order: Order
  page: Record<string, PageElement>
  currentForm: HTMLElement
  secondTicker: number
  refreshOnPopupClose: boolean
  accelerateOrderForm: AccelerateOrderForm
  stampers: PageElement[]

  constructor (main: HTMLElement) {
    super()
    const page = this.page = Doc.idDescendants(main)
    this.stampers = Doc.applySelector(main, '[data-stamp]')
    net = parseInt(main.dataset.net || '')
    // Find the order
    this.orderID = main.dataset.oid || ''

    Doc.cleanTemplates(page.matchCardTmpl)

    const setStamp = () => {
      for (const span of this.stampers) {
        span.textContent = Doc.timeSince(parseInt(span.dataset.stamp || ''))
      }
    }
    setStamp()

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => {
        if (this.refreshOnPopupClose) {
          window.location.replace(window.location.href)
          return
        }
        Doc.hide(page.forms)
      })
    })

    // Some static elements on this page contain assets that can be linked
    // to blockchain explorers (such as Etherscan) so users can easily
    // examine funding/acceleration coins data there. We'd need to set up
    // such hyperlinks here.
    main.querySelectorAll('[data-explorer-id]').forEach((link: PageElement) => {
      const assetID = parseInt(link.dataset.explorerId || '')
      setCoinHref(assetID, link)
    })

    if (page.cancelBttn) {
      Doc.bind(page.cancelBttn, 'click', () => {
        this.showForm(page.cancelForm)
      })
    }

    Doc.bind(page.accelerateBttn, 'click', () => {
      this.showAccelerateForm()
    })

    const success = () => {
      this.refreshOnPopupClose = true
    }
    // Do not call cleanTemplates before creating the AccelerateOrderForm
    setOptionTemplates(page)
    this.accelerateOrderForm = new AccelerateOrderForm(page.accelerateForm, success)
    Doc.cleanTemplates(page.booleanOptTmpl, page.rangeOptTmpl, page.orderOptTmpl)

    // If the user clicks outside of a form, it should close the page overlay.
    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) {
        if (this.refreshOnPopupClose) {
          window.location.reload()
          return
        }
        Doc.hide(page.forms)
        page.cancelPass.value = ''
      }
    })

    // Cancel order form
    bindForm(page.cancelForm, page.cancelSubmit, async () => { this.submitCancel() })

    this.secondTicker = window.setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds

    app().registerNoteFeeder({
      order: (note: OrderNote) => { this.handleOrderNote(note) },
      match: (note: MatchNote) => { this.handleMatchNote(note) }
    })

    this.start()
  }

  async start () {
    let ord = app().order(this.orderID)
    // app().order can only access active orders. If the order is not active,
    // we'll need to get the data from the database.
    if (ord) this.order = ord
    else {
      ord = await this.fetchOrder()
    }

    // Swap out the dot-notation symbols with token-aware symbols.
    this.page.mktBaseSymbol.replaceWith(Doc.symbolize(ord.baseSymbol))
    this.page.mktQuoteSymbol.replaceWith(Doc.symbolize(ord.quoteSymbol))

    this.setAccelerationButtonVis()
    this.showMatchCards()
  }

  unload () {
    clearInterval(this.secondTicker)
  }

  /* fetchOrder fetches the order from the client. */
  async fetchOrder (): Promise<Order> {
    const res = await postJSON('/api/order', this.orderID)
    if (!app().checkResponse(res)) throw res.msg
    this.order = res.order
    return this.order
  }

  /*
   * setImmutableMatchCardElements sets the match card elements that are never
   * changed.
   */
  setImmutableMatchCardElements (matchCard: HTMLElement, match: Match) {
    const tmpl = Doc.parseTemplate(matchCard)

    tmpl.matchID.textContent = match.matchID

    const time = new Date(match.stamp)
    tmpl.matchTime.textContent = time.toLocaleTimeString('en-GB', {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    })

    tmpl.matchTimeAgo.dataset.stamp = match.stamp.toString()
    tmpl.matchTimeAgo.textContent = Doc.timeSince(match.stamp)
    this.stampers.push(tmpl.matchTimeAgo)

    const orderPortion = OrderUtil.orderPortion(this.order, match)
    const baseSymbol = Doc.bipSymbol(this.order.baseID)
    const quoteSymbol = Doc.bipSymbol(this.order.quoteID)
    const baseUnitInfo = app().unitInfo(this.order.baseID)
    const quoteUnitInfo = app().unitInfo(this.order.quoteID)
    const quoteAmount = OrderUtil.baseToQuote(match.rate, match.qty)

    if (match.isCancel) {
      Doc.show(tmpl.cancelInfoDiv)
      Doc.hide(tmpl.infoDiv, tmpl.status, tmpl.statusHdr)

      if (this.order.sell) {
        tmpl.cancelAmount.textContent = Doc.formatCoinValue(match.qty, baseUnitInfo)
        tmpl.cancelIcon.src = Doc.logoPathFromID(this.order.baseID)
      } else {
        tmpl.cancelAmount.textContent = Doc.formatCoinValue(quoteAmount, quoteUnitInfo)
        tmpl.cancelIcon.src = Doc.logoPathFromID(this.order.quoteID)
      }

      tmpl.cancelOrderPortion.textContent = orderPortion

      return
    }

    Doc.show(tmpl.infoDiv)
    Doc.hide(tmpl.cancelInfoDiv)

    tmpl.orderPortion.textContent = orderPortion

    if (match.side === OrderUtil.Maker) {
      tmpl.side.textContent = intl.prep(intl.ID_MAKER)
      Doc.show(
        tmpl.makerSwapYou,
        tmpl.makerRedeemYou,
        tmpl.takerSwapThem,
        tmpl.takerRedeemThem
      )
      Doc.hide(
        tmpl.takerSwapYou,
        tmpl.takerRedeemYou,
        tmpl.makerSwapThem,
        tmpl.makerRedeemThem
      )
    } else {
      tmpl.side.textContent = intl.prep(intl.ID_TAKER)
      Doc.hide(
        tmpl.makerSwapYou,
        tmpl.makerRedeemYou,
        tmpl.takerSwapThem,
        tmpl.takerRedeemThem
      )
      Doc.show(
        tmpl.takerSwapYou,
        tmpl.takerRedeemYou,
        tmpl.makerSwapThem,
        tmpl.makerRedeemThem
      )
    }

    if ((match.side === OrderUtil.Maker && this.order.sell) ||
          (match.side === OrderUtil.Taker && !this.order.sell)) {
      tmpl.makerSwapAsset.textContent = baseSymbol
      tmpl.takerSwapAsset.textContent = quoteSymbol
      tmpl.makerRedeemAsset.textContent = quoteSymbol
      tmpl.takerRedeemAsset.textContent = baseSymbol
    } else {
      tmpl.makerSwapAsset.textContent = quoteSymbol
      tmpl.takerSwapAsset.textContent = baseSymbol
      tmpl.makerRedeemAsset.textContent = baseSymbol
      tmpl.takerRedeemAsset.textContent = quoteSymbol
    }

    const rate = app().conventionalRate(this.order.baseID, this.order.quoteID, match.rate)
    tmpl.rate.textContent = `${rate} ${baseSymbol}/${quoteSymbol}`

    if (this.order.sell) {
      tmpl.refundAsset.textContent = baseSymbol
      tmpl.fromAmount.textContent = Doc.formatCoinValue(match.qty, baseUnitInfo)
      tmpl.toAmount.textContent = Doc.formatCoinValue(quoteAmount, quoteUnitInfo)
      tmpl.fromIcon.src = Doc.logoPathFromID(this.order.baseID)
      tmpl.toIcon.src = Doc.logoPathFromID(this.order.quoteID)
    } else {
      tmpl.refundAsset.textContent = quoteSymbol
      tmpl.fromAmount.textContent = Doc.formatCoinValue(quoteAmount, quoteUnitInfo)
      tmpl.toAmount.textContent = Doc.formatCoinValue(match.qty, baseUnitInfo)
      tmpl.fromIcon.src = Doc.logoPathFromID(this.order.quoteID)
      tmpl.toIcon.src = Doc.logoPathFromID(this.order.baseID)
    }
  }

  /*
   * setMutableMatchCardElements sets the match card elements which may get
   * updated on each update to the match.
   */
  setMutableMatchCardElements (matchCard: HTMLElement, m: Match) {
    if (m.isCancel) {
      return
    }

    const tmpl = Doc.parseTemplate(matchCard)
    tmpl.status.textContent = OrderUtil.matchStatusString(m)

    const setCoin = (pendingName: string, linkName: string, coin: Coin) => {
      const formatCoinID = (cid: string) => {
        if (cid.startsWith(coinIDTakerFoundMakerRedemption)) {
          const makerAddr = cid.substring(coinIDTakerFoundMakerRedemption.length)
          return intl.prep(intl.ID_TAKER_FOUND_MAKER_REDEMPTION, { makerAddr: makerAddr })
        }
        return cid
      }
      const coinLink = tmpl[linkName]
      const pendingSpan = tmpl[pendingName]
      if (!coin) {
        Doc.show(tmpl[pendingName])
        Doc.hide(tmpl[linkName])
        return
      }
      coinLink.textContent = formatCoinID(coin.stringID)
      coinLink.dataset.explorerCoin = coin.stringID
      setCoinHref(coin.assetID, coinLink)
      Doc.hide(pendingSpan)
      Doc.show(coinLink)
    }

    setCoin('makerSwapPending', 'makerSwapCoin', makerSwapCoin(m))
    setCoin('takerSwapPending', 'takerSwapCoin', takerSwapCoin(m))
    setCoin('makerRedeemPending', 'makerRedeemCoin', makerRedeemCoin(m))
    setCoin('takerRedeemPending', 'takerRedeemCoin', takerRedeemCoin(m))
    setCoin('refundPending', 'refundCoin', m.refund)

    if (m.status === OrderUtil.MakerSwapCast && !m.revoked && !m.refund) {
      const c = makerSwapCoin(m)
      tmpl.makerSwapMsg.textContent = confirmationString(c)
      Doc.hide(tmpl.takerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg)
      Doc.show(tmpl.makerSwapMsg)
    } else if (m.status === OrderUtil.TakerSwapCast && !m.revoked && !m.refund) {
      const c = takerSwapCoin(m)
      tmpl.takerSwapMsg.textContent = confirmationString(c)
      Doc.hide(tmpl.makerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg)
      Doc.show(tmpl.takerSwapMsg)
    } else if (inConfirmingMakerRedeem(m) && !m.revoked && !m.refund) {
      tmpl.makerRedeemMsg.textContent = confirmationString(m.redeem)
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.takerRedeemMsg)
      Doc.show(tmpl.makerRedeemMsg)
    } else if (inConfirmingTakerRedeem(m) && !m.revoked && !m.refund) {
      tmpl.takerRedeemMsg.textContent = confirmationString(m.redeem)
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.makerRedeemMsg)
      Doc.show(tmpl.takerRedeemMsg)
    } else {
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg)
    }

    Doc.setVis(!m.isCancel && (makerSwapCoin(m) || !m.revoked), tmpl.makerSwap)
    Doc.setVis(!m.isCancel && (takerSwapCoin(m) || !m.revoked), tmpl.takerSwap)
    Doc.setVis(!m.isCancel && (makerRedeemCoin(m) || !m.revoked), tmpl.makerRedeem)
    // When revoked, there is uncertainty about the taker redeem coin. The taker
    // redeem may be needed if maker redeems while taker is waiting to refund.
    Doc.setVis(!m.isCancel && (takerRedeemCoin(m) || (!m.revoked && m.active) || ((m.side === OrderUtil.Taker) && m.active && (m.counterRedeem || !m.refund))), tmpl.takerRedeem)
    // The refund placeholder should not be shown if there is a counter redeem.
    Doc.setVis(!m.isCancel && (m.refund || (m.revoked && m.active && !m.counterRedeem)), tmpl.refund)
  }

  /*
   * addNewMatchCard adds a new card to the list of match cards.
   */
  addNewMatchCard (match: Match) {
    const page = this.page
    const matchCard = page.matchCardTmpl.cloneNode(true) as HTMLElement
    matchCard.dataset.matchID = match.matchID
    this.setImmutableMatchCardElements(matchCard, match)
    this.setMutableMatchCardElements(matchCard, match)
    page.matchBox.appendChild(matchCard)
  }

  /*
   * showMatchCards creates cards for each match in the order.
   */
  showMatchCards () {
    const order = this.order
    if (!order) return
    if (!order.matches) return
    order.matches.sort((a, b) => a.stamp - b.stamp)
    order.matches.forEach((match) => this.addNewMatchCard(match))
  }

  /* showCancel shows a form to confirm submission of a cancel order. */
  showCancel () {
    const order = this.order
    const page = this.page
    const remaining = order.qty - order.filled
    const asset = OrderUtil.isMarketBuy(order) ? app().assets[order.quoteID] : app().assets[order.baseID]
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining, asset.unitInfo)
    page.cancelUnit.textContent = asset.unitInfo.conventional.unit.toUpperCase()
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
   * setAccelerationButtonVis shows the acceleration button if the order can
   * be accelerated.
   */
  setAccelerationButtonVis () {
    const order = this.order
    if (!order) return
    const page = this.page
    Doc.setVis(app().canAccelerateOrder(order), page.accelerateBttn, page.actionsLabel)
  }

  /* showAccelerateForm shows a form to accelerate an order */
  async showAccelerateForm () {
    const loaded = app().loading(this.page.accelerateBttn)
    this.accelerateOrderForm.refresh(this.order)
    loaded()
    this.showForm(this.page.accelerateForm)
  }

  /*
   * handleOrderNote is the handler for the 'order'-type notification, which are
   * used to update an order's status.
   */
  handleOrderNote (note: OrderNote) {
    const page = this.page
    const order = note.order
    if (order.id !== this.orderID) return
    this.order = order
    const bttn = page.cancelBttn
    if (bttn && order.status > OrderUtil.StatusBooked) Doc.hide(bttn)
    page.status.textContent = OrderUtil.statusString(order)
    for (const m of order.matches || []) this.processMatch(m)
    this.setAccelerationButtonVis()
  }

  /* handleMatchNote handles a 'match' notification. */
  handleMatchNote (note: MatchNote) {
    if (note.orderID !== this.orderID) return
    this.processMatch(note.match)
    this.setAccelerationButtonVis()
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
    if (card) {
      this.setMutableMatchCardElements(card, m)
    } else {
      this.addNewMatchCard(m)
    }
  }
}

/*
 * confirmationString is a string describing the state of confirmations for a
 * coin
 * */
function confirmationString (coin: Coin) {
  if (!coin.confs || coin.confs.required === 0) return ''
  return `${coin.confs.count} / ${coin.confs.required} ${intl.prep(intl.ID_CONFIRMATIONS)}`
}

// makerSwapCoin return's the maker's swap coin.
function makerSwapCoin (m: Match) : Coin {
  return (m.side === OrderUtil.Maker) ? m.swap : m.counterSwap
}

// takerSwapCoin return's the taker's swap coin.
function takerSwapCoin (m: Match) {
  return (m.side === OrderUtil.Maker) ? m.counterSwap : m.swap
}

// makerRedeemCoin return's the maker's redeem coin.
function makerRedeemCoin (m: Match) {
  return (m.side === OrderUtil.Maker) ? m.redeem : m.counterRedeem
}

// takerRedeemCoin return's the taker's redeem coin.
function takerRedeemCoin (m: Match) {
  return (m.side === OrderUtil.Maker) ? m.counterRedeem : m.redeem
}

/*
* inConfirmingMakerRedeem will be true if we are the maker, and we are waiting
* on confirmations for our own redeem.
*/
function inConfirmingMakerRedeem (m: Match) {
  return m.status < OrderUtil.MatchConfirmed && m.side === OrderUtil.Maker && m.status >= OrderUtil.MakerRedeemed
}

/*
* inConfirmingTakerRedeem will be true if we are the taker, and we are waiting
* on confirmations for our own redeem.
*/
function inConfirmingTakerRedeem (m: Match) {
  return m.status < OrderUtil.MatchConfirmed && m.side === OrderUtil.Taker && m.status >= OrderUtil.MatchComplete
}

/*
 * setCoinHref sets the hyperlink element's href attribute based on provided
 * assetID and data-explorer-coin value present on supplied link element.
 */
function setCoinHref (assetID: number, link: PageElement) {
  const assetExplorer = CoinExplorers[assetID]
  if (!assetExplorer) return
  const formatter = assetExplorer[net]
  if (!formatter) return
  link.classList.remove('plainlink')
  link.classList.add('subtlelink')
  link.href = formatter(link.dataset.explorerCoin || '')
}

const ethExplorers: Record<number, (cid: string) => string> = {
  [Mainnet]: (cid: string) => {
    if (cid.startsWith(coinIDTakerFoundMakerRedemption)) {
      const makerAddr = cid.substring(coinIDTakerFoundMakerRedemption.length)
      return `https://etherscan.io/address/${makerAddr}`
    }
    if (cid.length === 42) {
      return `https://etherscan.io/address/${cid}`
    }
    return `https://etherscan.io/tx/${cid}`
  },
  [Testnet]: (cid: string) => {
    if (cid.startsWith(coinIDTakerFoundMakerRedemption)) {
      const makerAddr = cid.substring(coinIDTakerFoundMakerRedemption.length)
      return `https://goerli.etherscan.io/address/${makerAddr}`
    }
    if (cid.length === 42) {
      return `https://goerli.etherscan.io/address/${cid}`
    }
    return `https://goerli.etherscan.io/tx/${cid}`
  }
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
    [Testnet]: (cid: string) => `https://sochain.com/tx/LTCTEST/${cid.split(':')[0]}`
  },
  20: {
    [Mainnet]: (cid: string) => `https://digiexplorer.info/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://testnetexplorer.digibyteservers.io/tx/${cid.split(':')[0]}`
  },
  60: ethExplorers,
  60001: ethExplorers,
  3: { // doge
    [Mainnet]: (cid: string) => `https://dogeblocks.com/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/dogecoin/testnet/tx/${cid.split(':')[0]}`
  },
  145: { // bch
    [Mainnet]: (cid: string) => `https://bch.loping.net/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://tbch4.loping.net/tx/${cid.split(':')[0]}`
  }
}
