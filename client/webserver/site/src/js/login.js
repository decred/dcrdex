import { app } from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { LoginForm } from './forms'

export default class LoginPage extends BasePage {
  constructor (body) {
    super()
    this.form = Doc.idel(body, 'loginForm')
    Doc.show(this.form)
    this.loginForm = new LoginForm(app, this.form, () => { this.loggedIn() })
    this.loginForm.focus()
  }

  /* login submits the sign-in form and parses the result. */
  async loggedIn (e) {
    await app().fetchUser()
    app().loadPage('markets')
  }
}
