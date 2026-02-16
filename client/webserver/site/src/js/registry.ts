declare global {
  interface Window {
    log: (...args: any) => void
    enableLogger: (loggerID: string, enable: boolean) => void
    recordLogger: (loggerID: string, enable: boolean) => void
    dumpLogger: (loggerID: string) => void
    mmstatus: () => Promise<MarketMakingStatus>
    testFormatFourSigFigs: () => void
    testFormatRateFullPrecision: () => void
    user: () => User
    cexBook: () => Promise<void>
    mmStatus: () => MarketMakingStatus
    isWebview?: () => boolean
    webkit: any | undefined
    electron: any | undefined
    openUrl: (url: string) => void
    open: (url?: string | URL, target?: string, features?: string) => WindowProxy | null
    sendOSNotification (title: string, body?: string): void
    clearLocale (): void
  }
}

export enum ConnectionStatus {
  Disconnected = 0,
  Connected = 1,
  InvalidCert = 2,
}

export interface BondOptions {
  bondAssetID: number
  targetTier: number
  maxBondedAmt: number
}

export interface Reputation {
  bondedTier: number
  penalties: number
  legacyTier: boolean
  score: number
}

export interface ExchangeAuth {
  rep: Reputation
  bondAssetID: number
  pendingStrength: number
  weakStrength: number
  liveStrength: number
  targetTier: number
  effectiveTier: number
  maxBondedAmt: number
  penaltyComps: number
  pendingBonds: PendingBondState[]
  expiredBonds: any[]
  compensation: number
}

export interface Exchange {
  host: string
  acctID: string
  auth: ExchangeAuth
  markets: Record<string, Market>
  assets: Record<number, Asset>
  connectionStatus: ConnectionStatus
  viewOnly: boolean
  bondAssets: Record<string, BondAsset>
  candleDurs: string[]
  maxScore: number
  penaltyThreshold: number
  disabled: boolean
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
  parcelsize: number
  ratestep: number
  epochlen: number
  startepoch: number
  buybuffer: number
  orders: Order[]
  spot: Spot | undefined
  atomToConv: number
  inflight: InFlightOrder[]
  minimumRate: number
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
  readyToTick: boolean
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
  low24: number
  high24: number
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

export interface BondAsset {
  ver: number
  id: number
  confs: number
  amount: number
}

export interface PendingBondState {
  symbol: string
  assetID: number
  coinID: string
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
}

export interface Token {
  parentID: number
  name: string
  unitInfo: UnitInfo
  contractAddress: string
  definition: WalletDefinition
  supportedAssetVersions: number[]
}

export enum ApprovalStatus {
  Approved = 0,
  Pending = 1,
  NotApproved = 2
}

export interface FeeState {
  rate: number
  send: number
  swap: number
  redeem: number
  refund: number
  stampMS: number
}

export interface SyncStatus {
  synced: boolean
  targetHeight: number
  startingBlocks: number
  blocks: number
  txs: number | undefined
}

export interface WalletState {
  symbol: string
  assetID: number
  version: number
  type: string
  class: string
  traits: number
  open: boolean
  running: boolean
  disabled: boolean
  balance: WalletBalance
  address: string
  encrypted: boolean
  peerCount: number
  synced: boolean
  syncProgress: number
  syncStatus: SyncStatus
  approved: Record<number, ApprovalStatus>
  bridgeApproved?: Record<string, ApprovalStatus>
  feeState?: FeeState
  pendingTxs: Record<string, WalletTransaction>
}

export interface WalletInfo {
  name: string
  version: number
  availablewallets: WalletDefinition[]
  versions: number[]
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
  bondlocked: number
  bondReserves: number
  reservesDeficit: number
  other: Record<string, CustomBalance>
}

export interface CustomBalance {
  amt: number
  locked: boolean
}

export interface WalletDefinition {
  seeded: boolean
  type: string
  tab: string
  description: string
  configpath: string
  configopts: ConfigOption[]
  multifundingopts: OrderOption[]
  noauth: boolean
  guidelink: string
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
  repeatN?: number
  regAsset?: number
  required?: boolean
  dependsOn?: string
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
  feeRateDenom: string
}

export interface Denomination {
  unit: string
  conversionFactor: number
}

export interface ExtensionConfiguredWallet {
  hiddenFields: string[]
  disableWalletType: boolean
  disablePassword: boolean
  disableStaking: boolean
  disablePrivacy: boolean
}

export interface ExtensionModeConfig {
  name: string
  restrictedWallets: Record<string, ExtensionConfiguredWallet>
}

export interface MeshMarket {
  baseID: number
  quoteID: number
}

export interface Mesh {
  markets: Record<string, MeshMarket>
  assetVersions: Record<number, number>
}

export interface User {
  exchanges: Record<string, Exchange>
  inited: boolean
  seedgentime: number
  assets: Record<number, SupportedAsset>
  fiatRates: Record<number, number>
  bots: BotReport[]
  net: number
  extensionModeConfig: ExtensionModeConfig
  actions: ActionRequiredNote[]
  mesh: Mesh | undefined
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

export interface BridgeNote extends CoreNote {
  sourceAssetID: number
  destAssetID: number
  txID: string
  completionTxIDs: string[]
  amount: number
  complete: boolean
}

export interface BondNote extends CoreNote {
  asset: number
  confirmations: number
  dex: string
  coinID: string | null
  tier: number | null
  auth: ExchangeAuth | null
}

export interface ReputationNote extends CoreNote {
  host: string
  rep: Reputation
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

export interface WalletSyncNote extends CoreNote {
  assetID: number
  syncStatus: SyncStatus
  syncProgress: number
}

export type WalletStateNote = WalletConfigNote

export interface WalletCreationNote extends CoreNote {
  assetID: number
}

export interface BaseWalletNote {
  route: string
  assetID: number
}

export interface TipChangeNote extends BaseWalletNote {
  tip: number
  data: any
}

export interface CustomWalletNote extends BaseWalletNote {
  payload: any
}

export interface TransactionNote extends BaseWalletNote {
  transaction: WalletTransaction
  new: boolean
}

export interface ActionRequiredNote extends BaseWalletNote {
  uniqueID: string
  actionID: string
  payload: any
}

export interface ActionResolvedNote extends BaseWalletNote {
  uniqueID: string
}

export interface TransactionActionNote {
  tx: WalletTransaction
  nonce: number
  newFees: number
}

export interface WalletNote extends CoreNote {
  payload: BaseWalletNote
}

export interface CoreActionRequiredNote extends CoreNote {
  payload: ActionRequiredNote
}

export interface RejectedTxData {
  assetID: number
  orderID: string
  coinID: string
  coinFmt: string
  txType: string
}

export interface SpotPriceNote extends CoreNote {
  host: string
  spots: Record<string, Spot>
}

export interface RunStatsNote extends CoreNote {
  host: string
  baseID: number
  quoteID: number
  stats?: RunStats
}

export interface RunEventNote extends CoreNote {
  host: string
  baseID: number
  quoteID: number
  startTime: number
  event: MarketMakingEvent
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

export interface LotFees {
  swap: number
  redeem: number
  refund: number
}

export interface LotFeeRange {
  max: LotFees
  estimated: LotFees
}

export interface AssetBookingFees extends LotFeeRange {
  bookingFeesPerLot: number
  bookingFeesPerCounterLot: number
  bookingFees: number
  swapReservesFactor: number // (1 + orderReservesFactor)
  redeemReservesFactor: number
  tokenFeesPerSwap: number
}

export interface BookingFees {
  base: AssetBookingFees
  quote: AssetBookingFees
}

export interface MarketReport {
  price: number
  oracles: OracleReport[]
  baseFiatRate: number
  quoteFiatRate: number
  baseFees: LotFeeRange
  quoteFees: LotFeeRange
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

export interface PageElement extends HTMLElement {
  value?: string
  src?: string
  files?: FileList
  checked?: boolean
  href?: string
  htmlFor?: string
  name?: string
  options?: HTMLOptionElement[]
  selectedIndex?: number
  disabled?: boolean
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
  roundX?: boolean
  roundY?: boolean
}

export interface OrderOption extends ConfigOption {
  boolean?: BooleanConfig
  xyRange?: XYRange
  showByDefault?: boolean
  quoteAssetOnly?: boolean
}

export interface SwapEstimate {
  lots: number
  value: number
  maxFees: number
  realisticWorstCase: number
  realisticBestCase: number
  feeReservesPerLot: number
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
  userOpRequired: boolean
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

export interface OrderFilterMarket {
  baseID: number
  quoteID: number
}

export interface OrderFilter {
  n?: number
  offset?: string
  hosts?: string[]
  assets?: number[]
  market?: OrderFilterMarket
  statuses?: number[]
}

export interface OrderPlacement {
  lots: number
  gapFactor: number
}

export interface AutoRebalanceConfig {
  minBaseTransfer: number
  minQuoteTransfer: number
  internalOnly: boolean
}

export type GapStrategy = 'multiplier' | 'absolute' | 'absolute-plus' | 'percent' | 'percent-plus'

export interface BasicMarketMakingConfig {
  gapStrategy: GapStrategy
  sellPlacements: OrderPlacement[]
  buyPlacements: OrderPlacement[]
  driftTolerance: number
}

export interface ArbMarketMakingPlacement {
  lots: number
  multiplier: number
}

export interface MultiHopCfg {
  baseAssetMarket: [number, number]
  quoteAssetMarket: [number, number]
  marketOrders: boolean
  limitOrdersBuffer: number
}

export interface ArbMarketMakingConfig {
  buyPlacements: ArbMarketMakingPlacement[]
  sellPlacements: ArbMarketMakingPlacement[]
  profit: number
  driftTolerance: number
  orderPersistence: number
  multiHop?: MultiHopCfg
}

export interface SimpleArbConfig {
  profitTrigger: number
  maxActiveArbs: number
  numEpochsLeaveOpen: number
}

export interface BotCEXCfg {
  name: string
  autoRebalance?: AutoRebalanceConfig
}

export interface BotBalanceAllocation {
  dex: Record<number, number>
  cex: Record<number, number>
}

export interface QuickBalanceConfig {
  buysBuffer: number
  sellsBuffer: number
  buyFeeReserve: number
  sellFeeReserve: number
  bridgeFeeReserve: number
  slippageBuffer: number
}

export interface UIConfig {
  quickBalance: QuickBalanceConfig
  usingQuickBalance: boolean
}

export interface BotConfig {
  host: string
  baseID: number
  quoteID: number
  cexBaseID: number
  cexQuoteID: number
  baseBridgeName: string
  quoteBridgeName: string
  baseWalletOptions: Record<string, string> | null
  quoteWalletOptions: Record<string, string> | null
  cexName: string
  uiConfig: UIConfig
  alloc?: BotBalanceAllocation
  autoRebalance?: AutoRebalanceConfig
  basicMarketMakingConfig?: BasicMarketMakingConfig
  arbMarketMakingConfig?: ArbMarketMakingConfig
  simpleArbConfig?: SimpleArbConfig
}

export interface CEXConfig {
  name: string
  apiKey: string
  apiSecret: string
  apiPassphrase?: string
}

export interface MarketWithHost {
  host: string
  baseID: number
  quoteID: number
}

export interface MMCEXStatus {
  config: CEXConfig
  connected: boolean
  connectErr: string
  markets: Record<string, CEXMarket>
  balances: Record<number, ExchangeBalance>
}

export interface BotBalance {
  available: number
  locked: number
  pending: number
  reserved: number
}

export interface BotBalances {
  dex: BotBalance
  cex: BotBalance
}

export interface BotInventory {
  avail: number
  locked: number // includes BotBalance.reserved
  total: number // avail + locked
}

export interface RunningBotInventory {
  avail: number
  locked: number
  dex: BotInventory
  cex: BotInventory
}

export interface CEXNotification extends CoreNote {
  cexName: string
  note: any
}

export interface CEXBalanceUpdate {
  assetID: number
  balance: ExchangeBalance
}

export interface EpochReportNote extends CoreNote {
  host: string
  baseID: number
  quoteID: number
  report?: EpochReport
}

export interface CEXProblemsNote extends CoreNote {
  host: string
  baseID: number
  quoteID: number
  problems?: CEXProblems
}

export interface FeeEstimates extends LotFeeRange {
  bookingFeesPerLot: number
  bookingFees: number
  tokenFeesPerSwap: number
}

export interface FeeGapStats {
  basisPrice: number
  feeGap: number
  remoteGap: number
  roundTripFees: number
}

export interface RunStats {
  initialBalances: Record<number, number>
  dexBalances: Record<number, BotBalance>
  cexBalances: Record<number, BotBalance>
  profitLoss: ProfitLoss
  startTime: number
  pendingDeposits: number
  pendingWithdrawals: number
  completedMatches: number
  tradedUSD: number
  feeGap: FeeGapStats
}

export interface StampedError {
  stamp: number
  error: string
}

export interface BotProblems {
  walletNotSynced: Record<number, boolean>
  noWalletPeers: Record<number, boolean>
  accountSuspended: boolean
  userLimitTooLow: boolean
  noPriceSource: boolean
  oracleFiatMismatch: boolean
  cexOrderbookUnsynced: boolean
  causesSelfMatch: boolean
  unknownError: string
}

export interface TradePlacement {
  rate: number
  lots: number
  standingLots: number
  orderedLots: number
  counterTradeRate: number
  multiHopRates: [number, number]
  requiredDex: Record<number, number>
  requiredCex: number
  usedDex: Record<number, number>
  usedCex: number
  error?: BotProblems
}

export interface OrderReport {
  placements: TradePlacement[]
  fees: LotFeeRange
  availableDexBals: Record<number, BotBalance>
  requiredDexBals: Record<number, number>
  remainingDexBals: Record<number, number>
  usedDexBals: Record<number, number>
  availableCexBal: BotBalance
  requiredCexBal: number
  remainingCexBal: number
  usedCexBal: number
  error?: BotProblems
}

export interface EpochReport {
  epochNum: number
  preOrderProblems?: BotProblems
  buysReport?: OrderReport
  sellsReport?: OrderReport
}

export interface CEXProblems {
  depositErr: Record<number, StampedError>
  withdrawErr: Record<number, StampedError>
  tradeErr: StampedError
}

export interface MMBotStatus {
  config: BotConfig
  running: boolean
  stopping?: boolean
  runStats?: RunStats
  latestEpoch?: EpochReport
  cexProblems?: CEXProblems
}

export interface MarketMakingStatus {
  cexes: Record<string, MMCEXStatus>
  bots: MMBotStatus[]
}

export interface DEXOrderEvent {
  id: string
  rate: number
  qty: number
  sell: boolean
  transactions: WalletTransaction[]
}

export interface CEXOrderEvent {
  id: string
  rate: number
  qty: number
  sell: boolean
  baseFilled: number
  quoteFilled: number
  baseID?: number
  quoteID?: number
  market?: boolean
}

export interface DepositEvent {
  assetID: number
  cexAssetID: number
  transaction?: WalletTransaction
  bridgeTx?: WalletTransaction
  cexCredit: number
}

export interface WithdrawalEvent {
  id: string
  assetID: number
  cexAssetID: number
  transaction: WalletTransaction
  bridgeTx?: WalletTransaction
  cexDebit: number
}

export interface BalanceEffects {
  settled: Record<number, number>
  pending: Record<number, number>
  locked: Record<number, number>
  reserved: Record<number, number>
}

export interface MarketMakingEvent {
  id: number
  timestamp: number
  balanceEffects: BalanceEffects
  pending: boolean
  dexOrderEvent?: DEXOrderEvent
  cexOrderEvent?: CEXOrderEvent
  depositEvent?: DepositEvent
  withdrawalEvent?: WithdrawalEvent
}

interface MarketDay {
  vol: number
  quoteVol: number
  priceChange: number
  priceChangePct: number
  avgPrice: number
  lastPrice: number
  openPrice: number
  highPrice: number
  lowPrice: number
}

export interface CEXMarket {
  baseID: number
  quoteID: number
  baseMinWithdraw: number
  quoteMinWithdraw: number
  day: MarketDay
}

export interface OracleReport {
  host: string
  usdVol: number
  bestBuy: number
  bestSell: number
}

export interface ExchangeBalance {
  available: number
  locked: number
}

// changing the order of the elements in this enum will affect
// the sorting of the peers table in wallets.ts.
export enum PeerSource {
  WalletDefault,
  UserAdded,
  Discovered,
}

export interface BalanceState {
  fiatRates: Record<number, number>
  balances: Record<number, BotBalance>
  invMods: Record<number, number>
}

export interface Amount {
  atoms: number
  conventional: number
  fmt: string
  usd: number
  fmtUSD: string
}

export interface ProfitLoss {
  initial: Record<number, Amount>
  initialUSD: number
  mods: Record<number, Amount>
  modsUSD: number
  final: Record<number, Amount>
  finalUSD: number
  diffs: Record<number, Amount>
  profit: number
  profitRatio: number
}

export interface StampedBotConfig {
  timestamp: number
  cfg: BotConfig
}

export interface MarketMakingRunOverview {
  endTime: number
  cfgs: StampedBotConfig[]
  initialBalances: Record<number, number>
  profitLoss: ProfitLoss
  finalState: BalanceState
}

export interface WalletPeer {
  addr: string
  source: PeerSource
  connected: boolean
}

export interface TicketTransaction {
  hash: string
  ticketPrice: number
  fees: number
  stamp: number
  blockHeight: number
}

export interface Ticket {
  tx: TicketTransaction
  status: number
  spender: string
}

export interface TBChoice {
  id: string
  description: string
}

export interface TBAgenda {
  id: string
  description: string
  currentChoice: string
  choices: TBChoice[]
}

export interface TKeyPolicyResult {
  key: string
  policy: string
  ticket?: string
}

export interface TBTreasurySpend {
  hash: string
  value: number
  currentPolicy: string
}

export interface Stances {
  agendas: TBAgenda[]
  tspends: TBTreasurySpend[]
  treasuryKeys: TKeyPolicyResult[]
}

export interface TicketStats {
  totalRewards: number
  ticketCount: number
  votes: number
  revokes: number
  mempool: number
  queued: number
}

export interface TicketStakingStatus {
  ticketPrice: number
  votingSubsidy: number
  vsp: string
  isRPC: boolean
  tickets: Ticket[]
  stances: Stances
  stats: TicketStats
}

export interface ProposalsMeta {
  proposalsInProgress: Proposal[]
}

export interface Proposal {
  token: string
  name: string
  username: string
  version: string
  voteStatus: string
}

// VotingServiceProvider is information about a voting service provider.
export interface VotingServiceProvider {
  url: string
  network: number
  launched: number
  lastUpdated: number
  apiVersions: number[]
  feePercentage: number
  closed: boolean
  voting: number
  voted: number
  revoked: number
  vspdVersion: string
  blockHeight: number
  netShare: number
}

export interface BondTxInfo {
  bondID: string
  lockTime: number
  accountID: string
}

export interface BridgeCounterpartTx {
  assetID: number
  ids: string[]
  complete: boolean
  amountReceived: number
  fees: number
}

export interface BridgeFeesAndLimits {
  fees: Record<number, number>
  minLimit: number
  maxLimit: number
  hasLimits: boolean
}

export interface BridgeResult {
  ok: boolean
  txID?: string
  msg?: string
}

export interface BridgeApprovalStatusResult {
  ok: boolean
  status: number // 0=Approved, 1=Pending, 2=NotApproved
  msg?: string
}

export interface BridgeApprovalResult {
  ok: boolean
  txID?: string
  msg?: string
}

// Bridge approval status constants
export const BridgeApprovalApproved = 0
export const BridgeApprovalPending = 1
export const BridgeApprovalNotApproved = 2

export interface WalletTransaction {
  type: number
  id: string
  amount: number
  fees: number
  timestamp: number
  blockNumber: number
  tokenID?: number
  recipient?: string
  bondInfo?: BondTxInfo
  additionalData: Record<string, string>
  isUserOp: boolean
  userOpTxID: string
  bridgeCounterpartTx?: BridgeCounterpartTx
  bridgeName?: string
  confirmed: boolean
  confirms?: {
    current: number
    target: number
  }
  rejected: boolean
}

export interface TxHistoryRequest {
  n: number
  refID?: string
  past: boolean
  ignoreTypes?: number[]
}

export interface TxHistoryResult {
  txs : WalletTransaction[]
  moreAvailable: boolean
}

export const PrepaidBondID = 2147483647

export interface Application {
  assets: Record<number, SupportedAsset>
  seedGenTime: number
  user: User
  mmStatus: MarketMakingStatus
  header: HTMLElement
  headerSpace: HTMLElement
  walletMap: Record<number, WalletState>
  exchanges: Record<string, Exchange>
  fiatRatesMap: Record<number, number>
  showPopups: boolean
  commitHash: string
  authed: boolean
  onionUrl: string
  start (): Promise<void>
  reconnected (): void
  fetchUser (): Promise<User | void>
  fetchMMStatus (): Promise<User | void>
  loadPage (page: string, data?: any, skipPush?: boolean): Promise<boolean>
  attach (data: any): void
  bindTooltips (ancestor: HTMLElement): void
  bindUrlHandlers (ancestor: HTMLElement): void
  attachHeader (): void
  updateMarketElements (ancestor: PageElement, baseID: number, quoteID: number, xc?: Exchange): void
  showDropdown (icon: HTMLElement, dialog: HTMLElement): void
  ackNotes (): void
  setNoteTimes (noteList: HTMLElement): void
  bindInternalNavigation (ancestor: HTMLElement): void
  updateMenuItemsDisplay (): void
  attachCommon (node: HTMLElement): void
  updateBondConfs (dexAddr: string, coinID: string, confs: number, assetID: number): void
  handleBondNote (note: BondNote): void
  loggedIn (notes: CoreNote[], pokes: CoreNote[]): void
  setPokes(pokes: CoreNote[]): void
  botStatus (host: string, baseID: number, quoteID: number): MMBotStatus | undefined
  notify (note: CoreNote): void
  log (loggerID: string, ...msg: any): void
  prependPokeElement (note: CoreNote): void
  prependNoteElement (note: CoreNote, skipSave?: boolean): void
  prependListElement (noteList: HTMLElement, note: CoreNote, el: NoteElement): void
  loading (el: HTMLElement): () => void
  orders (host: string, mktID: string): Order[]
  haveActiveOrders (assetID: number): boolean
  order (oid: string): Order | null
  canAccelerateOrder(order: Order): boolean
  unitInfo (assetID: number, xc?: Exchange): UnitInfo
  baseChainSymbol (assetID: number): string
  extensionWallet (assetID: number): ExtensionConfiguredWallet | undefined
  conventionalRate (baseID: number, quoteID: number, encRate: number, xc?: Exchange): number
  walletDefinition (assetID: number, walletType: string): WalletDefinition
  currentWalletDefinition (assetID: number): WalletDefinition
  fetchBalance (assetID: number): Promise<WalletBalance>
  checkResponse (resp: APIResponse): boolean
  signOut (): Promise<void>
  registerNoteFeeder (receivers: Record<string, (n: CoreNote) => void>): void
  parentAsset(assetID: number): SupportedAsset
  allBridgePaths (): Promise<Record<number, Record<number, string[]>>>
  bridgeFeesAndLimits (fromAssetID: number, toAssetID: number, bridgeName: string): Promise<BridgeFeesAndLimits | null>
  bridge (fromAssetID: number, toAssetID: number, amount: number, bridgeName: string): Promise<BridgeResult>
  bridgeApprovalStatus (assetID: number, bridgeName: string): Promise<BridgeApprovalStatusResult>
  approveBridgeContract (assetID: number, bridgeName: string): Promise<BridgeApprovalResult>
  unapproveBridgeContract (assetID: number, bridgeName: string): Promise<BridgeApprovalResult>
  pendingBridges (assetID: number): Promise<WalletTransaction[]>
  bridgeHistory (assetID: number, n: number, refID?: string, past?: boolean): Promise<WalletTransaction[]>
  bindUnits (ancestor: PageElement): void
  prettyPrintAssetID(assetID: number): string
}

// TODO: Define an interface for Application?
let application: Application

export function registerApplication (a: Application) {
  application = a
}

export function app (): Application {
  return application
}
