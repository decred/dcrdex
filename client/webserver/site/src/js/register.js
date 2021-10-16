import { app } from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, DEXAddressForm, LoginForm, ConfirmRegistrationForm, FeeAssetSelectionForm, slideSwap, bind as bindForm } from './forms'
import * as intl from './locales'

export default class RegistrationPage extends BasePage {
  constructor (body) {
    super()
    this.body = body
    this.pwCache = {}
    this.currentDEX = null
    this.currentForm = body.querySelector('form:not(.d-hide)')
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

    this.newWalletForm = new NewWalletForm(page.newWalletForm, () => {
      this.regAssetForm.refresh()
      slideSwap(page.newWalletForm, page.confirmRegForm)
    }, this.pwCache, () => this.animateRegAsset(page.newWalletForm))

    // ADD DEX
    this.dexAddrForm = new DEXAddressForm(page.dexAddrForm, async (xc, certFile) => {
      this.confirmRegisterForm.setExchange(xc, certFile)
      this.regAssetForm.setExchange(xc)
      this.animateRegAsset(page.dexAddrForm)
    }, this.pwCache)

    // SELECT REG ASSET
    this.regAssetForm = new FeeAssetSelectionForm(page.regAssetForm, assetID => {
      const asset = app().assets[assetID]
      this.confirmRegisterForm.setAsset(assetID)
      if (asset.wallet) {
        // TODO: After #1230 is in, we'll want to check the balance here and
        // show the low balance modal if appropriate.
        this.animateConfirmForm(page.regAssetForm)
        return
      }
      this.newWalletForm.setAsset(assetID)
      this.newWalletForm.loadDefaults()
      slideSwap(page.regAssetForm, page.newWalletForm)
    })

    // TODO: handle wallet syncing
    // Doc.bind(page.depoWaitClose, 'click', () => {
    //   if (this.balanceTimer) {
    //     clearInterval(this.balanceTimer)
    //     this.balanceTimer = null
    //     this.changeForm(page.confirmRegForm)
    //   }
    // })

    // SUBMIT DEX REGISTRATION
    this.confirmRegisterForm = new ConfirmRegistrationForm(page.confirmRegForm, () => {
      this.registerDEXSuccess()
    }, () => {
      this.animateRegAsset(page.confirmRegForm)
    }, this.pwCache)

    const currentForm = page.forms.querySelector(':scope > form:not(.d-hide)')
    switch (currentForm) {
      case page.loginForm:
        this.loginForm.animate()
        break
      case page.dexAddrForm:
        this.dexAddrForm.animate()
    }

    // Attempt to load the dcrwallet configuration from the default location.
    if (app().user.authed) this.auth()
    this.notifiers = {
      walletstate: note => this.confirmRegisterForm.handleWalletStateNote(note)
    }
  }

  unload () {
    delete this.pwCache.pw
  }

  // auth should be called once user is known to be authed with the server.
  async auth () {
    await app().fetchUser()
  }

  /* Swap in the asset selection form and run the animation. */
  async animateRegAsset (oldForm) {
    Doc.hide(oldForm)
    this.regAssetForm.animate()
    Doc.show(this.page.regAssetForm)
  }

  /* Swap in the confirmation form and run the animation. */
  async animateConfirmForm (oldForm) {
    this.confirmRegisterForm.animate()
    Doc.hide(oldForm)
    Doc.show(this.page.confirmRegForm)
  }

  /* Set the application password. Attached to form submission. */
  async setAppPass () {
    const page = this.page
    Doc.hide(page.appPWErrMsg)
    const pw = page.appPW.value
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
      cert = await this.dexAddrForm.page.certFile.files[0].text()
    }
    return cert
  }

  /* gets the dex address input by the user */
  getDexAddr () {
    return this.page.dexAddr.value
  }

  /* Called after successful registration to a DEX. */
  async registerDEXSuccess () {
    await app().fetchUser()
    app().loadPage('markets')
  }

  async newWalletCreated (assetID) {
    this.confirmRegisterForm.refresh()
    this.confirmRegisterForm.selectRow(assetID)
    const user = await app().fetchUser()
    const page = this.page
    const asset = user.assets[assetID]
    const wallet = asset.wallet
    const bal = wallet.balance
    const regAmt = this.currentDEX.regFees[asset.symbol].amount
    if (bal.available >= regAmt) {
      this.changeForm(page.confirmRegForm)
      return
    }

    page.depoWaitFee = `${Doc.formatCoinValue(regAmt / 1e8)} ${asset.symbol}`
    page.depoAddr.textContent = wallet.address
    page.depoWaitBal.textContent = `${Doc.formatCoinValue(bal.available / 1e8)} ${asset.symbol}`
    page.depoRefresh.textContent = '15'

    const enough = async (fetchNew) => {
      const bal = fetchNew ? await app().fetchBalance(assetID) : user.assets[assetID].wallet.balance
      page.depoWaitBal.textContent = `${Doc.formatCoinValue(bal.available / 1e8)} ${asset.symbol}`
      if (bal.available < regAmt) return false
      clearInterval(this.balanceTimer)
      this.changeForm(page.confirmRegForm)
      return true
    }

    this.balanceTimer = setInterval(() => {
      let remaining = parseInt(page.depoRefresh.textContent)
      remaining--
      if (remaining > 0) {
        enough(false)
        page.depoRefresh.textContent = remaining
        return
      }
      enough(true)
      page.depoRefresh.textContent = '15'
    }, 1000)

    this.changeForm(page.depoWait)
  }
}
