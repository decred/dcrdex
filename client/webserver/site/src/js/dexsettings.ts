import Doc, { Animation, AniToggle } from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import * as forms from './forms'
import * as intl from './locales'
import { ReputationMeter, strongTier } from './account'
import {
  app,
  PageElement,
  ConnectionStatus,
  Exchange,
  WalletState,
  PrepaidBondID
} from './registry'

interface Animator {
  animate: (() => Promise<void>)
}

interface BondOptionsForm {
  host?: string // Required, but set by updateBondOptions
  bondAssetID?: number
  targetTier?: number
  penaltyComps?: number
}

const animationLength = 300

export default class DexSettingsPage extends BasePage {
  body: HTMLElement
  forms: PageElement[]
  currentForm: PageElement
  page: Record<string, PageElement>
  host: string
  accountDisabled:boolean
  keyup: (e: KeyboardEvent) => void
  dexAddrForm: forms.DEXAddressForm
  bondFeeBufferCache: Record<string, number>
  newWalletForm: forms.NewWalletForm
  regAssetForm: forms.FeeAssetSelectionForm
  walletWaitForm: forms.WalletWaitForm
  confirmRegisterForm: forms.ConfirmRegistrationForm
  reputationMeter: ReputationMeter
  animation: Animation
  renewToggle: AniToggle

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const host = this.host = body.dataset.host ? body.dataset.host : ''
    const xc = app().exchanges[host]
    const page = this.page = Doc.idDescendants(body)
    this.forms = Doc.applySelector(page.forms, ':scope > form')

    this.confirmRegisterForm = new forms.ConfirmRegistrationForm(page.confirmRegForm, async () => {
      this.showSuccess(intl.prep(intl.ID_TRADING_TIER_UPDATED))
      this.renewToggle.setState(this.confirmRegisterForm.tier > 0)
      await app().fetchUser()
      app().updateMenuItemsDisplay()
    }, () => {
      this.runAnimation(this.regAssetForm, page.regAssetForm)
    })
    this.confirmRegisterForm.setExchange(xc, '')

    this.walletWaitForm = new forms.WalletWaitForm(page.walletWait, () => {
      this.runAnimation(this.confirmRegisterForm, page.confirmRegForm)
    }, () => {
      this.runAnimation(this.regAssetForm, page.regAssetForm)
    })
    this.walletWaitForm.setExchange(xc)

    this.newWalletForm = new forms.NewWalletForm(
      page.newWalletForm,
      assetID => this.newWalletCreated(assetID, this.confirmRegisterForm.tier),
      () => this.runAnimation(this.regAssetForm, page.regAssetForm)
    )

    this.regAssetForm = new forms.FeeAssetSelectionForm(page.regAssetForm, async (assetID: number, tier: number) => {
      if (assetID === PrepaidBondID) {
        await app().fetchUser()
        this.updateReputation()
        this.showSuccess(intl.prep(intl.ID_TRADING_TIER_UPDATED))
        return
      }
      const asset = app().assets[assetID]
      const wallet = asset.wallet
      if (wallet) {
        const loaded = app().loading(page.regAssetForm)
        const bondsFeeBuffer = await this.getBondsFeeBuffer(assetID, page.regAssetForm)
        this.confirmRegisterForm.setAsset(assetID, tier, bondsFeeBuffer)
        loaded()
        this.progressTierFormsWithWallet(assetID, wallet)
        return
      }
      this.confirmRegisterForm.setAsset(assetID, tier, 0)
      this.newWalletForm.setAsset(assetID)
      this.showForm(page.newWalletForm)
    })
    this.regAssetForm.setExchange(xc, '')

    this.reputationMeter = new ReputationMeter(page.repMeter)
    this.reputationMeter.setHost(host)

    Doc.bind(page.exportDexBtn, 'click', () => this.exportAccount())

    this.accountDisabled = body.dataset.disabled === 'true'
    Doc.bind(page.toggleAccountStatusBtn, 'click', () => {
      if (!this.accountDisabled) this.prepareAccountDisable(page.disableAccountForm)
      else this.toggleAccountStatus(false)
    })
    Doc.bind(page.updateCertBtn, 'click', () => page.certFileInput.click())
    Doc.bind(page.updateHostBtn, 'click', () => this.prepareUpdateHost())
    Doc.bind(page.certFileInput, 'change', () => this.onCertFileChange())
    Doc.bind(page.goBackToSettings, 'click', () => app().loadPage('settings'))

    const showTierForm = () => {
      this.regAssetForm.setExchange(app().exchanges[host], '') // reset form
      this.showForm(page.regAssetForm)
    }
    Doc.bind(page.changeTier, 'click', () => { showTierForm() })
    const willAutoRenew = xc.auth.targetTier > 0
    this.renewToggle = new AniToggle(page.toggleAutoRenew, page.renewErr, willAutoRenew, async (newState: boolean) => {
      if (this.accountDisabled) return
      if (newState) showTierForm()
      else return this.disableAutoRenew()
    })
    Doc.bind(page.autoRenewBox, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      if (!this.accountDisabled) page.toggleAutoRenew.click()
    })

    page.penaltyComps.textContent = String(xc.auth.penaltyComps)
    const hideCompInput = () => {
      Doc.hide(page.penaltyCompInput)
      Doc.show(page.penaltyComps)
    }
    Doc.bind(page.penaltyCompBox, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      const xc = app().exchanges[this.host]
      page.penaltyCompInput.value = String(xc.auth.penaltyComps)
      Doc.hide(page.penaltyComps)
      Doc.show(page.penaltyCompInput)
      page.penaltyCompInput.focus()
      const checkClick = (e: MouseEvent) => {
        if (Doc.mouseInElement(e, page.penaltyCompBox)) return
        hideCompInput()
        Doc.unbind(document, 'click', checkClick)
      }
      Doc.bind(document, 'click', checkClick)
    })

    Doc.bind(page.penaltyCompInput, 'keyup', async (e: KeyboardEvent) => {
      Doc.hide(page.penaltyCompsErr)
      if (e.key === 'Escape') {
        hideCompInput()
        return
      }
      if (!(e.key === 'Enter')) return
      const penaltyComps = parseInt(page.penaltyCompInput.value || '')
      if (isNaN(penaltyComps)) {
        Doc.show(page.penaltyCompsErr)
        page.penaltyCompsErr.textContent = intl.prep(intl.ID_INVALID_COMPS_VALUE)
        return
      }
      const loaded = app().loading(page.otherBondSettings)
      try {
        await this.updateBondOptions({ penaltyComps })
        loaded()
        page.penaltyComps.textContent = String(penaltyComps)
      } catch (e) {
        loaded()
        Doc.show(page.penaltyCompsErr)
        page.penaltyCompsErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: e.msg })
      }
      hideCompInput()
    })

    this.dexAddrForm = new forms.DEXAddressForm(page.dexAddrForm, async (xc: Exchange) => {
      window.location.assign(`/dexsettings/${xc.host}`)
    }, this.host)

    // forms.bind(page.bondDetailsForm, page.updateBondOptionsConfirm, () => this.updateBondOptions())
    forms.bind(page.disableAccountForm, page.disableAccountConfirm, () => this.toggleAccountStatus(true))

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        this.closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    Doc.applySelector(page.forms, '.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })

    app().registerNoteFeeder({
      conn: () => { this.setConnectionStatus() },
      reputation: () => { this.updateReputation() },
      feepayment: () => { this.updateReputation() },
      bondpost: () => { this.updateReputation() }
    })

    this.setConnectionStatus()
    this.updateReputation()
  }

  unload () {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  async progressTierFormsWithWallet (assetID: number, wallet: WalletState) {
    const { page, confirmRegisterForm: { fees } } = this
    const asset = app().assets[assetID]
    const { bondAssets } = this.regAssetForm.xc
    const bondAsset = bondAssets[asset.symbol]
    if (!wallet.open) {
      const loaded = app().loading(page.forms)
      const res = await postJSON('/api/openwallet', { assetID: assetID })
      loaded()
      if (!app().checkResponse(res)) {
        this.regAssetForm.setAssetError(`error unlocking wallet: ${res.msg}`)
        this.runAnimation(this.regAssetForm, page.regAssetForm)
      }
      return
    }
    if (wallet.synced && wallet.balance.available >= 2 * bondAsset.amount + fees) {
      // If we are raising our tier, we'll show a confirmation form
      this.progressTierFormWithSyncedFundedWallet(assetID)
      return
    }
    this.walletWaitForm.setWallet(assetID, fees, this.confirmRegisterForm.tier)
    this.showForm(page.walletWait)
  }

  async progressTierFormWithSyncedFundedWallet (bondAssetID: number) {
    const xc = app().exchanges[this.host]
    const targetTier = this.confirmRegisterForm.tier
    const page = this.page
    const strongTier = xc.auth.liveStrength + xc.auth.pendingStrength - xc.auth.weakStrength
    if (targetTier > xc.auth.targetTier && targetTier > strongTier) {
      this.runAnimation(this.confirmRegisterForm, page.confirmRegForm)
      return
    }
    // Lowering tier
    const loaded = app().loading(this.body)
    try {
      await this.updateBondOptions({ bondAssetID, targetTier })
      loaded()
    } catch (e) {
      loaded()
      this.regAssetForm.setTierError(e.msg)
      return
    }
    // this.animateConfirmForm(page.regAssetForm)
    this.showSuccess(intl.prep(intl.ID_TRADING_TIER_UPDATED))
  }

  updateReputation () {
    const page = this.page
    const auth = app().exchanges[this.host].auth
    const { rep: { penalties }, targetTier, expiredBonds } = auth
    const displayTier = strongTier(auth)
    page.targetTier.textContent = String(targetTier)
    page.effectiveTier.textContent = String(displayTier)
    page.penalties.textContent = String(penalties)
    page.bondsPendingRefund.textContent = `${expiredBonds?.length || 0}`
    this.reputationMeter.update()
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement) {
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

  async runAnimation (ani: Animator, form: PageElement) {
    Doc.hide(this.currentForm)
    await ani.animate()
    this.currentForm = form
    Doc.show(form)
  }

  closePopups () {
    Doc.hide(this.page.forms)
    if (this.animation) this.animation.stop()
  }

  async showSuccess (msg: string) {
    this.forms.forEach(form => Doc.hide(form))
    this.currentForm = this.page.checkmarkForm
    this.animation = forms.showSuccess(this.page, msg)
    await this.animation.wait()
    this.animation = new Animation(1500, () => { /* pass */ }, '', () => {
      if (this.currentForm === this.page.checkmarkForm) this.closePopups()
    })
  }

  // exportAccount exports and downloads the account info.
  async exportAccount () {
    const { page, host } = this
    const req = { host }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/exportaccount', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.exportAccountErr.textContent = res.msg
      Doc.show(page.exportAccountErr)
      return
    }
    res.account.bonds = res.bonds // maintain backward compat of JSON file
    const accountForExport = JSON.parse(JSON.stringify(res.account))
    const a = document.createElement('a')
    a.setAttribute('download', 'dcrAccount-' + host + '.json')
    a.setAttribute('href', 'data:text/json,' + JSON.stringify(accountForExport, null, 2))
    a.click()
    Doc.hide(page.forms)
  }

  // toggleAccountStatus enables or disables the account associated with the
  // provided host.
  async toggleAccountStatus (disable:boolean) {
    const page = this.page
    Doc.hide(page.errMsg)
    let host: string|null = this.host
    if (disable) host = page.disableAccountHost.textContent
    const req = { host, disable: disable }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/toggleaccountstatus', req)
    loaded()
    if (!app().checkResponse(res)) {
      if (disable) {
        page.disableAccountErr.textContent = res.msg
        Doc.show(page.disableAccountErr)
      } else {
        page.errMsg.textContent = res.msg
        Doc.show(page.errMsg)
      }
      return
    }
    if (disable) {
      this.page.toggleAccountStatusBtn.textContent = intl.prep(intl.ID_ENABLE_ACCOUNT)
      Doc.hide(page.forms)
    } else {
      this.page.toggleAccountStatusBtn.textContent = intl.prep(intl.ID_DISABLE_ACCOUNT)
    }
    this.accountDisabled = disable
    window.location.assign(`/dexsettings/${host}`)
  }

  async prepareAccountDisable (disableAccountForm: HTMLElement) {
    const page = this.page
    page.disableAccountHost.textContent = this.host
    page.disableAccountErr.textContent = ''
    this.showForm(disableAccountForm)
  }

  // Retrieve an estimate for the tx fee needed to create new bond reserves.
  async getBondsFeeBuffer (assetID: number, form: HTMLElement) {
    const loaded = app().loading(form)
    const res = await postJSON('/api/bondsfeebuffer', { assetID })
    loaded()
    if (!app().checkResponse(res)) {
      return 0
    }
    return res.feeBuffer
  }

  async prepareUpdateHost () {
    const page = this.page
    this.dexAddrForm.refresh()
    this.showForm(page.dexAddrForm)
  }

  async onCertFileChange () {
    const page = this.page
    Doc.hide(page.errMsg)
    const files = page.certFileInput.files
    let cert
    if (files && files.length) cert = await files[0].text()
    if (!cert) return
    const req = { host: this.host, cert: cert }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/updatecert', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
    } else {
      Doc.show(page.updateCertMsg)
      setTimeout(() => { Doc.hide(page.updateCertMsg) }, 5000)
    }
  }

  setConnectionStatus () {
    const page = this.page
    const exchange = app().user.exchanges[this.host]
    const displayIcons = (connected: boolean) => {
      if (connected) {
        Doc.hide(page.disconnectedIcon)
        Doc.show(page.connectedIcon)
      } else {
        Doc.show(page.disconnectedIcon)
        Doc.hide(page.connectedIcon)
      }
    }
    if (exchange) {
      switch (exchange.connectionStatus) {
        case ConnectionStatus.Connected:
          displayIcons(true)
          page.connectionStatus.textContent = intl.prep(intl.ID_CONNECTED)
          break
        case ConnectionStatus.Disconnected:
          displayIcons(false)
          if (this.accountDisabled) page.connectionStatus.textContent = intl.prep(intl.ID_ACCOUNT_DISABLED_MSG)
          else page.connectionStatus.textContent = intl.prep(intl.ID_DISCONNECTED)
          break
        case ConnectionStatus.InvalidCert:
          displayIcons(false)
          page.connectionStatus.textContent = `${intl.prep(intl.ID_DISCONNECTED)} - ${intl.prep(intl.ID_INVALID_CERTIFICATE)}`
      }
    }
  }

  async disableAutoRenew () {
    const loaded = app().loading(this.page.otherBondSettings)
    try {
      this.updateBondOptions({ targetTier: 0 })
      loaded()
    } catch (e) {
      loaded()
      throw e
    }
  }

  /*
   * updateBondOptions is called when the form to update bond options is
   * submitted.
   */
  async updateBondOptions (conf: BondOptionsForm): Promise<any> {
    conf.host = this.host
    await postJSON('/api/updatebondoptions', conf)
    const targetTier = conf.targetTier ?? app().exchanges[this.host].auth.targetTier
    this.renewToggle.setState(targetTier > 0)
  }

  async newWalletCreated (assetID: number, tier: number) {
    this.regAssetForm.refresh()
    const user = await app().fetchUser()
    if (!user) return
    const page = this.page
    const asset = user.assets[assetID]
    const wallet = asset.wallet
    const xc = app().exchanges[this.host]
    const bondAmt = xc.bondAssets[asset.symbol].amount

    const bondsFeeBuffer = await this.getBondsFeeBuffer(assetID, page.newWalletForm)
    this.confirmRegisterForm.setFees(assetID, bondsFeeBuffer)

    if (wallet.synced && wallet.balance.available >= 2 * bondAmt + bondsFeeBuffer) {
      this.progressTierFormWithSyncedFundedWallet(assetID)
      return
    }

    this.walletWaitForm.setWallet(assetID, bondsFeeBuffer, tier)
    await this.showForm(page.walletWait)
  }
}
