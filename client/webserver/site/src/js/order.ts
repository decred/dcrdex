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
import { formatCoinID, setCoinHref } from './coinexplorers'

// lockTimeMakerMs must match the value returned from LockTimeMaker func in
// bisonw.
const lockTimeMakerMs = 20 * 60 * 60 * 1000
// lockTimeTakerMs must match the value returned from LockTimeTaker func in
// bisonw.
const lockTimeTakerMs = 8 * 60 * 60 * 1000

const animationLength = 500

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
    this.page.mktBaseSymbol.replaceWith(Doc.symbolize(app().assets[ord.baseID]))
    this.page.mktQuoteSymbol.replaceWith(Doc.symbolize(app().assets[ord.quoteID]))

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
    tmpl.matchTime.textContent = time.toLocaleTimeString(Doc.languages(), {
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
    const [bUnit, qUnit] = [baseUnitInfo.conventional.unit.toLowerCase(), quoteUnitInfo.conventional.unit.toLowerCase()]
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
      tmpl.makerSwapAsset.textContent = bUnit
      tmpl.takerSwapAsset.textContent = qUnit
      tmpl.makerRedeemAsset.textContent = qUnit
      tmpl.takerRedeemAsset.textContent = bUnit
    } else {
      tmpl.makerSwapAsset.textContent = qUnit
      tmpl.takerSwapAsset.textContent = bUnit
      tmpl.makerRedeemAsset.textContent = bUnit
      tmpl.takerRedeemAsset.textContent = qUnit
    }

    const rate = app().conventionalRate(this.order.baseID, this.order.quoteID, match.rate)
    tmpl.rate.textContent = `${rate} ${bUnit}/${qUnit}`

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
    if (m.isCancel) return

    const tmpl = Doc.parseTemplate(matchCard)
    tmpl.status.textContent = OrderUtil.matchStatusString(m)

    const tryShowCoin = (pendingEl: PageElement, coinLink: PageElement, coin: Coin) => {
      if (!coin) {
        Doc.hide(coinLink)
        Doc.show(pendingEl)
        return
      }
      coinLink.textContent = formatCoinID(coin.stringID)
      coinLink.dataset.explorerCoin = coin.stringID
      setCoinHref(coin.assetID, coinLink)
      Doc.show(coinLink)
      Doc.hide(pendingEl)
    }

    tryShowCoin(tmpl.makerSwapPending, tmpl.makerSwapCoin, makerSwapCoin(m))
    tryShowCoin(tmpl.takerSwapPending, tmpl.takerSwapCoin, takerSwapCoin(m))
    tryShowCoin(tmpl.makerRedeemPending, tmpl.makerRedeemCoin, makerRedeemCoin(m))
    tryShowCoin(tmpl.takerRedeemPending, tmpl.takerRedeemCoin, takerRedeemCoin(m))
    if (!m.refund) {
      // Special messaging for pending refunds.
      let lockTime = lockTimeMakerMs
      if (m.side === OrderUtil.Taker) lockTime = lockTimeTakerMs
      const refundAfter = new Date(m.stamp + lockTime)
      if (Date.now() > refundAfter.getTime()) tmpl.refundPending.textContent = intl.prep(intl.ID_REFUND_IMMINENT)
      else {
        const refundAfterStr = refundAfter.toLocaleTimeString(Doc.languages(), {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        })
        tmpl.refundPending.textContent = intl.prep(intl.ID_REFUND_WILL_HAPPEN_AFTER, { refundAfterTime: refundAfterStr })
      }
      Doc.hide(tmpl.refundCoin)
      Doc.show(tmpl.refundPending)
    } else {
      tmpl.refundCoin.textContent = formatCoinID(m.refund.stringID)
      tmpl.refundCoin.dataset.explorerCoin = m.refund.stringID
      setCoinHref(m.refund.assetID, tmpl.refundCoin)
      Doc.show(tmpl.refundCoin)
      Doc.hide(tmpl.refundPending)
    }

    if (m.status === OrderUtil.MakerSwapCast && !m.revoked && !m.refund) {
      const c = makerSwapCoin(m)
      tmpl.makerSwapMsg.textContent = confirmationString(c)
      Doc.hide(tmpl.takerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg, tmpl.refundMsg)
      Doc.show(tmpl.makerSwapMsg)
    } else if (m.status === OrderUtil.TakerSwapCast && !m.revoked && !m.refund) {
      const c = takerSwapCoin(m)
      tmpl.takerSwapMsg.textContent = confirmationString(c)
      Doc.hide(tmpl.makerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg, tmpl.refundMsg)
      Doc.show(tmpl.takerSwapMsg)
    } else if (inConfirmingMakerRedeem(m) && !m.revoked && !m.refund) {
      tmpl.makerRedeemMsg.textContent = confirmationString(m.redeem)
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.takerRedeemMsg, tmpl.refundMsg)
      Doc.show(tmpl.makerRedeemMsg)
    } else if (inConfirmingTakerRedeem(m) && !m.revoked && !m.refund) {
      tmpl.takerRedeemMsg.textContent = confirmationString(m.redeem)
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.makerRedeemMsg, tmpl.refundMsg)
      Doc.show(tmpl.takerRedeemMsg)
    } else if (inConfirmingRefund(m)) {
      tmpl.refundMsg.textContent = confirmationString(m.refund)
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg)
      Doc.show(tmpl.refundMsg)
    } else {
      Doc.hide(tmpl.makerSwapMsg, tmpl.takerSwapMsg, tmpl.makerRedeemMsg, tmpl.takerRedeemMsg, tmpl.refundMsg)
    }

    if (!m.revoked) {
      // Match is still following the usual success-path, it is desirable for the
      // user to see it in full (even if to learn how atomic swap is supposed to
      // work).

      Doc.setVis(makerSwapCoin(m) || m.active, tmpl.makerSwap)
      Doc.setVis(takerSwapCoin(m) || m.active, tmpl.takerSwap)
      Doc.setVis(makerRedeemCoin(m) || m.active, tmpl.makerRedeem)
      // When maker isn't aware of taker redeem coin, once the match becomes inactive
      // (nothing else maker is expected to do in this match) just hide taker redeem.
      Doc.setVis(takerRedeemCoin(m) || m.active, tmpl.takerRedeem)
      // Refunding isn't a usual part of success-path, but don't rule it out.
      Doc.setVis(m.refund, tmpl.refund)
    } else {
      // Match diverged from the usual success-path, since this could have happened
      // at any step it is hard (maybe impossible) to predict the final state this
      // match will end up in, so show only steps that already happened plus all
      // the possibilities on the next step ahead.

      // If we don't have swap coins after revocation, we won't show the pending message.
      Doc.setVis(makerSwapCoin(m), tmpl.makerSwap)
      Doc.setVis(takerSwapCoin(m), tmpl.takerSwap)
      const takerRefundsAfter = new Date(m.stamp + lockTimeTakerMs)
      const takerLockTimeExpired = Date.now() > takerRefundsAfter.getTime()
      // When match is revoked and both swaps are present, maker redeem might still show up:
      // - as maker, we'll try to redeem until taker locktime expires (if taker refunds
      //   we won't be able to redeem; even if taker hasn't refunded just yet - it
      //   becomes too dangerous to redeem after taker locktime expired because maker
      //   reveals his secret when redeeming, and taker might be able to submit both
      //   redeem and refund transactions before maker's redeem gets mined), so we'll
      //   have to show redeem pending element until maker redeem shows up, or until
      //   we give up on redeeming due to taker locktime expiry.
      // - as taker, we should expect maker redeeming any time, so we'll have to show
      //   redeem pending element until maker redeem shows up, or until we refund.
      Doc.setVis(makerRedeemCoin(m) || (takerSwapCoin(m) && m.active && !m.refund && !takerLockTimeExpired), tmpl.makerRedeem)
      // When maker isn't aware of taker redeem coin, once the match becomes inactive
      // (nothing else maker is expected to do in this match) just hide taker redeem.
      Doc.setVis(takerRedeemCoin(m) || (makerRedeemCoin(m) && m.active && !m.refund), tmpl.takerRedeem)
      // As taker, show refund placeholder only if we have outstanding swap to refund.
      // There is no need to wait for anything else, we can show refund placeholder
      // (to inform the user that it is likely to happen) right after match revocation.
      let expectingRefund = Boolean(takerSwapCoin(m)) // as taker
      if (m.side === OrderUtil.Maker) {
        // As maker, show refund placeholder only if we have outstanding swap to refund.
        // If we don't have taker swap there is no need to wait for anything else, we
        // can show refund placeholder (to inform the user that it is likely to happen)
        // right after match revocation.
        expectingRefund = Boolean(makerSwapCoin(m))
        // If we discover taker swap we'll be trying to redeem it (instead of trying
        // to refund our own swap) until taker refunds, so start showing refund
        // placeholder only after taker is expected to start his refund process in
        // this case.
        if (takerSwapCoin(m)) {
          expectingRefund = expectingRefund && takerLockTimeExpired
        }
      }
      Doc.setVis(m.refund || (m.active && !m.redeem && !m.counterRedeem && expectingRefund), tmpl.refund)
    }
  }

  /*
   * addNewMatchCard adds a new card to the list of match cards.
   */
  addNewMatchCard (match: Match) {
    const page = this.page
    const matchCard = page.matchCardTmpl.cloneNode(true) as HTMLElement
    app().bindUrlHandlers(matchCard)
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
      orderID: order.id
    }
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
 * coin.
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
* inConfirmingRefund will be true if we are waiting on confirmations for our refund.
*/
function inConfirmingRefund (m: Match) {
  return m.status < OrderUtil.MatchConfirmed && m.refund && m.refund.confs.count <= m.refund.confs.required
}
