import Doc from './doc'
import BasePage from './basepage'
import State from './state'
import { postJSON } from './http'
import * as forms from './forms'

import {
  app,
  PageElement,
  ConnectionStatus
} from './registry'

const animationLength = 300

export default class DexSettingsPage extends BasePage {
  body: HTMLElement
  forms: PageElement[]
  currentForm: PageElement
  page: Record<string, PageElement>
  host: string
  keyup: (e: KeyboardEvent) => void

  constructor (body: HTMLElement) {
    super()
    this.body = body
    this.host = body.dataset.host ? body.dataset.host : ''
    const page = this.page = Doc.idDescendants(body)
    this.forms = Doc.applySelector(page.forms, ':scope > form')

    Doc.bind(page.exportDexBtn, 'click', () => this.prepareAccountExport(page.authorizeAccountExportForm))
    Doc.bind(page.disableAcctBtn, 'click', () => this.prepareAccountDisable(page.disableAccountForm))
    Doc.bind(page.updateCertBtn, 'click', () => page.certFileInput.click())
    Doc.bind(page.certFileInput, 'change', () => this.onCertFileChange())

    forms.bind(page.authorizeAccountExportForm, page.authorizeExportAccountConfirm, () => this.exportAccount())
    forms.bind(page.disableAccountForm, page.disableAccountConfirm, () => this.disableAccount())

    const closePopups = () => {
      Doc.hide(page.forms)
    }

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { closePopups() })
    })

    this.notifiers = {
      conn: () => { this.setConnectionStatus() }
    }

    this.setConnectionStatus()
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

  // exportAccount exports and downloads the account info.
  async exportAccount () {
    const page = this.page
    const pw = page.exportAccountAppPass.value
    const host = page.exportAccountHost.textContent
    page.exportAccountAppPass.value = ''
    const req = {
      pw,
      host
    }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/exportaccount', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.exportAccountErr.textContent = res.msg
      Doc.show(page.exportAccountErr)
      return
    }
    const accountForExport = JSON.parse(JSON.stringify(res.account))
    const a = document.createElement('a')
    a.setAttribute('download', 'dcrAccount-' + host + '.json')
    a.setAttribute('href', 'data:text/json,' + JSON.stringify(accountForExport, null, 2))
    a.click()
    Doc.hide(page.forms)
  }

  // disableAccount disables the account associated with the provided host.
  async disableAccount () {
    const page = this.page
    const pw = page.disableAccountAppPW.value
    const host = page.disableAccountHost.textContent
    page.disableAccountAppPW.value = ''
    const req = {
      pw,
      host
    }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/disableaccount', req)
    loaded()
    if (!app().checkResponse(res, true)) {
      page.disableAccountErr.textContent = res.msg
      Doc.show(page.disableAccountErr)
      return
    }
    Doc.hide(page.forms)
    window.location.assign('/settings')
  }

  async prepareAccountExport (authorizeAccountExportForm: HTMLElement) {
    const page = this.page
    page.exportAccountHost.textContent = this.host
    page.exportAccountErr.textContent = ''
    if (State.passwordIsCached()) {
      this.exportAccount()
    } else {
      this.showForm(authorizeAccountExportForm)
    }
  }

  async prepareAccountDisable (disableAccountForm: HTMLElement) {
    const page = this.page
    page.disableAccountHost.textContent = this.host
    page.disableAccountErr.textContent = ''
    this.showForm(disableAccountForm)
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
    if (!app().checkResponse(res, true)) {
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
          page.connectionStatus.textContent = 'Connected'
          break
        case ConnectionStatus.Disconnected:
          displayIcons(false)
          page.connectionStatus.textContent = 'Disconnected'
          break
        case ConnectionStatus.InvalidCert:
          displayIcons(false)
          page.connectionStatus.textContent = 'Disconnected - Invalid Certificate'
      }
    }
  }
}
