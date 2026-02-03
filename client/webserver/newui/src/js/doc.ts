import { SupportedAsset, UnitInfo } from './registry'

interface BipInfo {
  assetID: number
  logo: string
}

const languages = navigator.languages.filter((locale: string) => locale !== 'c')

const RateEncodingFactor = 1e8 // same as value defined in ./orderutil

const log10RateEncodingFactor = Math.round(Math.log10(RateEncodingFactor))

const intFormatter = new Intl.NumberFormat(languages, { maximumFractionDigits: 0 })

const fourSigFigs = new Intl.NumberFormat(languages, {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 4
})

/* A cache for formatters used for Doc.formatCoinValue. */
const decimalFormatters: Record<number, Intl.NumberFormat> = {}

/*
 * decimalFormatter gets the formatCoinValue formatter for the specified decimal
 * precision.
 */
function decimalFormatter (prec: number) {
  return formatter(decimalFormatters, 2, prec)
}

/* A cache for formatters used for Doc.formatFullPrecision. */
const fullPrecisionFormatters: Record<number, Intl.NumberFormat> = {}

/*
 * fullPrecisionFormatter gets the formatFullPrecision formatter for the
 * specified decimal precision.
 */
function fullPrecisionFormatter (prec: number, locales?: string | string[]) {
  return formatter(fullPrecisionFormatters, prec, prec, locales)
}

/*
 * formatter gets the formatter from the supplied cache if it already exists,
 * else creates it.
 */
function formatter (formatters: Record<string, Intl.NumberFormat>, min: number, max: number, locales?: string | string[]): Intl.NumberFormat {
  const k = `${min}-${max}`
  let fmt = formatters[k]
  if (!fmt) {
    fmt = new Intl.NumberFormat(locales ?? languages, {
      minimumFractionDigits: min,
      maximumFractionDigits: max
    })
    formatters[k] = fmt
  }
  return fmt
}

/*
 * convertToConventional converts the value in atomic units to conventional
 * units.
 */
function convertToConventional (v: number, unitInfo: UnitInfo) {
  const f = unitInfo.conventional.conversionFactor
  v /= f
  const prec = Math.round(Math.log10(f))
  return [v, prec]
}

export default class Doc {
  static bipMap: Record<string, BipInfo> = {}

  static registerAssets (assets: SupportedAsset[]) {
    for (const { id: assetID, symbol } of assets) {
      const logoSymbol = symbol.split('.')[0] // e.g. usdc.eth => usdc
      this.bipMap[symbol] = { assetID, logo: `/img/coins/${logoSymbol}.png` }
    }
  }

  /* bind binds the function to the event for the element. */
  static bind (el: EventTarget, ev: string | string[], f: EventListenerOrEventListenerObject, opts?: any /* EventListenerOptions */): void {
    for (const e of (Array.isArray(ev) ? ev : [ev])) el.addEventListener(e, f, opts)
  }

  /* unbind removes the handler for the event from the element. */
  static unbind (el: EventTarget, ev: string, f: (e: Event) => void): void {
    el.removeEventListener(ev, f)
  }

  /*
   * logoPath creates a path to a png logo for the specified ticker symbol. If
   * the symbol is not a supported asset, the generic letter logo will be
   * requested instead.
   */
  static logoPath (symbol: string): string {
    const bipInfo = this.bipMap[symbol]
    if (bipInfo) return bipInfo.logo
    return `/img/coins/${symbol.substring(0, 1)}.png`
  }

  /*
   * formatCoinValue formats the value in atomic units into a string
   * representation in conventional units. If the value happens to be an
   * integer, no decimals are displayed. Trailing zeros may be truncated.
   */
  static formatCoinValue (vAtomic: number, unitInfo?: UnitInfo): string {
    const [v, prec] = convertToConventional(vAtomic, unitInfo)
    if (Number.isInteger(v)) return intFormatter.format(v)
    return decimalFormatter(prec).format(v)
  }

  static conventionalCoinValue (vAtomic: number, unitInfo?: UnitInfo): number {
    const [v] = convertToConventional(vAtomic, unitInfo)
    return v
  }

  /*
   * formatRateFullPrecision formats rate to represent it exactly at rate step
   * precision, trimming non-effectual zeros if there are any.
   */
  static formatRateFullPrecision (encRate: number, bui: UnitInfo, qui: UnitInfo, rateStepEnc: number) {
    const r = bui.conventional.conversionFactor / qui.conventional.conversionFactor
    const convRate = encRate * r / RateEncodingFactor
    const rateStepDigits = log10RateEncodingFactor - Math.floor(Math.log10(rateStepEnc)) -
      Math.floor(Math.log10(bui.conventional.conversionFactor) - Math.log10(qui.conventional.conversionFactor))
    if (rateStepDigits <= 0) return intFormatter.format(convRate)
    return fullPrecisionFormatter(rateStepDigits).format(convRate)
  }

  static formatFourSigFigs (n: number, maxDecimals?: number): string {
    return formatSigFigsWithFormatters(intFormatter, fourSigFigs, n, maxDecimals)
  }

  static addrHost (addr: string) {
    // Handle IPv6 cases like "[::1]:7232"
    if (addr.startsWith("[")) return addr.substring(1, addr.lastIndexOf("]"));
    return addr.split(":")[0];
  }
}

function formatSigFigsWithFormatters (intFormatter: Intl.NumberFormat, sigFigFormatter: Intl.NumberFormat, n: number, maxDecimals?: number, locales?: string | string[]): string {
  if (n >= 1000) return intFormatter.format(n)
  const s = sigFigFormatter.format(n)
  if (typeof maxDecimals !== 'number') return s
  const fractional = sigFigFormatter.formatToParts(n).filter((part: Intl.NumberFormatPart) => part.type === 'fraction')[0]?.value ?? ''
  if (fractional.length <= maxDecimals) return s
  return fullPrecisionFormatter(maxDecimals, locales).format(n)
}