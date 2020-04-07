import Doc from './doc'
import BasePage from './basepage'
import State from './state'

export default class SettingsPage extends BasePage {
  constructor (application, body) {
    super()
    const darkMode = Doc.idel(body, 'darkMode')
    Doc.bind(darkMode, 'click', () => {
      State.dark(darkMode.checked)
      if (darkMode.checked) {
        document.body.classList.add('dark')
      } else {
        document.body.classList.remove('dark')
      }
    })
  }
}
