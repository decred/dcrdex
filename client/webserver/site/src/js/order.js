import Doc from './doc'
import BasePage from './basepage'
import * as Order from './orderutil'
import { bind as bindForm } from './forms'
import { postJSON } from './http'

const Mainnet = 0
const Testnet = 1
// const Regtest = 3

const animationLength = 500

var app

export default class OrderPage extends BasePage {
  constructor (application, main) {
    super()
    app = application
    const stampers = main.querySelectorAll('[data-stamp]')
    const net = parseInt(main.dataset.net)
    // Find the order
    this.orderID = main.dataset.oid
    this.order = app.order(this.orderID)
    // app.order can only access active orders. If the order is not active,
    // we'll need to get the data from the database.
    if (!this.order) this.fetchOrder()
    const page = this.page = Doc.parsePage(main, [
      'cancelBttn', 'cancelRemain', 'cancelUnit', 'cancelForm', 'forms',
      'cancelSubmit', 'cancelPass', 'status'
    ])

    if (page.cancelBttn) {
      Doc.bind(page.cancelBttn, 'click', () => {
        this.showForm(page.cancelForm)
      })
    }

    // If the user clicks outside of a form, it should close the page overlay.
    Doc.bind(page.forms, 'mousedown', e => {
      if (!Doc.mouseInElement(e, this.currentForm)) {
        Doc.hide(page.forms)
        page.cancelPass.value = ''
      }
    })

    // Cancel order form
    bindForm(page.cancelForm, page.cancelSubmit, async () => { this.submitCancel() })

    main.querySelectorAll('[data-explorer-id]').forEach(link => {
      const assetExplorer = CoinExplorers[parseInt(link.dataset.explorerId)]
      if (!assetExplorer) return
      const formatter = assetExplorer[net]
      if (!formatter) return
      link.classList.remove('plainlink')
      link.classList.add('subtlelink')
      link.href = formatter(link.dataset.explorerCoin)
    })

    const setStamp = () => {
      for (const span of stampers) {
        span.textContent = Doc.timeSince(parseInt(span.dataset.stamp))
      }
    }
    setStamp()

    this.secondTicker = setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds

    this.notifiers = {
      order: note => { this.handleOrderNote(note) }
    }
  }

  unload () {
    clearInterval(this.secondTicker)
  }

  /* fetchOrder fetches the order from the client. */
  async fetchOrder () {
    var res = await postJSON('/api/order', this.orderID)
    if (!app.checkResponse(res)) return
    this.order = res.order
  }

  /* showCancel shows a form to confirm submission of a cancel order. */
  showCancel () {
    const order = this.order
    const page = this.page
    const remaining = order.qty - order.filled
    page.cancelRemain.textContent = Doc.formatCoinValue(remaining / 1e8)
    const symbol = Order.isMarketBuy(order) ? this.market.quote.symbol : this.market.base.symbol
    page.cancelUnit.textContent = symbol.toUpperCase()
    this.showForm(page.cancelForm)
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form) {
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
    app.loading(page.cancelForm)
    var res = await postJSON('/api/cancel', req)
    app.loaded()
    if (!app.checkResponse(res)) return
    page.status.textContent = 'cancelling'
    Doc.hide(page.forms)
    order.cancelling = true
  }

  /*
   * handleOrderNote is the handler for the 'order'-type notification, which are
   * used to update an order's status.
   */
  handleOrderNote (note) {
    const order = note.order
    const bttn = this.page.cancelBttn
    if (bttn && order.id === this.orderID) {
      if (bttn && order.status > Order.StatusBooked) Doc.hide(bttn)
      this.page.status.textContent = Order.statusString(order)
    }
  }
}

const CoinExplorers = {
  42: { // dcr
    [Mainnet]: cid => {
      const [txid, vout] = cid.split(':')
      return `https://explorer.dcrdata.org/tx/${txid}/out/${vout}`
    },
    [Testnet]: cid => {
      const [txid, vout] = cid.split(':')
      return `https://testnet.dcrdata.org/tx/${txid}/out/${vout}`
    }
  },
  0: { // btc
    [Mainnet]: cid => `https://bitaps.com/${cid.split(':')[0]}`,
    [Testnet]: cid => `https://tbtc.bitaps.com/${cid.split(':')[0]}`
  },
  2: { // ltc
    [Mainnet]: cid => `https://ltc.bitaps.com/${cid.split(':')[0]}`,
    [Testnet]: cid => `https://tltc.bitaps.com/${cid.split(':')[0]}`
  }
}
