import Doc from './doc'
import BasePage from './basepage'
import State from './state'

var app

export default class SettingsPage extends BasePage {
  constructor (application, body) {
    super()
    app = application
    const page = this.page = Doc.parsePage(body, ['darkMode', 'commitHash'])
    Doc.bind(page.darkMode, 'click', () => {
      State.dark(page.darkMode.checked)
      if (page.darkMode.checked) {
        document.body.classList.add('dark')
      } else {
        document.body.classList.remove('dark')
      }
    })
    page.commitHash.textContent = app.commitHash.substring(0, 7)
  }
}
