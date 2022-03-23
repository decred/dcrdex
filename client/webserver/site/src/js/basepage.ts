import { CoreNote } from './registry'

export default class BasePage {
  notifiers?: Record<string, (n: CoreNote) => void>

  /* notify is called when a notification is received by the app. */
  notify (note: CoreNote) {
    if (!this.notifiers || !this.notifiers[note.type]) return
    this.notifiers[note.type](note)
  }

  /* unload is called when the user navigates away from the page. */
  unload () {
    // should be implemented by inheriting class.
  }
}
