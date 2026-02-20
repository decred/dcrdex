import Doc from './doc'
import {
  app,
  UnitInfo,
  SupportedAsset,
  WalletInfo
} from './registry'

export class NetworkAsset {
  assetID: number
  symbol: string
  ui: UnitInfo
  chainName: string
  chainLogo: string
  ticker: string
  token?: {
    parentMade: boolean
    parentID: number
    feeUI: UnitInfo
  }

  constructor (a: SupportedAsset) {
    const { id: assetID, symbol, name, token, unitInfo: ui, unitInfo: { conventional: { unit: ticker } } } = a
    this.assetID = assetID
    this.ticker = ticker
    this.symbol = symbol
    this.ui = ui
    this.chainName = token ? app().assets[token.parentID].name : name
    this.chainLogo = token ? Doc.logoPath(app().assets[token.parentID].symbol) : Doc.logoPath(symbol)
    if (token) this.token = { parentID: token.parentID, feeUI: app().unitInfo(token.parentID), parentMade: Boolean(app().assets[token.parentID].wallet) }
  }

  get bal () {
    const w = app().assets[this.assetID].wallet
    return w?.balance ?? { available: 0, locked: 0, immature: 0 }
  }

  updateTokenParentMade () {
    if (!this.token) return false
    this.token.parentMade = Boolean(app().assets[this.token.parentID].wallet)
  }
}

export class TickerAsset {
  ticker: string // normalized e.g. WETH -> ETH
  hasWallets: boolean
  cFactor: number
  bestID: number
  logoSymbol: string
  name: string
  networkAssets: NetworkAsset[]
  networkAssetLookup: Record<number, NetworkAsset>
  haveAllFiatRates: boolean
  isMultiNet: boolean
  hasTokens: boolean
  ui: UnitInfo

  constructor (a: SupportedAsset) {
    const { id: assetID, name, symbol, unitInfo: ui, unitInfo: { conventional: { conversionFactor: cFactor } } } = a
    this.ticker = normalizedTicker(a)
    this.cFactor = cFactor
    this.networkAssets = []
    this.networkAssetLookup = {}
    this.bestID = assetID
    this.name = name
    this.logoSymbol = symbol
    this.ui = ui
    this.addNetworkAsset(a)
  }

  addNetworkAsset (a: SupportedAsset) {
    const { id: assetID, symbol, name, wallet: w, token, unitInfo: ui } = a
    const xcRate = app().fiatRatesMap[assetID]
    if (!xcRate) this.haveAllFiatRates = false
    this.hasTokens = this.hasTokens || Boolean(token)
    if (!token) { // prefer the native asset data, e.g. weth.polygon -> eth
      // Use the lowest asset ID as the primary. This ensures e.g.
      // Ethereum (60) takes priority over Base for the ETH ticker.
      if (assetID < this.bestID) {
        this.bestID = assetID
        this.logoSymbol = symbol
        this.name = name
        this.ui = ui
      }
    }
    const ca = new NetworkAsset(a)
    this.hasWallets = this.hasWallets || Boolean(w) || Boolean(ca.token?.parentMade)
    this.networkAssets.push(ca)
    this.networkAssetLookup[a.id] = ca
    this.networkAssets.sort((a: NetworkAsset, b: NetworkAsset) => {
      if (a.token && !b.token) return 1
      if (!a.token && b.token) return -1
      return a.ticker.localeCompare(b.ticker)
    })
    this.isMultiNet = this.networkAssets.length > 1
  }

  walletInfo (): WalletInfo | undefined {
    for (const { assetID } of this.networkAssets) {
      const { info } = app().assets[assetID]
      if (info) return info
    }
  }

  updateHasWallets () {
    for (const ta of this.networkAssets) {
      ta.updateTokenParentMade()
      const { assetID, token } = ta
      if (app().walletMap[assetID] || token?.parentMade) {
        this.hasWallets = true
        return
      }
    }
  }

  /*
   * blockchainWallet returns the assetID and wallet for the blockchain for
   * which this ticker is a native asset, if it exists.
   */
  blockchainWallet () {
    for (const { assetID } of this.networkAssets) {
      const { wallet, token } = app().assets[assetID]
      if (!token) return { assetID, wallet }
    }
  }

  get avail () {
    return this.networkAssets.reduce((sum: number, ca: NetworkAsset) => sum + ca.bal.available, 0)
  }

  get immature () {
    return this.networkAssets.reduce((sum: number, ca: NetworkAsset) => sum + ca.bal.immature, 0)
  }

  get locked () {
    return this.networkAssets.reduce((sum: number, ca: NetworkAsset) => sum + ca.bal.locked, 0)
  }

  get total () {
    return this.networkAssets.reduce((sum: number, { bal: { available, locked, immature } }: NetworkAsset) => {
      return sum + available + locked + immature
    }, 0)
  }

  get xcRate () {
    return app().fiatRatesMap[this.bestID]
  }
}

export function normalizedTicker (a: SupportedAsset): string {
  const ticker = a.unitInfo.conventional.unit
  return ticker === 'WETH' ? 'ETH' : ticker === 'WBTC' ? 'BTC' : ticker
}
