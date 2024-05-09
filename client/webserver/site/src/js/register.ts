import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import {
  NewWalletForm,
  DEXAddressForm,
  DiscoverAccountForm,
  LoginForm,
  ConfirmRegistrationForm,
  FeeAssetSelectionForm,
  WalletWaitForm,
  slideSwap,
  AppPassResetForm
} from './forms'
import {
  app,
  PasswordCache,
  Exchange,
  PageElement,
  PrepaidBondID
} from './registry'
import State from './state'

interface RegistrationPageData {
  host: string
  backTo?: string
}

export default class RegistrationPage extends BasePage {
  body: HTMLElement
  data: RegistrationPageData
  pwCache: PasswordCache
  xc: Exchange
  page: Record<string, PageElement>
  loginForm: LoginForm
  appPassResetForm: AppPassResetForm
  dexAddrForm: DEXAddressForm
  discoverAcctForm: DiscoverAccountForm
  newWalletForm: NewWalletForm
  regAssetForm: FeeAssetSelectionForm
  walletWaitForm: WalletWaitForm
  confirmRegisterForm: ConfirmRegistrationForm

  constructor (body: HTMLElement, data: RegistrationPageData) {
    super()
    this.body = body
    this.data = data
    this.pwCache = { pw: '' }
    const page = this.page = Doc.idDescendants(body)

    if (data.host && page.dexAddrForm.classList.contains('selected')) {
      page.dexAddrForm.classList.remove('selected')
      page.discoverAcctForm.classList.add('selected')
      page.discoverAcctForm.dataset.host = data.host
    }

    this.loginForm = new LoginForm(page.loginForm, async () => {
      if (this.discoverAcctForm) {
        this.discoverAcctForm.refresh()
        slideSwap(page.loginForm, page.discoverAcctForm)
      } else {
        this.dexAddrForm.refresh()
        slideSwap(page.loginForm, page.dexAddrForm)
      }
    }, this.pwCache)

    const prepAndDisplayLoginForm = () => {
      Doc.hide(page.resetAppPWForm)
      this.loginForm.refresh()
      Doc.show(page.loginForm)
      this.loginForm.focus()
    }

    this.appPassResetForm = new AppPassResetForm(page.resetAppPWForm, () => { prepAndDisplayLoginForm() })
    Doc.bind(page.forgotPassBtn, 'click', () => slideSwap(page.loginForm, page.resetAppPWForm))
    Doc.bind(page.resetPassFormCloser, 'click', () => { prepAndDisplayLoginForm() })
    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, page.resetAppPWForm) && Doc.isDisplayed(page.resetAppPWForm)) { prepAndDisplayLoginForm() }
    })

    // Hide the form closers for the registration process except for the
    // password reset form closer.
    for (const el of body.querySelectorAll('.form-closer')) if (el !== page.resetPassFormCloser) Doc.hide(el)

    this.newWalletForm = new NewWalletForm(
      page.newWalletForm,
      assetID => this.newWalletCreated(assetID, this.confirmRegisterForm.tier),
      this.pwCache,
      () => this.animateRegAsset(page.newWalletForm)
    )

    // ADD DEX
    this.dexAddrForm = new DEXAddressForm(page.dexAddrForm, async (xc, certFile) => {
      this.requestFeepayment(page.dexAddrForm, xc, certFile)
    }, this.pwCache)

    const addr = page.discoverAcctForm.dataset.host
    if (addr) {
      this.discoverAcctForm = new DiscoverAccountForm(page.discoverAcctForm, addr, async (xc) => {
        this.requestFeepayment(page.discoverAcctForm, xc, '')
      }, this.pwCache)
    }

    // SELECT REG ASSET
    this.regAssetForm = new FeeAssetSelectionForm(page.regAssetForm, async (assetID: number, tier: number) => {
      if (assetID === PrepaidBondID) {
        this.registerDEXSuccess()
        return
      }
      const asset = app().assets[assetID]
      const wallet = asset.wallet
      if (wallet) {
        const bondAsset = this.xc.bondAssets[asset.symbol]
        const bondsFeeBuffer = await this.getBondsFeeBuffer(assetID, page.regAssetForm)
        this.confirmRegisterForm.setAsset(assetID, tier, bondsFeeBuffer)
        if (wallet.synced && wallet.balance.available >= 2 * bondAsset.amount + bondsFeeBuffer) {
          this.animateConfirmForm(page.regAssetForm)
          return
        }
        this.walletWaitForm.setWallet(assetID, bondsFeeBuffer, tier)
        slideSwap(page.regAssetForm, page.walletWait)
        return
      }
      this.confirmRegisterForm.tier = tier
      this.newWalletForm.setAsset(assetID)
      slideSwap(page.regAssetForm, page.newWalletForm)
    }, this.pwCache)

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
        break
      case page.discoverAcctForm:
        this.discoverAcctForm.animate()
    }
    Doc.show(currentForm)

    // There's nothing on the page.discoverAcctForm except to receive user pass
    // before attempting to discover user account and there's no need to have
    // them click another button when we can carry on without user interaction.
    if (currentForm === page.discoverAcctForm && (State.passwordIsCached() || this.pwCache?.pw)) {
      this.discoverAcctForm.page.submit.click()
    }

    if (app().authed) this.auth()
  }

  unload () {
    this.pwCache.pw = ''
  }

  // auth should be called once user is known to be authed with the server.
  async auth () {
    await app().fetchUser()
  }

  async requestFeepayment (oldForm: HTMLElement, xc: Exchange, certFile: string) {
    this.xc = xc
    this.confirmRegisterForm.setExchange(xc, certFile)
    this.walletWaitForm.setExchange(xc)
    this.regAssetForm.setExchange(xc, certFile)
    this.animateRegAsset(oldForm)
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
    await app().loadPage(this.data.backTo || 'markets')
  }

  async newWalletCreated (assetID: number, tier: number) {
    this.regAssetForm.refresh()
    const user = await app().fetchUser()
    if (!user) return
    const page = this.page
    const asset = user.assets[assetID]
    const wallet = asset.wallet
    const bondAmt = this.xc.bondAssets[asset.symbol].amount

    const bondsFeeBuffer = await this.getBondsFeeBuffer(assetID, page.newWalletForm)
    this.walletWaitForm.setWallet(assetID, bondsFeeBuffer, tier)
    this.confirmRegisterForm.setAsset(assetID, tier, bondsFeeBuffer)
    if (wallet.synced && wallet.balance.available >= 2 * bondAmt + bondsFeeBuffer) {
      await this.animateConfirmForm(page.newWalletForm)
      return
    }

    await slideSwap(page.newWalletForm, page.walletWait)
  }
}
