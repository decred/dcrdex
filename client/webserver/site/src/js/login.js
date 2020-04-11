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
    app.loading(page.loginForm)
    Doc.hide(page.errMsg)
    const pw = page.pw.value
    page.pw.value = ''
    if (pw === '') {
      page.errMsg.textContent = 'password cannot be empty'
      Doc.show(page.errMsg)
      return
    }
    app.loaded()
    var res = await postJSON('/api/login', { pass: pw })
    if (!app.checkResponse(res)) return
    res.notes.reverse()
    app.setNotes(res.notes)
    await app.fetchUser()
    app.setLogged(true)
    app.loadPage('markets')
  }
}
