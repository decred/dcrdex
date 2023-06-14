import { app } from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { LoginForm } from './forms'

export default class LoginPage extends BasePage {
  form: HTMLElement
  loginForm: LoginForm

  constructor (body: HTMLElement) {
    super()
    this.form = Doc.idel(body, 'loginForm')
    Doc.show(this.form)
    this.loginForm = new LoginForm(this.form, () => { this.loggedIn() })
    this.loginForm.focus()
  }

  /* login submits the sign-in form and parses the result. */
  async loggedIn () {
    await app().fetchUser()
    await app().loadPage('wallets')
  }
}
