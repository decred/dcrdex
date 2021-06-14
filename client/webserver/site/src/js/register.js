import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, bindOpenWallet, bind as bindForm } from './forms'
import { feeSendErr } from './constants'

const DCR_ID = 42
const animationLength = 300

let app

export default class RegistrationPage extends BasePage {
  constructor (application, body) {
    super()
    app = application
    this.body = body
    this.notifiers = {}
    const page = this.page = Doc.parsePage(body, [
      // Form 1: Set the application password
      'appPWForm', 'appPW', 'appPWAgain', 'appPWSubmit', 'appPWErrMsg',
      // Form 2: Create Decred wallet
      'newWalletForm',
      // Form 3: Unlock Decred wallet
      'unlockWalletForm',
      // Form 4: Configure DEX server
      'dexAddrForm', 'dexAddr', 'certFile', 'selectedCert', 'removeCert',
      'addCert', 'submitDEXAddr', 'dexAddrErr', 'dexCertFile', 'dexNeedCert',
      'dexShowMore',
      // Form 5: Confirm DEX registration and pay fee
      'confirmRegForm', 'feeDisplay', 'dexDCRLotSize', 'appPass', 'submitConfirm', 'regErr',
      'dexCertBox', 'failedRegForm', 'regFundsErr'
    ])

    // Hide the form closers for the registration process.
    body.querySelectorAll('.form-closer').forEach(el => Doc.hide(el))

    // SET APP PASSWORD
    bindForm(page.appPWForm, page.appPWSubmit, () => this.setAppPass())

    // NEW DCR WALLET
    // This form is only shown if there is no DCR wallet yet.
    this.walletForm = new NewWalletForm(app, page.newWalletForm, () => {
      this.changeForm(page.newWalletForm, page.dexAddrForm)
    })

    // OPEN DCR WALLET
    // This form is only shown if there is a wallet, but it's not open.
    bindOpenWallet(app, page.unlockWalletForm, () => {
      this.changeForm(page.unlockWalletForm, page.dexAddrForm)
    })

    // ADD DEX
    // tls certificate upload
    this.defaultTLSText = 'none selected'
    page.selectedCert.textContent = this.defaultTLSText
    Doc.bind(page.certFile, 'change', () => this.readCert())
    Doc.bind(page.removeCert, 'click', () => this.resetCert())
    Doc.bind(page.addCert, 'click', () => page.certFile.click())
    bindForm(page.dexAddrForm, page.submitDEXAddr, () => this.checkDEX())
    Doc.bind(page.dexShowMore, 'click', () => {
      Doc.hide(page.dexShowMore)
      Doc.show(page.dexCertBox)
    })

    // SUBMIT DEX REGISTRATION
    bindForm(page.confirmRegForm, page.submitConfirm, () => this.registerDEX())

    // Attempt to load the dcrwallet configuration from the default location.
    if (app.user.authed) this.auth()
  }

  // auth should be called once user is known to be authed with the server.
  async auth () {
    await app.fetchUser()
    this.walletForm.setAsset(app.assets[DCR_ID])
    this.page.unlockWalletForm.setAsset(app.assets[DCR_ID])
    this.walletForm.loadDefaults()
  }

  /* Swap this currently displayed form1 for form2 with an animation. */
  async changeForm (form1, form2) {
    const shift = this.body.offsetWidth / 2
    await Doc.animate(animationLength, progress => {
      form1.style.right = `${progress * shift}px`
    }, 'easeInHard')
    Doc.hide(form1)
    form1.style.right = '0'
    form2.style.right = -shift
    Doc.show(form2)
    if (form2.querySelector('input')) {
      form2.querySelector('input').focus()
    }
    await Doc.animate(animationLength, progress => {
      form2.style.right = `${-shift + progress * shift}px`
    }, 'easeOutHard')
    form2.style.right = '0'
  }

  /* Set the application password. Attached to form submission. */
  async setAppPass () {
    const page = this.page
    Doc.hide(page.appPWErrMsg)
    const pw = page.appPW.value
    const pwAgain = page.appPWAgain.value
    if (pw === '') {
      page.appPWErrMsg.textContent = 'password cannot be empty'
      Doc.show(page.appPWErrMsg)
      return
    }
    if (pw !== pwAgain) {
      page.appPWErrMsg.textContent = 'passwords do not match'
      Doc.show(page.appPWErrMsg)
      return
    }
    // Clear the notification cache. Useful for development purposes, since
    // the Application will only clear them on login, which would leave old
    // browser-cached notifications in place after registering even if the
    // client db is wiped.
    app.setNotes([])
    page.appPW.value = ''
    page.appPWAgain.value = ''
    const loaded = app.loading(page.appPWForm)
    const res = await postJSON('/api/init', { pass: pw })
    loaded()
    if (!app.checkResponse(res)) {
      page.appErrMsg.textContent = res.msg
      Doc.show(page.appErrMsg)
      return
    }
    this.auth()
    app.updateMenuItemsDisplay()
    this.changeForm(page.appPWForm, page.newWalletForm)
  }

  /* Get the reg fees for the DEX. */
  async checkDEX () {
    const page = this.page
    Doc.hide(page.dexAddrErr)
    const addr = page.dexAddr.value
    if (addr === '') {
      page.dexAddrErr.textContent = 'DEX address cannot be empty'
      Doc.show(page.dexAddrErr)
      return
    }

    let cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }

    const loaded = app.loading(page.dexAddrForm)
    const res = await postJSON('/api/getdexinfo', {
      addr: addr,
      cert: cert
    })
    loaded()
    if (!app.checkResponse(res, true)) {
      if (res.msg === 'certificate required') {
        Doc.hide(page.dexShowMore)
        Doc.show(page.dexCertBox, page.dexNeedCert)
      } else {
        page.dexAddrErr.textContent = res.msg
        Doc.show(page.dexAddrErr)
      }

      return
    }
    this.fee = res.xc.feeAsset.amount
    const balanceFeeRegistration = app.user.assets[DCR_ID].wallet.balance.available
    if (balanceFeeRegistration < this.fee) {
      await this.changeForm(page.dexAddrForm, page.failedRegForm)
      page.regFundsErr.textContent = `Looks like there is not enough funds for
       paying the registration fee. Amount needed:
       ${Doc.formatCoinValue(this.fee / 1e8)} Amount available:
       ${Doc.formatCoinValue(balanceFeeRegistration / 1e8)}.

       Deposit funds and try again.`
      Doc.show(page.regFundsErr)
      return
    }

    page.feeDisplay.textContent = Doc.formatCoinValue(this.fee / 1e8)
    const dcrAsset = res.xc.assets['42']
    if (dcrAsset) page.dexDCRLotSize.textContent = Doc.formatCoinValue(dcrAsset.lotSize / 1e8)
    await this.changeForm(page.dexAddrForm, page.confirmRegForm)
  }

  /* Authorize DEX registration. */
  async registerDEX () {
    const page = this.page
    Doc.hide(page.regErr)
    let cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }
    const registration = {
      addr: page.dexAddr.value,
      pass: page.appPass.value,
      fee: this.fee,
      cert: cert
    }
    page.appPass.value = ''
    const loaded = app.loading(page.confirmRegForm)
    const res = await postJSON('/api/register', registration)
    loaded()
    if (!app.checkResponse(res)) {
      // show different form with no passphrase input in case of no funds.
      if (res.code === feeSendErr) {
        await this.changeForm(page.confirmRegForm, page.failedRegForm)
        page.regFundsErr.textContent = res.msg
        Doc.show(page.regFundsErr)
        return
      }

      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      return
    }
    // Need to get a fresh market list. May consider handling this with a
    // websocket update instead.
    await app.fetchUser()
    app.loadPage('markets')
  }

  async readCert () {
    const page = this.page
    const files = page.certFile.files
    if (!files.length) return
    page.selectedCert.textContent = files[0].name
    Doc.show(page.removeCert)
    Doc.hide(page.addCert)
  }

  resetCert () {
    const page = this.page
    page.certFile.value = ''
    page.selectedCert.textContent = this.defaultTLSText
    Doc.hide(page.removeCert)
    Doc.show(page.addCert)
  }
}
