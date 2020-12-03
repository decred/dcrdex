import Doc from './doc'
import BasePage from './basepage'
import State from './state'
import { postJSON } from './http'
import * as forms from './forms'

const animationLength = 300

let app

export default class SettingsPage extends BasePage {
  constructor (application, body) {
    super()
    app = application
    const page = this.page = Doc.parsePage(body, [
      'darkMode', 'commitHash',
      'addADex',
      // Form to configure DEX server
      'dexAddrForm', 'dexAddr', 'certFile', 'selectedCert', 'removeCert', 'addCert',
      'submitDEXAddr', 'dexAddrErr',
      // Form to confirm DEX registration and pay fee
      'forms', 'confirmRegForm', 'feeDisplay', 'appPass', 'submitConfirm', 'regErr',
      // Others
      'showPokes'
    ])

    Doc.bind(page.darkMode, 'click', () => {
      State.dark(page.darkMode.checked)
      if (page.darkMode.checked) {
        document.body.classList.add('dark')
      } else {
        document.body.classList.remove('dark')
      }
    })

    Doc.bind(page.showPokes, 'click', () => {
      const show = page.showPokes.checked
      State.setCookie('popups', show ? '1' : '0')
      app.showPopups = show
    })

    page.commitHash.textContent = app.commitHash.substring(0, 7)
    Doc.bind(page.addADex, 'click', () => this.showForm(page.dexAddrForm))
    Doc.bind(page.certFile, 'change', () => this.onCertFileChange())
    Doc.bind(page.removeCert, 'click', () => this.clearCertFile())
    Doc.bind(page.addCert, 'click', () => this.page.certFile.click())
    forms.bind(page.dexAddrForm, page.submitDEXAddr, () => { this.verifyDEX() })
    forms.bind(page.confirmRegForm, page.submitConfirm, () => { this.registerDEX() })

    const closePopups = () => {
      Doc.hide(page.forms)
      page.appPass.value = ''
    }

    Doc.bind(page.forms, 'mousedown', e => {
      if (!Doc.mouseInElement(e, this.currentForm)) { closePopups() }
    })

    this.keyup = e => {
      if (e.key === 'Escape') {
        closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { closePopups() })
    })
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form) {
    const page = this.page
    this.currentForm = form
    Doc.hide(page.dexAddrForm, page.confirmRegForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0px'
  }

  /**
   * onCertFileChange when the input certFile changed, read the file
   * and setting cert name into text of selectedCert to display on the view
   */
  async onCertFileChange () {
    const page = this.page
    const files = page.certFile.files
    if (!files.length) return
    page.selectedCert.textContent = files[0].name
    Doc.show(page.removeCert)
    Doc.hide(page.addCert)
  }

  /* clearCertFile cleanup certFile value and selectedCert text */
  clearCertFile () {
    const page = this.page
    page.certFile.value = ''
    page.selectedCert.textContent = this.defaultTLSText
    Doc.hide(page.removeCert)
    Doc.show(page.addCert)
  }

  /* Get the reg fees for the DEX. */
  async verifyDEX () {
    const page = this.page
    Doc.hide(page.dexAddrErr)
    const addr = page.dexAddr.value
    if (addr === '') {
      page.dexAddrErr.textContent = 'URL cannot be empty'
      Doc.show(page.dexAddrErr)
      return
    }

    let cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }

    const loaded = app.loading(page.dexAddrForm)
    const res = await postJSON('/api/getfee', {
      addr: addr,
      cert: cert
    })
    loaded()
    if (!app.checkResponse(res)) {
      page.dexAddrErr.textContent = res.msg
      Doc.show(page.dexAddrErr)
      return
    }
    this.fee = res.fee

    page.feeDisplay.textContent = Doc.formatCoinValue(res.fee / 1e8)
    await this.showForm(page.confirmRegForm)
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
    if (!app.checkResponse(res)) {
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      loaded()
      return
    }
    page.dexAddr.value = ''
    this.clearCertFile()
    Doc.hide(page.forms)
    await app.fetchUser()
    loaded()
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /settings page.
   */
  unload () {
    Doc.unbind(document, 'keyup', this.keyup)
  }
}
