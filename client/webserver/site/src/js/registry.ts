declare global {
  interface Window {
    log: (...args: any) => void
    enableLogger: (loggerID: string, enable: boolean) => void
    recordLogger: (loggerID: string, enable: boolean) => void
    dumpLogger: (loggerID: string) => void
    localeDiscrepancies: () => void
  }
}

export enum ConnectionStatus {
  Disconnected = 0,
  Connected = 1,
  InvalidCert = 2,
}

export interface Exchange {
  host: string
  acctID: string
  markets: Record<string, Market>
  assets: Record<number, Asset>
  connectionStatus: ConnectionStatus
  feeAsset: FeeAsset // DEPRECATED. DCR.
  regFees: Record<string, FeeAsset>
  pendingFee: PendingFeeState | null
  candleDurs: string[]
}

export interface Candle {
  startStamp: number
  endStamp: number
  matchVolume: number
  quoteVolume: number
  highRate: number
  lowRate: number
  startRate: number
  endRate: number
}

export interface CandlesPayload {
  dur: string
  ms: number
  candles: Candle[]
}

export interface Market {
  name: string
  baseid: number
  basesymbol: string
  quoteid: number
  quotesymbol: string
  lotsize: number
  ratestep: number
  epochlen: number
  startepoch: number
  buybuffer: number
  orders: Order[]
  spot: Spot | undefined
  atomToConv: number
  inflight: InFlightOrder[]
}

export interface InFlightOrder extends Order {
  tempID: number
}

export interface Order {
  host: string
  baseID: number
  baseSymbol: string
  quoteID: number
  quoteSymbol: string
  market: string
  type: number
  id: string
  stamp: number
  submitTime: number
  sig: string
  status: number
  epoch: number
  qty: number
  sell: boolean
  filled: number
  matches: Match[]
  cancelling: boolean
  canceled: boolean
  feesPaid: FeeBreakdown
  fundingCoins: Coin[]
  accelerationCoins: Coin[]
  lockedamt: number
  rate: number // limit only
  tif: number // limit only
  targetOrderID: string // cancel only
}

export interface Match {
  matchID: string
  status: number
  active: boolean
  revoked: boolean
  rate: number
  qty: number
  side: number
  feeRate: number
  swap: Coin
  counterSwap: Coin
  redeem: Coin
  counterRedeem: Coin
  refund: Coin
  stamp: number
  isCancel: boolean
}

export interface Spot {
  stamp: number
  baseID: number
  quoteID: number
  rate: number
  bookVolume: number // Unused?
  change24: number
  vol24: number
}

export interface Asset {
  id: number
  symbol: string
  version: number
  maxFeeRate: number
  swapSize: number
  swapSizeBase: number
  redeemSize: number
  swapConf: number
  unitInfo: UnitInfo
}

export interface FeeAsset {
  id: number
  confs: number
  amount: number
}

export interface PendingFeeState {
  symbol: string
  assetID: number
  confs: number
}

export interface FeeBreakdown {
  swap: number
  redemption: number
}

export interface SupportedAsset {
  id: number
  symbol: string
  name: string
  wallet: WalletState
  info?: WalletInfo
  token?: Token
  unitInfo: UnitInfo
  walletCreationPending: boolean
}

export interface Token {
  parentID: number
  name: string
  unitInfo: UnitInfo
  definition: WalletDefinition
}

export interface WalletState {
  symbol: string
  assetID: number
  version: number
  type: string
  traits: number
  open: boolean
  running: boolean
  disabled: boolean
  balance: WalletBalance
  address: string
  units: string
  encrypted: boolean
  peerCount: number
  synced: boolean
  syncProgress: number
}

export interface WalletInfo {
  name: string
  version: number
  availablewallets: WalletDefinition[]
  emptyidx: number
  unitinfo: UnitInfo
}

export interface WalletBalance {
  available: number
  immature: number
  locked: number
  stamp: string // time.Time
  orderlocked: number
  contractlocked: number
  other: Record<string, number>
}

export interface WalletDefinition {
  seeded: boolean
  type: string
  tab: string
  description: string
  configpath: string
  configopts: ConfigOption[]
  noauth: boolean
}

export interface ConfigOption {
  key: string
  displayname: string
  description: string
  default: any
  max: any
  min: any
  noecho: boolean
  isboolean: boolean
  isdate: boolean
  disablewhenactive: boolean
  isBirthdayConfig: boolean
  repeatable?: string
  noauth: boolean
  regAsset?: number
  required?: boolean
}

export interface Coin {
  id: string
  stringID: string
  assetID: number
  symbol: string
  confs: Confirmations
}

export interface Confirmations {
  required: number
  count: number
}

export interface UnitInfo {
  atomicUnit: string
  conventional: Denomination
  denominations: Denomination[]
}

export interface Denomination {
  unit: string
  conversionFactor: number
}

export interface User {
  exchanges: Record<string, Exchange>
  inited: boolean
  seedgentime: number
  assets: Record<number, SupportedAsset>
  fiatRates: Record<number, number>
  authed: boolean // added by webserver
  ok: boolean // added by webserver
  bots: BotReport[]
}

export interface CoreNote {
  type: string
  topic: string
  subject: string
  details: string
  severity: number
  stamp: number
  acked: boolean
  id: string
}

export interface FeePaymentNote extends CoreNote {
  asset: number
  confirmations: number
  dex: string
}

export interface BalanceNote extends CoreNote {
  assetID: number
  balance: WalletBalance
}

export interface RateNote extends CoreNote {
  fiatRates: Record<number, number>
}

export interface WalletConfigNote extends CoreNote {
  wallet: WalletState
}

export type WalletStateNote = WalletConfigNote

export interface WalletCreationNote extends CoreNote {
  assetID: number
}

export interface SpotPriceNote extends CoreNote {
  host: string
  spots: Record<string, Spot>
}

export interface BotNote extends CoreNote {
  report: BotReport
}

export interface MatchNote extends CoreNote {
  orderID: string
  match: Match
  host: string
  marketID: string
}

export interface ConnEventNote extends CoreNote {
  host: string
  connectionStatus: ConnectionStatus
}

export interface OrderNote extends CoreNote {
  order: Order
  tempID: number
}

export interface RecentMatch {
  rate: number
  qty: number
  stamp: number
  sell: boolean
}

export interface EpochNote extends CoreNote {
  host: string
  marketID: string
  epoch: number
}

export interface APIResponse {
  requestSuccessful: boolean
  ok: boolean
  msg: string
  err?: string
}

export interface LogMessage {
  time: string
  msg: string
}

export interface NoteElement extends HTMLElement {
  note: CoreNote
}

export interface BalanceResponse extends APIResponse {
  balance: WalletBalance
}

export interface LayoutMetrics {
  bodyTop: number
  bodyLeft: number
  width: number
  height: number
  centerX: number
  centerY: number
}

export interface PasswordCache {
  pw: string
}

export interface PageElement extends HTMLElement {
  value?: string
  src?: string
  files?: FileList
  checked?: boolean
  href?: string
  htmlFor?: string
}

export interface BooleanConfig {
  reason: string
}

export interface XYRangePoint {
  label: string
  x: number
  y: number
}

export interface XYRange {
  start: XYRangePoint
  end: XYRangePoint
  xUnit: string
  yUnit: string
}

export interface OrderOption extends ConfigOption {
  boolean?: BooleanConfig
  xyRange?: XYRange
  showByDefault?: boolean
}

export interface SwapEstimate {
  lots: number
  value: number
  maxFees: number
  realisticWorstCase: number
  realisticBestCase: number
}

export interface RedeemEstimate {
  realisticBestCase: number
  realisticWorstCase: number
}

export interface PreSwap {
  estimate: SwapEstimate
  options: OrderOption[]
}

export interface PreRedeem {
  estimate: RedeemEstimate
  options: OrderOption[]
}

export interface OrderEstimate {
  swap: PreSwap
  redeem: PreRedeem
}

export interface MaxOrderEstimate {
  swap: SwapEstimate
  redeem: RedeemEstimate
}

export interface MaxSell {
  maxSell: MaxOrderEstimate
}

export interface MaxBuy {
  maxBuy: MaxOrderEstimate
}

export interface TradeForm {
  host: string
  isLimit: boolean
  sell: boolean
  base: number
  quote: number
  qty: number
  rate: number
  tifnow: boolean
  options: Record<string, any>
}

export interface BookUpdate {
  action: string
  host: string
  marketID: string
  matchesSummary: RecentMatch[]
  payload: any
}

export interface MiniOrder {
  qty: number
  qtyAtomic: number
  rate: number
  msgRate: number
  epoch: number
  sell: boolean
  token: string
}

export interface CoreOrderBook {
  sells: MiniOrder[]
  buys: MiniOrder[]
  epoch: MiniOrder[]
  recentMatches: RecentMatch[]
}

export interface MarketOrderBook {
  base: number
  quote: number
  book: CoreOrderBook
}

export interface RemainderUpdate {
  token: string
  qty: number
  qtyAtomic: number
}

export interface OrderFilter {
  n?: number
  offset?: string
  hosts: string[]
  assets: number[]
  statuses: number[]
}

export interface MakerProgram {
  host: string
  baseID: number
  quoteID: number
  lots: number
  oracleWeighting: number
  oracleBias: number
  driftTolerance: number
  gapFactor: number
  gapStrategy: string
}

export interface BotOrder {
  host: string
  marketID: string
  orderID: string
}

export interface BotReport {
  programID: number
  program: MakerProgram
  running: boolean
  orders: BotOrder
}

export interface MarketReport {
  basisPrice: number
  price: number
  oracles: OracleReport[]
  breakEvenSpread: number
}

export interface OracleReport {
  host: string
  usdVol: number
  bestBuy: number
  bestSell: number
}

// changing the order of the elements in this enum will affect
// the sorting of the peers table in wallets.ts.
export enum PeerSource {
  WalletDefault,
  UserAdded,
  Discovered,
}

export interface WalletPeer {
  addr: string
  source: PeerSource
  connected: boolean
}

export interface Application {
  assets: Record<number, SupportedAsset>
  seedGenTime: number
  user: User
  header: HTMLElement
  headerSpace: HTMLElement
  walletMap: Record<number, WalletState>
  exchanges: Record<string, Exchange>
  fiatRatesMap: Record<number, number>
  showPopups: boolean
  commitHash: string
  authed(): boolean
  start (): Promise<void>
  reconnected (): void
  fetchUser (): Promise<User | void>
  loadPage (page: string, data?: any, skipPush?: boolean): Promise<boolean>
  attach (data: any): void
  bindTooltips (ancestor: HTMLElement): void
  attachHeader (): void
  showDropdown (icon: HTMLElement, dialog: HTMLElement): void
  ackNotes (): void
  setNoteTimes (noteList: HTMLElement): void
  bindInternalNavigation (ancestor: HTMLElement): void
  storeNotes (): void
  updateMenuItemsDisplay (): void
  attachCommon (node: HTMLElement): void
  updateExchangeRegistration (dexAddr: string, confs: number, assetID: number): void
  handleFeePaymentNote (note: FeePaymentNote): void
  setNotes (notes: CoreNote[]): void
  notify (note: CoreNote): void
  log (loggerID: string, ...msg: any): void
  prependPokeElement (note: CoreNote): void
  prependNoteElement (note: CoreNote, skipSave?: boolean): void
  prependListElement (noteList: HTMLElement, note: CoreNote, el: NoteElement): void
  loading (el: HTMLElement): () => void
  orders (host: string, mktID: string): Order[]
  haveAssetOrders (assetID: number): boolean
  walletIsActive (assetID: number): boolean
  order (oid: string): Order | null
  canAccelerateOrder(order: Order): boolean
  unitInfo (assetID: number, xc?: Exchange): UnitInfo
  conventionalRate (baseID: number, quoteID: number, encRate: number, xc?: Exchange): number
  walletDefinition (assetID: number, walletType: string): WalletDefinition
  currentWalletDefinition (assetID: number): WalletDefinition
  fetchBalance (assetID: number): Promise<WalletBalance>
  checkResponse (resp: APIResponse): boolean
  signOut (): Promise<void>
  registerNoteFeeder (receivers: Record<string, (n: CoreNote) => void>): void
}

// TODO: Define an interface for Application?
let application: Application

export function registerApplication (a: Application) {
  application = a
}

export function app (): Application {
  return application
}
