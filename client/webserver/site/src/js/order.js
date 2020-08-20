import Doc from './doc'
import BasePage from './basepage'

export default class OrderPage extends BasePage {
  constructor (application, main) {
    super()
    const page = this.page = Doc.parsePage(main, ['sinceOrder'])
    const orderStamp = parseInt(page.sinceOrder.dataset.stamp)

    const setStamp = () => {
      page.sinceOrder.textContent = Doc.timeSince(orderStamp)
    }
    setStamp()

    this.secondTicker = setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds
  }

  unload () {
    clearInterval(this.secondTicker)
  }
}
