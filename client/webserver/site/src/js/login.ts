import { PageElement, app } from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { AppPassResetForm, LoginForm, slideSwap } from './forms'

/*
  LoginPage holds the form for login and password reset.
*/
export default class LoginPage extends BasePage {
  loginForm: LoginForm
  page: Record<string, PageElement>
  appPassResetForm: AppPassResetForm

  constructor (body: HTMLElement) {
    super()
    const page = this.page = Doc.idDescendants(body)
    this.loginForm = new LoginForm(page.loginForm, () => { this.loggedIn() })

    const prepAndDisplayLoginForm = () => {
      Doc.hide(page.resetAppPWForm)
      this.loginForm.refresh()
      Doc.show(page.loginForm)
      this.loginForm.focus()
    }
    prepAndDisplayLoginForm()

    this.appPassResetForm = new AppPassResetForm(page.resetAppPWForm, () => { prepAndDisplayLoginForm() })
    Doc.bind(page.forgotPassBtn, 'click', () => slideSwap(page.loginForm, page.resetAppPWForm))
    Doc.bind(page.resetPassFormCloser, 'click', () => { prepAndDisplayLoginForm() })
    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, page.resetAppPWForm) && Doc.isDisplayed(page.resetAppPWForm)) { prepAndDisplayLoginForm() }
    })
  }

  /* login submits the sign-in form and parses the result. */
  async loggedIn () {
    await app().fetchUser()
    await app().loadPage('wallets')
  }
}
