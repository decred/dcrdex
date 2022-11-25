import Doc, { Animation } from './doc'
import BasePage from './basepage'
import { postJSON, Errors } from './http'
import {
  NewWalletForm,
  WalletConfigForm,
  UnlockWalletForm,
  DepositAddress,
  bind as bindForm
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
  RateNote,
  Order,
  OrderFilter,
  WalletCreationNote,
  Market,
  PeerSource,
  WalletPeer
} from './registry'

const animationLength = 300
const traitRescanner = 1
const traitLogFiler = 1 << 2
const traitRecoverer = 1 << 5
const traitWithdrawer = 1 << 6
const traitRestorer = 1 << 8
const traitTxFeeEstimator = 1 << 10
const traitPeerManager = 1 << 11
const traitsExtraOpts = traitLogFiler & traitRecoverer & traitRestorer & traitRescanner & traitPeerManager

interface ReconfigRequest {
  assetID: number
  walletType: string
  config: Record<string, string>
  newWalletPW?: string
  appPW: string
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

export default class WalletsPage extends BasePage {
  body: HTMLElement
  page: Record<string, PageElement>
  assetButtons: Record<number, AssetButton>
  newWalletForm: NewWalletForm
  reconfigForm: WalletConfigForm
  unlockForm: UnlockWalletForm
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
  maxSend: number

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)

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
      page.iconSelectTmpl, page.balanceDetailRow, page.recentOrderTmpl
    )

    Doc.bind(page.createWallet, 'click', () => this.showNewWallet(this.selectedAssetID))
    Doc.bind(page.connectBttn, 'click', () => this.doConnect(this.selectedAssetID))
    Doc.bind(page.send, 'click', () => this.showSendForm(this.selectedAssetID))
    Doc.bind(page.receive, 'click', () => this.showDeposit(this.selectedAssetID))
    Doc.bind(page.unlockBttn, 'click', () => this.openWallet(this.selectedAssetID))
    Doc.bind(page.lockBttn, 'click', () => this.lock(this.selectedAssetID))
    Doc.bind(page.reconfigureBttn, 'click', () => this.showReconfig(this.selectedAssetID))
    Doc.bind(page.rescanWallet, 'click', () => this.rescanWallet(this.selectedAssetID))

    // Bind the new wallet form.
    this.newWalletForm = new NewWalletForm(page.newWalletForm, (assetID: number) => {
      const fmtParams = { assetName: app().assets[assetID].name }
      this.assetUpdated(assetID, page.newWalletForm, intl.prep(intl.ID_NEW_WALLET_SUCCESS, fmtParams))
      this.sortAssetButtons()
    })

    // Bind the wallet reconfig form.
    this.reconfigForm = new WalletConfigForm(page.reconfigInputs, false)

    // Bind the wallet unlock form.
    this.unlockForm = new UnlockWalletForm(page.unlockWalletForm, (assetID: number) => this.openWalletSuccess(assetID, page.unlockWalletForm))

    // Bind the send form.
    bindForm(page.sendForm, page.submitSendForm, async () => { this.stepSend() })
    // Send confirmation form.
    bindForm(page.vSendForm, page.vSend, async () => { this.send() })
    // Cancel send confirmation form.
    Doc.bind(page.vCancelSend, 'click', async () => { this.cancelSend() })
    // Bind the wallet reconfiguration submission.
    bindForm(page.reconfigForm, page.submitReconfig, () => this.reconfig())

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => this.closePopups())
    })

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

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

    // New deposit address button.
    this.depositAddrForm = new DepositAddress(page.deposit)

    // Clicking on the available amount on the Send form populates the
    // amount field.
    Doc.bind(page.walletBal, 'click', () => { this.populateMaxSend() })

    // Display fiat value for current send amount.
    Doc.bind(page.sendAmt, 'input', () => {
      const { unitInfo: ui } = app().assets[this.selectedAssetID]
      const amt = parseFloat(page.sendAmt.value || '0')
      const conversionFactor = ui.conventional.conversionFactor
      this.showFiatValue(this.selectedAssetID, amt * conversionFactor, page.sendValue)
    })

    // Clicking on maxSend on the send form should populate the amount field.
    Doc.bind(page.maxSend, 'click', () => { this.populateMaxSend() })

    // Validate send address on input.
    Doc.bind(page.sendAddr, 'input', async () => {
      const asset = app().assets[this.selectedAssetID]
      Doc.hide(page.validAddr)
      page.sendAddr.classList.remove('invalid')
      const addr = page.sendAddr.value || ''
      if (!asset || addr === '') return
      const valid = await this.validateSendAddress(addr, asset.id)
      if (valid) Doc.show(page.validAddr)
      else page.sendAddr.classList.add('invalid')
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
      } else this.showReconfig(this.selectedAssetID, true)
    })

    app().registerNoteFeeder({
      fiatrateupdate: (note: RateNote) => { this.handleRatesNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      walletstate: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletconfig: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      createwallet: (note: WalletCreationNote) => { this.handleCreateWalletNote(note) }
    })

    const firstAsset = this.sortAssetButtons()
    let selectedAsset = State.selectedAsset()
    if (!selectedAsset) {
      selectedAsset = firstAsset.id
    }
    this.setSelectedAsset(selectedAsset)
  }

  closePopups () {
    Doc.hide(this.page.forms)
    if (this.animation) this.animation.stop()
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
    const value = Math.round(parseFloat(page.sendAmt.value || '') * conversionFactor)
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
    this.showFiatValue(assetID, txfee, page.vSendFeeFiat)
    page.vSendDestinationAmt.textContent = Doc.formatFullPrecision(value - txfee, ui)
    page.vTotalSend.textContent = Doc.formatFullPrecision(value, ui)
    this.showFiatValue(assetID, value, page.vTotalSendFiat)
    page.vSendAddr.textContent = page.sendAddr.value || ''
    const bal = wallet.balance.available - value
    page.balanceAfterSend.textContent = Doc.formatFullPrecision(bal, ui)
    this.showFiatValue(assetID, bal, page.balanceAfterSendFiat)
    Doc.show(page.approxSign)
    // NOTE: All tokens take this route because they cannot pay the fee.
    if (!subtract) {
      Doc.hide(page.approxSign)
      page.vSendDestinationAmt.textContent = Doc.formatFullPrecision(value, ui)
      let totalSend = value
      if (!token) totalSend += txfee
      page.vTotalSend.textContent = Doc.formatFullPrecision(totalSend, ui)
      this.showFiatValue(assetID, totalSend, page.vTotalSendFiat)
      let bal = wallet.balance.available - value
      if (!token) bal -= txfee
      // handle edge cases where bal is not enough to cover totalSend.
      // we don't want a minus display of user bal.
      if (bal <= 0) {
        page.balanceAfterSend.textContent = Doc.formatFullPrecision(0, ui)
        this.showFiatValue(assetID, 0, page.balanceAfterSendFiat)
      } else {
        page.balanceAfterSend.textContent = Doc.formatFullPrecision(bal, ui)
        this.showFiatValue(assetID, bal, page.balanceAfterSendFiat)
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
    if (visible) {
      Doc.hide(this.page.showIcon)
      Doc.show(this.page.hideIcon, this.page.changePW)
      this.page.switchPWMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_PASS)
      return
    }
    Doc.hide(this.page.hideIcon, this.page.changePW)
    Doc.show(this.page.showIcon)
    this.page.switchPWMsg.textContent = intl.prep(intl.ID_NEW_WALLET_PASS)
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
    const page = this.page
    page.successMessage.textContent = msg
    this.currentForm = page.checkmarkForm
    this.forms.forEach(form => Doc.hide(form))
    Doc.show(page.forms, page.checkmarkForm)
    page.checkmarkForm.style.right = '0'
    page.checkmark.style.fontSize = '0px'

    const [startR, startG, startB] = State.isDark() ? [223, 226, 225] : [51, 51, 51]
    const [endR, endG, endB] = [16, 163, 16]
    const [diffR, diffG, diffB] = [endR - startR, endG - startG, endB - startB]

    this.animation = new Animation(1200, (prog: number) => {
      page.checkmark.style.fontSize = `${prog * 80}px`
      page.checkmark.style.color = `rgb(${startR + prog * diffR}, ${startG + prog * diffG}, ${startB + prog * diffB})`
    }, 'easeOutElastic', () => {
      this.animation = new Animation(1500, () => { /* pass */ }, '', () => {
        if (this.currentForm === page.checkmarkForm) this.closePopups()
      })
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
      return a.symbol.localeCompare(b.symbol)
    })
    for (const a of sortedAssets) {
      const bttn = page.iconSelectTmpl.cloneNode(true) as HTMLElement
      page.assetSelect.appendChild(bttn)
      const tmpl = Doc.parseTemplate(bttn)
      this.assetButtons[a.id] = { tmpl, bttn }
      this.updateAssetButton(a.id)
      Doc.bind(bttn, 'click', () => {
        this.setSelectedAsset(a.id)
        State.setCookie(State.SelectedAssetCK, String(a.id))
      })
    }
    page.assetSelect.classList.remove('invisible')
    return sortedAssets[0]
  }

  updateAssetButton (assetID: number) {
    const a = app().assets[assetID]
    const { bttn, tmpl } = this.assetButtons[assetID]
    Doc.hide(tmpl.fiat, tmpl.noWallet)
    bttn.classList.add('nowallet')
    tmpl.img.src = Doc.logoPath(a.symbol)
    tmpl.name.textContent = a.name
    if (a.wallet) {
      bttn.classList.remove('nowallet')
      const { wallet: { balance: b }, unitInfo: ui } = a
      const totalBalance = b.available + b.locked + b.immature
      tmpl.balance.textContent = Doc.formatCoinValue(totalBalance, ui)
      const rate = app().fiatRatesMap[a.id]
      if (rate) {
        Doc.show(tmpl.fiat)
        tmpl.fiat.textContent = Doc.formatFiatConversion(totalBalance, rate, ui)
      }
    } else Doc.show(tmpl.noWallet)
  }

  setSelectedAsset (assetID: number) {
    const { assetSelect } = this.page
    for (const b of assetSelect.children) b.classList.remove('selected')
    this.assetButtons[assetID].bttn.classList.add('selected')
    this.selectedAssetID = assetID
    this.updateDisplayedAsset(assetID)
    this.showAvailableMarkets(assetID)
    this.showRecentActivity(assetID)
  }

  updateDisplayedAsset (assetID: number) {
    if (assetID !== this.selectedAssetID) return
    const { symbol, wallet, name } = app().assets[assetID]
    const page = this.page
    for (const el of document.querySelectorAll('[data-asset-name]')) el.textContent = name
    page.assetLogo.src = Doc.logoPath(symbol)
    Doc.hide(
      page.balanceBox, page.fiatBalanceBox, page.createWalletBox, page.walletDetails,
      page.sendReceive, page.connectBttnBox, page.statusLocked, page.statusReady,
      page.statusOff, page.unlockBttnBox, page.lockBttnBox, page.connectBttnBox,
      page.peerCountBox, page.syncProgressBox, page.statusDisabled
    )
    if (wallet) {
      this.updateDisplayedAssetBalance()

      const walletDef = app().walletDefinition(assetID, wallet.type)
      page.walletType.textContent = walletDef.tab
      const configurable = assetIsConfigurable(assetID)
      Doc.setVis(configurable, page.passwordWrapper)

      if (wallet.disabled) Doc.show(page.statusDisabled) // wallet is disabled
      else if (wallet.running) {
        Doc.show(page.sendReceive, page.peerCountBox, page.syncProgressBox)
        page.peerCount.textContent = String(wallet.peerCount)
        page.syncProgress.textContent = `${(wallet.syncProgress * 100).toFixed(1)}%`
        if (wallet.open) {
          Doc.show(page.statusReady)
          if (!app().haveAssetOrders(assetID) && wallet.encrypted) Doc.show(page.lockBttnBox)
        } else Doc.show(page.statusLocked, page.unlockBttnBox) // wallet not unlocked
      } else Doc.show(page.statusOff, page.connectBttnBox) // wallet not running
    } else Doc.show(page.createWalletBox) // no wallet

    page.walletDetailsBox.classList.remove('invisible')
  }

  updateDisplayedAssetBalance (): void {
    const page = this.page
    const { wallet, unitInfo: ui, symbol, id: assetID } = app().assets[this.selectedAssetID]
    const bal = wallet.balance
    Doc.show(page.balanceBox, page.walletDetails)
    const totalBalance = bal.available + bal.locked + bal.immature
    page.balance.textContent = Doc.formatCoinValue(totalBalance, ui)
    Doc.empty(page.balanceUnit)
    page.balanceUnit.appendChild(Doc.symbolize(symbol))
    const rate = app().fiatRatesMap[assetID]
    if (rate) {
      Doc.show(page.fiatBalanceBox)
      page.fiatBalance.textContent = Doc.formatFiatConversion(totalBalance, rate, ui)
    }
    Doc.empty(page.balanceDetailBox)
    const addSubBalance = (category: string, subBalance: number) => {
      const row = page.balanceDetailRow.cloneNode(true) as PageElement
      page.balanceDetailBox.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.category.textContent = category
      tmpl.subBalance.textContent = Doc.formatCoinValue(subBalance, ui)
    }
    addSubBalance('Available', bal.available)
    addSubBalance('Locked', bal.locked) // Hide if zero?
    addSubBalance('Immature', bal.immature) // Hide if zero?
    const sortedCats = Object.entries(bal.other || {})
    sortedCats.sort((a: [string, number], b: [string, number]): number => a[0].localeCompare(b[0]))
    for (const [cat, sub] of sortedCats) addSubBalance(cat, sub)
  }

  showAvailableMarkets (assetID: number) {
    const page = this.page
    const exchanges = app().user.exchanges
    const markets: [string, Market][] = []
    for (const xc of Object.values(exchanges)) {
      if (!xc.markets) continue
      for (const mkt of Object.values(xc.markets)) {
        if (mkt.baseid === assetID || mkt.quoteid === assetID) markets.push([xc.host, mkt])
      }
    }

    const spotVolume = (assetID: number, mkt: Market): number => {
      const spot = mkt.spot
      if (!spot) return 0
      const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
      const volume = assetID === mkt.baseid ? spot.vol24 : spot.vol24 * spot.rate / OrderUtil.RateEncodingFactor
      return volume / conversionFactor
    }

    markets.sort((a: [string, Market], b: [string, Market]): number => {
      const [hostA, mktA] = a
      const [hostB, mktB] = b
      if (!mktA.spot && !mktB.spot) return hostA.localeCompare(hostB)
      return spotVolume(assetID, mktB) - spotVolume(assetID, mktA)
    })
    Doc.empty(page.availableMarkets)

    for (const [host, mkt] of markets) {
      const { spot, baseid, basesymbol, quoteid, quotesymbol } = mkt
      const row = page.marketRow.cloneNode(true) as PageElement
      page.availableMarkets.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.host.textContent = host
      tmpl.baseLogo.src = Doc.logoPath(basesymbol)
      tmpl.quoteLogo.src = Doc.logoPath(quotesymbol)
      Doc.empty(tmpl.baseSymbol, tmpl.quoteSymbol)
      tmpl.baseSymbol.appendChild(Doc.symbolize(basesymbol))
      tmpl.quoteSymbol.appendChild(Doc.symbolize(quotesymbol))

      if (spot) {
        const convRate = app().conventionalRate(baseid, quoteid, spot.rate, exchanges[host])
        tmpl.price.textContent = fourSigFigs(convRate)
        tmpl.priceQuoteUnit.textContent = quotesymbol.toUpperCase()
        tmpl.priceBaseUnit.textContent = basesymbol.toUpperCase()
        tmpl.volume.textContent = fourSigFigs(spotVolume(assetID, mkt))
        tmpl.volumeUnit.textContent = assetID === baseid ? basesymbol.toUpperCase() : quotesymbol.toUpperCase()
      } else Doc.hide(tmpl.priceBox, tmpl.volumeBox)
      Doc.bind(row, 'click', () => app().loadPage('markets', { host, base: baseid, quote: quoteid }))
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
      let from: string, to: string
      const [baseUnitInfo, quoteUnitInfo] = [app().unitInfo(ord.baseID), app().unitInfo(ord.quoteID)]
      if (ord.sell) {
        [from, to] = [ord.baseSymbol, ord.quoteSymbol]
        tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        if (ord.type === OrderUtil.Limit) {
          tmpl.toQty.textContent = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
        }
      } else {
        [from, to] = [ord.quoteSymbol, ord.baseSymbol]
        if (ord.type === OrderUtil.Market) {
          tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        } else {
          tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
          tmpl.toQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        }
      }

      tmpl.fromLogo.src = Doc.logoPath(from)
      Doc.empty(tmpl.fromSymbol, tmpl.toSymbol)
      tmpl.fromSymbol.appendChild(Doc.symbolize(from))
      tmpl.toLogo.src = Doc.logoPath(to)
      tmpl.toSymbol.appendChild(Doc.symbolize(to))
      tmpl.status.textContent = OrderUtil.statusString(ord)
      tmpl.filled.textContent = `${(OrderUtil.filled(ord) / ord.qty * 100).toFixed(1)}%`
      tmpl.age.textContent = Doc.timeSince(ord.submitTime)
      tmpl.link.href = `order/${ord.id}`
      app().bindInternalNavigation(row)
    }
    page.orderActivityBox.classList.remove('invisible')
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
    if (!State.passwordIsCached()) {
      this.showOpen(assetID)
    } else {
      const open = {
        assetID: assetID
      }
      const res = await postJSON('/api/openwallet', open)
      if (app().checkResponse(res)) {
        this.openWalletSuccess(assetID)
      } else {
        this.showOpen(assetID, `Error opening wallet: ${res.msg}`)
      }
    }
  }

  /* Show the form used to unlock a wallet. */
  async showOpen (assetID: number, errorMsg?: string) {
    const page = this.page
    // await this.hideBox()
    this.unlockForm.refresh(app().assets[assetID])
    if (errorMsg) this.unlockForm.showErrorOnly(errorMsg)
    this.showForm(page.unlockWalletForm)
  }

  /* Show the form used to change wallet configuration settings. */
  async showReconfig (assetID: number, skipAnimation?: boolean) {
    const page = this.page
    Doc.hide(page.changeWalletType, page.changeTypeHideIcon, page.reconfigErr, page.showChangeType, page.changeTypeHideIcon)
    Doc.hide(page.reconfigErr)
    Doc.hide(page.enableWallet, page.disableWallet)
    // Hide update password section by default
    this.changeWalletPW = false
    this.setPWSettingViz(this.changeWalletPW)
    const asset = app().assets[assetID]

    const currentDef = app().currentWalletDefinition(assetID)
    const walletDefs = asset.token ? [asset.token.definition] : asset.info ? asset.info.availablewallets : []

    if (walletDefs.length > 1) {
      Doc.empty(page.changeWalletTypeSelect)
      Doc.show(page.showChangeType, page.changeTypeShowIcon)
      page.changeTypeMsg.textContent = intl.prep(intl.ID_CHANGE_WALLET_TYPE)
      for (const wDef of walletDefs) {
        const option = document.createElement('option') as HTMLOptionElement
        if (wDef.type === currentDef.type) option.selected = true
        option.value = option.textContent = wDef.type
        page.changeWalletTypeSelect.appendChild(option)
      }
    } else {
      Doc.hide(page.showChangeType)
    }

    const wallet = app().walletMap[assetID]
    Doc.setVis(wallet.traits & traitLogFiler, page.downloadLogs)
    Doc.setVis(wallet.traits & traitRecoverer, page.recoverWallet)
    Doc.setVis(wallet.traits & traitRestorer, page.exportWallet)
    Doc.setVis(wallet.traits & traitRescanner, page.rescanWallet)
    Doc.setVis(wallet.traits & traitPeerManager, page.managePeers)

    Doc.setVis(wallet.traits & traitsExtraOpts, page.otherActionsLabel)

    if (wallet.disabled) Doc.show(page.enableWallet)
    else Doc.show(page.disableWallet)

    page.recfgAssetLogo.src = Doc.logoPath(asset.symbol)
    page.recfgAssetName.textContent = asset.name
    if (!skipAnimation) this.showForm(page.reconfigForm)
    const loaded = app().loading(page.reconfigForm)
    const res = await postJSON('/api/walletsettings', { assetID })
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.reconfigErr, res.msg)
      return
    }
    const assetHasActiveOrders = app().haveAssetOrders(assetID)
    this.reconfigForm.update(currentDef.configopts || [], { assetHasActiveOrders })
    this.reconfigForm.setConfig(res.map)
    this.updateDisplayedReconfigFields(currentDef)
  }

  changeWalletType () {
    const page = this.page
    const walletType = page.changeWalletTypeSelect.value || ''
    const walletDef = app().walletDefinition(this.selectedAssetID, walletType)
    this.reconfigForm.update(walletDef.configopts || [], {})
    this.updateDisplayedReconfigFields(walletDef)
  }

  updateDisplayedReconfigFields (walletDef: WalletDefinition) {
    if (walletDef.seeded || walletDef.type === 'token') {
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
    const { wallet, name, unitInfo: ui, symbol, token } = app().assets[assetID]
    Doc.hide(page.toggleSubtract)
    page.subtractCheckBox.checked = false

    const isWithdrawer = (wallet.traits & traitWithdrawer) !== 0
    if (isWithdrawer) {
      Doc.show(page.toggleSubtract)
    }

    Doc.hide(page.validAddr, page.sendErr, page.maxSendDisplay)
    page.sendAddr.classList.remove('invalid')
    page.sendAddr.value = ''
    page.sendAmt.value = ''
    this.showFiatValue(assetID, 0, page.sendValue)
    page.walletBal.textContent = Doc.formatFullPrecision(wallet.balance.available, ui)
    page.sendLogo.src = Doc.logoPath(symbol)
    page.sendName.textContent = name
    // page.sendFee.textContent = wallet.feerate
    // page.sendUnit.textContent = wallet.units

    if (wallet.balance.available > 0 && (wallet.traits & traitTxFeeEstimator) !== 0) {
      const feeReq = {
        assetID: assetID,
        subtract: isWithdrawer,
        value: wallet.balance.available
      }

      const loaded = app().loading(this.body)
      const res = await postJSON('/api/txfee', feeReq)
      loaded()
      if (app().checkResponse(res)) {
        let canSend = wallet.balance.available
        if (!token) {
          canSend -= res.txfee
        }
        this.maxSend = canSend
        page.maxSend.textContent = Doc.formatFullPrecision(canSend, ui)
        this.showFiatValue(assetID, canSend, page.maxSendFiat)
        if (token) {
          const { unitInfo: feeUI, symbol: feeSymbol } = app().assets[token.parentID]
          page.maxSendFee.textContent = Doc.formatFullPrecision(res.txfee, feeUI) + ' ' + feeSymbol
          this.showFiatValue(token.parentID, res.txfee, page.maxSendFeeFiat)
        } else {
          page.maxSendFee.textContent = Doc.formatFullPrecision(res.txfee, ui)
          this.showFiatValue(assetID, res.txfee, page.maxSendFeeFiat)
        }
        Doc.show(page.maxSendDisplay)
      }
    }

    this.showFiatValue(assetID, 0, page.sendValue)
    page.walletBal.textContent = Doc.formatFullPrecision(wallet.balance.available, ui)
    page.sendLogo.src = Doc.logoPath(wallet.symbol)
    page.sendName.textContent = name
    box.dataset.assetID = String(assetID)
    this.showForm(box)
  }

  /* doConnect connects to a wallet via the connectwallet API route. */
  async doConnect (assetID: number) {
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/connectwallet', { assetID })
    loaded()
    if (!app().checkResponse(res)) return
    this.updateDisplayedAsset(assetID)
  }

  /* openWalletSuccess is the success callback for wallet unlocking. */
  async openWalletSuccess (assetID: number, form?: PageElement) {
    this.assetUpdated(assetID, form, intl.prep(intl.ID_WALLET_UNLOCKED))
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
    const asset = app().assets[this.selectedAssetID]
    if (!asset) return
    // Populate send amount with max send value and ensure we don't check
    // subtract checkbox for assets that don't have a withdraw method.
    if ((asset.wallet.traits & traitWithdrawer) === 0) {
      page.sendAmt.value = String(this.maxSend / asset.unitInfo.conventional.conversionFactor)
      this.showFiatValue(asset.id, this.maxSend, page.sendValue)
      page.subtractCheckBox.checked = false
    } else {
      const amt = asset.wallet.balance.available
      page.sendAmt.value = String(amt / asset.unitInfo.conventional.conversionFactor)
      this.showFiatValue(asset.id, amt, page.sendValue)
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
      value: Math.round(parseFloat(page.sendAmt.value ?? '') * conversionFactor),
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
    if (!page.appPW.value && !State.passwordIsCached()) {
      Doc.showFormError(page.reconfigErr, intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG))
      return
    }

    let walletType = app().currentWalletDefinition(assetID).type
    if (!Doc.isHidden(page.changeWalletType)) {
      walletType = page.changeWalletTypeSelect.value || ''
    }

    const loaded = app().loading(page.reconfigForm)
    const req: ReconfigRequest = {
      assetID: assetID,
      config: this.reconfigForm.map(assetID),
      appPW: page.appPW.value ?? '',
      walletType: walletType
    }
    if (this.changeWalletPW) req.newWalletPW = page.newPW.value
    const res = await postJSON('/api/reconfigurewallet', req)
    page.appPW.value = ''
    page.newPW.value = ''
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.reconfigErr, res.msg)
      return
    }
    this.assetUpdated(assetID, page.reconfigForm, intl.prep(intl.ID_RECONFIG_SUCCESS))
  }

  /* lock instructs the API to lock the wallet. */
  async lock (assetID: number): Promise<void> {
    const page = this.page
    const loaded = app().loading(page.newWalletForm)
    const res = await postJSON('/api/closewallet', { assetID: assetID })
    loaded()
    if (!app().checkResponse(res)) return
    this.updateDisplayedAsset(assetID)
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
      assetID: this.selectedAssetID,
      appPW: page.recoverWalletPW.value
    }
    page.recoverWalletPW.value = ''
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
  }

  // showFiatValue displays the fiat equivalent for the provided amount.
  showFiatValue (assetID: number, amount: number, display: PageElement): void {
    const rate = app().fiatRatesMap[assetID]
    if (rate) {
      display.textContent = Doc.formatFiatConversion(amount, rate, app().unitInfo(assetID))
      Doc.show(display.parentElement as Element)
    } else Doc.hide(display.parentElement as Element)
  }

  /*
   * handleWalletStateNote is a handler for both the 'walletstate' and
   * 'walletconfig' notifications.
   */
  handleWalletStateNote (note: WalletStateNote): void {
    this.updateAssetButton(note.wallet.assetID)
    this.assetUpdated(note.wallet.assetID)
    if (note.topic === 'WalletPeersUpdate' &&
        note.wallet.assetID === this.selectedAssetID &&
        Doc.isDisplayed(this.page.managePeersForm)) {
      this.updateWalletPeersTable()
    }
  }

  /*
   * handleCreateWalletNote is a handler for 'createwallet' notifications.
   */
  handleCreateWalletNote (note: WalletCreationNote) {
    this.updateAssetButton(note.assetID)
    this.assetUpdated(note.assetID)
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /wallets page.
   */
  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }
}

/*
 * assetIsConfigurable indicates whether there are any user-configurable wallet
 * settings for the asset.
 */
function assetIsConfigurable (assetID: number) {
  const asset = app().assets[assetID]
  if (asset.token) {
    const opts = asset.token.definition.configopts
    return opts && opts.length > 0
  }
  if (!asset.info) throw Error('this asset isn\'t an asset, I guess')
  const defs = asset.info.availablewallets
  const zerothOpts = defs[0].configopts
  return defs.length > 1 || (zerothOpts && zerothOpts.length > 0)
}

const FourSigFigs = new Intl.NumberFormat((navigator.languages as string[]), {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 4
})

const intFormatter = new Intl.NumberFormat((navigator.languages as string[]))

function fourSigFigs (v: number): string {
  if (v < 100) return FourSigFigs.format(v)
  return intFormatter.format(Math.round(v))
}
