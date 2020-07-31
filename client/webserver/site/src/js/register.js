import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, bindOpenWallet, bind as bindForm } from './forms'

const DCR_ID = 42
const animationLength = 300

var app

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
      'dexAddrForm', 'dexAddr', 'certFile', 'selectedCert', 'removeCert', 'addCert',
      'submitDEXAddr', 'dexAddrErr',
      // Form 5: Confirm DEX registration and pay fee
      'confirmRegForm', 'feeDisplay', 'appPass', 'submitConfirm', 'regErr'
    ])

    // SET APP PASSWORD
    bindForm(page.appPWForm, page.appPWSubmit, () => { this.setAppPass() })

    // NEW DCR WALLET
    // This form is only shown if there is no DCR wallet yet.
    this.walletForm = new NewWalletForm(app, page.newWalletForm, () => {
      this.changeForm(page.newWalletForm, page.dexAddrForm)
    })
    this.walletForm.setAsset(app.assets[DCR_ID])

    // OPEN DCR WALLET
    // This form is only shown if there is a wallet, but it's not open.
    bindOpenWallet(app, page.unlockWalletForm, () => {
      this.changeForm(page.unlockWalletForm, page.dexAddrForm)
    })
    page.unlockWalletForm.setAsset(app.assets[DCR_ID])

    // ADD DEX
    // tls certificate upload
    this.defaultTLSText = 'none selected'
    page.selectedCert.textContent = this.defaultTLSText
    Doc.bind(page.certFile, 'change', () => this.readCert())
    Doc.bind(page.removeCert, 'click', () => this.resetCert())
    Doc.bind(page.addCert, 'click', () => this.page.certFile.click())
    bindForm(page.dexAddrForm, page.submitDEXAddr, () => { this.checkDEX() })

    // SUBMIT DEX REGISTRATION
    bindForm(page.confirmRegForm, page.submitConfirm, () => { this.registerDEX() })
  }

  /* Swap this currently displayed form1 for form2 with an animation. */
  async changeForm (form1, form2) {
    const shift = this.body.offsetWidth / 2
    await Doc.animate(animationLength, progress => {
      form1.style.right = `${progress * shift}px`
    }, 'easeInHard')
    Doc.hide(form1)
    form1.style.right = '0px'
    form2.style.right = -shift
    Doc.show(form2)
    form2.querySelector('input').focus()
    await Doc.animate(animationLength, progress => {
      form2.style.right = `${-shift + progress * shift}px`
    }, 'easeOutHard')
    form2.style.right = '0px'
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
    app.loading(page.appPWForm)
    var res = await postJSON('/api/init', { pass: pw })
    app.loaded()
    if (!app.checkResponse(res)) {
      page.appErrMsg.textContent = res.msg
      Doc.show(page.appErrMsg)
      return
    }
    app.user.authed = true // no need to call app.fetchUser(), much hasn't changed.
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

    var cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }

    app.loading(page.dexAddrForm)
    var res = await postJSON('/api/getfee', {
      addr: addr,
      cert: cert
    })
    app.loaded()
    if (!app.checkResponse(res)) {
      page.dexAddrErr.textContent = res.msg
      Doc.show(page.dexAddrErr)
      return
    }
    this.fee = res.fee

    page.feeDisplay.textContent = Doc.formatCoinValue(res.fee / 1e8)
    await this.changeForm(page.dexAddrForm, page.confirmRegForm)
  }

  /* Authorize DEX registration. */
  async registerDEX () {
    const page = this.page
    Doc.hide(page.regErr)
    var cert = ''
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
    app.loading(page.confirmRegForm)
    var res = await postJSON('/api/register', registration)
    app.loaded()
    if (!app.checkResponse(res)) {
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
