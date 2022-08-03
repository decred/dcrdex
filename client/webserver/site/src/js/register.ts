import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import {
  NewWalletForm,
  DEXAddressForm,
  LoginForm,
  ConfirmRegistrationForm,
  FeeAssetSelectionForm,
  WalletWaitForm,
  slideSwap,
  bind as bindForm
} from './forms'
import * as intl from './locales'
import {
  app,
  PasswordCache,
  Exchange,
  PageElement
} from './registry'

export default class RegistrationPage extends BasePage {
  body: HTMLElement
  pwCache: PasswordCache
  currentDEX: Exchange
  page: Record<string, PageElement>
  loginForm: LoginForm
  dexAddrForm: DEXAddressForm
  newWalletForm: NewWalletForm
  regAssetForm: FeeAssetSelectionForm
  walletWaitForm: WalletWaitForm
  confirmRegisterForm: ConfirmRegistrationForm

  constructor (body: HTMLElement) {
    super()
    this.body = body
    this.pwCache = { pw: '' }
    const page = this.page = Doc.idDescendants(body)

    // Hide the form closers for the registration process.
    body.querySelectorAll('.form-closer').forEach(el => Doc.hide(el))

    // SET APP PASSWORD
    bindForm(page.appPWForm, page.appPWSubmit, () => this.setAppPass())
    Doc.bind(page.showSeedRestore, 'click', () => {
      Doc.show(page.seedRestore)
      Doc.hide(page.showSeedRestore)
    })

    this.loginForm = new LoginForm(page.loginForm, async () => {
      await app().fetchUser()
      this.dexAddrForm.refresh()
      slideSwap(page.loginForm, page.dexAddrForm)
    }, this.pwCache)

    this.newWalletForm = new NewWalletForm(
      page.newWalletForm,
      assetID => this.newWalletCreated(assetID),
      this.pwCache,
      () => this.animateRegAsset(page.newWalletForm)
    )

    // ADD DEX
    this.dexAddrForm = new DEXAddressForm(page.dexAddrForm, async (xc, certFile) => {
      this.currentDEX = xc
      this.confirmRegisterForm.setExchange(xc, certFile)
      this.walletWaitForm.setExchange(xc)
      this.regAssetForm.setExchange(xc)
      this.animateRegAsset(page.dexAddrForm)
    }, this.pwCache)

    // SELECT REG ASSET
    this.regAssetForm = new FeeAssetSelectionForm(page.regAssetForm, async assetID => {
      this.confirmRegisterForm.setAsset(assetID)

      const asset = app().assets[assetID]
      const wallet = asset.wallet
      if (wallet) {
        const fee = this.currentDEX.regFees[asset.symbol]
        if (wallet.synced && wallet.balance.available > fee.amount) {
          this.animateConfirmForm(page.regAssetForm)
          return
        }
        const txFee = await this.getRegistrationTxFeeEstimate(assetID, page.regAssetForm)
        this.walletWaitForm.setWallet(wallet, txFee)
        slideSwap(page.regAssetForm, page.walletWait)
        return
      }
      this.newWalletForm.setAsset(assetID)
      slideSwap(page.regAssetForm, page.newWalletForm)
    })

    this.walletWaitForm = new WalletWaitForm(page.walletWait, () => {
      this.animateConfirmForm(page.walletWait)
    }, () => { this.animateRegAsset(page.walletWait) })

    // SUBMIT DEX REGISTRATION
    this.confirmRegisterForm = new ConfirmRegistrationForm(page.confirmRegForm, () => {
      this.registerDEXSuccess()
    }, () => {
      this.animateRegAsset(page.confirmRegForm)
    }, this.pwCache)

    const currentForm = Doc.safeSelector(page.forms, ':scope > form.selected')
    currentForm.classList.remove('selected')
    switch (currentForm) {
      case page.loginForm:
        this.loginForm.animate()
        break
      case page.dexAddrForm:
        this.dexAddrForm.animate()
    }
    Doc.show(currentForm)

    // Attempt to load the dcrwallet configuration from the default location.
    if (app().user.authed) this.auth()
  }

  unload () {
    this.pwCache.pw = ''
  }

  // auth should be called once user is known to be authed with the server.
  async auth () {
    await app().fetchUser()
  }

  /* Swap in the asset selection form and run the animation. */
  async animateRegAsset (oldForm: HTMLElement) {
    Doc.hide(oldForm)
    this.regAssetForm.animate()
    Doc.show(this.page.regAssetForm)
  }

  /* Swap in the confirmation form and run the animation. */
  async animateConfirmForm (oldForm: HTMLElement) {
    this.confirmRegisterForm.animate()
    Doc.hide(oldForm)
    Doc.show(this.page.confirmRegForm)
  }

  // Retrieve an estimate for the tx fee needed to pay the registration fee.
  async getRegistrationTxFeeEstimate (assetID: number, form: HTMLElement) {
    const cert = await this.getCertFile()
    const loaded = app().loading(form)
    const res = await postJSON('/api/regtxfee', {
      addr: this.currentDEX.host,
      cert: cert,
      asset: assetID
    })
    loaded()
    if (!app().checkResponse(res, true)) {
      return 0
    }
    return res.txfee
  }

  /* Set the application password. Attached to form submission. */
  async setAppPass () {
    const page = this.page
    Doc.hide(page.appPWErrMsg)
    const pw = page.appPW.value || ''
    const pwAgain = page.appPWAgain.value
    if (pw === '') {
      page.appPWErrMsg.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      Doc.show(page.appPWErrMsg)
      return
    }
    if (pw !== pwAgain) {
      page.appPWErrMsg.textContent = intl.prep(intl.ID_PASSWORD_NOT_MATCH)
      Doc.show(page.appPWErrMsg)
      return
    }

    // Clear the notification cache. Useful for development purposes, since
    // the Application will only clear them on login, which would leave old
    // browser-cached notifications in place after registering even if the
    // client db is wiped.
    app().setNotes([])
    page.appPW.value = ''
    page.appPWAgain.value = ''
    const loaded = app().loading(page.appPWForm)
    const seed = page.seedInput.value
    const rememberPass = page.rememberPass.checked
    const res = await postJSON('/api/init', {
      pass: pw,
      seed,
      rememberPass
    })
    loaded()
    if (!app().checkResponse(res)) {
      page.appPWErrMsg.textContent = res.msg
      Doc.show(page.appPWErrMsg)
      return
    }
    this.pwCache.pw = pw
    this.auth()
    app().updateMenuItemsDisplay()
    this.newWalletForm.refresh()
    this.dexAddrForm.refresh()
    await slideSwap(page.appPWForm, page.dexAddrForm)
  }

  /* gets the contents of the cert file */
  async getCertFile () {
    let cert = ''
    if (this.dexAddrForm.page.certFile.value) {
      const files = this.dexAddrForm.page.certFile.files
      if (files && files.length) cert = await files[0].text()
    }
    return cert
  }

  /* Called after successful registration to a DEX. */
  async registerDEXSuccess () {
    await app().fetchUser()
    await app().loadPage('markets')
  }

  async newWalletCreated (assetID: number) {
    this.regAssetForm.refresh()
    const user = await app().fetchUser()
    if (!user) return
    const page = this.page
    const asset = user.assets[assetID]
    const wallet = asset.wallet
    const feeAmt = this.currentDEX.regFees[asset.symbol].amount

    if (wallet.synced && wallet.balance.available > feeAmt) {
      await this.animateConfirmForm(page.newWalletForm)
      return
    }

    const txFee = await this.getRegistrationTxFeeEstimate(assetID, page.newWalletForm)
    this.walletWaitForm.setWallet(wallet, txFee)
    await slideSwap(page.newWalletForm, page.walletWait)
  }
}
