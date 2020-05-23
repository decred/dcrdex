import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import * as forms from './forms'

var app

export default class LoginPage extends BasePage {
  constructor (application, body) {
    super()
    app = application
    const page = this.page = Doc.parsePage(body, [
      'submit', 'errMsg', 'loginForm', 'pw'
    ])
    forms.bind(page.loginForm, page.submit, () => { this.login() })
    page.pw.focus()
  }

  /* login submits the sign-in form and parses the result. */
  async login (e) {
    const page = this.page
    Doc.hide(page.errMsg)
    const pw = page.pw.value
    page.pw.value = ''
    if (pw === '') {
      page.errMsg.textContent = 'password cannot be empty'
      Doc.show(page.errMsg)
      return
    }
    app.loading(page.loginForm)
    var res = await postJSON('/api/login', { pass: pw })
    app.loaded()
    if (!app.checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
      return
    }
    if (res.notes) {
      res.notes.reverse()
    }
    app.setNotes(res.notes || [])
    await app.fetchUser()
    app.loadPage('markets')
  }
}
