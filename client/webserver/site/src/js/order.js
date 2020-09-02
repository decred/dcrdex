import Doc from './doc'
import BasePage from './basepage'

const Mainnet = 0
const Testnet = 1
// const Regtest = 3

export default class OrderPage extends BasePage {
  constructor (application, main) {
    super()
    const stampers = main.querySelectorAll('[data-stamp]')
    const net = parseInt(main.dataset.net)

    const setStamp = () => {
      for (const span of stampers) {
        span.textContent = Doc.timeSince(parseInt(span.dataset.stamp))
      }
    }
    setStamp()

    main.querySelectorAll('[data-explorer-id]').forEach(link => {
      const assetExplorer = CoinExplorers[parseInt(link.dataset.explorerId)]
      if (!assetExplorer) return
      const formatter = assetExplorer[net]
      if (!formatter) return
      link.href = formatter(link.dataset.explorerCoin)
    })

    this.secondTicker = setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds
  }

  unload () {
    clearInterval(this.secondTicker)
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
