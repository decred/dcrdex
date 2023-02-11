import { CoreNote } from './registry'

export const IGNORE = 0
export const DATA = 1
export const POKE = 2
export const SUCCESS = 3
export const WARNING = 4
export const ERROR = 5

export function saveNtfnsSettings () {
  if (Notification.permission !== 'granted') Notification.requestPermission()
}

export const showNotification = (note: CoreNote) => {
  // xxx add dex icon?
  //  const icon = '';
  const notification = new Notification(note.subject, { body: note.details })
  notification.onclick = () => {
    notification.close()
    window.parent.focus()
  }
}
