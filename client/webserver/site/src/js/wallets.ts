import Doc, { Animation, AniToggle, parseFloatDefault, setupCopyBtn } from './doc'
import BasePage from './basepage'
import { postJSON, Errors } from './http'
import {
  NewWalletForm,
  WalletConfigForm,
  DepositAddress,
  Forms
} from './forms'
import State from './state'
import * as intl from './locales'
import * as OrderUtil from './orderutil'
import { NetworkAsset, TickerAsset, normalizedTicker } from './assets'
import React from 'react'
import { createRoot, Root } from 'react-dom/client'
import { BridgingPopup, BridgePopupHandle } from './bridging'
import {
  app,
  PageElement,
  SupportedAsset,
  WalletDefinition,
  BalanceNote,
  WalletStateNote,
  WalletSyncNote,
  Order,
  OrderFilter,
  WalletCreationNote,
  BaseWalletNote,
  WalletNote,
  CustomWalletNote,
  TipChangeNote,
  Exchange,
  Market,
  PeerSource,
  WalletPeer,
  ApprovalStatus,
  WalletState,
  UnitInfo,
  TicketStakingStatus,
  VotingServiceProvider,
  Ticket,
  TicketStats,
  TxHistoryResult,
  TxHistoryRequest,
  TransactionNote,
  WalletTransaction,
  BridgeNote
} from './registry'
import { CoinExplorers } from './coinexplorers'

interface DecredTicketTipUpdate {
  ticketPrice: number
  votingSubsidy: number
  stats: TicketStats
}

interface TicketPurchaseUpdate extends BaseWalletNote {
  err?: string
  remaining: number
  tickets?: Ticket[]
  stats?: TicketStats
}

const animationLength = 300
const traitRescanner = 1
const traitLogFiler = 1 << 2
const traitRecoverer = 1 << 5
const traitWithdrawer = 1 << 6
const traitRestorer = 1 << 8
const traitTxFeeEstimator = 1 << 9
const traitPeerManager = 1 << 10
const traitTokenApprover = 1 << 13
const traitTicketBuyer = 1 << 15
const traitFundsMixer = 1 << 17

const traitsExtraOpts = traitLogFiler | traitRecoverer | traitRestorer | traitRescanner | traitPeerManager | traitTokenApprover

export const ticketStatusUnknown = 0
export const ticketStatusUnmined = 1
export const ticketStatusImmature = 2
export const ticketStatusLive = 3
export const ticketStatusVoted = 4
export const ticketStatusMissed = 5
export const ticketStatusExpired = 6
export const ticketStatusUnspent = 7
export const ticketStatusRevoked = 8

export const ticketStatusTranslationKeys = [
  intl.ID_TICKET_STATUS_UNKNOWN,
  intl.ID_TICKET_STATUS_UNMINED,
  intl.ID_TICKET_STATUS_IMMATURE,
  intl.ID_TICKET_STATUS_LIVE,
  intl.ID_TICKET_STATUS_VOTED,
  intl.ID_TICKET_STATUS_MISSED,
  intl.ID_TICKET_STATUS_EXPIRED,
  intl.ID_TICKET_STATUS_UNSPENT,
  intl.ID_TICKET_STATUS_REVOKED
]

export const txTypeUnknown = 0
export const txTypeSend = 1
export const txTypeReceive = 2
export const txTypeSwap = 3
export const txTypeRedeem = 4
export const txTypeRefund = 5
export const txTypeSplit = 6
export const txTypeCreateBond = 7
export const txTypeRedeemBond = 8
export const txTypeApproveToken = 9
export const txTypeAcceleration = 10
export const txTypeSelfSend = 11
export const txTypeRevokeTokenApproval = 12
export const txTypeTicketPurchase = 13
export const txTypeTicketVote = 14
export const txTypeTicketRevocation = 15
export const txTypeSwapOrSend = 16
export const txTypeMixing = 17
export const txTypeBridgeInitiation = 18
export const txTypeBridgeCompletion = 19

const positiveTxTypes: number[] = [
  txTypeReceive,
  txTypeRedeem,
  txTypeRefund,
  txTypeRedeemBond,
  txTypeTicketVote,
  txTypeTicketRevocation,
  txTypeBridgeCompletion
]

const negativeTxTypes: number[] = [
  txTypeSend,
  txTypeSwap,
  txTypeCreateBond,
  txTypeTicketPurchase,
  txTypeSwapOrSend,
  txTypeBridgeInitiation
]

const noAmtTxTypes: number[] = [
  txTypeSplit,
  txTypeApproveToken,
  txTypeAcceleration,
  txTypeRevokeTokenApproval
]

function txTypeSignAndClass (txType: number): [string, string] {
  if (positiveTxTypes.includes(txType)) return ['+', 'positive-tx']
  if (negativeTxTypes.includes(txType)) return ['-', 'negative-tx']
  return ['', '']
}

const txTypeTranslationKeys = [
  intl.ID_TX_TYPE_UNKNOWN,
  intl.ID_TX_TYPE_SEND,
  intl.ID_TX_TYPE_RECEIVE,
  intl.ID_TX_TYPE_SWAP,
  intl.ID_TX_TYPE_REDEEM,
  intl.ID_TX_TYPE_REFUND,
  intl.ID_TX_TYPE_SPLIT,
  intl.ID_TX_TYPE_CREATE_BOND,
  intl.ID_TX_TYPE_REDEEM_BOND,
  intl.ID_TX_TYPE_APPROVE_TOKEN,
  intl.ID_TX_TYPE_ACCELERATION,
  intl.ID_TX_TYPE_SELF_TRANSFER,
  intl.ID_TX_TYPE_REVOKE_TOKEN_APPROVAL,
  intl.ID_TX_TYPE_TICKET_PURCHASE,
  intl.ID_TX_TYPE_TICKET_VOTE,
  intl.ID_TX_TYPE_TICKET_REVOCATION,
  intl.ID_TX_TYPE_SWAP_OR_SEND,
  intl.ID_TX_TYPE_MIX,
  intl.ID_TX_TYPE_BRIDGE_INITIATION,
  intl.ID_TX_TYPE_BRIDGE_COMPLETION
]

const txHistoryPageSize = 10

export function txTypeString (txType: number): string {
  return intl.prep(txTypeTranslationKeys[txType])
}

const ticketPageSize = 10
const scanStartMempool = -1

interface ReconfigRequest {
  assetID: number
  walletType: string
  config: Record<string, string>
  newWalletPW?: string
}

interface RescanRecoveryRequest {
  assetID: number
  appPW?: string
  force?: boolean
}

interface WalletRestoration {
  target: string
  seed: string
  seedName: string
  instructions: string
}

interface TicketPagination {
  number: number
  history: Ticket[]
  scanned: boolean // Reached the end of history. All tickets cached.
}

interface WalletsPageData {
  goBack?: string
}

interface reconfigSettings {
  skipAnimation?: boolean
  elevateProviders?: boolean
}

interface PendingTx extends WalletTransaction {
  networkAsset: NetworkAsset
  tr: PageElement
  tmpl: Record<string, PageElement>
}

let net = 0

export default class WalletsPage extends BasePage {
  body: HTMLElement
  data?: WalletsPageData
  page: Record<string, PageElement>
  forms: Forms
  selectedTicker: TickerAsset
  tickerMap: Record<string, TickerAsset>
  tickerList: TickerAsset[]
  balTracker: Record<string, number>
  tickerTemplates: Record<string, Record<string, PageElement>>
  tickerButtons: Record<string, PageElement>
  balanceDetails: Record<string, PageElement>
  walletConfig: Record<string, PageElement>
  newWalletForm: NewWalletForm
  reconfigForm: WalletConfigForm
  walletCfgGuide: PageElement
  depositAddrForm: DepositAddress
  keyup: (e: KeyboardEvent) => void
  changeWalletPW: boolean
  displayed: HTMLElement
  animation: Animation
  formsList: PageElement[]
  forceReq: RescanRecoveryRequest
  forceUrl: string
  currentForm: PageElement
  restoreInfoCard: HTMLElement
  selectedWalletID: number
  stakeStatus: TicketStakingStatus
  maxSend: number
  unapprovingTokenVersion: number
  ticketPage: TicketPagination
  currTx: WalletTransaction | undefined
  mixing: boolean
  mixerToggle: AniToggle
  secondTicker: number
  pendingTxs: Record<string, PendingTx>
  txHistory: {
    assetID: number
    currentPage: number
    pgs: TxHistoryResult[]
    isMixing: boolean
  }

  bridgePaths: Record<number, Record<number, string[]>>
  bridgingRoot: Root | null
  bridgingPopupRef: React.RefObject<BridgePopupHandle>

  constructor (body: HTMLElement, data?: WalletsPageData) {
    super()
    this.body = body
    this.data = data
    const page = this.page = Doc.idDescendants(body)
    this.balTracker = {}
    this.tickerTemplates = {}
    this.tickerButtons = {}
    this.selectedWalletID = -1
    this.pendingTxs = {}
    this.txHistory = { pgs: [], currentPage: 0, assetID: -1, isMixing: false }
    this.bridgePaths = {}
    this.bridgingRoot = null
    this.bridgingPopupRef = React.createRef<BridgePopupHandle>()
    this.loadBridgePaths()

    this.balanceDetails = Doc.parseTemplate(page.balanceDetails)
    this.walletConfig = Doc.parseTemplate(page.walletConfig)
    this.walletConfig.div = page.walletConfig

    net = app().user.net

    const setStamp = () => {
      for (const pt of Object.values(this.pendingTxs)) pt.tmpl.age.textContent = Doc.timeSince(pt.timestamp * 1000)
      for (const row of this.page.txHistoryTableBody.children) {
        const age = Doc.tmplElement(row as PageElement, 'age')
        age.textContent = Doc.timeSince(parseInt(age.dataset.timestamp as string))
      }
    }
    this.secondTicker = window.setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds

    Doc.cleanTemplates(
      page.restoreInfoCard, page.connectedIconTmpl, page.disconnectedIconTmpl,
      page.removeIconTmpl, page.tickerBalsBox, page.blockchainBalanceTmpl,
      page.multiNetTxFeeTmpl, page.multiNetFeeRateTmpl, page.netTxFeeTmpl,
      page.netSelectBttnTmpl, page.pendingTxTmpl
    )
    this.restoreInfoCard = page.restoreInfoCard.cloneNode(true) as HTMLElement
    Doc.show(page.connectedIconTmpl, page.disconnectedIconTmpl, page.removeIconTmpl)

    Doc.cleanTemplates(
      page.iconSelectTmpl, page.recentOrderTmpl, page.vspRowTmpl,
      page.ticketHistoryRowTmpl, page.votingChoiceTmpl, page.votingAgendaTmpl, page.tspendTmpl,
      page.tkeyTmpl, page.txHistoryRowTmpl, page.txHistoryDateRowTmpl
    )

    Doc.bind(page.cancelForce, 'click', () => { this.forms.close() })
    Doc.bind(page.createWallet, 'click', () => this.showNewWallet(this.selectedWalletID))
    Doc.bind(page.connectBttn, 'click', () => this.doConnect(this.selectedWalletID))
    Doc.bind(page.send, 'click', () => this.showSendForm())
    Doc.bind(page.receive, 'click', () => this.showDeposit())
    Doc.bind(page.bridge, 'click', () => this.showBridgingPopup())
    Doc.bind(page.unlockBttn, 'click', () => this.openWallet(this.selectedWalletID))
    Doc.bind(page.lockBttn, 'click', () => this.lock(this.selectedWalletID))
    Doc.bind(page.reconfigureBttn, 'click', () => this.showReconfig(this.selectedWalletID))
    Doc.bind(page.rescanWallet, 'click', () => this.rescanWallet(this.selectedWalletID))

    const getTxID = (): string => {
      if (!this.currTx) return ''
      if (this.currTx.isUserOp) return this.currTx.userOpTxID
      return this.currTx.id
    }
    Doc.bind(page.copyTxIDBtn, 'click', () => { setupCopyBtn(getTxID(), page.txDetailsID, page.copyTxIDBtn, '#1e7d11') })
    Doc.bind(page.copyUserOpIDBtn, 'click', () => { setupCopyBtn(this.currTx?.id || '', page.txDetailsUserOpID, page.copyUserOpIDBtn, '#1e7d11') })
    Doc.bind(page.copyRecipientBtn, 'click', () => { setupCopyBtn(this.currTx?.recipient || '', page.txDetailsRecipient, page.copyRecipientBtn, '#1e7d11') })
    Doc.bind(page.copyBondIDBtn, 'click', () => { setupCopyBtn(this.currTx?.bondInfo?.bondID || '', page.txDetailsBondID, page.copyBondIDBtn, '#1e7d11') })
    Doc.bind(page.copyBondAccountIDBtn, 'click', () => { setupCopyBtn(this.currTx?.bondInfo?.accountID || '', page.txDetailsBondAccountID, page.copyBondAccountIDBtn, '#1e7d11') })
    Doc.bind(page.hideMixTxsCheckbox, 'change', () => { this.showTxHistory(this.selectedWalletID) })

    // Forms
    this.forms = new Forms(page.forms)
    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') this.forms.close()
    }
    Doc.bind(document, 'keyup', this.keyup)

    this.newWalletForm = new NewWalletForm(page.newWalletForm, async (assetID: number) => {
      await app().fetchUser()
      const fmtParams = { assetName: app().assets[assetID].name }
      this.assetUpdated(assetID, page.newWalletForm, intl.prep(intl.ID_NEW_WALLET_SUCCESS, fmtParams))
      for (const ta of this.tickerList) ta.updateHasWallets()
      this.refreshBalances()
      this.sortTickers()
      this.updateGlobalBalance()
      if (this.selectedTicker.networkAssetLookup[assetID]) this.updateDisplayedTicker()
      this.updateTicketBuyer()
      this.updatePrivacy()
    })

    this.reconfigForm = new WalletConfigForm(page.reconfigInputs, false)
    this.walletCfgGuide = Doc.tmplElement(page.reconfigForm, 'walletCfgGuide')
    this.depositAddrForm = new DepositAddress(page.deposit)
    this.mixerToggle = new AniToggle(page.toggleMixer, page.mixingErr, false, (newState: boolean) => { return this.updateMixerState(newState) })

    Doc.bind(page.submitSendForm, 'click', async () => { this.stepSend() })
    Doc.bind(page.vSend, 'click', async () => { this.send() })
    Doc.bind(page.submitReconfig, 'click', () => this.reconfig())
    Doc.bind(page.downloadLogs, 'click', async () => { this.downloadLogs() })
    Doc.bind(page.exportWallet, 'click', async () => { this.displayExportWalletAuth() })
    Doc.bind(page.recoverWallet, 'click', async () => { this.showRecoverWallet() })
    Doc.bind(page.exportWalletAuthSubmit, 'click', async () => { this.exportWalletAuthSubmit() })
    Doc.bind(page.recoverWalletSubmit, 'click', () => { this.recoverWallet() })
    Doc.bind(page.confirmForceSubmit, 'click', async () => { this.confirmForceSubmit() })
    Doc.bind(page.disableWallet, 'click', async () => { this.showToggleWalletStatus(true) })
    Doc.bind(page.enableWallet, 'click', async () => { this.showToggleWalletStatus(false) })
    Doc.bind(page.toggleWalletStatusSubmit, 'click', async () => { this.toggleWalletStatus() })
    Doc.bind(page.managePeers, 'click', async () => { this.showManagePeersForm() })
    Doc.bind(page.addPeerSubmit, 'click', async () => { this.submitAddPeer() })
    Doc.bind(page.unapproveTokenAllowance, 'click', async () => { this.showUnapproveTokenAllowanceTableForm() })
    Doc.bind(page.unapproveTokenSubmit, 'click', async () => { this.submitUnapproveTokenAllowance() })
    Doc.bind(page.showVSPs, 'click', () => { this.showVSPPicker() })
    Doc.bind(page.vspDisplay, 'click', () => { this.showVSPPicker() })
    Doc.bind(page.customVspSubmit, 'click', async () => { this.setCustomVSP() })
    Doc.bind(page.purchaseTicketsBttn, 'click', () => { this.showPurchaseTicketsDialog() })
    Doc.bind(page.purchaserSubmit, 'click', () => { this.purchaseTickets() })
    Doc.bind(page.purchaserInput, 'change', () => { this.purchaserInputChanged() })
    Doc.bind(page.ticketHistory, 'click', () => { this.showTicketHistory() })
    Doc.bind(page.ticketHistoryNextPage, 'click', () => { this.nextTicketPage() })
    Doc.bind(page.ticketHistoryPrevPage, 'click', () => { this.prevTicketPage() })
    Doc.bind(page.setVotes, 'click', () => { this.showSetVotesDialog() })
    Doc.bind(page.purchaseTicketsErrCloser, 'click', () => { Doc.hide(page.purchaseTicketsErrBox) })
    Doc.bind(page.privacyInfoBttn, 'click', () => { this.forms.show(page.mixingInfo) })
    Doc.bind(page.walletBal, 'click', () => { this.populateMaxSend() })
    Doc.bind(page.expandPendingTxs, 'click', () => { this.toggleExpandPendingTxs() })
    Doc.bind(page.txHistoryBack, 'click', () => { this.showTxHistoryPage(this.txHistory.currentPage - 1) })
    Doc.bind(page.txHistoryFwd, 'click', () => { this.showTxHistoryPage(this.txHistory.currentPage + 1) })

    // Display fiat value for current send amount.
    Doc.bind(page.sendAmt, 'input', () => {
      const assetID = parseInt(page.sendForm.dataset.assetID || '')
      const ui = app().unitInfo(assetID)
      const amt = parseFloatDefault(page.sendAmt.value)
      const conversionFactor = ui.conventional.conversionFactor
      Doc.showFiatValue(page.sendValue, amt * conversionFactor, app().fiatRatesMap[this.selectedWalletID], ui)
    })

    // Clicking on maxSend on the send form should populate the amount field.
    Doc.bind(page.maxSend, 'click', () => { this.populateMaxSend() })

    // Validate send address on input.
    Doc.bind(page.sendAddr, 'input', async () => {
      const asset = app().assets[this.selectedWalletID]
      page.sendAddr.classList.remove('border-danger', 'border-success')
      const addr = page.sendAddr.value || ''
      if (!asset || addr === '') return
      const valid = await this.validateSendAddress(addr, asset.id)
      if (valid) page.sendAddr.classList.add('border-success')
      else page.sendAddr.classList.add('border-danger')
    })

    // A link on the wallet reconfiguration form to show/hide the password field.
    Doc.bind(page.showChangePW, 'click', () => {
      this.changeWalletPW = !this.changeWalletPW
      this.setPWSettingViz(this.changeWalletPW)
    })

    // Changing the type of wallet.
    Doc.bind(page.changeWalletTypeSelect, 'change', () => {
      this.changeWalletType()
    })
    Doc.bind(page.showChangeType, 'click', () => {
      if (Doc.isHidden(page.changeWalletType)) {
        Doc.show(page.changeWalletType, page.changeTypeHideIcon)
        Doc.hide(page.changeTypeShowIcon)
        page.changeTypeMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_TYPE)
      } else this.showReconfig(this.selectedWalletID, { skipAnimation: true })
    })

    app().registerNoteFeeder({
      fiatrateupdate: () => { this.handleRatesNote() },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      walletstate: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletconfig: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletsync: (note: WalletSyncNote) => {
        if (note.assetID === this.selectedWalletID) this.updateSyncAndPeers()
      },
      createwallet: (note: WalletCreationNote) => { this.handleCreateWalletNote(note) },
      walletnote: (note: WalletNote) => { this.handleCustomWalletNote(note) },
      bridge: (note: BridgeNote) => { this.handleBridgeNote(note) }
    })

    this.prepareTickerAssets()
    this.setTickerButtons()
    this.refreshBalances()
    this.updateGlobalBalance()
    let lastTicker = State.fetchLocal(State.selectedAssetLK)
    if (!lastTicker || !this.tickerMap[lastTicker]) lastTicker = 'DCR'
    const expanded = Boolean(State.fetchLocal(State.pendingTxsExpandedLK))
    if (expanded) {
      page.expandPendingTxs.classList.add('ico-arrowup')
      page.expandPendingTxs.classList.remove('ico-arrowdown')
    }
    this.start(lastTicker)
  }

  async start (firstTicker: string) {
    await this.setSelectedTicker(firstTicker)
    this.page.walletDetailsBox.classList.remove('invisible')
    this.page.assetSelect.classList.remove('invisible')
    this.page.secondColumn.classList.remove('invisible')
  }

  async safePost (path: string, args: any): Promise<any> {
    const assetID = this.selectedWalletID
    const res = await postJSON(path, args)
    if (assetID !== this.selectedWalletID) throw Error('asset changed during request. aborting')
    return res
  }

  // stepSend makes a request to get an estimated fee and displays the confirm
  // send form.
  async stepSend () {
    const page = this.page
    Doc.hide(page.vSendErr, page.sendErr, page.vSendEstimates, page.txFeeNotAvailable)
    const assetID = parseInt(page.sendForm.dataset.assetID || '')
    const token = app().assets[assetID].token
    const subtract = page.subtractCheckBox.checked || false
    const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
    const value = Math.round(parseFloatDefault(page.sendAmt.value, 0) * conversionFactor)
    const addr = page.sendAddr.value || ''
    if (addr === '') return Doc.showFormError(page.sendErr, intl.prep(intl.ID_INVALID_ADDRESS_MSG, { address: addr }))
    const { wallet, unitInfo: ui, symbol } = app().assets[assetID]

    // txfee will not be available if wallet is not a fee estimator or the
    // request failed.
    let txfee = 0
    if ((wallet.traits & traitTxFeeEstimator) !== 0) {
      const open = {
        addr: page.sendAddr.value,
        assetID: assetID,
        subtract: subtract,
        value: value
      }

      const loaded = app().loading(page.sendForm)
      const res = await postJSON('/api/txfee', open)
      loaded()
      if (!app().checkResponse(res)) {
        page.txFeeNotAvailable.dataset.tooltip = intl.prep(intl.ID_TXFEE_ERR_MSG, { err: res.msg })
        Doc.show(page.txFeeNotAvailable)
        // We still want to ensure user address is valid before proceeding to send
        // confirm form if there's an error while calculating the transaction fee.
        const valid = await this.validateSendAddress(addr, assetID)
        if (!valid) return Doc.showFormError(page.sendErr, intl.prep(intl.ID_INVALID_ADDRESS_MSG, { address: addr || '' }))
      } else if (res.ok) {
        if (!res.validaddress) return Doc.showFormError(page.sendErr, intl.prep(intl.ID_INVALID_ADDRESS_MSG, { address: page.sendAddr.value || '' }))
        txfee = res.txfee
        Doc.show(page.vSendEstimates)
      }
    } else {
      // Validate only the send address for assets that are not fee estimators.
      const valid = await this.validateSendAddress(addr, assetID)
      if (!valid) return Doc.showFormError(page.sendErr, intl.prep(intl.ID_INVALID_ADDRESS_MSG, { address: addr || '' }))
    }

    page.vSendSymbol.textContent = symbol.toUpperCase()
    page.vSendLogo.src = Doc.logoPath(symbol)

    let feeAsset: any = null
    if (token) {
      feeAsset = app().assets[token.parentID]
      const { unitInfo: feeUI, symbol: feeSymbol } = feeAsset
      page.vSendFee.textContent = Doc.formatFullPrecision(txfee, feeUI) + ' ' + feeSymbol
    } else {
      page.vSendFee.textContent = Doc.formatFullPrecision(txfee, ui)
    }
    const xcRate = app().fiatRatesMap[assetID]
    if (token) {
      Doc.showFiatValue(page.vSendFeeFiat, txfee, app().fiatRatesMap[token.parentID], feeAsset.unitInfo)
    } else {
      Doc.showFiatValue(page.vSendFeeFiat, txfee, xcRate, ui)
    }
    page.vSendDestinationAmt.textContent = Doc.formatFullPrecision(value - txfee, ui)
    page.vTotalSend.textContent = Doc.formatFullPrecision(value, ui)
    Doc.showFiatValue(page.vTotalSendFiat, value, xcRate, ui)
    page.vSendAddr.textContent = page.sendAddr.value || ''
    const bal = wallet.balance.available - value
    page.balanceAfterSend.textContent = Doc.formatFullPrecision(bal, ui)
    Doc.showFiatValue(page.balanceAfterSendFiat, bal, xcRate, ui)
    Doc.show(page.approxSign)
    // NOTE: All tokens take this route because they cannot pay the fee.
    if (!subtract) {
      Doc.hide(page.approxSign)
      page.vSendDestinationAmt.textContent = Doc.formatFullPrecision(value, ui)
      let totalSend = value
      if (!token) totalSend += txfee
      page.vTotalSend.textContent = Doc.formatFullPrecision(totalSend, ui)
      Doc.showFiatValue(page.vTotalSendFiat, totalSend, xcRate, ui)
      let bal = wallet.balance.available - value
      if (!token) bal -= txfee
      // handle edge cases where bal is not enough to cover totalSend.
      // we don't want a minus display of user bal.
      if (bal <= 0) {
        page.balanceAfterSend.textContent = Doc.formatFullPrecision(0, ui)
        Doc.showFiatValue(page.balanceAfterSendFiat, 0, xcRate, ui)
      } else {
        page.balanceAfterSend.textContent = Doc.formatFullPrecision(bal, ui)
        Doc.showFiatValue(page.balanceAfterSendFiat, bal, xcRate, ui)
      }
    }
    Doc.hide(page.sendForm)
    await this.forms.show(page.vSendForm)
  }

  // cancelSend displays the send form if user wants to make modification.
  async cancelSend () {
    const page = this.page
    Doc.hide(page.vSendForm, page.sendErr)
    await this.forms.show(page.sendForm)
  }

  /*
   * validateSendAddress validates the provided address for an asset.
   */
  async validateSendAddress (addr: string, assetID: number): Promise<boolean> {
    const resp = await postJSON('/api/validateaddress', { addr: addr, assetID: assetID })
    return app().checkResponse(resp)
  }

  /*
   * setPWSettingViz sets the visibility of the password field section.
   */
  setPWSettingViz (visible: boolean) {
    const page = this.page
    if (visible) {
      Doc.hide(page.showIcon)
      Doc.show(page.hideIcon, page.changePW)
      page.switchPWMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_PASS)
      return
    }
    Doc.hide(page.hideIcon, page.changePW)
    Doc.show(page.showIcon)
    page.switchPWMsg.textContent = intl.prep(intl.ID_NEW_WALLET_PASS)
  }

  /*
   * assetVersionUsedByDEXes returns a map of the versions of the
   * currently selected asset to the DEXes that use that version.
   */
  assetVersionUsedByDEXes (): Record<number, string[]> {
    const assetID = this.selectedWalletID
    const versionToDEXes = {} as Record<number, string[]>
    const exchanges = app().exchanges

    for (const host in exchanges) {
      const exchange = exchanges[host]
      const exchangeAsset = exchange.assets[assetID]
      if (!exchangeAsset) continue
      if (!versionToDEXes[exchangeAsset.version]) {
        versionToDEXes[exchangeAsset.version] = []
      }
      versionToDEXes[exchangeAsset.version].push(exchange.host)
    }

    return versionToDEXes
  }

  /*
   * submitUnapproveTokenAllowance submits a request to the server to
   * unapprove a version of the currently selected token's swap contract.
   */
  async submitUnapproveTokenAllowance () {
    const page = this.page
    const path = '/api/unapprovetoken'
    const res = await postJSON(path, {
      assetID: this.selectedWalletID,
      version: this.unapprovingTokenVersion
    })
    if (!app().checkResponse(res)) {
      page.unapproveTokenErr.textContent = res.msg
      Doc.show(page.unapproveTokenErr)
      return
    }

    const assetExplorer = CoinExplorers[this.selectedWalletID]
    if (assetExplorer && assetExplorer[net]) {
      page.unapproveTokenTxID.href = assetExplorer[net](res.txID)
    }
    page.unapproveTokenTxID.textContent = res.txID
    Doc.hide(page.unapproveTokenSubmissionElements, page.unapproveTokenErr)
    Doc.show(page.unapproveTokenTxMsg)
  }

  /*
   * showUnapproveTokenAllowanceForm displays the form for unapproving
   * a specific version of the currently selected token's swap contract.
   */
  async showUnapproveTokenAllowanceForm (version: number) {
    const page = this.page
    this.unapprovingTokenVersion = version
    Doc.show(page.unapproveTokenSubmissionElements)
    Doc.hide(page.unapproveTokenTxMsg, page.unapproveTokenErr)
    const asset = app().assets[this.selectedWalletID]
    if (!asset || !asset.token) return
    const parentAsset = app().assets[asset.token.parentID]
    if (!parentAsset) return
    Doc.empty(page.tokenAllowanceRemoveSymbol)
    page.tokenAllowanceRemoveSymbol.appendChild(Doc.symbolize(asset, true))
    page.tokenAllowanceRemoveVersion.textContent = version.toString()

    const path = '/api/approvetokenfee'
    const res = await postJSON(path, {
      assetID: this.selectedWalletID,
      version: version,
      approving: false
    })
    if (!app().checkResponse(res)) {
      page.unapproveTokenErr.textContent = res.msg
      Doc.show(page.unapproveTokenErr)
    } else {
      let feeText = `${Doc.formatCoinValue(res.txFee, parentAsset.unitInfo)} ${parentAsset.unitInfo.conventional.unit}`
      const rate = app().fiatRatesMap[parentAsset.id]
      if (rate) {
        feeText += ` (${Doc.formatFiatConversion(res.txFee, rate, parentAsset.unitInfo)} USD)`
      }
      page.unapprovalFeeEstimate.textContent = feeText
    }
    this.forms.show(page.unapproveTokenForm)
  }

  /*
   * showUnapproveTokenAllowanceTableForm displays a table showing each of the
   * versions of a token's swap contract that have been approved and allows the
   * user to unapprove any of them.
   */
  async showUnapproveTokenAllowanceTableForm () {
    const page = this.page
    const asset = app().assets[this.selectedWalletID]
    if (!asset || !asset.wallet || !asset.wallet.approved) return
    while (page.tokenVersionBody.firstChild) {
      page.tokenVersionBody.removeChild(page.tokenVersionBody.firstChild)
    }
    Doc.empty(page.tokenVersionTableAssetSymbol)
    page.tokenVersionTableAssetSymbol.appendChild(Doc.symbolize(asset, true))
    const versionToDEXes = this.assetVersionUsedByDEXes()

    let showTable = false
    for (let i = 0; i <= asset.wallet.version; i++) {
      const approvalStatus = asset.wallet.approved[i]
      if (approvalStatus === undefined || approvalStatus !== ApprovalStatus.Approved) {
        continue
      }
      showTable = true
      const row = page.tokenVersionRow.cloneNode(true) as PageElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.version.textContent = i.toString()
      if (versionToDEXes[i]) {
        tmpl.usedBy.textContent = versionToDEXes[i].join(', ')
      }
      const removeIcon = this.page.removeIconTmpl.cloneNode(true)
      Doc.bind(removeIcon, 'click', () => {
        this.showUnapproveTokenAllowanceForm(i)
      })
      tmpl.remove.appendChild(removeIcon)
      page.tokenVersionBody.appendChild(row)
    }
    Doc.setVis(showTable, page.tokenVersionTable)
    Doc.setVis(!showTable, page.tokenVersionNone)
    this.forms.show(page.unapproveTokenTableForm)
  }

  /*
   * updateWalletPeers retrieves the wallet peers and displays them in the
   * wallet peers table.
   */
  async updateWalletPeersTable () {
    const page = this.page

    Doc.hide(page.peerSpinner)

    const res = await postJSON('/api/getwalletpeers', {
      assetID: this.selectedWalletID
    })
    if (!app().checkResponse(res)) {
      page.managePeersErr.textContent = res.msg
      Doc.show(page.managePeersErr)
      return
    }

    while (page.peersTableBody.firstChild) {
      page.peersTableBody.removeChild(page.peersTableBody.firstChild)
    }

    const peers: WalletPeer[] = res.peers || []
    peers.sort((a: WalletPeer, b: WalletPeer): number => {
      return a.source - b.source
    })

    const defaultText = intl.prep(intl.ID_DEFAULT)
    const addedText = intl.prep(intl.ID_ADDED)
    const discoveredText = intl.prep(intl.ID_DISCOVERED)

    peers.forEach((peer: WalletPeer) => {
      const row = page.peerTableRow.cloneNode(true) as PageElement
      const tmpl = Doc.parseTemplate(row)

      tmpl.addr.textContent = peer.addr

      switch (peer.source) {
        case PeerSource.WalletDefault:
          tmpl.source.textContent = defaultText
          break
        case PeerSource.UserAdded:
          tmpl.source.textContent = addedText
          break
        case PeerSource.Discovered:
          tmpl.source.textContent = discoveredText
          break
      }

      let connectionIcon
      if (peer.connected) {
        connectionIcon = this.page.connectedIconTmpl.cloneNode(true)
      } else {
        connectionIcon = this.page.disconnectedIconTmpl.cloneNode(true)
      }
      tmpl.connected.appendChild(connectionIcon)

      if (peer.source === PeerSource.UserAdded) {
        const removeIcon = this.page.removeIconTmpl.cloneNode(true)
        Doc.bind(removeIcon, 'click', async () => {
          Doc.hide(page.managePeersErr)
          const res = await postJSON('/api/removewalletpeer', {
            assetID: this.selectedWalletID,
            addr: peer.addr
          })
          if (!app().checkResponse(res)) {
            page.managePeersErr.textContent = res.msg
            Doc.show(page.managePeersErr)
            return
          }
          this.spinUntilPeersUpdate()
        })
        tmpl.remove.appendChild(removeIcon)
      }

      page.peersTableBody.appendChild(row)
    })
  }

  // showManagePeersForm displays the manage peers form.
  async showManagePeersForm () {
    const page = this.page
    await this.updateWalletPeersTable()
    Doc.hide(page.managePeersErr)
    this.forms.show(page.managePeersForm)
  }

  // submitAddPeers sends a request for the wallet to connect to a new
  // peer.
  async submitAddPeer () {
    const page = this.page
    Doc.hide(page.managePeersErr)
    const res = await postJSON('/api/addwalletpeer', {
      assetID: this.selectedWalletID,
      addr: page.addPeerInput.value
    })
    if (!app().checkResponse(res)) {
      page.managePeersErr.textContent = res.msg
      Doc.show(page.managePeersErr)
      return
    }
    this.spinUntilPeersUpdate()
    page.addPeerInput.value = ''
  }

  /*
   * spinUntilPeersUpdate will show the spinner on the manage peers fork.
   * If it is still showing after 10 seconds, the peers table will be updated
   * instead of waiting for a notification.
   */
  async spinUntilPeersUpdate () {
    const page = this.page
    Doc.show(page.peerSpinner)
    setTimeout(() => {
      if (Doc.isDisplayed(page.peerSpinner)) {
        this.updateWalletPeersTable()
      }
    }, 10000)
  }

  /*
   * showToggleWalletStatus displays the toggleWalletStatusConfirm form with
   * relevant help message.
   */
  showToggleWalletStatus (disable: boolean) {
    const page = this.page
    Doc.hide(page.toggleWalletStatusErr, page.walletStatusDisable, page.disableWalletMsg, page.walletStatusEnable, page.enableWalletMsg)
    if (disable) Doc.show(page.walletStatusDisable, page.disableWalletMsg)
    else Doc.show(page.walletStatusEnable, page.enableWalletMsg)
    this.forms.show(page.toggleWalletStatusConfirm)
  }

  /*
   * toggleWalletStatus toggles a wallets status to either disabled or enabled.
   */
  async toggleWalletStatus () {
    const page = this.page
    Doc.hide(page.toggleWalletStatusErr)

    const asset = app().assets[this.selectedWalletID]
    const disable = !asset.wallet.disabled
    const url = '/api/togglewalletstatus'
    const req = {
      assetID: this.selectedWalletID,
      disable: disable
    }

    const fmtParams = { assetName: asset.name }
    const loaded = app().loading(page.toggleWalletStatusConfirm)
    const res = await postJSON(url, req)
    loaded()
    if (!app().checkResponse(res)) {
      if (res.code === Errors.activeOrdersErr) page.toggleWalletStatusErr.textContent = intl.prep(intl.ID_ACTIVE_ORDERS_ERR_MSG, fmtParams)
      else page.toggleWalletStatusErr.textContent = res.msg
      Doc.show(page.toggleWalletStatusErr)
      return
    }

    let successMsg = intl.prep(intl.ID_WALLET_DISABLED_MSG, fmtParams)
    if (!disable) successMsg = intl.prep(intl.ID_WALLET_ENABLED_MSG, fmtParams)
    this.assetUpdated(this.selectedWalletID, page.toggleWalletStatusConfirm, successMsg)
  }

  /*
   * showBox shows the box with a fade-in animation.
   */
  async showBox (box: HTMLElement, focuser?: PageElement) {
    box.style.opacity = '0'
    Doc.show(box)
    if (focuser) focuser.focus()
    await Doc.animate(animationLength, progress => {
      box.style.opacity = `${progress}`
    }, 'easeOut')
    box.style.opacity = '1'
    this.displayed = box
  }

  /* Show the new wallet form. */
  async showNewWallet (assetID: number) {
    const page = this.page
    const box = page.newWalletForm
    this.newWalletForm.setAsset(assetID)
    const defaultsLoaded = this.newWalletForm.loadDefaults()
    await this.forms.show(box)
    await defaultsLoaded
  }

  prepareTickerAssets () {
    const tickerList: TickerAsset[] = []
    const tickerMap: Record<string, TickerAsset> = {}
    for (const a of Object.values(app().user.assets)) {
      const normedTicker = normalizedTicker(a)
      let ta = tickerMap[normedTicker]
      if (ta) {
        ta.addNetworkAsset(a)
        continue
      }
      ta = new TickerAsset(a)
      tickerList.push(ta)
      tickerMap[normedTicker] = ta
    }
    this.tickerList = tickerList
    this.tickerMap = tickerMap
  }

  sortTickers () {
    const { page, tickerList, tickerButtons } = this
    tickerList.sort((a: TickerAsset, b: TickerAsset) => {
      if (a.hasWallets && !b.hasWallets) return -1
      if (!a.hasWallets && b.hasWallets) return 1
      if (!a.hasWallets && !b.hasWallets) return a.ticker === 'DCR' ? -1 : 1
      const [aTotal, bTotal] = [a.total, b.total]
      if (aTotal === 0 && bTotal === 0) return a.ticker.localeCompare(b.ticker)
      else if (aTotal === 0) return 1
      else if (aTotal === 0) return -1
      const [aFiat, bFiat] = [a.xcRate, b.xcRate]
      if (aFiat && !bFiat) return -1
      if (!aFiat && bFiat) return 1
      return bFiat * bTotal - aFiat * aTotal
    })
    Doc.empty(page.tickerBalsBox)
    for (const { ticker } of tickerList) page.tickerBalsBox.appendChild(tickerButtons[ticker])
  }

  refreshBalances () {
    const { balTracker, tickerList, tickerTemplates, updateTickerButtonTemplate } = this
    for (const ta of tickerList) {
      const { ticker, total, xcRate, cFactor } = ta
      balTracker[ticker] = total / cFactor * xcRate
      updateTickerButtonTemplate(ta, tickerTemplates[ticker])
    }
  }

  setTickerButtons () {
    const { page, tickerList } = this
    Doc.empty(page.assetSelect)
    page.assetSelect.appendChild(page.globalBalanceBox)
    page.assetSelect.appendChild(page.tickerBalsBox)
    Doc.empty(page.tickerBalsBox)
    for (const ta of tickerList) {
      const { ticker, logoSymbol } = ta
      const div = page.tickerBalTmpl.cloneNode(true) as PageElement
      this.tickerButtons[ticker] = div
      Doc.bind(div, 'click', () => this.setSelectedTicker(ticker))
      const tmpl = Doc.parseTemplate(div)
      this.tickerTemplates[ticker] = tmpl
      tmpl.logo.src = Doc.logoPath(logoSymbol)
      tmpl.ticker.textContent = ticker
    }
    this.sortTickers()
  }

  updateTickerButtonTemplate (ta: TickerAsset, tmpl: Record<string, PageElement>) {
    const { total, cFactor, hasWallets, xcRate } = ta
    Doc.setVis(hasWallets && xcRate, tmpl.fiatBox)
    if (hasWallets) {
      tmpl.bal.textContent = Doc.formatFourSigFigs(total / cFactor)
      tmpl.fiatBal.textContent = Doc.formatFourSigFigs(total / cFactor * xcRate, 2)
      tmpl.logo.classList.remove('greyscale', 'faded')
      tmpl.ticker.classList.remove('grey')
    } else {
      tmpl.logo.classList.add('greyscale', 'faded')
      tmpl.ticker.classList.add('grey')
    }
  }

  updateGlobalBalance () {
    const totalUSD = Object.values(this.balTracker).reduce((total, fiatBal) => total + fiatBal, 0)
    this.page.globalBalance.textContent = Doc.formatFourSigFigs(totalUSD, 2)
  }

  updateAssetBalance (assetID: number) {
    const ticker = normalizedTicker(app().assets[assetID])
    const ta = this.tickerMap[ticker]
    const { total, xcRate, cFactor } = ta
    this.balTracker[ticker] = total / cFactor * xcRate
    this.updateTickerButtonTemplate(ta, this.tickerTemplates[ticker])
    this.updateGlobalBalance()
  }

  async setSelectedTicker (ticker: string) {
    const ta = this.selectedTicker = this.tickerMap[ticker]
    this.selectedWalletID = ta.blockchainWallet()?.assetID ?? -1
    const { page } = this
    const { logoSymbol, name, isMultiNet, hasTokens } = ta
    Doc.setText(page.walletDetailsBox, '[data-ticker]', ticker)
    Doc.setText(page.secondColumn, '[data-ticker]', ticker)
    Doc.setText(page.walletDetailsBox, '[data-asset-name]', name)
    Doc.setSrc(page.walletDetailsBox, '[data-logo]', Doc.logoPath(logoSymbol))
    page.content.classList.toggle('multinet', isMultiNet)
    page.content.classList.toggle('token', hasTokens)
    for (const div of Array.from(page.docs.children) as PageElement[]) Doc.setVis(div.dataset.docTicker === ticker, div)
    this.updateDisplayedTicker()
    this.showAvailableMarkets()
    this.showPendingTxs()
    for (const p of [
      this.updateTicketBuyer(),
      this.updatePrivacy(),
      State.storeLocal(State.selectedAssetLK, ticker),
      this.showRecentActivity()
    ]) await p
  }

  updateDisplayedTicker () {
    const { page, selectedTicker: ta } = this
    const chainWallet = ta.blockchainWallet()
    Doc.setVis(chainWallet && !chainWallet.wallet, page.createWalletBox)
    Doc.setVis(ta.hasWallets, page.sendReceiveBox)
    const w = chainWallet?.wallet
    Doc.setVis(w, page.walletConfig)

    // Show bridge button if any network asset supports bridging
    const hasBridging = ta.networkAssets.some(na => this.hasBridgingSupport(na.assetID))
    Doc.setVis(hasBridging && ta.hasWallets, page.bridge)

    if (w) {
      Doc.show(page.walletConfig)
      page.blockchainClass.textContent = w.class
      const walletDef = app().walletDefinition(w.assetID, w.type)
      page.walletType.textContent = walletDef.tab
      this.updateSyncAndPeers()
    }

    this.updateDisplayedTickerBalance()
    this.updateFeeState()
  }

  updateDisplayedTickerBalance (): void {
    const { page, selectedTicker: ta, balanceDetails: { balance, fiatBalance, fiatBalanceBox } } = this
    const { ui, total, cFactor, xcRate } = ta
    balance.textContent = Doc.formatFourSigFigs(total / cFactor)
    Doc.setVis(xcRate, fiatBalanceBox)
    if (xcRate) fiatBalance.textContent = Doc.formatFourSigFigs(total / cFactor * xcRate, 2)
    const chainWallet = ta.blockchainWallet()
    // Only show balance breakdown if this is multi-chain or if this is unichain
    // and has a wallet
    const showBalanceBreakdown = Boolean(chainWallet?.wallet) || ta.isMultiNet
    Doc.setVis(showBalanceBreakdown, page.balanceBreakdownBox)
    Doc.setVis(total > 0, page.send)
    if (!showBalanceBreakdown) return

    Doc.empty(page.balanceBreakdown)
    for (const { assetID, chainName, chainLogo, bal: { available, locked, immature }, token } of ta.networkAssets) {
      const { wallet: w } = app().assets[assetID]
      const tr = Doc.clone(page.blockchainBalanceTmpl)
      page.balanceBreakdown.appendChild(tr)
      const tmpl = Doc.parseTemplate(tr)
      tmpl.chainLogo.src = chainLogo
      tmpl.chainName.textContent = chainName
      const usable = w || token?.parentMade
      if (usable) {
        if (immature > 0) Doc.formatCoinValue((immature), ui)
        if (locked > 0) Doc.formatCoinValue((locked), ui)
        tmpl.avail.textContent = Doc.formatCoinValue(available, ui)
        tmpl.allocation.textContent = String(total ? Math.round((available + locked + immature) / total * 100) : 0) + '%'
      }
      Doc.bind(tmpl.txsBttn, 'click', () => this.showTxHistory(assetID))
      Doc.bind(tmpl.createWalletBttn, 'click', () => this.showNewWallet(token?.parentID ?? assetID))

      Doc.setVis(usable, tmpl.txsBttn)
      Doc.setVis(!usable, tmpl.createWalletBttn)
    }

    // TODO: handle reserves deficit with a notification.
    // if (bal.reservesDeficit > 0) addPrimaryBalance(intl.prep(intl.ID_RESERVES_DEFICIT), bal.reservesDeficit, intl.prep(intl.ID_RESERVES_DEFICIT_MSG))

    // page.purchaserBal.textContent = Doc.formatFourSigFigs(bal.available / ui.conventional.conversionFactor)
    // app().bindTooltips(page.balanceDetailBox)
  }

  updateSyncAndPeers () {
    const { page, selectedWalletID: assetID } = this
    const w = app().walletMap[assetID]
    const { peerCount, syncProgress, syncStatus, encrypted, open: unlocked, running, disabled } = w

    Doc.hide(page.txSyncBox, page.txFindingAddrs, page.txProgress)
    if (running) {
      page.peerCount.textContent = String(peerCount)
      page.syncProgress.textContent = `${(syncProgress * 100).toFixed(1)}%`
      page.syncHeight.textContent = String(syncStatus.blocks)
      if (syncStatus.txs !== undefined) {
        Doc.show(page.txSyncBox)
        if (syncStatus.txs === 0 && syncStatus.blocks >= syncStatus.targetHeight) Doc.show(page.txFindingAddrs)
        else {
          Doc.show(page.txProgress)
          const prog = syncStatus.txs / syncStatus.targetHeight
          page.txProgress.textContent = `${(prog * 100).toFixed(1)}%`
        }
      }
    } else {
      page.peerCount.textContent = '—'
      page.syncProgress.textContent = '—'
      page.syncHeight.textContent = '—'
    }

    Doc.hide(
      page.statusReady, page.statusLocked, page.statusOff, page.statusDisabled,
      page.statusSyncing, page.connectBttn, page.lockBttn, page.unlockBttn
    )

    if (disabled) return Doc.show(page.statusDisabled)
    if (!running) return Doc.show(page.connectBttn, page.statusLocked)
    const syncing = syncProgress < 1 || syncStatus.txs !== undefined
    if (syncing) return Doc.show(page.statusSyncing)
    Doc.show(page.statusReady)
    const hasActiveOrders = app().haveActiveOrders(assetID)
    const lockable = unlocked && encrypted && !hasActiveOrders
    const unlockable = encrypted && !unlocked
    Doc.setVis(unlockable, page.unlockBttn)
    Doc.setVis(lockable, page.lockBttn)
    if (unlockable) Doc.show(page.unlockBttn)
    else if (lockable) Doc.show(page.lockBttn)
  }

  updateFeeState () {
    const { page, selectedTicker: ta } = this
    const { ui, xcRate, networkAssets } = ta

    page.feeStateXcRate.textContent = Doc.formatFourSigFigs(xcRate)

    const formatUSD = (el: PageElement, v: number, feeUI: UnitInfo, feeFiatRate: number) => {
      const tmpl = Doc.parseTemplate(el)
      const fv = v / feeUI.conventional.conversionFactor * feeFiatRate
      Doc.setVis(fv <= 0.001, tmpl.lessThan)
      tmpl.value.textContent = Doc.formatFourSigFigs(Math.max(fv, 0.001), fv >= 0.1 ? 2 : 3)
    }

    const feeAssetStuff = (ca: NetworkAsset): [number, UnitInfo, number] => {
      const { assetID, token } = ca
      const feeAssetID = token ? token.parentID : assetID
      const feeUI = token?.feeUI ?? ui
      const feeFiatRate = app().fiatRatesMap[feeAssetID]
      return [feeAssetID, feeUI, feeFiatRate]
    }

    Doc.setVis(ta.hasWallets, page.txFeesBox)
    if (ta.isMultiNet) {
      Doc.empty(page.netTxFees)
      for (const ca of networkAssets) {
        const { assetID, chainName, chainLogo } = ca
        const [feeAssetID, feeUI, feeFiatRate] = feeAssetStuff(ca)
        const tr = Doc.clone(page.netTxFeeTmpl)
        page.netTxFees.appendChild(tr)
        const tmpl = Doc.parseTemplate(tr)
        tmpl.chainLogo.src = chainLogo
        tmpl.chainName.textContent = chainName
        const w = app().walletMap[assetID]
        if (!w?.feeState) continue
        // remove dummies
        for (const dummy of Array.from(tr.children).slice(1)) tr.removeChild(dummy)
        const { send, swap, redeem, rate } = w.feeState

        const addTD = (v: number) => {
          const td = Doc.clone(page.multiNetTxFeeTmpl)
          tr.appendChild(td)
          const tdTmpl = Doc.parseTemplate(td)
          Doc.formatBestValueElement(tdTmpl.chainUnits, feeAssetID, v, feeUI)
          formatUSD(tdTmpl.fiatUnits, v, feeUI, feeFiatRate)
        }

        addTD(send)
        addTD(redeem) // buy
        addTD(swap) // sell
        // Rate
        const td = Doc.clone(page.multiNetFeeRateTmpl)
        tr.appendChild(td)
        Doc.formatBestRateElement(td, feeAssetID, rate, feeUI)
      }
      app().bindUnits(page.netTxFees)
    } else {
      const [feeAssetID, feeUI, feeFiatRate] = feeAssetStuff(networkAssets[0])
      const w = app().walletMap[feeAssetID]
      if (!w?.feeState) return
      const { rate, send, swap, redeem } = w.feeState
      Doc.formatBestRateElement(page.networkFeeRate, feeAssetID, rate, feeUI)
      Doc.formatBestValueElement(page.feeStateSendFees, feeAssetID, send, feeUI)
      Doc.formatBestValueElement(page.feeStateSellFees, feeAssetID, swap, feeUI)
      Doc.formatBestValueElement(page.feeStateBuyFees, feeAssetID, redeem, feeUI)
      formatUSD(page.feeStateSendFiat, send, feeUI, feeFiatRate)
      formatUSD(page.feeStateSellFiat, swap, feeUI, feeFiatRate)
      formatUSD(page.feeStateBuyFiat, redeem, feeUI, feeFiatRate)
    }
  }

  async updateTicketBuyer () {
    const { page, selectedWalletID: assetID } = this
    if (assetID === -1) return Doc.hide(page.stakingBox)
    const { wallet, unitInfo: ui } = app().assets[assetID]
    Doc.hide(
      page.pickVSP, page.stakingSummary, page.stakingErr,
      page.vspDisplayBox, page.ticketPriceBox, page.purchaseTicketsBox,
      page.stakingRpcSpvMsg, page.ticketsDisabled
    )
    const showStakingBox = wallet?.running && Boolean(wallet.traits & traitTicketBuyer)
    Doc.setVis(showStakingBox, page.stakingBox)
    if (!showStakingBox) return
    this.ticketPage = {
      number: 0,
      history: [],
      scanned: false
    }
    const loaded = app().loading(page.stakingBox)
    const res = await this.safePost('/api/stakestatus', assetID)
    loaded()
    if (!app().checkResponse(res)) {
      // Look for common error for RPC + SPV wallet.
      if (res.msg.includes('disconnected from consensus RPC')) {
        Doc.show(page.stakingRpcSpvMsg)
        return
      }
      Doc.show(page.stakingErr)
      page.stakingErr.textContent = res.msg
      return
    }
    Doc.show(page.stakingSummary, page.ticketPriceBox)
    const stakeStatus = res.status as TicketStakingStatus
    this.stakeStatus = stakeStatus
    page.stakingAgendaCount.textContent = String(stakeStatus.stances.agendas.length)
    page.stakingTspendCount.textContent = String(stakeStatus.stances.tspends.length)
    page.purchaserCurrentPrice.textContent = Doc.formatFourSigFigs(stakeStatus.ticketPrice / ui.conventional.conversionFactor)
    page.purchaserBal.textContent = Doc.formatCoinValue(wallet.balance.available, ui)
    this.updateTicketStats(stakeStatus.stats, ui, stakeStatus.ticketPrice, stakeStatus.votingSubsidy)
    // If this is an extension wallet, we'll might to disable all controls.
    const disableStaking = app().extensionWallet(this.selectedWalletID)?.disableStaking
    if (disableStaking) {
      Doc.hide(page.setVotes, page.showVSPs)
      Doc.show(page.ticketsDisabled)
      page.extensionModeAppName.textContent = app().user.extensionModeConfig.name
      return
    }

    this.setVSPViz(stakeStatus.vsp)
  }

  setVSPViz (vsp: string) {
    const { page, stakeStatus } = this
    Doc.hide(page.vspDisplayBox)
    if (vsp) {
      Doc.show(page.vspDisplayBox, page.purchaseTicketsBox)
      Doc.hide(page.pickVSP)
      page.vspURL.textContent = vsp
      return
    }
    Doc.setVis(!stakeStatus.isRPC, page.pickVSP)
    Doc.setVis(stakeStatus.isRPC, page.purchaseTicketsBox)
  }

  updateTicketStats (stats: TicketStats, ui: UnitInfo, ticketPrice?: number, votingSubsidy?: number) {
    const { page, stakeStatus } = this
    stakeStatus.stats = stats
    if (ticketPrice) stakeStatus.ticketPrice = ticketPrice
    if (votingSubsidy) stakeStatus.votingSubsidy = votingSubsidy
    const liveTicketCount = stakeStatus.tickets.filter((tkt: Ticket) => tkt.status <= ticketStatusLive && tkt.status >= ticketStatusUnmined).length
    page.stakingTicketCount.textContent = String(liveTicketCount)
    page.immatureTicketCount.textContent = String(stats.mempool)
    Doc.setVis(stats.mempool > 0, page.immatureTicketCountBox)
    page.queuedTicketCount.textContent = String(stats.queued)
    page.formQueuedTix.textContent = String(stats.queued)
    Doc.setVis(stats.queued > 0, page.formQueueTixBox, page.queuedTicketCountBox)
    page.totalTicketCount.textContent = String(stats.ticketCount)
    page.totalTicketRewards.textContent = Doc.formatFourSigFigs(stats.totalRewards / ui.conventional.conversionFactor)
    page.totalTicketVotes.textContent = String(stats.votes)
    if (ticketPrice) page.ticketPrice.textContent = Doc.formatFourSigFigs(ticketPrice / ui.conventional.conversionFactor)
    if (votingSubsidy) page.votingSubsidy.textContent = Doc.formatFourSigFigs(votingSubsidy / ui.conventional.conversionFactor)
  }

  async showVSPPicker () {
    const assetID = this.selectedWalletID
    const page = this.page
    this.forms.show(page.vspPicker)
    Doc.empty(page.vspPickerList)
    Doc.hide(page.stakingErr)
    const loaded = app().loading(page.vspPicker)
    const res = await this.safePost('/api/listvsps', assetID)
    loaded()
    if (!app().checkResponse(res)) {
      Doc.show(page.stakingErr)
      page.stakingErr.textContent = res.msg
      return
    }
    const vsps = res.vsps as VotingServiceProvider[]
    for (const vsp of vsps) {
      const row = page.vspRowTmpl.cloneNode(true) as PageElement
      page.vspPickerList.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.url.textContent = vsp.url
      tmpl.feeRate.textContent = vsp.feePercentage.toFixed(2)
      tmpl.voting.textContent = String(vsp.voting)
      Doc.bind(row, 'click', () => {
        Doc.hide(page.stakingErr)
        this.setVSP(assetID, vsp)
      })
    }
  }

  showPurchaseTicketsDialog () {
    const page = this.page
    page.purchaserInput.value = ''
    Doc.hide(page.purchaserErr)
    this.forms.show(this.page.purchaseTicketsForm)
    page.purchaserInput.focus()
  }

  purchaserInputChanged () {
    const page = this.page
    const n = parseInt(page.purchaserInput.value || '0')
    if (n <= 1) {
      page.purchaserInput.value = '1'
      return
    }
    page.purchaserInput.value = String(n)
  }

  async purchaseTickets () {
    const { page, selectedWalletID: assetID } = this
    // DRAFT NOTE: The user will get an actual ticket count somewhere in the
    // range 1 <= tickets_purchased <= n. See notes in
    // (*spvWallet).PurchaseTickets.
    // How do we handle this at the UI. Or do we handle it all in the backend
    // somehow?
    const n = parseInt(page.purchaserInput.value || '0')
    if (n < 1) return
    // TODO: Add confirmation dialog.
    const loaded = app().loading(page.purchaseTicketsForm)
    const res = await this.safePost('/api/purchasetickets', { assetID, n })
    loaded()
    if (!app().checkResponse(res)) {
      page.purchaserErr.textContent = res.msg
      Doc.show(page.purchaserErr)
      return
    }
    this.forms.showSuccess(intl.prep(intl.ID_TICKETS_PURCHASED, { n: n.toLocaleString(Doc.languages()) }))
  }

  processTicketPurchaseUpdate (walletNote: CustomWalletNote) {
    const { stakeStatus, selectedWalletID, page } = this
    const { assetID } = walletNote
    const { err, remaining, tickets, stats } = walletNote.payload as TicketPurchaseUpdate
    if (assetID !== selectedWalletID) return
    if (err) {
      Doc.show(page.purchaseTicketsErrBox)
      page.purchaseTicketsErr.textContent = err
      return
    }
    if (tickets) stakeStatus.tickets = tickets.concat(stakeStatus.tickets)
    if (stats) this.updateTicketStats(stats, app().assets[assetID].unitInfo)
    stakeStatus.stats.queued = remaining
    page.queuedTicketCount.textContent = String(remaining)
    page.formQueuedTix.textContent = String(remaining)
    Doc.setVis(remaining > 0, page.queuedTicketCountBox)
  }

  async setVSP (assetID: number, vsp: VotingServiceProvider) {
    this.forms.close()
    const page = this.page
    const loaded = app().loading(page.stakingBox)
    const res = await this.safePost('/api/setvsp', { assetID, url: vsp.url })
    loaded()
    if (!app().checkResponse(res)) {
      Doc.show(page.stakingErr)
      page.stakingErr.textContent = res.msg
      return
    }
    this.setVSPViz(vsp.url)
  }

  setCustomVSP () {
    const assetID = this.selectedWalletID
    const vsp = { url: this.page.customVspUrl.value } as VotingServiceProvider
    this.setVSP(assetID, vsp)
  }

  pageOfTickets (pgNum: number) {
    const { stakeStatus, ticketPage } = this
    let startOffset = pgNum * ticketPageSize
    const pageOfTickets: Ticket[] = []
    if (startOffset < stakeStatus.tickets.length) {
      pageOfTickets.push(...stakeStatus.tickets.slice(startOffset, startOffset + ticketPageSize))
      if (pageOfTickets.length < ticketPageSize) {
        const need = ticketPageSize - pageOfTickets.length
        pageOfTickets.push(...ticketPage.history.slice(0, need))
      }
    } else {
      startOffset -= stakeStatus.tickets.length
      pageOfTickets.push(...ticketPage.history.slice(startOffset, startOffset + ticketPageSize))
    }
    return pageOfTickets
  }

  displayTicketPage (pageNumber: number, pageOfTickets: Ticket[]) {
    const { page, selectedWalletID: assetID } = this
    const ui = app().unitInfo(assetID)
    const coinLink = CoinExplorers[assetID][net]
    Doc.empty(page.ticketHistoryRows)
    page.ticketHistoryPage.textContent = String(pageNumber)
    for (const { tx, status } of pageOfTickets) {
      const tr = page.ticketHistoryRowTmpl.cloneNode(true) as PageElement
      page.ticketHistoryRows.appendChild(tr)
      app().bindUrlHandlers(tr)
      const tmpl = Doc.parseTemplate(tr)
      tmpl.age.textContent = Doc.timeSince(tx.stamp * 1000)
      tmpl.price.textContent = Doc.formatFullPrecision(tx.ticketPrice, ui)
      tmpl.status.textContent = intl.prep(ticketStatusTranslationKeys[status])
      tmpl.hashStart.textContent = tx.hash.slice(0, 6)
      tmpl.hashEnd.textContent = tx.hash.slice(-6)
      tmpl.detailsLinkUrl.setAttribute('href', coinLink(tx.hash))
    }
  }

  async ticketPageN (pageNumber: number) {
    const { page, stakeStatus, ticketPage, selectedWalletID: assetID } = this
    const pageOfTickets = this.pageOfTickets(pageNumber)
    if (pageOfTickets.length < ticketPageSize && !ticketPage.scanned) {
      const n = ticketPageSize - pageOfTickets.length
      const lastList = ticketPage.history.length > 0 ? ticketPage.history : stakeStatus.tickets
      const scanStart = lastList.length > 0 ? lastList[lastList.length - 1].tx.blockHeight : scanStartMempool
      const skipN = lastList.filter((tkt: Ticket) => tkt.tx.blockHeight === scanStart).length
      const loaded = app().loading(page.ticketHistoryForm)
      const res = await this.safePost('/api/ticketpage', { assetID, scanStart, n, skipN })
      loaded()
      if (!app().checkResponse(res)) {
        console.error('error fetching ticket page', res.msg)
        return
      }
      this.ticketPage.history.push(...res.tickets)
      pageOfTickets.push(...res.tickets)
      if (res.tickets.length < n) this.ticketPage.scanned = true
    }

    const totalTix = stakeStatus.tickets.length + ticketPage.history.length
    Doc.setVis(totalTix >= ticketPageSize, page.ticketHistoryPagination)
    Doc.setVis(totalTix > 0, page.ticketHistoryTable)
    Doc.setVis(totalTix === 0, page.noTicketsMessage)
    if (pageOfTickets.length === 0) {
      // Probably ended with a page of size ticketPageSize, so didn't know we
      // had hit the end until the user clicked the arrow and we went looking
      // for the next. Would be good to figure out a way to hide the arrow in
      // that case.
      Doc.hide(page.ticketHistoryNextPage)
      return
    }
    this.displayTicketPage(pageNumber, pageOfTickets)
    ticketPage.number = pageNumber
    const atEnd = pageNumber * ticketPageSize + pageOfTickets.length === totalTix
    Doc.setVis(!atEnd || !ticketPage.scanned, page.ticketHistoryNextPage)
    Doc.setVis(pageNumber > 0, page.ticketHistoryPrevPage)
  }

  async showTicketHistory () {
    this.forms.show(this.page.ticketHistoryForm)
    await this.ticketPageN(this.ticketPage.number)
  }

  async nextTicketPage () {
    await this.ticketPageN(this.ticketPage.number + 1)
  }

  async prevTicketPage () {
    await this.ticketPageN(this.ticketPage.number - 1)
  }

  showSetVotesDialog () {
    const { page, stakeStatus, selectedWalletID: assetID } = this
    const ui = app().unitInfo(assetID)
    Doc.hide(page.votingFormErr)
    const coinLink = CoinExplorers[assetID][app().user.net]
    const upperCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

    const setVotes = async (req: any) => {
      Doc.hide(page.votingFormErr)
      const loaded = app().loading(page.votingForm)
      const res = await this.safePost('/api/setvotes', req)
      loaded()
      if (!app().checkResponse(res)) {
        Doc.show(page.votingFormErr)
        page.votingFormErr.textContent = res.msg
        throw Error(res.msg)
      }
    }

    const setAgendaChoice = async (agendaID: string, choiceID: string) => {
      await setVotes({ assetID, choices: { [agendaID]: choiceID } })
      for (const agenda of stakeStatus.stances.agendas) if (agenda.id === agendaID) agenda.currentChoice = choiceID
    }

    Doc.empty(page.votingAgendas)
    for (const agenda of stakeStatus.stances.agendas) {
      const div = page.votingAgendaTmpl.cloneNode(true) as PageElement
      page.votingAgendas.appendChild(div)
      const tmpl = Doc.parseTemplate(div)
      tmpl.description.textContent = agenda.description
      for (const choice of agenda.choices) {
        const div = page.votingChoiceTmpl.cloneNode(true) as PageElement
        tmpl.choices.appendChild(div)
        const choiceTmpl = Doc.parseTemplate(div)
        choiceTmpl.id.textContent = upperCase(choice.id)
        choiceTmpl.id.dataset.tooltip = choice.description
        choiceTmpl.radio.value = choice.id
        choiceTmpl.radio.name = agenda.id
        Doc.bind(choiceTmpl.radio, 'change', () => {
          if (!choiceTmpl.radio.checked) return
          setAgendaChoice(agenda.id, choice.id)
        })
        if (choice.id === agenda.currentChoice) choiceTmpl.radio.checked = true
      }
      app().bindTooltips(tmpl.choices)
    }

    const setTspendVote = async (txHash: string, policyID: string) => {
      await setVotes({ assetID, tSpendPolicy: { [txHash]: policyID } })
      for (const tspend of stakeStatus.stances.tspends) if (tspend.hash === txHash) tspend.currentPolicy = policyID
    }

    Doc.empty(page.votingTspends)
    for (const tspend of stakeStatus.stances.tspends) {
      const div = page.tspendTmpl.cloneNode(true) as PageElement
      page.votingTspends.appendChild(div)
      app().bindUrlHandlers(div)
      const tmpl = Doc.parseTemplate(div)
      for (const opt of [tmpl.yes, tmpl.no]) {
        opt.name = tspend.hash
        if (tspend.currentPolicy === opt.value) opt.checked = true
        Doc.bind(opt, 'change', () => {
          if (!opt.checked) return
          setTspendVote(tspend.hash, opt.value ?? '')
        })
      }
      if (tspend.value > 0) tmpl.value.textContent = Doc.formatFourSigFigs(tspend.value / ui.conventional.conversionFactor)
      else Doc.hide(tmpl.value)
      tmpl.hash.textContent = tspend.hash
      tmpl.explorerLink.setAttribute('href', coinLink(tspend.hash))
    }

    const setTKeyPolicy = async (key: string, policy: string) => {
      await setVotes({ assetID, treasuryPolicy: { [key]: policy } })
      for (const tkey of stakeStatus.stances.treasuryKeys) if (tkey.key === key) tkey.policy = policy
    }

    Doc.empty(page.votingTKeys)
    for (const keyPolicy of (stakeStatus.stances.treasuryKeys ?? [])) {
      const div = page.tkeyTmpl.cloneNode(true) as PageElement
      page.votingTKeys.appendChild(div)
      const tmpl = Doc.parseTemplate(div)
      for (const opt of [tmpl.yes, tmpl.no]) {
        opt.name = keyPolicy.key
        if (keyPolicy.policy === opt.value) opt.checked = true
        Doc.bind(opt, 'change', () => {
          if (!opt.checked) return
          setTKeyPolicy(keyPolicy.key, opt.value ?? '')
        })
      }
      tmpl.key.textContent = keyPolicy.key
    }

    this.forms.show(page.votingForm)
  }

  async updatePrivacy () {
    const { page, selectedWalletID: assetID } = this
    this.mixing = false
    if (assetID === -1) return Doc.hide(page.mixingBox)
    const disablePrivacy = app().extensionWallet(assetID)?.disablePrivacy
    const { wallet: w } = app().assets[assetID]
    const showMixingBox = !disablePrivacy && w?.running && Boolean(w.traits & traitFundsMixer)
    Doc.setVis(showMixingBox, page.mixingBox)
    if (!showMixingBox) return
    Doc.hide(page.mixerOff, page.mixerOn)
    // TODO: Show special messaging if the asset supports mixing but not this
    // wallet type.
    Doc.show(page.mixerLoading)
    const res = await this.safePost('/api/mixingstats', { assetID })
    Doc.hide(page.mixerLoading)
    if (!app().checkResponse(res)) {
      Doc.show(page.mixingErr)
      page.mixingErr.textContent = res.msg
      return
    }

    this.mixing = res.stats.enabled as boolean
    if (this.mixing) Doc.show(page.mixerOn)
    else Doc.show(page.mixerOff)
    this.mixerToggle.setState(this.mixing)
  }

  async updateMixerState (on: boolean) {
    const page = this.page
    Doc.hide(page.mixingErr)
    const loaded = app().loading(page.mixingBox)
    const res = await postJSON('/api/configuremixer', { assetID: this.selectedWalletID, enabled: on })
    loaded()
    if (!app().checkResponse(res)) {
      page.mixingErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: res.msg })
      Doc.show(page.mixingErr)
      return
    }
    Doc.setVis(on, page.mixerOn)
    Doc.setVis(!on, page.mixerOff)
    this.mixerToggle.setState(on)
  }

  showAvailableMarkets () {
    const { page, selectedTicker: { networkAssetLookup } } = this
    const exchanges = app().user.exchanges
    const markets: [string, Exchange, Market, NetworkAsset][] = []
    for (const xc of Object.values(exchanges)) {
      for (const mkt of Object.values(xc.markets ?? [])) {
        if (networkAssetLookup[mkt.baseid]) markets.push([xc.host, xc, mkt, networkAssetLookup[mkt.baseid]])
        else if (networkAssetLookup[mkt.quoteid]) markets.push([xc.host, xc, mkt, networkAssetLookup[mkt.quoteid]])
      }
    }

    const spotVolume = (assetID: number, mkt: Market): number => {
      const spot = mkt.spot
      if (!spot) return 0
      const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
      const volume = assetID === mkt.baseid ? spot.vol24 : spot.vol24 * spot.rate / OrderUtil.RateEncodingFactor
      return volume / conversionFactor
    }

    markets.sort((a: [string, Exchange, Market, NetworkAsset], b: [string, Exchange, Market, NetworkAsset]): number => {
      const [hostA, , mktA, caA] = a
      const [hostB, , mktB, caB] = b
      if (!mktA.spot && !mktB.spot) return hostA.localeCompare(hostB)
      return spotVolume(caA.assetID, mktB) - spotVolume(caB.assetID, mktA)
    })
    Doc.empty(page.availableMarkets)

    for (const [host, xc, mkt, ca] of markets) {
      const { spot, baseid, basesymbol, quoteid, quotesymbol } = mkt
      const row = page.marketRow.cloneNode(true) as PageElement
      page.availableMarkets.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.host.textContent = host
      tmpl.baseLogo.src = Doc.logoPath(basesymbol)
      tmpl.quoteLogo.src = Doc.logoPath(quotesymbol)
      Doc.empty(tmpl.baseSymbol, tmpl.quoteSymbol)
      tmpl.baseSymbol.appendChild(Doc.symbolize(xc.assets[baseid], true))
      tmpl.quoteSymbol.appendChild(Doc.symbolize(xc.assets[quoteid], true))

      if (spot) {
        const convRate = app().conventionalRate(baseid, quoteid, spot.rate, exchanges[host])
        tmpl.price.textContent = Doc.formatFourSigFigs(convRate)
        const fmtSymbol = (s: string) => s.split('.')[0].toUpperCase()
        tmpl.priceQuoteUnit.textContent = fmtSymbol(quotesymbol)
        tmpl.priceBaseUnit.textContent = fmtSymbol(basesymbol)
        tmpl.volume.textContent = Doc.formatFourSigFigs(spotVolume(ca.assetID, mkt))
        tmpl.volumeUnit.textContent = ca.assetID === baseid ? fmtSymbol(basesymbol) : fmtSymbol(quotesymbol)
      } else Doc.hide(tmpl.priceBox, tmpl.volumeBox)
      Doc.bind(row, 'click', () => app().loadPage('markets', { host, baseID: baseid, quoteID: quoteid }))
    }
  }

  showPendingTxs () {
    const { page, selectedTicker: { networkAssets } } = this
    const pendingTxs: PendingTx[] = []
    this.pendingTxs = {}
    for (const na of networkAssets) {
      const w = app().walletMap[na.assetID]
      if (!w) continue
      for (const tx of Object.values(w.pendingTxs)) {
        if (tx.timestamp === 0) continue
        const pt = { ...tx, networkAsset: na } as PendingTx
        pendingTxs.push(pt)
        this.pendingTxs[tx.id] = pt
      }
    }
    pendingTxs.sort((a: WalletTransaction, b: WalletTransaction) => a.timestamp < b.timestamp ? 1 : -1)
    Doc.empty(page.pendingTxs)
    for (const pt of pendingTxs) {
      this.initializePendingTx(pt)
      page.pendingTxs.appendChild(pt.tr)
      updatePendingTx(pt)
    }
    this.setPendingTxVisibility()
  }

  initializePendingTx (pt: PendingTx) {
    pt.tr = Doc.clone(this.page.pendingTxTmpl)
    const tmpl = pt.tmpl = Doc.parseTemplate(pt.tr)
    const { type: txType, id: txID, networkAsset: { assetID, chainLogo, chainName } } = pt
    tmpl.chainLogo.src = chainLogo
    tmpl.chainName.textContent = chainName
    tmpl.type.textContent = txTypeString(txType)
    tmpl.id.textContent = trimStringWithEllipsis(txID, 12)
    const coinEx = CoinExplorers[assetID]
    if (coinEx == null) {
      return
    }
    const coinLink = coinEx[net]
    if (coinLink == null) {
      return
    }
    tmpl.id.href = coinLink(txID)
  }

  async showRecentActivity () {
    const { page, selectedTicker: ta } = this
    const loaded = app().loading(page.orderActivityBox)
    const filter: OrderFilter = {
      n: 20,
      assets: ta.networkAssets.map((ca: NetworkAsset) => ca.assetID),
      hosts: [],
      statuses: []
    }
    const res = await postJSON('/api/orders', filter)
    loaded()
    page.orderActivityBox.classList.remove('invisible')
    Doc.hide(page.noActivity, page.orderActivity)
    if (!res.orders || res.orders.length === 0) {
      Doc.show(page.noActivity)
      return
    }
    Doc.show(page.orderActivity)
    Doc.empty(page.recentOrders)
    for (const ord of (res.orders as Order[])) {
      const row = page.recentOrderTmpl.cloneNode(true) as PageElement
      page.recentOrders.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      let from: SupportedAsset, to: SupportedAsset
      const [baseUnitInfo, quoteUnitInfo] = [app().unitInfo(ord.baseID), app().unitInfo(ord.quoteID)]
      if (ord.sell) {
        [from, to] = [app().assets[ord.baseID], app().assets[ord.quoteID]]
        tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        if (ord.type === OrderUtil.Limit) {
          tmpl.toQty.textContent = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
        }
      } else {
        [from, to] = [app().assets[ord.quoteID], app().assets[ord.baseID]]
        if (ord.type === OrderUtil.Market) {
          tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        } else {
          tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
          tmpl.toQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        }
      }

      tmpl.fromLogo.src = Doc.logoPath(from.symbol)
      Doc.empty(tmpl.fromSymbol, tmpl.toSymbol)
      tmpl.fromSymbol.appendChild(Doc.symbolize(from, true))
      tmpl.toLogo.src = Doc.logoPath(to.symbol)
      tmpl.toSymbol.appendChild(Doc.symbolize(to, true))
      tmpl.status.textContent = OrderUtil.statusString(ord)
      tmpl.filled.textContent = `${(OrderUtil.filled(ord) / ord.qty * 100).toFixed(1)}%`
      tmpl.age.textContent = Doc.timeSince(ord.submitTime)
      tmpl.link.href = `order/${ord.id}`
      app().bindInternalNavigation(row)
    }
  }

  updateTxHistoryRow (row: PageElement, tx: WalletTransaction, assetID: number) {
    const tmpl = Doc.parseTemplate(row)
    let amtAssetID = assetID
    let feesAssetID = assetID
    if (tx.tokenID) {
      amtAssetID = tx.tokenID
      if (assetID !== tx.tokenID) feesAssetID = assetID
      else {
        const asset = app().assets[assetID]
        if (asset.token) feesAssetID = asset.token.parentID
        else console.error(`unable to determine fee asset for tx ${tx.id}`)
      }
    }
    const amtAssetUI = app().unitInfo(amtAssetID)
    const feesAssetUI = app().unitInfo(feesAssetID)
    tmpl.age.textContent = Doc.timeSince(tx.timestamp * 1000)
    tmpl.age.dataset.timestamp = String(tx.timestamp * 1000)
    Doc.setVis(tx.timestamp === 0, tmpl.pending)
    Doc.setVis(tx.timestamp !== 0, tmpl.age)
    let txType = txTypeString(tx.type)
    if (tx.tokenID && tx.tokenID !== assetID) {
      const tokenAsset = app().assets[tx.tokenID]
      const tokenSymbol = tokenAsset.unitInfo.conventional.unit
      txType = `${tokenSymbol} ${txType}`
    }
    tmpl.type.textContent = txType
    tmpl.id.textContent = trimStringWithEllipsis(tx.id, 12)
    tmpl.id.setAttribute('title', tx.id)
    tmpl.fees.textContent = Doc.formatCoinValue(tx.fees, feesAssetUI)
    if (noAmtTxTypes.includes(tx.type)) {
      tmpl.amount.textContent = '-'
    } else {
      const [u, c] = txTypeSignAndClass(tx.type)
      const amt = Doc.formatCoinValue(tx.amount, amtAssetUI)
      tmpl.amount.textContent = `${u}${amt}`
      if (c !== '') tmpl.amount.classList.add(c)
    }
  }

  txHistoryRow (tx: WalletTransaction, assetID: number): PageElement {
    const row = this.page.txHistoryRowTmpl.cloneNode(true) as PageElement
    row.dataset.txid = tx.id
    Doc.bind(row, 'click', () => this.showTxDetailsPopup(tx))
    this.updateTxHistoryRow(row, tx, assetID)
    return row
  }

  txHistoryDateRow (date: string): PageElement {
    const row = this.page.txHistoryDateRowTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(row)
    tmpl.date.textContent = date
    return row
  }

  setTxDetailsPopupElements (tx: WalletTransaction) {
    const page = this.page

    // Block explorer
    const assetExplorer = CoinExplorers[this.selectedWalletID]
    if (assetExplorer && assetExplorer[net]) {
      page.txViewBlockExplorer.href = assetExplorer[net](tx.id)
    }

    // Tx type
    let txType = txTypeString(tx.type)
    if (tx.tokenID && tx.tokenID !== this.selectedWalletID) {
      const tokenSymbol = app().assets[tx.tokenID].symbol.split('.')[0].toUpperCase()
      txType = `${tokenSymbol} ${txType}`
    }
    page.txDetailsType.textContent = txType
    Doc.setVis(tx.type === txTypeSwapOrSend, page.txTypeTooltip)
    page.txTypeTooltip.dataset.tooltip = intl.prep(intl.ID_SWAP_OR_SEND_TOOLTIP)

    // Amount
    if (noAmtTxTypes.includes(tx.type)) {
      Doc.hide(page.txDetailsAmtSection)
    } else {
      let assetID = this.selectedWalletID
      if (tx.tokenID) assetID = tx.tokenID
      Doc.show(page.txDetailsAmtSection)
      const ui = app().unitInfo(assetID)
      const amt = Doc.formatCoinValue(tx.amount, ui)
      const [s, c] = txTypeSignAndClass(tx.type)
      page.txDetailsAmount.textContent = `${s}${amt} ${ui.conventional.unit}`
      if (c !== '') page.txDetailsAmount.classList.add(c)
    }

    // Fee
    let feeAsset = this.selectedWalletID
    if (tx.tokenID !== undefined) {
      const asset = app().assets[tx.tokenID]
      if (asset.token) {
        feeAsset = asset.token.parentID
      } else {
        console.error(`wallet transaction ${tx.id} is supposed to be a token tx, but asset ${tx.tokenID} is not a token`)
      }
    }
    const feeUI = app().unitInfo(feeAsset)
    const fee = Doc.formatCoinValue(tx.fees, feeUI)
    page.txDetailsFee.textContent = `${fee} ${feeUI.conventional.unit}`

    // Time / block number
    page.txDetailsBlockNumber.textContent = `${tx.blockNumber}`
    const date = new Date(tx.timestamp * 1000)
    const dateStr = date.toLocaleDateString()
    const timeStr = date.toLocaleTimeString()
    page.txDetailsTimestamp.textContent = `${dateStr} ${timeStr}`
    Doc.setVis(tx.blockNumber === 0, page.timestampPending, page.blockNumberPending)
    Doc.setVis(tx.blockNumber !== 0, page.txDetailsBlockNumber, page.txDetailsTimestamp)

    // Tx ID / User Op ID
    Doc.setVis(tx.isUserOp, page.txDetailsUserOpIDSection)
    if (tx.isUserOp) {
      page.txDetailsUserOpID.textContent = trimStringWithEllipsis(tx.id, 20)
      page.txDetailsUserOpID.setAttribute('title', tx.id)
      const txIDString = tx.userOpTxID || 'Unsubmitted'
      page.txDetailsID.textContent = trimStringWithEllipsis(txIDString, 20)
      page.txDetailsID.setAttribute('title', txIDString)
    } else {
      page.txDetailsID.textContent = trimStringWithEllipsis(tx.id, 20)
      page.txDetailsID.setAttribute('title', tx.id)
    }

    // Recipient
    if (tx.recipient) {
      Doc.show(page.txDetailsRecipientSection)
      page.txDetailsRecipient.textContent = trimStringWithEllipsis(tx.recipient, 20)
      page.txDetailsRecipient.setAttribute('title', tx.recipient)
    } else {
      Doc.hide(page.txDetailsRecipientSection)
    }

    // Bond Info
    if (tx.bondInfo) {
      Doc.show(page.txDetailsBondIDSection, page.txDetailsBondLocktimeSection)
      Doc.setVis(tx.bondInfo.accountID !== '', page.txDetailsBondAccountIDSection)
      page.txDetailsBondID.textContent = trimStringWithEllipsis(tx.bondInfo.bondID, 20)
      page.txDetailsBondID.setAttribute('title', tx.bondInfo.bondID)
      const date = new Date(tx.bondInfo.lockTime * 1000)
      const dateStr = date.toLocaleDateString()
      const timeStr = date.toLocaleTimeString()
      page.txDetailsBondLocktime.textContent = `${dateStr} ${timeStr}`
      page.txDetailsBondAccountID.textContent = trimStringWithEllipsis(tx.bondInfo.accountID, 20)
      page.txDetailsBondAccountID.setAttribute('title', tx.bondInfo.accountID)
    } else {
      Doc.hide(page.txDetailsBondIDSection, page.txDetailsBondLocktimeSection, page.txDetailsBondAccountIDSection)
    }

    // Nonce
    if (tx.additionalData && tx.additionalData.Nonce) {
      Doc.show(page.txDetailsNonceSection)
      page.txDetailsNonce.textContent = `${tx.additionalData.Nonce}`
    } else {
      Doc.hide(page.txDetailsNonceSection)
    }
  }

  showTxDetailsPopup (tx: WalletTransaction) {
    this.currTx = tx
    this.setTxDetailsPopupElements(tx)
    this.forms.show(this.page.txDetails)
  }

  txDate (tx: WalletTransaction): string {
    if (tx.timestamp === 0) {
      return (new Date()).toLocaleDateString()
    }
    return (new Date(tx.timestamp * 1000)).toLocaleDateString()
  }

  handleTxNote (na: NetworkAsset, tx: WalletTransaction) {
    const { page, selectedWalletID, pendingTxs } = this
    this.depositAddrForm.handleTx(selectedWalletID, tx)
    const pt = pendingTxs[tx.id]
    if (tx.confirmed) {
      delete pendingTxs[tx.id]
      if (pt) pt.tr.remove()
    } else if (tx.timestamp > 0) {
      if (pt) {
        Object.assign(pt, tx)
        updatePendingTx(pt)
      } else {
        const newPT = { ...tx, networkAsset: na } as PendingTx
        pendingTxs[tx.id] = newPT
        this.initializePendingTx(newPT)
        page.pendingTxs.prepend(newPT.tr)
        updatePendingTx(newPT)
      }
    }
    this.setPendingTxVisibility()

    // if (!Doc.isDisplayed(page.txHistoryForm)) return
    // const w = app().assets[ca.assetID].wallet
    // const hideMixing = (w.traits & traitFundsMixer) !== 0 && !!page.hideMixTxs.checked
    // if (hideMixing && tx.type === txTypeMixing) return
    // if (newTx) {
    //   if (!this.oldestTx) {
    //     Doc.show(page.txHistoryTable)
    //     Doc.hide(page.noTxHistory)
    //     page.txHistoryTableBody.appendChild(this.txHistoryDateRow(this.txDate(tx)))
    //     page.txHistoryTableBody.appendChild(this.txHistoryRow(tx, selectedWalletID))
    //     this.oldestTx = tx
    //   } else if (this.txDate(tx) !== this.txHistoryTableNewestDate()) {
    //     page.txHistoryTableBody.insertBefore(this.txHistoryRow(tx, selectedWalletID), page.txHistoryTableBody.children[0])
    //     page.txHistoryTableBody.insertBefore(this.txHistoryDateRow(this.txDate(tx)), page.txHistoryTableBody.children[0])
    //   } else {
    //     page.txHistoryTableBody.insertBefore(this.txHistoryRow(tx, selectedWalletID), page.txHistoryTableBody.children[1])
    //   }
    //   return
    // }
    // for (const row of page.txHistoryTableBody.children) {
    //   const peRow = row as PageElement
    //   if (peRow.dataset.txid === tx.id) {
    //     this.updateTxHistoryRow(peRow, tx, selectedWalletID)
    //     break
    //   }
    // }
    // if (tx.id === this.currTx?.id) {
    //   this.setTxDetailsPopupElements(tx)
    // }
  }

  setPendingTxVisibility () {
    const { page, pendingTxs } = this
    const n = Object.keys(pendingTxs).length
    Doc.setVis(n === 0, page.noPendingTxs)
    Doc.setVis(n > 0, page.somePendingTxs)
    if (n === 0) return Doc.hide(page.pendingTxsListBox)
    page.pendingTxsCount.textContent = String(n)
    const expanded = page.expandPendingTxs.classList.contains('ico-arrowup')
    Doc.setVis(expanded, page.pendingTxsListBox)
  }

  toggleExpandPendingTxs () {
    const { page: { expandPendingTxs: div } } = this
    const expanded = div.classList.contains('ico-arrowup')
    State.storeLocal(State.pendingTxsExpandedLK, !expanded)
    if (expanded) {
      div.classList.remove('ico-arrowup')
      div.classList.add('ico-arrowdown')
    } else {
      div.classList.add('ico-arrowup')
      div.classList.remove('ico-arrowdown')
    }
    this.setPendingTxVisibility()
  }

  async getTxHistory (assetID: number, ignoreTypes?: number[], after?: string): Promise<TxHistoryResult> {
    const req: TxHistoryRequest = { n: txHistoryPageSize, refID: after, past: true, ignoreTypes }
    return postJSON('/api/txhistory', Object.assign({ assetID }, req))
  }

  async showTxHistory (assetID: number) {
    const { page, txHistory } = this
    Doc.hide(page.txHistoryTable, page.noTxHistory, page.hideMixTxs)

    txHistory.assetID = assetID
    const isMixing = txHistory.isMixing = (app().assets[assetID].wallet.traits & traitFundsMixer) !== 0
    Doc.setVis(txHistory.isMixing, page.hideMixTxs)
    this.forms.show(page.txHistoryForm)

    const ignoreTypes = isMixing && page.hideMixTxsCheckbox.checked ? [txTypeMixing] : undefined
    const txRes = await this.getTxHistory(assetID, ignoreTypes)
    if (txRes.txs.length === 0) {
      Doc.show(page.noTxHistory)
      return
    }
    this.txHistory.pgs = [txRes]
    this.showTxHistoryPage(0)
  }

  async showTxHistoryPage (pgIdx: number) {
    const { page, txHistory, txHistory: { pgs, assetID, isMixing } } = this
    txHistory.currentPage = pgIdx
    let txRes = pgs[pgIdx]
    if (!txRes) {
      if (pgs.length < pgIdx) return console.error(`page ${pgIdx + 1} requested with only ${pgs.length} pages of history`)
      const lastTxs = txHistory.pgs[pgIdx - 1].txs
      const refID = lastTxs[lastTxs.length - 1].id
      const ignoreTypes = isMixing && page.hideMixTxsCheckbox.checked ? [txTypeMixing] : undefined
      txRes = await this.getTxHistory(assetID, ignoreTypes, refID)
      pgs.push(txRes)
    }
    Doc.empty(page.txHistoryTableBody)
    let oldestDate = this.txDate(txRes.txs[0])
    page.txHistoryTableBody.appendChild(this.txHistoryDateRow(oldestDate))
    for (const tx of txRes.txs) {
      const date = this.txDate(tx)
      if (date !== oldestDate) {
        oldestDate = date
        page.txHistoryTableBody.appendChild(this.txHistoryDateRow(date))
      }
      const row = this.txHistoryRow(tx, assetID)
      page.txHistoryTableBody.appendChild(row)
    }
    Doc.show(page.txHistoryTable)
    Doc.setVis(pgIdx > 0 || txRes.moreAvailable, page.txHistoryMoreAvailable)
    Doc.setVis(txRes.moreAvailable, page.txHistoryFwd)
    Doc.setVis(pgIdx > 0, page.txHistoryBack)
    page.txHistoryPg.textContent = String(pgIdx + 1)
  }

  async rescanWallet (assetID: number) {
    const page = this.page
    Doc.hide(page.reconfigErr)

    const url = '/api/rescanwallet'
    const req = { assetID: assetID }

    const loaded = app().loading(this.body)
    const res = await postJSON(url, req)
    loaded()
    if (res.code === Errors.activeOrdersErr) {
      this.forceUrl = url
      this.forceReq = req
      this.showConfirmForce()
      return
    }
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.reconfigErr, res.msg)
      return
    }
    this.assetUpdated(assetID, page.reconfigForm, intl.prep(intl.ID_RESCAN_STARTED))
  }

  showConfirmForce () {
    Doc.hide(this.page.confirmForceErr)
    this.forms.show(this.page.confirmForce)
  }

  showRecoverWallet () {
    Doc.hide(this.page.recoverWalletErr)
    this.forms.show(this.page.recoverWalletConfirm)
  }

  /* Show the open wallet form if the password is not cached, and otherwise
   * attempt to open the wallet. This unlocks all wallets for the selected
   * ticker's network assets (e.g., ETH on mainnet and Base).
   */
  async openWallet (assetID: number) {
    const { selectedTicker: ta } = this
    // Collect all asset IDs that need to be unlocked
    const assetsToUnlock: number[] = []
    for (const na of ta.networkAssets) {
      const asset = app().assets[na.assetID]
      const wallet = asset?.wallet
      // Only include wallets that exist, are encrypted, and not already open
      if (wallet && wallet.encrypted && !wallet.open) {
        assetsToUnlock.push(na.assetID)
      }
    }

    // If no wallets need unlocking, just use the provided assetID
    if (assetsToUnlock.length === 0) {
      assetsToUnlock.push(assetID)
    }

    // Unlock all wallets
    const errors: string[] = []
    for (const id of assetsToUnlock) {
      const res = await postJSON('/api/openwallet', { assetID: id })
      if (!app().checkResponse(res)) {
        const asset = app().assets[id]
        errors.push(`${asset?.name || id}: ${res.msg || 'unknown error'}`)
      }
    }

    if (errors.length > 0) {
      console.error('openwallet errors:', errors)
    }

    this.assetUpdated(assetID, undefined, intl.prep(intl.ID_WALLET_UNLOCKED))
  }

  /* Show the form used to change wallet configuration settings. */
  async showReconfig (assetID: number, cfg?: reconfigSettings) {
    const page = this.page
    Doc.hide(
      page.changeWalletType, page.changeTypeHideIcon, page.reconfigErr,
      page.showChangeType, page.changeTypeHideIcon, page.reconfigErr,
      page.enableWallet, page.disableWallet
    )
    // Hide update password section by default
    this.changeWalletPW = false
    this.setPWSettingViz(this.changeWalletPW)
    const asset = app().assets[assetID]

    const currentDef = app().currentWalletDefinition(assetID)
    const walletDefs = asset.token ? [asset.token.definition] : asset.info ? asset.info.availablewallets : []
    const disableWalletType = app().extensionWallet(assetID)?.disableWalletType
    if (walletDefs.length > 1 && !disableWalletType) {
      Doc.empty(page.changeWalletTypeSelect)
      Doc.show(page.showChangeType, page.changeTypeShowIcon)
      page.changeTypeMsg.textContent = intl.prep(intl.ID_CHANGE_WALLET_TYPE)
      for (const wDef of walletDefs) {
        const option = document.createElement('option') as HTMLOptionElement
        if (wDef.type === currentDef.type) option.selected = true
        option.value = option.textContent = wDef.type
        page.changeWalletTypeSelect.appendChild(option)
      }
    }

    if (cfg?.elevateProviders) {
      for (const opt of (currentDef.configopts)) if (opt.key === 'providers') opt.required = true
    }

    const wallet = app().walletMap[assetID]
    Doc.setVis(wallet.traits & traitLogFiler, page.downloadLogs)
    Doc.setVis(wallet.traits & traitRecoverer, page.recoverWallet)
    Doc.setVis(wallet.traits & traitRestorer, page.exportWallet)
    Doc.setVis(wallet.traits & traitRescanner, page.rescanWallet)
    Doc.setVis(wallet.traits & traitPeerManager && !wallet.disabled, page.managePeers)
    Doc.setVis(wallet.traits & traitTokenApprover && !wallet.disabled, page.unapproveTokenAllowance)

    Doc.setVis(wallet.traits & traitsExtraOpts, page.otherActionsLabel)

    if (wallet.disabled) Doc.show(page.enableWallet)
    else Doc.show(page.disableWallet)

    this.showOrHideRecoverySupportMsg(wallet, currentDef.seeded)

    page.recfgAssetLogo.src = Doc.logoPath(asset.symbol)
    page.recfgAssetName.textContent = asset.name
    if (!cfg?.skipAnimation) this.forms.show(page.reconfigForm)
    const loaded = app().loading(page.reconfigForm)
    const res = await postJSON('/api/walletsettings', { assetID })
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.reconfigErr, res.msg)
      return
    }
    const assetHasActiveOrders = app().haveActiveOrders(assetID)
    this.reconfigForm.update(asset.id, currentDef.configopts || [], assetHasActiveOrders)
    this.setGuideLink(currentDef.guidelink)
    this.reconfigForm.setConfig(res.map)
    this.updateDisplayedReconfigFields(currentDef)
  }

  showOrHideRecoverySupportMsg (wallet: WalletState, seeded: boolean) {
    this.setRecoverySupportMsgViz(seeded && !wallet.running && !wallet.disabled && Boolean(wallet.traits & traitRecoverer), wallet.symbol)
  }

  setRecoverySupportMsgViz (viz: boolean, symbol: string) {
    const page = this.page
    if (viz) {
      page.reconfigSupportMsg.textContent = intl.prep(intl.ID_WALLET_RECOVERY_SUPPORT_MSG, { walletSymbol: symbol.toLocaleUpperCase() })
      Doc.show(page.reconfigSupportMsg)
      page.submitReconfig.setAttribute('disabled', '')
      page.submitReconfig.classList.add('grey')
      return
    }
    page.submitReconfig.removeAttribute('disabled')
    page.submitReconfig.classList.remove('grey')
    Doc.empty(page.reconfigSupportMsg)
    Doc.hide(page.reconfigSupportMsg)
  }

  changeWalletType () {
    const page = this.page
    const walletType = page.changeWalletTypeSelect.value || ''
    const walletDef = app().walletDefinition(this.selectedWalletID, walletType)
    this.reconfigForm.update(this.selectedWalletID, walletDef.configopts || [], false)
    const wallet = app().walletMap[this.selectedWalletID]
    const currentDef = app().currentWalletDefinition(this.selectedWalletID)
    if (walletDef.type !== currentDef.type) this.setRecoverySupportMsgViz(false, wallet.symbol)
    else this.showOrHideRecoverySupportMsg(wallet, walletDef.seeded)
    this.setGuideLink(walletDef.guidelink)
    this.updateDisplayedReconfigFields(walletDef)
  }

  setGuideLink (guideLink: string) {
    Doc.hide(this.walletCfgGuide)
    if (guideLink !== '') {
      this.walletCfgGuide.href = guideLink
      Doc.show(this.walletCfgGuide)
    }
  }

  updateDisplayedReconfigFields (walletDef: WalletDefinition) {
    const disablePassword = app().extensionWallet(this.selectedWalletID)?.disablePassword
    if (walletDef.seeded || walletDef.type === 'token' || disablePassword) {
      Doc.hide(this.page.showChangePW, this.reconfigForm.fileSelector)
      this.changeWalletPW = false
      this.setPWSettingViz(false)
    } else Doc.show(this.page.showChangePW, this.reconfigForm.fileSelector)
  }

  /* Display a deposit address. */
  async showDeposit () {
    const { page, selectedTicker: { networkAssets } } = this
    const assetIDs = networkAssets.map(({ assetID }: NetworkAsset) => assetID)
    this.depositAddrForm.setAssetSelect(assetIDs)
    this.forms.show(page.deposit)
  }

  async showSendForm () {
    const { page, selectedTicker: { networkAssets } } = this
    const fundedAssets: NetworkAsset[] = []
    for (const ca of networkAssets) if (ca.bal.available > 0) fundedAssets.push(ca)
    if (fundedAssets.length === 1) return this.showSendAssetForm(fundedAssets[0].assetID)
    Doc.empty(page.netSelectBox)
    for (const { assetID, chainLogo, chainName, bal, ui } of networkAssets) {
      const bttn = Doc.clone(page.netSelectBttnTmpl)
      page.netSelectBox.appendChild(bttn)
      const tmpl = Doc.parseTemplate(bttn)
      tmpl.logo.src = chainLogo
      tmpl.chainName.textContent = chainName
      tmpl.bal.textContent = Doc.formatCoinValue(bal.available, ui)
      Doc.bind(bttn, 'click', () => { this.showSendAssetForm(assetID) })
    }
    this.forms.show(page.sendChainSelectForm)
  }

  async showSendAssetForm (assetID: number) {
    const { page } = this
    const { wallet, unitInfo: ui, symbol, token } = app().assets[assetID]
    Doc.hide(page.toggleSubtract)
    page.subtractCheckBox.checked = false

    const isWithdrawer = (wallet.traits & traitWithdrawer) !== 0
    if (isWithdrawer) {
      Doc.show(page.toggleSubtract)
    }

    Doc.hide(page.sendErr, page.maxSendDisplay, page.sendTokenMsgBox)
    page.sendAddr.classList.remove('border-danger', 'border-success')
    page.sendAddr.value = ''
    page.sendAmt.value = ''
    const xcRate = app().fiatRatesMap[assetID]
    Doc.showFiatValue(page.sendValue, 0, xcRate, ui)
    page.walletBal.textContent = Doc.formatFullPrecision(wallet.balance.available, ui)
    page.sendLogo.src = Doc.logoPath(symbol)
    page.sendName.textContent = ui.conventional.unit
    if (token) {
      const parentAsset = app().assets[token.parentID]
      page.sendTokenParentLogo.src = Doc.logoPath(parentAsset.symbol)
      page.sendTokenParentName.textContent = parentAsset.name
      Doc.show(page.sendTokenMsgBox)
    }
    // page.sendFee.textContent = wallet.feerate
    // page.sendUnit.textContent = wallet.units

    if (wallet.balance.available > 0 && (wallet.traits & traitTxFeeEstimator) !== 0) {
      const feeReq = {
        assetID: assetID,
        subtract: isWithdrawer,
        maxWithdraw: true,
        value: wallet.balance.available
      }

      const loaded = app().loading(this.body)
      const res = await postJSON('/api/txfee', feeReq)
      loaded()
      if (app().checkResponse(res)) {
        let canSend = wallet.balance.available
        if (!token) {
          canSend -= res.txfee
          if (canSend < 0) canSend = 0
        }

        this.maxSend = canSend
        page.maxSend.textContent = Doc.formatFullPrecision(canSend, ui)
        Doc.showFiatValue(page.maxSendFiat, canSend, xcRate, ui)
        if (token) {
          const feeUI = app().assets[token.parentID].unitInfo
          page.maxSendFee.textContent = Doc.formatFullPrecision(res.txfee, feeUI) + ' ' + feeUI.conventional.unit
          Doc.showFiatValue(page.maxSendFeeFiat, res.txfee, app().fiatRatesMap[token.parentID], feeUI)
        } else {
          page.maxSendFee.textContent = Doc.formatFullPrecision(res.txfee, ui)
          Doc.showFiatValue(page.maxSendFeeFiat, res.txfee, xcRate, ui)
        }
        Doc.show(page.maxSendDisplay)
      }
    }

    Doc.showFiatValue(page.sendValue, 0, xcRate, ui)
    page.walletBal.textContent = Doc.formatFullPrecision(wallet.balance.available, ui)
    page.sendForm.dataset.assetID = String(assetID)
    this.forms.show(page.sendForm)
  }

  /* doConnect connects to a wallet via the connectwallet API route. */
  async doConnect (assetID: number) {
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/connectwallet', { assetID })
    loaded()
    if (!app().checkResponse(res)) {
      const { symbol } = app().assets[assetID]
      const page = this.page
      page.errorModalMsg.textContent = intl.prep(intl.ID_CONNECT_WALLET_ERR_MSG, { assetName: symbol, errMsg: res.msg })
      this.forms.show(page.errorModal)
    }
    this.updateSyncAndPeers()
  }

  assetUpdated (assetID: number, oldForm?: PageElement, successMsg?: string) {
    if (this.selectedTicker.networkAssetLookup[assetID]) this.updateDisplayedTicker()
    this.updateAssetBalance(assetID)
    if (oldForm && Object.is(this.forms.currentForm, oldForm)) {
      if (successMsg) this.forms.showSuccess(successMsg)
      else this.forms.close()
    }
  }

  /* populateMaxSend populates the amount field with the max amount the wallet
     can send. The max send amount can be the maximum amount based on our
     pre-estimation or the asset's wallet balance.
  */
  async populateMaxSend () {
    const page = this.page
    const { id: assetID, unitInfo: ui, wallet } = app().assets[this.selectedWalletID]
    // Populate send amount with max send value and ensure we don't check
    // subtract checkbox for assets that don't have a withdraw method.
    const xcRate = app().fiatRatesMap[assetID]
    if ((wallet.traits & traitWithdrawer) === 0) {
      page.sendAmt.value = String(this.maxSend / ui.conventional.conversionFactor)
      Doc.showFiatValue(page.sendValue, this.maxSend, xcRate, ui)
      page.subtractCheckBox.checked = false
    } else {
      const amt = wallet.balance.available
      page.sendAmt.value = String(amt / ui.conventional.conversionFactor)
      Doc.showFiatValue(page.sendValue, amt, xcRate, ui)
      page.subtractCheckBox.checked = true
    }
  }

  /* send submits the send form to the API. */
  async send (): Promise<void> {
    const page = this.page
    const assetID = parseInt(page.sendForm.dataset.assetID ?? '')
    const subtract = page.subtractCheckBox.checked ?? false
    const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
    const pw = page.vSendPw.value || ''
    page.vSendPw.value = ''
    if (pw === '') {
      Doc.showFormError(page.vSendErr, intl.prep(intl.ID_NO_PASS_ERROR_MSG))
      return
    }
    const open = {
      assetID: assetID,
      address: page.sendAddr.value,
      subtract: subtract,
      value: Math.round(parseFloatDefault(page.sendAmt.value) * conversionFactor),
      pw: pw
    }
    const loaded = app().loading(page.vSendForm)
    const res = await postJSON('/api/send', open)
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.vSendErr, res.msg)
      return
    }
    const name = app().assets[assetID].name
    this.assetUpdated(assetID, page.vSendForm, intl.prep(intl.ID_SEND_SUCCESS, { assetName: name }))
  }

  /* update wallet configuration */
  async reconfig (): Promise<void> {
    const page = this.page
    const assetID = this.selectedWalletID
    Doc.hide(page.reconfigErr)
    let walletType = app().currentWalletDefinition(assetID).type
    if (!Doc.isHidden(page.changeWalletType)) {
      walletType = page.changeWalletTypeSelect.value || ''
    }

    const loaded = app().loading(page.reconfigForm)
    const req: ReconfigRequest = {
      assetID: assetID,
      config: this.reconfigForm.map(assetID),
      walletType: walletType
    }
    if (this.changeWalletPW) req.newWalletPW = page.newPW.value
    const res = await this.safePost('/api/reconfigurewallet', req)
    page.newPW.value = ''
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.reconfigErr, res.msg)
      return
    }
    if (this.data?.goBack) {
      app().loadPage(this.data.goBack)
      return
    }
    this.assetUpdated(assetID, page.reconfigForm, intl.prep(intl.ID_RECONFIG_SUCCESS))
    this.updateTicketBuyer()
    // this.showTxHistory(assetID)
    this.updatePrivacy()
  }

  /* lock instructs the API to lock the wallet. */
  async lock (assetID: number): Promise<void> {
    const page = this.page
    const loaded = app().loading(page.newWalletForm)
    const res = await postJSON('/api/closewallet', { assetID: assetID })
    loaded()
    if (!app().checkResponse(res)) return
    this.updateSyncAndPeers()
    this.updatePrivacy()
  }

  async downloadLogs (): Promise<void> {
    const search = new URLSearchParams('')
    search.append('assetid', `${this.selectedWalletID}`)
    const url = new URL(window.location.href)
    url.search = search.toString()
    url.pathname = '/wallets/logfile'
    if (window.electron !== undefined || window.isWebview !== undefined) {
      window.open(url.toString(), '_self')
    } else {
      window.open(url.toString())
    }
  }

  // displayExportWalletAuth displays a form to warn the user about the
  // dangers of exporting a wallet, and asks them to enter their password.
  async displayExportWalletAuth (): Promise<void> {
    const page = this.page
    Doc.hide(page.exportWalletErr)
    page.exportWalletPW.value = ''
    this.forms.show(page.exportWalletAuth)
  }

  // exportWalletAuthSubmit is called after the user enters their password to
  // authorize looking up the information to restore their wallet in an
  // external wallet.
  async exportWalletAuthSubmit (): Promise<void> {
    const page = this.page
    const req = {
      assetID: this.selectedWalletID,
      pass: page.exportWalletPW.value
    }
    const url = '/api/restorewalletinfo'
    const loaded = app().loading(page.forms)
    const res = await postJSON(url, req)
    loaded()
    if (app().checkResponse(res)) {
      page.exportWalletPW.value = ''
      this.displayRestoreWalletInfo(res.restorationinfo)
    } else {
      Doc.showFormError(page.exportWalletErr, res.msg)
    }
  }

  // displayRestoreWalletInfo displays the information needed to restore a
  // wallet in external wallets.
  async displayRestoreWalletInfo (info: WalletRestoration[]): Promise<void> {
    const page = this.page
    Doc.empty(page.restoreInfoCardsList)
    for (const wr of info) {
      const card = this.restoreInfoCard.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(card)
      tmpl.name.textContent = wr.target
      tmpl.seed.textContent = wr.seed
      tmpl.seedName.textContent = `${wr.seedName}:`
      tmpl.instructions.textContent = wr.instructions
      page.restoreInfoCardsList.appendChild(card)
    }
    this.forms.show(page.restoreWalletInfo)
  }

  async recoverWallet (): Promise<void> {
    const page = this.page
    Doc.hide(page.recoverWalletErr)
    const req = {
      assetID: this.selectedWalletID
    }
    const url = '/api/recoverwallet'
    const loaded = app().loading(page.forms)
    const res = await postJSON(url, req)
    loaded()
    if (res.code === Errors.activeOrdersErr) {
      this.forceUrl = url
      this.forceReq = req
      this.showConfirmForce()
    } else if (app().checkResponse(res)) {
      this.forms.close()
    } else {
      Doc.showFormError(page.recoverWalletErr, res.msg)
    }
  }

  /*
   * confirmForceSubmit resubmits either the recover or rescan requests with
   * force set to true. These two requests require force to be set to true if
   * they are called while the wallet is managing active orders.
   */
  async confirmForceSubmit (): Promise<void> {
    const page = this.page
    this.forceReq.force = true
    const loaded = app().loading(page.forms)
    const res = await postJSON(this.forceUrl, this.forceReq)
    loaded()
    if (app().checkResponse(res)) this.forms.close()
    else {
      Doc.showFormError(page.confirmForceErr, res.msg)
    }
  }

  /* handleBalance handles notifications updating a wallet's balance and assets'
     value in default fiat rate.
  . */
  handleBalanceNote (note: BalanceNote): void {
    this.updateAssetBalance(note.assetID)
    if (this.selectedTicker.networkAssetLookup[note.assetID]) this.updateDisplayedTickerBalance()
    // Forward to bridge popup if open
    if (this.bridgingPopupRef.current) {
      this.bridgingPopupRef.current.handleBalanceUpdate(note.assetID)
    }
  }

  /* handleRatesNote handles fiat rate notifications, updating the fiat value of
   *  all supported assets.
   */
  handleRatesNote (): void {
    this.updateDisplayedTickerBalance()
    this.updateFeeState()
    this.refreshBalances()
    this.updateGlobalBalance()
  }

  /*
   * handleWalletStateNote is a handler for both the 'walletstate' and
   * 'walletconfig' notifications.
   */
  handleWalletStateNote (note: WalletStateNote): void {
    const { assetID } = note.wallet
    if (this.selectedTicker.networkAssetLookup[assetID]) this.updateDisplayedTicker()
    if (assetID === this.selectedWalletID) this.updateFeeState()
    if (note.topic === 'WalletPeersUpdate' &&
      assetID === this.selectedWalletID &&
      Doc.isDisplayed(this.page.managePeersForm)) {
      this.updateWalletPeersTable()
    }
    // Forward to bridge popup if open
    if (this.bridgingPopupRef.current) {
      this.bridgingPopupRef.current.handleWalletState(note)
    }
  }

  /*
   * handleBridgeNote is a handler for 'bridge' notifications.
   * It forwards bridge status updates to the bridge popup if open.
   */
  handleBridgeNote (note: BridgeNote): void {
    if (this.bridgingPopupRef.current) {
      this.bridgingPopupRef.current.handleBridgeUpdate(note)
    }
  }

  /*
   * handleCreateWalletNote is a handler for 'createwallet' notifications.
   */
  handleCreateWalletNote (note: WalletCreationNote) {
    if (this.selectedTicker.networkAssetLookup[note.assetID]) this.updateDisplayedTicker()
    // Reload bridge paths since the new wallet may enable new bridge routes
    this.loadBridgePaths()
  }

  handleCustomWalletNote (note: WalletNote) {
    const walletNote = note.payload as BaseWalletNote
    switch (walletNote.route) {
      case 'tipChange': {
        const n = walletNote as TipChangeNote
        if (n.assetID === this.selectedWalletID) this.page.syncHeight.textContent = String(n.tip)
        switch (n.assetID) {
          case 42: { // dcr
            if (!this.stakeStatus) return
            const data = n.data as DecredTicketTipUpdate
            const synced = app().walletMap[n.assetID].synced
            if (synced) {
              const ui = app().unitInfo(n.assetID)
              this.updateTicketStats(data.stats, ui, data.ticketPrice, data.votingSubsidy)
            }
          }
        }
        break
      }
      case 'ticketPurchaseUpdate': {
        this.processTicketPurchaseUpdate(walletNote as CustomWalletNote)
        break
      }
      case 'transaction': {
        const n = walletNote as TransactionNote
        const ca = this.selectedTicker.networkAssetLookup[n.assetID]
        if (ca) this.handleTxNote(ca, n.transaction)
        if (this.bridgingPopupRef.current) {
          this.bridgingPopupRef.current.handleTransactionNote(n)
        }
        break
      }
      // case 'transactionHistorySynced' : {
      //   const n = walletNote
      //   if (n.assetID === this.selectedWalletID) this.showTxHistory(n.assetID)
      //   break
      // }
    }
  }

  /*
   * loadBridgePaths fetches the available bridge paths for all assets.
   */
  async loadBridgePaths () {
    try {
      this.bridgePaths = await app().allBridgePaths()
      this.updateBridgeButtonVisibility()
    } catch (e) {
      console.error('Failed to load bridge paths:', e)
    }
  }

  /*
   * updateBridgeButtonVisibility shows/hides the bridge button based on
   * whether the current asset supports bridging.
   */
  updateBridgeButtonVisibility () {
    const { page, selectedTicker: ta } = this
    if (!ta) return
    const hasBridging = ta.networkAssets.some(na => this.hasBridgingSupport(na.assetID))
    Doc.setVis(hasBridging && ta.hasWallets, page.bridge)
  }

  /*
   * hasBridgingSupport returns true if the asset has a wallet and can bridge
   * to at least one destination that also has a wallet.
   */
  hasBridgingSupport (assetID: number): boolean {
    const asset = app().assets[assetID]
    if (!asset?.wallet) return false

    const paths = this.bridgePaths[assetID]
    if (!paths) return false

    // Check if any destination has a wallet
    for (const destAssetID of Object.keys(paths)) {
      const destAsset = app().assets[Number(destAssetID)]
      if (destAsset?.wallet) return true
    }
    return false
  }

  /*
   * showBridgingPopup displays the bridging popup for the currently selected
   * asset. It selects the first network asset that supports bridging.
   */
  showBridgingPopup () {
    const { page, selectedTicker: ta } = this

    // Find first network asset with bridging support
    const bridgeableAsset = ta.networkAssets.find(na => this.hasBridgingSupport(na.assetID))
    if (!bridgeableAsset) return

    const container = page.bridgingPopupContainer

    Doc.show(container)

    if (!this.bridgingRoot) {
      this.bridgingRoot = createRoot(container)
    }

    const networkAssetIDs = ta.networkAssets.map((na: NetworkAsset) => na.assetID)
    this.bridgingRoot.render(
      React.createElement(BridgingPopup, {
        ref: this.bridgingPopupRef,
        networkAssetIDs: networkAssetIDs,
        bridgePaths: this.bridgePaths,
        onClose: () => this.closeBridgingPopup()
      })
    )
  }

  /*
   * closeBridgingPopup closes the bridging popup and cleans up the React tree.
   */
  closeBridgingPopup () {
    const { page } = this
    Doc.hide(page.bridgingPopupContainer)
    if (this.bridgingRoot) {
      this.bridgingRoot.unmount()
      this.bridgingRoot = null
    }
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /wallets page.
   */
  unload (): void {
    clearInterval(this.secondTicker)
    Doc.unbind(document, 'keyup', this.keyup)
    if (this.bridgingRoot) {
      this.bridgingRoot.unmount()
      this.bridgingRoot = null
    }
  }
}

function updatePendingTx (pt: PendingTx) {
  const { tmpl } = pt
  tmpl.age.textContent = Doc.timeSince(pt.timestamp * 1000)
  if (noAmtTxTypes.includes(pt.type)) {
    tmpl.amount.textContent = '-'
  } else {
    const [u, c] = txTypeSignAndClass(pt.type)
    const amt = Doc.formatCoinValue(pt.amount, pt.networkAsset.ui)
    tmpl.amount.textContent = `${u}${amt}`
    if (c !== '') tmpl.amount.classList.add(c)
  }
  if (pt.confirms) tmpl.confirms.textContent = `${pt.confirms.current} / ${pt.confirms.target}`
}

function trimStringWithEllipsis (str: string, maxLen: number): string {
  if (str.length <= maxLen) return str
  return `${str.substring(0, maxLen / 2)}...${str.substring(str.length - maxLen / 2)}`
}
