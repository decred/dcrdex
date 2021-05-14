import { app } from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, DEXAddressForm, ConfirmRegistrationForm, bind as bindForm } from './forms'
import * as intl from './locales'

const animationLength = 300

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

    this.walletForm = new NewWalletForm(page.newWalletForm, assetID => { this.newWalletCreated(assetID) },
      this.pwCache, () => this.changeForm(page.confirmRegForm))
    // ADD DEX
    this.dexAddrForm = new DEXAddressForm(page.dexAddrForm, async (xc) => {
      this.confirmRegisterForm.setExchange(xc)
      this.currentDEX = xc
      await this.changeForm(page.confirmRegForm)
    }, this.pwCache)

    // SUBMIT DEX REGISTRATION
    this.confirmRegisterForm = new ConfirmRegistrationForm(
      page.confirmRegForm,
      {
        getCertFile: () => this.getCertFile(),
        getDexAddr: () => this.getDexAddr()
      },
      () => this.registerDEXSuccess(),
      (msg) => this.registerDEXFundsFail(msg),
      (assetId) => {
        this.walletForm.setAsset(assetId)
        this.walletForm.loadDefaults()
        this.changeForm(page.newWalletForm)
      })

    Doc.bind(page.depoWaitClose, 'click', () => {
      if (this.balanceTimer) {
        clearInterval(this.balanceTimer)
        this.balanceTimer = null
        this.changeForm(page.confirmRegForm)
      }
    })
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

  /* Swap this currently displayed form1 for form2 with an animation. */
  async changeForm (newForm) {
    const oldForm = this.currentForm
    this.currentForm = newForm
    const shift = this.body.offsetWidth / 2
    await Doc.animate(animationLength, progress => {
      oldForm.style.right = `${progress * shift}px`
    }, 'easeInHard')
    Doc.hide(oldForm)
    oldForm.style.right = '0'
    newForm.style.right = -shift
    Doc.show(newForm)
    if (newForm.querySelector('input')) {
      newForm.querySelector('input').focus()
    }
    await Doc.animate(animationLength, progress => {
      newForm.style.right = `${-shift + progress * shift}px`
    }, 'easeOutHard')
    newForm.style.right = '0'
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
    this.walletForm.refresh()
    this.dexAddrForm.refresh()
    await this.changeForm(page.dexAddrForm)
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

  /* Called when dex registration failed due to insufficient funds. */
  async registerDEXFundsFail (msg) {
    const page = this.page
    page.regFundsErr.textContent = msg
    Doc.show(page.regFundsErr)
    await this.changeForm(page.failedRegForm)
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
