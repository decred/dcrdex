import {
  app,
  PageElement
} from './registry'
import * as intl from './locales'

export const Mainnet = 0
export const Testnet = 1
export const Simnet = 2

const coinIDTakerFoundMakerRedemption = 'TakerFoundMakerRedemption:'

/* ethBasedExplorerArg returns the explorer argument for ETH, ERC20 and EVM
   compatible assets, whether the return value is an address, and whether a
   link should be returned. */
function ethBasedExplorerArg (cid: string): [string, boolean] {
  if (cid.startsWith('userOpHash')) {
    const parts = cid.split(',')
    return [parts[0].substring('userOpHash:'.length), false]
  }
  if (cid.startsWith(coinIDTakerFoundMakerRedemption)) return [cid.substring(coinIDTakerFoundMakerRedemption.length), true]
  else if (cid.length === 42) return [cid, true]
  else return [cid, false]
}

const ethExplorers: Record<number, (cid: string) => string> = {
  [Mainnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://etherscan.io/address/${arg}` : `https://etherscan.io/tx/${arg}`
  },
  [Testnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://sepolia.etherscan.io/address/${arg}` : `https://sepolia.etherscan.io/tx/${arg}`
  },
  [Simnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://etherscan.io/address/${arg}` : `https://etherscan.io/tx/${arg}`
  }
}

const polygonExplorers: Record<number, (cid: string) => string> = {
  [Mainnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://polygonscan.com/address/${arg}` : `https://polygonscan.com/tx/${arg}`
  },
  [Testnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://amoy.polygonscan.com/address/${arg}` : `https://amoy.polygonscan.com/tx/${arg}`
  },
  [Simnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://polygonscan.com/address/${arg}` : `https://polygonscan.com/tx/${arg}`
  }
}

export const CoinExplorers: Record<number, Record<number, (cid: string) => string>> = {
  42: { // dcr
    [Mainnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      if (vout !== undefined) return `https://explorer.dcrdata.org/tx/${txid}/out/${vout}`
      return `https://explorer.dcrdata.org/tx/${txid}`
    },
    [Testnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      if (vout !== undefined) return `https://testnet.dcrdata.org/tx/${txid}/out/${vout}`
      return `https://testnet.dcrdata.org/tx/${txid}`
    },
    [Simnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      if (vout !== undefined) return `http://127.0.0.1:17779/tx/${txid}/out/${vout}`
      return `https://127.0.0.1:17779/tx/${txid}`
    }
  },
  0: { // btc
    [Mainnet]: (cid: string) => `https://mempool.space/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://mempool.space/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://mempool.space/tx/${cid.split(':')[0]}`
  },
  2: { // ltc
    [Mainnet]: (cid: string) => `https://ltc.bitaps.com/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://sochain.com/tx/LTCTEST/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://ltc.bitaps.com/${cid.split(':')[0]}`
  },
  20: { // dgb
    [Mainnet]: (cid: string) => `https://digiexplorer.info/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://testnetexplorer.digibyteservers.io/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://digiexplorer.info/tx/${cid.split(':')[0]}`
  },
  60: ethExplorers,
  60001: ethExplorers,
  60002: ethExplorers,
  3: { // doge
    [Mainnet]: (cid: string) => `https://dogeblocks.com/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/dogecoin/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://dogeblocks.com/tx/${cid.split(':')[0]}`
  },
  5: { // dash
    [Mainnet]: (cid: string) => `https://blockexplorer.one/dash/mainnet/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/dash/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://blockexplorer.one/dash/mainnet/tx/${cid.split(':')[0]}`
  },
  133: { // zec
    [Mainnet]: (cid: string) => `https://zcashblockexplorer.com/transactions/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/zcash/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://zcashblockexplorer.com/transactions/${cid.split(':')[0]}`
  },
  147: { // zcl
    [Mainnet]: (cid: string) => `https://explorer.zcl.zelcore.io/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://explorer.zcl.zelcore.io/tx/${cid.split(':')[0]}`
  },
  136: { // firo
    [Mainnet]: (cid: string) => `https://explorer.firo.org/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://testexplorer.firo.org/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://explorer.firo.org/tx/${cid.split(':')[0]}`
  },
  145: { // bch
    [Mainnet]: (cid: string) => `https://bch.loping.net/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://tbch4.loping.net/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://bch.loping.net/tx/${cid.split(':')[0]}`
  },
  966: polygonExplorers,
  966001: polygonExplorers,
  966002: polygonExplorers,
  966003: polygonExplorers,
  966004: polygonExplorers
}

export function formatCoinID (cid: string) : string[] {
  if (cid.startsWith(coinIDTakerFoundMakerRedemption)) {
    const makerAddr = cid.substring(coinIDTakerFoundMakerRedemption.length)
    return [intl.prep(intl.ID_TAKER_FOUND_MAKER_REDEMPTION, { makerAddr: makerAddr })]
  }
  if (cid.startsWith('userOpHash:')) {
    return cid.split(',')
  }
  return [cid]
}

/*
 * baseChainID returns the asset ID for the asset's parent if the asset is a
 * token, otherwise the ID for the asset itself.
 */
function baseChainID (assetID: number) {
  const asset = app().user.assets[assetID]
  return asset.token ? asset.token.parentID : assetID
}

/*
 * setCoinHref sets the hyperlink element's href attribute based on provided
 * assetID and data-explorer-coin value present on supplied link element.
 */
export function setCoinHref (assetID: number, link: PageElement) {
  const net = app().user.net
  const assetExplorer = CoinExplorers[baseChainID(assetID)]
  if (!assetExplorer) return
  const formatter = assetExplorer[net]
  if (!formatter) return
  link.classList.remove('plainlink')
  link.classList.add('subtlelink')
  link.href = formatter(link.dataset.explorerCoin || '')
}
