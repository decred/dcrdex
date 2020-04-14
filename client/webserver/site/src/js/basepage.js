export default class BasePage {
  /* notify is called when a notification is received by the app. */
  notify (note) {
    if (!this.notifiers || !this.notifiers[note.type]) return
    this.notifiers[note.type](note)
  }

  /* unload is called when the user navigates away from the page. */
  unload () {}
}
