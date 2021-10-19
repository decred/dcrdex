import { app } from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import * as forms from './forms'
import * as intl from './locales'

export default class LoginPage extends BasePage {
  constructor (body) {
    super()
    const page = this.page = Doc.idDescendants(body)
    forms.bind(page.loginForm, page.submit, () => { this.login() })
    page.pw.focus()
  }

  /* login submits the sign-in form and parses the result. */
  async login (e) {
    const page = this.page
    Doc.hide(page.errMsg)
    const pw = page.pw.value
    page.pw.value = ''
    const rememberPass = page.rememberPass.checked
    if (pw === '') {
      page.errMsg.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      Doc.show(page.errMsg)
      return
    }
    const loaded = app().loading(page.loginForm)
    const res = await postJSON('/api/login', { pass: pw, rememberPass })
    loaded()
    if (!app().checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
      return
    }
    if (res.notes) {
      res.notes.reverse()
    }
    app().setNotes(res.notes || [])
    await app().fetchUser()
    app().loadPage('markets')
  }
}
