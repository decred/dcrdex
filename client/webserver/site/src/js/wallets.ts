import Doc, { Animation, AniToggle, parseFloatDefault, setupCopyBtn } from './doc'
import BasePage from './basepage'
import { postJSON, Errors } from './http'
import {
  NewWalletForm,
  WalletConfigForm,
  DepositAddress,
  bind as bindForm,
  showSuccess
} from './forms'
import State from './state'
import * as intl from './locales'
import * as OrderUtil from './orderutil'
import {
  app,
  PageElement,
  SupportedAsset,
  WalletDefinition,
  BalanceNote,
  WalletStateNote,
  WalletSyncNote,
  RateNote,
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
  TransactionNote,
  WalletTransaction,
  FeeState
} from './registry'
import { CoinExplorers } from './coinexplorers'

interface DecredTicketTipUpdate {
  ticketPrice: number
  votingSubsidy: number
  stats: TicketStats
}

interface TicketPurchaseUpdate extends BaseWalletNote {
  err?: string
  remaining:number
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
const traitHistorian = 1 << 16
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

const positiveTxTypes : number[] = [
  txTypeReceive,
  txTypeRedeem,
  txTypeRefund,
  txTypeRedeemBond,
  txTypeTicketVote,
  txTypeTicketRevocation
]

const negativeTxTypes : number[] = [
  txTypeSend,
  txTypeSwap,
  txTypeCreateBond,
  txTypeTicketPurchase,
  txTypeSwapOrSend
]

const noAmtTxTypes : number[] = [
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
  intl.ID_TX_TYPE_MIX
]

export function txTypeString (txType: number) : string {
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

interface AssetButton {
  tmpl: Record<string, PageElement>
  bttn: PageElement
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

let net = 0

export default class WalletsPage extends BasePage {
  body: HTMLElement
  data?: WalletsPageData
  page: Record<string, PageElement>
  assetButtons: Record<number, AssetButton>
  newWalletForm: NewWalletForm
  reconfigForm: WalletConfigForm
  walletCfgGuide: PageElement
  depositAddrForm: DepositAddress
  keyup: (e: KeyboardEvent) => void
  changeWalletPW: boolean
  displayed: HTMLElement
  animation: Animation
  forms: PageElement[]
  forceReq: RescanRecoveryRequest
  forceUrl: string
  currentForm: PageElement
  restoreInfoCard: HTMLElement
  selectedAssetID: number
  stakeStatus: TicketStakingStatus
  maxSend: number
  unapprovingTokenVersion: number
  ticketPage: TicketPagination
  oldestTx: WalletTransaction | undefined
  currTx: WalletTransaction | undefined
  mixing: boolean
  mixerToggle: AniToggle
  stampers: PageElement[]
  secondTicker: number

  constructor (body: HTMLElement, data?: WalletsPageData) {
    super()
    this.body = body
    this.data = data
    const page = this.page = Doc.idDescendants(body)
    this.stampers = []
    net = app().user.net

    const setStamp = () => {
      for (const span of this.stampers) {
        if (span.dataset.stamp) {
          span.textContent = Doc.timeSince(parseInt(span.dataset.stamp || '') * 1000)
        }
      }
    }
    this.secondTicker = window.setInterval(() => {
      setStamp()
    }, 10000) // update every 10 seconds

    Doc.cleanTemplates(page.restoreInfoCard, page.connectedIconTmpl, page.disconnectedIconTmpl, page.removeIconTmpl)
    this.restoreInfoCard = page.restoreInfoCard.cloneNode(true) as HTMLElement
    Doc.show(page.connectedIconTmpl, page.disconnectedIconTmpl, page.removeIconTmpl)

    this.forms = Doc.applySelector(page.forms, ':scope > form')
    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })
    Doc.bind(page.cancelForce, 'click', () => { this.closePopups() })

    this.selectedAssetID = -1
    Doc.cleanTemplates(
      page.iconSelectTmpl, page.balanceDetailRow, page.recentOrderTmpl, page.vspRowTmpl,
      page.ticketHistoryRowTmpl, page.votingChoiceTmpl, page.votingAgendaTmpl, page.tspendTmpl,
      page.tkeyTmpl, page.txHistoryRowTmpl, page.txHistoryDateRowTmpl
    )

    Doc.bind(page.createWallet, 'click', () => this.showNewWallet(this.selectedAssetID))
    Doc.bind(page.connectBttn, 'click', () => this.doConnect(this.selectedAssetID))
    Doc.bind(page.send, 'click', () => this.showSendForm(this.selectedAssetID))
    Doc.bind(page.receive, 'click', () => this.showDeposit(this.selectedAssetID))
    Doc.bind(page.unlockBttn, 'click', () => this.openWallet(this.selectedAssetID))
    Doc.bind(page.lockBttn, 'click', () => this.lock(this.selectedAssetID))
    Doc.bind(page.reconfigureBttn, 'click', () => this.showReconfig(this.selectedAssetID))
    Doc.bind(page.needsProviderBttn, 'click', () => this.showReconfig(this.selectedAssetID))
    Doc.bind(page.rescanWallet, 'click', () => this.rescanWallet(this.selectedAssetID))
    Doc.bind(page.earlierTxs, 'click', () => this.loadEarlierTxs())

    Doc.bind(page.copyTxIDBtn, 'click', () => { setupCopyBtn(this.currTx?.id || '', page.txDetailsID, page.copyTxIDBtn, '#1e7d11') })
    Doc.bind(page.copyRecipientBtn, 'click', () => { setupCopyBtn(this.currTx?.recipient || '', page.txDetailsRecipient, page.copyRecipientBtn, '#1e7d11') })
    Doc.bind(page.copyBondIDBtn, 'click', () => { setupCopyBtn(this.currTx?.bondInfo?.bondID || '', page.txDetailsBondID, page.copyBondIDBtn, '#1e7d11') })
    Doc.bind(page.copyBondAccountIDBtn, 'click', () => { setupCopyBtn(this.currTx?.bondInfo?.accountID || '', page.txDetailsBondAccountID, page.copyBondAccountIDBtn, '#1e7d11') })
    Doc.bind(page.hideMixTxsCheckbox, 'change', () => { this.showTxHistory(this.selectedAssetID) })

    // Bind the new wallet form.
    this.newWalletForm = new NewWalletForm(page.newWalletForm, (assetID: number) => {
      const fmtParams = { assetName: app().assets[assetID].name }
      this.assetUpdated(assetID, page.newWalletForm, intl.prep(intl.ID_NEW_WALLET_SUCCESS, fmtParams))
      this.sortAssetButtons()
      this.updateTicketBuyer(assetID)
      this.updatePrivacy(assetID)
    })

    // Bind the wallet reconfig form.
    this.reconfigForm = new WalletConfigForm(page.reconfigInputs, false)

    this.walletCfgGuide = Doc.tmplElement(page.reconfigForm, 'walletCfgGuide')

    // Bind the send form.
    bindForm(page.sendForm, page.submitSendForm, async () => { this.stepSend() })
    // Send confirmation form.
    bindForm(page.vSendForm, page.vSend, async () => { this.send() })
    // Bind the wallet reconfiguration submission.
    bindForm(page.reconfigForm, page.submitReconfig, () => this.reconfig())

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => this.closePopups())
    })

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

    this.mixerToggle = new AniToggle(page.toggleMixer, page.mixingErr, false, (newState: boolean) => { return this.updateMixerState(newState) })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (Doc.isDisplayed(this.page.forms)) this.closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    Doc.bind(page.downloadLogs, 'click', async () => { this.downloadLogs() })
    Doc.bind(page.exportWallet, 'click', async () => { this.displayExportWalletAuth() })
    Doc.bind(page.recoverWallet, 'click', async () => { this.showRecoverWallet() })
    bindForm(page.exportWalletAuth, page.exportWalletAuthSubmit, async () => { this.exportWalletAuthSubmit() })
    bindForm(page.recoverWalletConfirm, page.recoverWalletSubmit, () => { this.recoverWallet() })
    bindForm(page.confirmForce, page.confirmForceSubmit, async () => { this.confirmForceSubmit() })
    Doc.bind(page.disableWallet, 'click', async () => { this.showToggleWalletStatus(true) })
    Doc.bind(page.enableWallet, 'click', async () => { this.showToggleWalletStatus(false) })
    bindForm(page.toggleWalletStatusConfirm, page.toggleWalletStatusSubmit, async () => { this.toggleWalletStatus() })
    Doc.bind(page.managePeers, 'click', async () => { this.showManagePeersForm() })
    Doc.bind(page.addPeerSubmit, 'click', async () => { this.submitAddPeer() })
    Doc.bind(page.unapproveTokenAllowance, 'click', async () => { this.showUnapproveTokenAllowanceTableForm() })
    Doc.bind(page.unapproveTokenSubmit, 'click', async () => { this.submitUnapproveTokenAllowance() })
    Doc.bind(page.showVSPs, 'click', () => { this.showVSPPicker() })
    Doc.bind(page.vspDisplay, 'click', () => { this.showVSPPicker() })
    bindForm(page.vspPicker, page.customVspSubmit, async () => { this.setCustomVSP() })
    Doc.bind(page.purchaseTicketsBttn, 'click', () => { this.showPurchaseTicketsDialog() })
    bindForm(page.purchaseTicketsForm, page.purchaserSubmit, () => { this.purchaseTickets() })
    Doc.bind(page.purchaserInput, 'change', () => { this.purchaserInputChanged() })
    Doc.bind(page.ticketHistory, 'click', () => { this.showTicketHistory() })
    Doc.bind(page.ticketHistoryNextPage, 'click', () => { this.nextTicketPage() })
    Doc.bind(page.ticketHistoryPrevPage, 'click', () => { this.prevTicketPage() })
    Doc.bind(page.setVotes, 'click', () => { this.showSetVotesDialog() })
    Doc.bind(page.purchaseTicketsErrCloser, 'click', () => { Doc.hide(page.purchaseTicketsErrBox) })
    Doc.bind(page.privacyInfoBttn, 'click', () => { this.showForm(page.mixingInfo) })

    // New deposit address button.
    this.depositAddrForm = new DepositAddress(page.deposit)

    // Clicking on the available amount on the Send form populates the
    // amount field.
    Doc.bind(page.walletBal, 'click', () => { this.populateMaxSend() })

    // Display fiat value for current send amount.
    Doc.bind(page.sendAmt, 'input', () => {
      const { unitInfo: ui } = app().assets[this.selectedAssetID]
      const amt = parseFloatDefault(page.sendAmt.value)
      const conversionFactor = ui.conventional.conversionFactor
      Doc.showFiatValue(page.sendValue, amt * conversionFactor, app().fiatRatesMap[this.selectedAssetID], ui)
    })

    // Clicking on maxSend on the send form should populate the amount field.
    Doc.bind(page.maxSend, 'click', () => { this.populateMaxSend() })

    // Validate send address on input.
    Doc.bind(page.sendAddr, 'input', async () => {
      const asset = app().assets[this.selectedAssetID]
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
      } else this.showReconfig(this.selectedAssetID, { skipAnimation: true })
    })

    app().registerNoteFeeder({
      fiatrateupdate: (note: RateNote) => { this.handleRatesNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      walletstate: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletconfig: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletsync: (note: WalletSyncNote) => { this.updateSyncAndPeers(note.assetID) },
      createwallet: (note: WalletCreationNote) => { this.handleCreateWalletNote(note) },
      walletnote: (note: WalletNote) => { this.handleCustomWalletNote(note) }
    })

    const firstAsset = this.sortAssetButtons()
    let selectedAsset = firstAsset.id
    const assetIDStr = State.fetchLocal(State.selectedAssetLK)
    if (assetIDStr) selectedAsset = Number(assetIDStr)
    this.setSelectedAsset(selectedAsset)

    setInterval(() => {
      for (const row of this.page.txHistoryTableBody.children) {
        const age = Doc.tmplElement(row as PageElement, 'age')
        age.textContent = Doc.timeSince(parseInt(age.dataset.timestamp as string))
      }
    }, 5000)
  }

  closePopups () {
    Doc.hide(this.page.forms)
    this.currTx = undefined
    if (this.animation) this.animation.stop()
  }

  async safePost (path: string, args: any): Promise<any> {
    const assetID = this.selectedAssetID
    const res = await postJSON(path, args)
    if (assetID !== this.selectedAssetID) throw Error('asset changed during request. aborting')
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

    if (token) {
      const { unitInfo: feeUI, symbol: feeSymbol } = app().assets[token.parentID]
      page.vSendFee.textContent = Doc.formatFullPrecision(txfee, feeUI) + ' ' + feeSymbol
    } else {
      page.vSendFee.textContent = Doc.formatFullPrecision(txfee, ui)
    }
    const xcRate = app().fiatRatesMap[assetID]
    Doc.showFiatValue(page.vSendFeeFiat, txfee, xcRate, ui)
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
    await this.showForm(page.vSendForm)
  }

  // cancelSend displays the send form if user wants to make modification.
  async cancelSend () {
    const page = this.page
    Doc.hide(page.vSendForm, page.sendErr)
    await this.showForm(page.sendForm)
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
    const assetID = this.selectedAssetID
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
      assetID: this.selectedAssetID,
      version: this.unapprovingTokenVersion
    })
    if (!app().checkResponse(res)) {
      page.unapproveTokenErr.textContent = res.msg
      Doc.show(page.unapproveTokenErr)
      return
    }

    const assetExplorer = CoinExplorers[this.selectedAssetID]
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
    const asset = app().assets[this.selectedAssetID]
    if (!asset || !asset.token) return
    const parentAsset = app().assets[asset.token.parentID]
    if (!parentAsset) return
    Doc.empty(page.tokenAllowanceRemoveSymbol)
    page.tokenAllowanceRemoveSymbol.appendChild(Doc.symbolize(asset, true))
    page.tokenAllowanceRemoveVersion.textContent = version.toString()

    const path = '/api/approvetokenfee'
    const res = await postJSON(path, {
      assetID: this.selectedAssetID,
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
    this.showForm(page.unapproveTokenForm)
  }

  /*
   * showUnapproveTokenAllowanceTableForm displays a table showing each of the
   * versions of a token's swap contract that have been approved and allows the
   * user to unapprove any of them.
   */
  async showUnapproveTokenAllowanceTableForm () {
    const page = this.page
    const asset = app().assets[this.selectedAssetID]
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
    this.showForm(page.unapproveTokenTableForm)
  }

  /*
   * updateWalletPeers retrieves the wallet peers and displays them in the
   * wallet peers table.
   */
  async updateWalletPeersTable () {
    const page = this.page

    Doc.hide(page.peerSpinner)

    const res = await postJSON('/api/getwalletpeers', {
      assetID: this.selectedAssetID
    })
    if (!app().checkResponse(res)) {
      page.managePeersErr.textContent = res.msg
      Doc.show(page.managePeersErr)
      return
    }

    while (page.peersTableBody.firstChild) {
      page.peersTableBody.removeChild(page.peersTableBody.firstChild)
    }

    const peers : WalletPeer[] = res.peers || []
    peers.sort((a: WalletPeer, b: WalletPeer) : number => {
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
            assetID: this.selectedAssetID,
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
    this.showForm(page.managePeersForm)
  }

  // submitAddPeers sends a request for the the wallet to connect to a new
  // peer.
  async submitAddPeer () {
    const page = this.page
    Doc.hide(page.managePeersErr)
    const res = await postJSON('/api/addwalletpeer', {
      assetID: this.selectedAssetID,
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
    this.showForm(page.toggleWalletStatusConfirm)
  }

  /*
   * toggleWalletStatus toggles a wallets status to either disabled or enabled.
   */
  async toggleWalletStatus () {
    const page = this.page
    Doc.hide(page.toggleWalletStatusErr)

    const asset = app().assets[this.selectedAssetID]
    const disable = !asset.wallet.disabled
    const url = '/api/togglewalletstatus'
    const req = {
      assetID: this.selectedAssetID,
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
    this.assetUpdated(this.selectedAssetID, page.toggleWalletStatusConfirm, successMsg)
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

  /* showForm shows a modal form with a little animation. */
  async showForm (form: PageElement) {
    const page = this.page
    this.currentForm = form
    this.forms.forEach(form => Doc.hide(form))
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  async showSuccess (msg: string) {
    this.forms.forEach(form => Doc.hide(form))
    this.currentForm = this.page.checkmarkForm
    this.animation = showSuccess(this.page, msg)
    await this.animation.wait()
    this.animation = new Animation(1500, () => { /* pass */ }, '', () => {
      if (this.currentForm === this.page.checkmarkForm) this.closePopups()
    })
  }

  /* Show the new wallet form. */
  async showNewWallet (assetID: number) {
    const page = this.page
    const box = page.newWalletForm
    this.newWalletForm.setAsset(assetID)
    const defaultsLoaded = this.newWalletForm.loadDefaults()
    await this.showForm(box)
    await defaultsLoaded
  }

  // sortAssetButtons displays supported assets, sorted. Returns first asset in the
  // list.
  sortAssetButtons (): SupportedAsset {
    const page = this.page
    this.assetButtons = {}
    Doc.empty(page.assetSelect)
    const sortedAssets = [...Object.values(app().assets)]
    sortedAssets.sort((a: SupportedAsset, b: SupportedAsset) => {
      if (a.wallet && !b.wallet) return -1
      if (!a.wallet && b.wallet) return 1
      if (!a.wallet && !b.wallet) return a.symbol === 'dcr' ? -1 : 1
      const [aBal, bBal] = [a.wallet.balance, b.wallet.balance]
      const [aTotal, bTotal] = [aBal.available + aBal.immature + aBal.locked, bBal.available + bBal.immature + bBal.locked]
      if (aTotal === 0 && bTotal === 0) return a.symbol.localeCompare(b.symbol)
      else if (aTotal === 0) return 1
      else if (aTotal === 0) return -1
      const [aFiat, bFiat] = [app().fiatRatesMap[a.id], app().fiatRatesMap[b.id]]
      if (aFiat && !bFiat) return -1
      if (!aFiat && bFiat) return 1
      return bFiat * bTotal - aFiat * aTotal
    })
    for (const a of sortedAssets) {
      const bttn = page.iconSelectTmpl.cloneNode(true) as HTMLElement
      page.assetSelect.appendChild(bttn)
      const tmpl = Doc.parseTemplate(bttn)
      this.assetButtons[a.id] = { tmpl, bttn }
      this.updateAssetButton(a.id)
      Doc.bind(bttn, 'click', () => {
        this.setSelectedAsset(a.id)
        State.storeLocal(State.selectedAssetLK, String(a.id))
      })
    }
    page.assetSelect.classList.remove('invisible')
    return sortedAssets[0]
  }

  updateAssetButton (assetID: number) {
    const a = app().assets[assetID]
    const { bttn, tmpl } = this.assetButtons[assetID]
    Doc.hide(tmpl.fiatBox, tmpl.noWallet)
    bttn.classList.add('nowallet')
    tmpl.img.src ||= Doc.logoPath(a.symbol) // don't initiate GET if already set (e.g. update on some notification)
    const symbolParts = a.symbol.split('.')
    if (symbolParts.length === 2) {
      const parentSymbol = symbolParts[1]
      tmpl.parentImg.classList.remove('d-hide')
      tmpl.parentImg.src ||= Doc.logoPath(parentSymbol)
    }
    if (this.selectedAssetID === assetID) bttn.classList.add('selected')
    tmpl.name.textContent = a.name
    if (a.wallet) {
      bttn.classList.remove('nowallet')
      const { wallet: { balance: b }, unitInfo: ui } = a
      const totalBalance = b.available + b.locked + b.immature
      const [s, unit] = Doc.formatBestUnitsFourSigFigs(totalBalance, ui)
      tmpl.balance.textContent = s
      tmpl.unit.textContent = unit
      Doc.show(tmpl.balanceBox)
      const fiatRate = app().fiatRatesMap[a.id]
      if (fiatRate) {
        Doc.show(tmpl.fiatBox)
        tmpl.fiat.textContent = Doc.formatFourSigFigs(totalBalance / ui.conventional.conversionFactor * fiatRate)
      }
    } else Doc.show(tmpl.noWallet)
  }

  async setSelectedAsset (assetID: number) {
    const { assetSelect } = this.page
    for (const b of assetSelect.children) b.classList.remove('selected')
    this.assetButtons[assetID].bttn.classList.add('selected')
    this.selectedAssetID = assetID
    this.page.hideMixTxsCheckbox.checked = true
    this.updateDisplayedAsset(assetID)
    this.showAvailableMarkets(assetID)
    const a = this.showRecentActivity(assetID)
    const b = this.showTxHistory(assetID)
    const c = this.updateTicketBuyer(assetID)
    const d = this.updatePrivacy(assetID)
    for (const p of [a, b, c, d]) await p
  }

  updateDisplayedAsset (assetID: number) {
    if (assetID !== this.selectedAssetID) return
    const { symbol, wallet, name, token, unitInfo } = app().assets[assetID]
    const { page, body } = this
    Doc.setText(body, '[data-asset-name]', name)
    Doc.setText(body, '[data-ticker]', unitInfo.conventional.unit)
    page.assetLogo.src = Doc.logoPath(symbol)
    Doc.hide(
      page.balanceBox, page.fiatBalanceBox, page.createWallet, page.walletDetails,
      page.sendReceive, page.connectBttnBox, page.statusLocked, page.statusReady,
      page.statusOff, page.unlockBttnBox, page.lockBttnBox, page.connectBttnBox,
      page.peerCountBox, page.syncProgressBox, page.statusDisabled, page.tokenInfoBox,
      page.needsProviderBox, page.feeStateBox, page.txSyncBox, page.txProgress,
      page.txFindingAddrs
    )
    this.checkNeedsProvider(assetID)
    if (token) {
      const parentAsset = app().assets[token.parentID]
      page.tokenParentLogo.src = Doc.logoPath(parentAsset.symbol)
      page.tokenParentName.textContent = parentAsset.name
      page.contractAddress.textContent = token.contractAddress
      Doc.show(page.tokenInfoBox)
    }
    if (wallet) {
      this.updateDisplayedAssetBalance()
      const { feeState, running, disabled, type: walletType } = wallet

      const walletDef = app().walletDefinition(assetID, walletType)
      page.walletType.textContent = walletDef.tab
      if (feeState) this.updateFeeState(feeState)
      if (disabled) Doc.show(page.statusDisabled) // wallet is disabled
      else if (running) {
        this.updateSyncAndPeers(wallet.assetID)
      } else Doc.show(page.statusOff, page.connectBttnBox) // wallet not running
    } else Doc.show(page.createWallet) // no wallet

    page.walletDetailsBox.classList.remove('invisible')
  }

  updateSyncAndPeers (assetID: number) {
    const { page, selectedAssetID } = this
    if (assetID !== selectedAssetID) return
    const { peerCount, syncProgress, syncStatus, encrypted, open, running } = app().walletMap[assetID]
    if (!running) return
    Doc.show(page.sendReceive, page.peerCountBox, page.syncProgressBox)
    page.peerCount.textContent = String(peerCount)
    page.syncProgress.textContent = `${(syncProgress * 100).toFixed(1)}%`
    if (open) {
      Doc.show(page.statusReady)
      if (!app().haveActiveOrders(assetID) && encrypted) Doc.show(page.lockBttnBox)
    } else Doc.show(page.statusLocked, page.unlockBttnBox) // wallet not unlocked
    Doc.setVis(syncStatus.txs !== undefined, page.txSyncBox)
    if (syncStatus.txs !== undefined) {
      Doc.hide(page.txProgress, page.txFindingAddrs)
      if (syncStatus.txs === 0 && syncStatus.blocks >= syncStatus.targetHeight) Doc.show(page.txFindingAddrs)
      else {
        Doc.show(page.txProgress)
        const prog = syncStatus.txs / syncStatus.targetHeight
        page.txProgress.textContent = `${(prog * 100).toFixed(1)}%`
      }
    }
  }

  updateFeeState (feeState: FeeState) {
    const { page, selectedAssetID: assetID } = this
    Doc.hide(page.feeStateBox)
    const { unitInfo: ui, token } = app().assets[assetID]
    const fiatRate = app().fiatRatesMap[assetID]
    if (!fiatRate) return
    const feeAssetID = token ? token.parentID : assetID
    const feeFiatRate = app().fiatRatesMap[feeAssetID]
    if (token && !feeFiatRate) return
    Doc.show(page.feeStateBox)
    const feeUI = token ? app().assets[token.parentID].unitInfo : ui
    Doc.formatBestRateElement(page.feeStateNetRate, feeAssetID, feeState.rate, feeUI)
    Doc.formatBestValueElement(page.feeStateSendFees, feeAssetID, feeState.send, feeUI)
    Doc.formatBestValueElement(page.feeStateSwapFees, feeAssetID, feeState.swap, feeUI)
    Doc.formatBestValueElement(page.feeStateRedeemFees, feeAssetID, feeState.redeem, feeUI)
    page.feeStateXcRate.textContent = Doc.formatFourSigFigs(fiatRate)
    const sendFiat = feeState.send / feeUI.conventional.conversionFactor * feeFiatRate
    page.feeStateSendFiat.textContent = Doc.formatFourSigFigs(sendFiat)
    const swapFiat = feeState.swap / feeUI.conventional.conversionFactor * feeFiatRate
    page.feeStateSwapFiat.textContent = Doc.formatFourSigFigs(swapFiat)
    const redeemFiat = feeState.redeem / feeUI.conventional.conversionFactor * feeFiatRate
    page.feeStateRedeemFiat.textContent = Doc.formatFourSigFigs(redeemFiat)
    Doc.show(page.feeStateBox)
  }

  async checkNeedsProvider (assetID: number) {
    const needs = await app().needsCustomProvider(assetID)
    const { page: { needsProviderBox: box, needsProviderBttn: bttn } } = this
    Doc.setVis(needs, box)
    if (!needs) return
    Doc.blink(bttn)
  }

  async updateTicketBuyer (assetID: number) {
    this.ticketPage = {
      number: 0,
      history: [],
      scanned: false
    }
    const { wallet, unitInfo: ui } = app().assets[assetID]
    const page = this.page
    Doc.hide(
      page.stakingBox, page.pickVSP, page.stakingSummary, page.stakingErr,
      page.vspDisplayBox, page.ticketPriceBox, page.purchaseTicketsBox,
      page.stakingRpcSpvMsg, page.ticketsDisabled
    )
    if (!wallet?.running || (wallet.traits & traitTicketBuyer) === 0) return
    Doc.show(page.stakingBox)
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
    const disableStaking = app().extensionWallet(this.selectedAssetID)?.disableStaking
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
    const assetID = this.selectedAssetID
    const page = this.page
    this.showForm(page.vspPicker)
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
    this.showForm(this.page.purchaseTicketsForm)
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
    const { page, selectedAssetID: assetID } = this
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
    this.showSuccess(intl.prep(intl.ID_TICKETS_PURCHASED, { n: n.toLocaleString(Doc.languages()) }))
  }

  processTicketPurchaseUpdate (walletNote: CustomWalletNote) {
    const { stakeStatus, selectedAssetID, page } = this
    const { assetID } = walletNote
    const { err, remaining, tickets, stats } = walletNote.payload as TicketPurchaseUpdate
    if (assetID !== selectedAssetID) return
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
    this.closePopups()
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
    const assetID = this.selectedAssetID
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
    const { page, selectedAssetID: assetID } = this
    const ui = app().unitInfo(assetID)
    const coinLink = CoinExplorers[assetID][app().user.net]
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
    const { page, stakeStatus, ticketPage, selectedAssetID: assetID } = this
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
    this.showForm(this.page.ticketHistoryForm)
    await this.ticketPageN(this.ticketPage.number)
  }

  async nextTicketPage () {
    await this.ticketPageN(this.ticketPage.number + 1)
  }

  async prevTicketPage () {
    await this.ticketPageN(this.ticketPage.number - 1)
  }

  showSetVotesDialog () {
    const { page, stakeStatus, selectedAssetID: assetID } = this
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

    this.showForm(page.votingForm)
  }

  async updatePrivacy (assetID: number) {
    const disablePrivacy = app().extensionWallet(assetID)?.disablePrivacy
    this.mixing = false
    const { wallet } = app().assets[assetID]
    const page = this.page
    Doc.hide(page.mixingBox, page.mixerOff, page.mixerOn)
    // TODO: Show special messaging if the asset supports mixing but not this
    // wallet type.
    if (disablePrivacy || !wallet?.running || (wallet.traits & traitFundsMixer) === 0) return
    Doc.show(page.mixingBox, page.mixerLoading)
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
    const res = await postJSON('/api/configuremixer', { assetID: this.selectedAssetID, enabled: on })
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

  updateDisplayedAssetBalance (): void {
    const page = this.page
    const asset = app().assets[this.selectedAssetID]
    const { wallet, unitInfo: ui, id: assetID } = asset
    const bal = wallet.balance
    Doc.show(page.balanceBox, page.walletDetails)
    const totalLocked = bal.locked + bal.contractlocked + bal.bondlocked
    const totalBalance = bal.available + totalLocked + bal.immature
    page.balance.textContent = Doc.formatCoinValue(totalBalance, ui)
    page.balanceUnit.textContent = ui.conventional.unit
    const rate = app().fiatRatesMap[assetID]
    if (rate) {
      Doc.show(page.fiatBalanceBox)
      page.fiatBalance.textContent = Doc.formatFiatConversion(totalBalance, rate, ui)
    }
    Doc.empty(page.balanceDetailBox)

    const addBalanceRow = (cat: string, bal: number, tooltipMsg?: string) => {
      const row = page.balanceDetailRow.cloneNode(true) as PageElement
      page.balanceDetailBox.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.name.textContent = cat
      if (tooltipMsg) {
        tmpl.tooltipMsg.dataset.tooltip = tooltipMsg
        Doc.show(tmpl.tooltipMsg)
      }
      tmpl.balance.textContent = Doc.formatCoinValue(bal, ui)
      return row
    }

    let lastSubLockedRow: PageElement | undefined
    let lastPrimaryRow: PageElement | undefined
    const addPrimaryBalance = (cat: string, bal: number, tooltipMsg?: string) => {
      lastSubLockedRow = undefined
      lastPrimaryRow = addBalanceRow(cat, bal, tooltipMsg)
    }
    const addSubBalance = (cat: string, bal: number, tooltipMsg?: string) => {
      lastSubLockedRow = addBalanceRow(cat, bal, tooltipMsg)
      lastSubLockedRow.classList.add('sub')
    }
    const setRowClasses = () => {
      if (!lastSubLockedRow) return
      (lastPrimaryRow as PageElement).classList.add('itemized')
      lastSubLockedRow.classList.add('last')
    }

    addPrimaryBalance(intl.prep(intl.ID_AVAILABLE_TITLE), bal.available, '')
    if (bal.other?.Shielded !== undefined) {
      const transparent = bal.available - bal.other.Shielded.amt
      addSubBalance(intl.prep(intl.ID_TRANSPARENT), transparent)
      addSubBalance(intl.prep(intl.ID_SHIELDED), bal.other.Shielded.amt)
    }
    setRowClasses()

    addPrimaryBalance(intl.prep(intl.ID_LOCKED_TITLE), totalLocked, intl.prep(intl.ID_LOCKED_BAL_MSG))
    if (bal.orderlocked > 0) addSubBalance(intl.prep(intl.ID_ORDER), bal.orderlocked, intl.prep(intl.ID_LOCKED_ORDER_BAL_MSG))
    if (bal.contractlocked > 0) addSubBalance(intl.prep(intl.ID_SWAPPING), bal.contractlocked, intl.prep(intl.ID_LOCKED_SWAPPING_BAL_MSG))
    if (bal.bondlocked > 0) addSubBalance(intl.prep(intl.ID_BONDED), bal.bondlocked, intl.prep(intl.ID_LOCKED_BOND_BAL_MSG))
    if (bal.bondReserves > 0) addSubBalance(intl.prep(intl.ID_BOND_RESERVES), bal.bondReserves, intl.prep(intl.ID_BOND_RESERVES_MSG))
    if (bal?.other?.Staked !== undefined) addSubBalance('Staked', bal.other.Staked.amt)
    setRowClasses()

    if (bal.immature) addPrimaryBalance(intl.prep(intl.ID_IMMATURE_TITLE), bal.immature, intl.prep(intl.ID_IMMATURE_BAL_MSG))
    if (bal?.other?.Unmixed !== undefined) addSubBalance('Unmixed', bal.other.Unmixed.amt)
    setRowClasses()

    // TODO: handle reserves deficit with a notification.
    // if (bal.reservesDeficit > 0) addPrimaryBalance(intl.prep(intl.ID_RESERVES_DEFICIT), bal.reservesDeficit, intl.prep(intl.ID_RESERVES_DEFICIT_MSG))

    page.purchaserBal.textContent = Doc.formatFourSigFigs(bal.available / ui.conventional.conversionFactor)
    app().bindTooltips(page.balanceDetailBox)
  }

  showAvailableMarkets (assetID: number) {
    const page = this.page
    const exchanges = app().user.exchanges
    const markets: [string, Exchange, Market][] = []
    for (const xc of Object.values(exchanges)) {
      if (!xc.markets) continue
      for (const mkt of Object.values(xc.markets)) {
        if (mkt.baseid === assetID || mkt.quoteid === assetID) markets.push([xc.host, xc, mkt])
      }
    }

    const spotVolume = (assetID: number, mkt: Market): number => {
      const spot = mkt.spot
      if (!spot) return 0
      const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
      const volume = assetID === mkt.baseid ? spot.vol24 : spot.vol24 * spot.rate / OrderUtil.RateEncodingFactor
      return volume / conversionFactor
    }

    markets.sort((a: [string, Exchange, Market], b: [string, Exchange, Market]): number => {
      const [hostA,, mktA] = a
      const [hostB,, mktB] = b
      if (!mktA.spot && !mktB.spot) return hostA.localeCompare(hostB)
      return spotVolume(assetID, mktB) - spotVolume(assetID, mktA)
    })
    Doc.empty(page.availableMarkets)

    for (const [host, xc, mkt] of markets) {
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
        tmpl.volume.textContent = Doc.formatFourSigFigs(spotVolume(assetID, mkt))
        tmpl.volumeUnit.textContent = assetID === baseid ? fmtSymbol(basesymbol) : fmtSymbol(quotesymbol)
      } else Doc.hide(tmpl.priceBox, tmpl.volumeBox)
      Doc.bind(row, 'click', () => app().loadPage('markets', { host, baseID: baseid, quoteID: quoteid }))
    }
    page.marketsOverviewBox.classList.remove('invisible')
  }

  async showRecentActivity (assetID: number) {
    const page = this.page
    const loaded = app().loading(page.orderActivityBox)
    const filter: OrderFilter = {
      n: 20,
      assets: [assetID],
      hosts: [],
      statuses: []
    }
    const res = await postJSON('/api/orders', filter)
    loaded()
    Doc.hide(page.noActivity, page.orderActivity)
    if (!res.orders || res.orders.length === 0) {
      Doc.show(page.noActivity)
      page.orderActivityBox.classList.remove('invisible')
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
    page.orderActivityBox.classList.remove('invisible')
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
    if (tx.timestamp > 0) tmpl.age.dataset.stamp = String(tx.timestamp)
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

  txHistoryRow (tx: WalletTransaction, assetID: number) : PageElement {
    const row = this.page.txHistoryRowTmpl.cloneNode(true) as PageElement
    row.dataset.txid = tx.id
    Doc.bind(row, 'click', () => this.showTxDetailsPopup(tx.id))
    this.updateTxHistoryRow(row, tx, assetID)
    const tmpl = Doc.parseTemplate(row)
    this.stampers.push(tmpl.age)
    return row
  }

  txHistoryDateRow (date: string) : PageElement {
    const row = this.page.txHistoryDateRowTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(row)
    tmpl.date.textContent = date
    return row
  }

  setTxDetailsPopupElements (tx: WalletTransaction) {
    const page = this.page

    // Block explorer
    const assetExplorer = CoinExplorers[this.selectedAssetID]
    if (assetExplorer && assetExplorer[net]) {
      page.txViewBlockExplorer.href = assetExplorer[net](tx.id)
    }

    // Tx type
    let txType = txTypeString(tx.type)
    if (tx.tokenID && tx.tokenID !== this.selectedAssetID) {
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
      let assetID = this.selectedAssetID
      if (tx.tokenID) assetID = tx.tokenID
      Doc.show(page.txDetailsAmtSection)
      const ui = app().unitInfo(assetID)
      const amt = Doc.formatCoinValue(tx.amount, ui)
      const [s, c] = txTypeSignAndClass(tx.type)
      page.txDetailsAmount.textContent = `${s}${amt} ${ui.conventional.unit}`
      if (c !== '') page.txDetailsAmount.classList.add(c)
    }

    // Fee
    let feeAsset = this.selectedAssetID
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

    // Tx ID
    page.txDetailsID.textContent = trimStringWithEllipsis(tx.id, 20)
    page.txDetailsID.setAttribute('title', tx.id)

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

  showTxDetailsPopup (id: string) {
    const tx = app().getWalletTx(this.selectedAssetID, id)
    if (!tx) {
      console.error(`wallet transaction ${id} not found`)
      return
    }
    this.currTx = tx
    this.setTxDetailsPopupElements(tx)
    this.showForm(this.page.txDetails)
  }

  txHistoryTableNewestDate () : string {
    if (this.page.txHistoryTableBody.children.length >= 1) {
      const tmpl = Doc.parseTemplate(this.page.txHistoryTableBody.children[0] as PageElement)
      return tmpl.date.textContent || ''
    }
    return ''
  }

  txDate (tx: WalletTransaction) : string {
    if (tx.timestamp === 0) {
      return (new Date()).toLocaleDateString()
    }
    return (new Date(tx.timestamp * 1000)).toLocaleDateString()
  }

  handleTxNote (tx: WalletTransaction, newTx: boolean) {
    const { selectedAssetID: assetID } = this
    this.depositAddrForm.handleTx(assetID, tx)
    const w = app().assets[this.selectedAssetID].wallet
    const hideMixing = (w.traits & traitFundsMixer) !== 0 && !!this.page.hideMixTxs.checked
    if (hideMixing && tx.type === txTypeMixing) return
    if (newTx) {
      if (!this.oldestTx) {
        Doc.show(this.page.txHistoryTable)
        Doc.hide(this.page.noTxHistory)
        this.page.txHistoryTableBody.appendChild(this.txHistoryDateRow(this.txDate(tx)))
        this.page.txHistoryTableBody.appendChild(this.txHistoryRow(tx, assetID))
        this.oldestTx = tx
      } else if (this.txDate(tx) !== this.txHistoryTableNewestDate()) {
        this.page.txHistoryTableBody.insertBefore(this.txHistoryRow(tx, assetID), this.page.txHistoryTableBody.children[0])
        this.page.txHistoryTableBody.insertBefore(this.txHistoryDateRow(this.txDate(tx)), this.page.txHistoryTableBody.children[0])
      } else {
        this.page.txHistoryTableBody.insertBefore(this.txHistoryRow(tx, assetID), this.page.txHistoryTableBody.children[1])
      }
      return
    }
    for (const row of this.page.txHistoryTableBody.children) {
      const peRow = row as PageElement
      if (peRow.dataset.txid === tx.id) {
        this.updateTxHistoryRow(peRow, tx, assetID)
        break
      }
    }
    if (tx.id === this.currTx?.id) {
      this.setTxDetailsPopupElements(tx)
    }
  }

  async getTxHistory (assetID: number, hideMixTxs: boolean, after?: string) : Promise<TxHistoryResult> {
    let numToFetch = 10
    if (hideMixTxs) numToFetch = 15

    const res : TxHistoryResult = { txs: [], lastTx: false }
    let ref = after

    for (let i = 0; i < 40; i++) {
      const currRes = await app().txHistory(assetID, numToFetch, ref)
      if (currRes.txs.length > 0) {
        ref = currRes.txs[currRes.txs.length - 1].id
      }
      let txs = currRes.txs
      if (hideMixTxs) {
        txs = txs.filter((tx) => tx.type !== txTypeMixing)
      }
      if (res.txs.length + txs.length > 10) {
        const numToPush = 10 - res.txs.length
        res.txs.push(...txs.slice(0, numToPush))
      } else {
        if (currRes.lastTx) res.lastTx = true
        res.txs.push(...txs)
      }
      if (res.txs.length >= 10 || currRes.lastTx) break
    }
    return res
  }

  async showTxHistory (assetID: number) {
    const page = this.page
    let txRes : TxHistoryResult
    Doc.hide(page.txHistoryTable, page.txHistoryBox, page.noTxHistory, page.earlierTxs, page.txHistoryNotAvailable, page.hideMixTxs)
    Doc.empty(page.txHistoryTableBody)
    const w = app().assets[assetID].wallet
    if (!w || w.disabled || (w.traits & traitHistorian) === 0) {
      Doc.show(page.txHistoryNotAvailable)
      return
    }

    this.oldestTx = undefined

    const isMixing = (w.traits & traitFundsMixer) !== 0
    Doc.setVis(isMixing, page.hideMixTxs)
    Doc.show(page.txHistoryBox)

    try {
      const hideMixing = isMixing && !!page.hideMixTxsCheckbox.checked
      txRes = await this.getTxHistory(assetID, hideMixing)
    } catch (err) {
      Doc.show(page.noTxHistory)
      return
    }
    if (txRes.txs.length === 0) {
      Doc.show(page.noTxHistory)
      return
    }

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
    this.oldestTx = txRes.txs[txRes.txs.length - 1]
    Doc.show(page.txHistoryTable)
    Doc.setVis(!txRes.lastTx, page.earlierTxs)
  }

  async loadEarlierTxs () {
    if (!this.oldestTx) return
    const page = this.page
    let txRes : TxHistoryResult
    const w = app().assets[this.selectedAssetID].wallet
    const hideMixing = (w.traits & traitFundsMixer) !== 0 && !!page.hideMixTxsCheckbox.checked
    try {
      txRes = await this.getTxHistory(this.selectedAssetID, hideMixing, this.oldestTx.id)
    } catch (err) {
      console.error(err)
      return
    }
    let oldestDate = this.txDate(this.oldestTx)
    for (const tx of txRes.txs) {
      const date = this.txDate(tx)
      if (date !== oldestDate) {
        oldestDate = date
        page.txHistoryTableBody.appendChild(this.txHistoryDateRow(date))
      }
      const row = this.txHistoryRow(tx, this.selectedAssetID)
      page.txHistoryTableBody.appendChild(row)
    }
    Doc.setVis(!txRes.lastTx, page.earlierTxs)
    if (txRes.txs.length > 0) {
      this.oldestTx = txRes.txs[txRes.txs.length - 1]
    }
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
    this.showForm(this.page.confirmForce)
  }

  showRecoverWallet () {
    Doc.hide(this.page.recoverWalletErr)
    this.showForm(this.page.recoverWalletConfirm)
  }

  /* Show the open wallet form if the password is not cached, and otherwise
   * attempt to open the wallet.
   */
  async openWallet (assetID: number) {
    const open = {
      assetID: assetID
    }
    const res = await postJSON('/api/openwallet', open)
    if (!app().checkResponse(res)) {
      console.error('openwallet error', res)
      return
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
    if (!cfg?.skipAnimation) this.showForm(page.reconfigForm)
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
    const walletDef = app().walletDefinition(this.selectedAssetID, walletType)
    this.reconfigForm.update(this.selectedAssetID, walletDef.configopts || [], false)
    const wallet = app().walletMap[this.selectedAssetID]
    const currentDef = app().currentWalletDefinition(this.selectedAssetID)
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
    const disablePassword = app().extensionWallet(this.selectedAssetID)?.disablePassword
    if (walletDef.seeded || walletDef.type === 'token' || disablePassword) {
      Doc.hide(this.page.showChangePW, this.reconfigForm.fileSelector)
      this.changeWalletPW = false
      this.setPWSettingViz(false)
    } else Doc.show(this.page.showChangePW, this.reconfigForm.fileSelector)
  }

  /* Display a deposit address. */
  async showDeposit (assetID: number) {
    this.depositAddrForm.setAsset(assetID)
    this.showForm(this.page.deposit)
  }

  /* Show the form to either send or withdraw funds. */
  async showSendForm (assetID: number) {
    const page = this.page
    const box = page.sendForm
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
    box.dataset.assetID = String(assetID)
    this.showForm(box)
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
      this.showForm(page.errorModal)
    }
    this.updateDisplayedAsset(assetID)
  }

  assetUpdated (assetID: number, oldForm?: PageElement, successMsg?: string) {
    if (assetID !== this.selectedAssetID) return
    this.updateDisplayedAsset(assetID)
    if (oldForm && Object.is(this.currentForm, oldForm)) {
      if (successMsg) this.showSuccess(successMsg)
      else this.closePopups()
    }
  }

  /* populateMaxSend populates the amount field with the max amount the wallet
     can send. The max send amount can be the maximum amount based on our
     pre-estimation or the asset's wallet balance.
  */
  async populateMaxSend () {
    const page = this.page
    const { id: assetID, unitInfo: ui, wallet } = app().assets[this.selectedAssetID]
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
    const assetID = this.selectedAssetID
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
    this.updateTicketBuyer(assetID)
    app().clearTxHistory(assetID)
    this.showTxHistory(assetID)
    this.updatePrivacy(assetID)
    this.checkNeedsProvider(assetID)
  }

  /* lock instructs the API to lock the wallet. */
  async lock (assetID: number): Promise<void> {
    const page = this.page
    const loaded = app().loading(page.newWalletForm)
    const res = await postJSON('/api/closewallet', { assetID: assetID })
    loaded()
    if (!app().checkResponse(res)) return
    this.updateDisplayedAsset(assetID)
    this.updatePrivacy(assetID)
  }

  async downloadLogs (): Promise<void> {
    const search = new URLSearchParams('')
    search.append('assetid', `${this.selectedAssetID}`)
    const url = new URL(window.location.href)
    url.search = search.toString()
    url.pathname = '/wallets/logfile'
    window.open(url.toString())
  }

  // displayExportWalletAuth displays a form to warn the user about the
  // dangers of exporting a wallet, and asks them to enter their password.
  async displayExportWalletAuth (): Promise<void> {
    const page = this.page
    Doc.hide(page.exportWalletErr)
    page.exportWalletPW.value = ''
    this.showForm(page.exportWalletAuth)
  }

  // exportWalletAuthSubmit is called after the user enters their password to
  // authorize looking up the information to restore their wallet in an
  // external wallet.
  async exportWalletAuthSubmit (): Promise<void> {
    const page = this.page
    const req = {
      assetID: this.selectedAssetID,
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
    this.showForm(page.restoreWalletInfo)
  }

  async recoverWallet (): Promise<void> {
    const page = this.page
    Doc.hide(page.recoverWalletErr)
    const req = {
      assetID: this.selectedAssetID
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
      this.closePopups()
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
    if (app().checkResponse(res)) this.closePopups()
    else {
      Doc.showFormError(page.confirmForceErr, res.msg)
    }
  }

  /* handleBalance handles notifications updating a wallet's balance and assets'
     value in default fiat rate.
  . */
  handleBalanceNote (note: BalanceNote): void {
    this.updateAssetButton(note.assetID)
    if (note.assetID === this.selectedAssetID) this.updateDisplayedAssetBalance()
  }

  /* handleRatesNote handles fiat rate notifications, updating the fiat value of
   *  all supported assets.
   */
  handleRatesNote (note: RateNote): void {
    this.updateAssetButton(this.selectedAssetID)
    if (!note.fiatRates[this.selectedAssetID]) return
    this.updateDisplayedAssetBalance()
    const { feeState } = app().walletMap[this.selectedAssetID]
    if (feeState) this.updateFeeState(feeState)
  }

  /*
   * handleWalletStateNote is a handler for both the 'walletstate' and
   * 'walletconfig' notifications.
   */
  handleWalletStateNote (note: WalletStateNote): void {
    const { assetID, feeState } = note.wallet
    this.updateAssetButton(assetID)
    this.assetUpdated(assetID)
    if (note.topic === 'WalletPeersUpdate' &&
        assetID === this.selectedAssetID &&
        Doc.isDisplayed(this.page.managePeersForm)) {
      this.updateWalletPeersTable()
    }
    if (feeState && assetID === this.selectedAssetID) this.updateFeeState(feeState)
  }

  /*
   * handleCreateWalletNote is a handler for 'createwallet' notifications.
   */
  handleCreateWalletNote (note: WalletCreationNote) {
    this.updateAssetButton(note.assetID)
    this.assetUpdated(note.assetID)
    this.showTxHistory(note.assetID)
  }

  handleCustomWalletNote (note: WalletNote) {
    const walletNote = note.payload as BaseWalletNote
    switch (walletNote.route) {
      case 'tipChange': {
        const n = walletNote as TipChangeNote
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
        if (n.assetID === this.selectedAssetID) this.handleTxNote(n.transaction, n.new)
        break
      }
      case 'transactionHistorySynced' : {
        const n = walletNote
        if (n.assetID === this.selectedAssetID) this.showTxHistory(n.assetID)
        break
      }
    }
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /wallets page.
   */
  unload (): void {
    clearInterval(this.secondTicker)
    Doc.unbind(document, 'keyup', this.keyup)
  }
}

function trimStringWithEllipsis (str: string, maxLen: number): string {
  if (str.length <= maxLen) return str
  return `${str.substring(0, maxLen / 2)}...${str.substring(str.length - maxLen / 2)}`
}
