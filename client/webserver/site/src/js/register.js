import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import * as forms from './forms'

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
      'appPWForm', 'appPWSubmit', 'appErrMsg', 'appPW', 'appPWAgain',
      // Form 2: Create Decred wallet
      'walletForm',
      // Form 3: Open Decred wallet
      'openForm',
      // Form 4: DEX address
      'urlForm', 'addrInput', 'submitAddr', 'feeDisplay', 'addrErr',
      'certInput', 'selectedCert', 'removeTLS', 'addTLS',
      // Form 5: Final form to initiate registration. Client app password.
      'pwForm', 'clientPass', 'submitPW', 'regErr'
    ])

    // SET APP PASSWORD
    forms.bind(page.appPWForm, page.appPWSubmit, () => { this.setAppPass() })

    // NEW DCR WALLET
    // This form is only shown if there is no DCR wallet yet.
    forms.bindNewWallet(app, page.walletForm, () => {
      this.changeForm(page.walletForm, page.urlForm)
    })
    page.walletForm.setAsset(app.assets[DCR_ID])

    // OPEN DCR WALLET
    // This form is only shown if there is a wallet, but it's not open.
    forms.bindOpenWallet(app, page.openForm, () => {
      this.changeForm(page.openForm, page.urlForm)
    })
    page.openForm.setAsset(app.assets[DCR_ID])

    // ENTER NEW DEX URL
    this.fee = null
    forms.bind(page.urlForm, page.submitAddr, () => { this.checkDEX() })
    // tls certificate upload
    this.defaultTLSText = page.selectedCert.textContent
    Doc.bind(page.certInput, 'change', () => this.readCert())
    Doc.bind(page.removeTLS, 'click', () => this.resetCert())
    Doc.bind(page.addTLS, 'click', () => this.page.certInput.click())

    // SUBMIT DEX REGISTRATION
    forms.bind(page.pwForm, page.submitPW, () => { this.registerDEX() })
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
    Doc.hide(page.appErrMsg)
    const pw = page.appPW.value
    const pwAgain = page.appPWAgain.value
    if (pw === '') {
      page.appErrMsg.textContent = 'password cannot be empty'
      Doc.show(page.appErrMsg)
      return
    }
    if (pw !== pwAgain) {
      page.appErrMsg.textContent = 'passwords do not match'
      Doc.show(page.appErrMsg)
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
    app.setLogged(true)
    const dcrWallet = app.walletMap[DCR_ID]
    if (!dcrWallet) {
      this.changeForm(page.appPWForm, page.walletForm)
      return
    }
    // Not really sure if these other cases are possible if the user hasn't
    // even set their password yet.
    if (!dcrWallet.open) {
      this.changeForm(page.appPWForm, page.openForm)
      return
    }
    this.changeForm(page.appPWForm, page.urlForm)
  }

  /* Get the reg fees for the DEX. */
  async checkDEX () {
    const page = this.page
    Doc.hide(page.addrErr)
    const url = page.addrInput.value
    if (url === '') {
      page.addrErr.textContent = 'URL cannot be empty'
      Doc.show(page.addrErr)
      return
    }

    var cert = ''
    if (page.certInput.value) {
      cert = await page.certInput.files[0].text()
    }

    app.loading(page.urlForm)
    var res = await postJSON('/api/getfee', {
      url: url,
      cert: cert
    })
    app.loaded()
    if (!app.checkResponse(res)) {
      page.addrErr.textContent = res.msg
      Doc.show(page.addrErr)
      return
    }
    page.feeDisplay.textContent = Doc.formatCoinValue(res.fee / 1e8)
    this.fee = res.fee

    const dcrWallet = app.walletMap[DCR_ID]
    if (!dcrWallet) {
      // There is no known Decred wallet, show the wallet form
      await this.changeForm(page.urlForm, page.walletForm)
      return
    }
    // The Decred wallet is known, check if it is open.
    if (!dcrWallet.open) {
      await this.changeForm(page.urlForm, page.openForm)
      return
    }
    // The Decred wallet is known and open, collect the main client password.
    await this.changeForm(page.urlForm, page.pwForm)
  }

  /* Authorize DEX registration. */
  async registerDEX () {
    const page = this.page
    Doc.hide(page.regErr)
    var cert = ''
    if (page.certInput.value) {
      cert = await page.certInput.files[0].text()
    }
    const registration = {
      url: page.addrInput.value,
      pass: page.clientPass.value,
      fee: this.fee,
      cert: cert
    }
    page.clientPass.value = ''
    app.loading(page.pwForm)
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
    const files = page.certInput.files
    if (!files.length) return
    page.selectedCert.textContent = files[0].name
    Doc.show(page.removeTLS)
    Doc.hide(page.addTLS)
  }

  resetCert () {
    const page = this.page
    page.certInput.value = ''
    page.selectedCert.textContent = this.defaultTLSText
    Doc.hide(page.removeTLS)
    Doc.show(page.addTLS)
  }
}
