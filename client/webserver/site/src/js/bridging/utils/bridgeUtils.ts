import Doc from '../../doc'
import { app, UnitInfo } from '../../registry'

export function bridgeDisplayName (bridgeName: string, variant: 'short' | 'long' = 'short'): string {
  const b = bridgeName.toLowerCase()
  if (b.includes('across')) return variant === 'long' ? 'Across Bridge' : 'Across'
  if (b.includes('usdc')) return variant === 'long' ? 'CCTP Bridge' : 'CCTP'
  if (b.includes('polygon')) return variant === 'long' ? 'Polygon Bridge' : 'Polygon'
  return bridgeName
}

export function bridgeLogoPath (bridgeName: string): string {
  const b = bridgeName.toLowerCase()
  if (b.includes('polygon')) return '/img/coins/polygon.png'
  if (b.includes('usdc')) return '/img/coins/usdc.png'
  if (b.includes('across')) return '/img/coins/across.png'
  return Doc.logoPath(bridgeName)
}

export function networkInfo (assetID: number): { name: string; symbol: string } {
  const asset = app().assets[assetID]
  if (!asset) return { name: `Network ${assetID}`, symbol: '' }
  if (asset.token) {
    const parent = app().assets[asset.token.parentID]
    return { name: parent?.name || asset.name, symbol: parent?.symbol || asset.symbol }
  }
  return { name: asset.name, symbol: asset.symbol }
}

export function getFeeAsset (assetID: number) {
  const asset = app().assets[assetID]
  if (!asset) return null
  if (asset.token) {
    return app().assets[asset.token.parentID] || null
  }
  return asset
}

export function formatDateTime (timestampSec: number): string {
  if (!timestampSec) return '-'
  const date = new Date(timestampSec * 1000)
  return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export function trimStringWithEllipsis (str: string, maxLen: number): string {
  if (str.length <= maxLen) return str
  const left = Math.floor(maxLen / 2)
  const right = maxLen - left
  return `${str.substring(0, left)}...${str.substring(str.length - right)}`
}

function decimalsFromFactor (conversionFactor: number): number | null {
  if (!Number.isFinite(conversionFactor) || conversionFactor <= 0) return null
  const pow = Math.log10(conversionFactor)
  const digits = Math.round(pow)
  // Ensure it's exactly a power of 10.
  if (Math.abs(pow - digits) > 1e-12) return null
  if (Math.pow(10, digits) !== conversionFactor) return null
  return digits
}

function isSafeIntegerString (s: string): boolean {
  // "0".."9" digits only.
  if (!/^\d+$/.test(s)) return false
  // Fast path: <= 15 digits is always safe for Number integer parsing.
  if (s.length <= 15) return true
  const n = Number(s)
  return Number.isSafeInteger(n) && String(Math.trunc(n)) === s.replace(/^0+/, '') // avoid "1e+21"
}

// Non-localized conventional string suitable for <input type="number">.
export function atomicToConventionalString (vAtomic: number, unitInfo: UnitInfo, trimZeros = true): string {
  const factor = unitInfo.conventional.conversionFactor
  const decimals = decimalsFromFactor(factor)
  if (decimals === null) {
    // Fallback: best effort. (Only used if some asset uses a non-power-of-ten factor.)
    const v = vAtomic / factor
    return String(v)
  }

  const atoms = Math.trunc(vAtomic)
  const neg = atoms < 0
  const absAtoms = Math.abs(atoms)
  const whole = Math.floor(absAtoms / factor)
  const frac = absAtoms - whole * factor
  let fracStr = String(frac).padStart(decimals, '0')
  if (trimZeros) fracStr = fracStr.replace(/0+$/, '')

  const sign = neg ? '-' : ''
  if (!fracStr || decimals === 0) return `${sign}${whole}`
  return `${sign}${whole}.${fracStr}`
}

/**
 * Calculate the maximum amount that can be bridged after accounting for fees.
 * When bridging a base asset, we must reserve funds for the initiation fee.
 */
export function calculateMaxBridgeableAmount (
  sourceAssetID: number,
  availableBalance: number,
  feesAndLimits: { fees: Record<number, number> } | null
): number {
  if (!feesAndLimits) return availableBalance

  const sourceAsset = app().assets[sourceAssetID]
  if (!sourceAsset) return availableBalance

  const isSourceBaseAsset = !sourceAsset.token
  if (!isSourceBaseAsset) {
    return availableBalance
  }

  const baseAssetID = sourceAsset.token ? sourceAsset.token.parentID : sourceAssetID
  const initiationFee = feesAndLimits.fees[baseAssetID] || 0
  const maxBridgeable = availableBalance - initiationFee

  return Math.max(0, maxBridgeable)
}

export function parseConventionalToAtomic (amountStr: string, unitInfo: UnitInfo): { atomic: number | null; error: string | null } {
  const s = amountStr.trim()
  if (!s) return { atomic: null, error: null }

  // Allow "1", "1.", ".1", "0.1". Disallow scientific notation.
  if (!/^(\d+(\.\d*)?|\.\d+)$/.test(s)) return { atomic: null, error: 'Invalid amount' }

  const factor = unitInfo.conventional.conversionFactor
  const decimals = decimalsFromFactor(factor)
  if (decimals === null) {
    const n = Number(s)
    if (!Number.isFinite(n)) return { atomic: null, error: 'Invalid amount' }
    return { atomic: Math.round(n * factor), error: null }
  }

  const [wholeRaw, fracRaw = ''] = s.split('.')
  const wholeStr = wholeRaw === '' ? '0' : wholeRaw
  if (!isSafeIntegerString(wholeStr)) return { atomic: null, error: 'Amount too large' }
  const whole = Number(wholeStr)
  if (!Number.isSafeInteger(whole)) return { atomic: null, error: 'Amount too large' }
  if (whole > Math.floor(Number.MAX_SAFE_INTEGER / factor)) return { atomic: null, error: 'Amount too large' }

  const fracPadded = (fracRaw + '0'.repeat(decimals + 1)).slice(0, decimals + 1) // +1 for rounding digit
  const fracMainStr = decimals > 0 ? fracPadded.slice(0, decimals) : ''
  const roundDigit = decimals > 0 ? fracPadded[decimals] : (fracRaw[0] || '0')
  const fracMain = fracMainStr ? Number(fracMainStr) : 0
  if (!Number.isSafeInteger(fracMain)) return { atomic: null, error: 'Invalid amount' }

  let atoms = whole * factor + fracMain
  if (roundDigit >= '5') atoms += 1
  if (!Number.isSafeInteger(atoms)) return { atomic: null, error: 'Amount too large' }
  return { atomic: atoms, error: null }
}
