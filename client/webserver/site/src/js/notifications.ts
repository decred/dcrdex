import { CoreNote, app } from './registry'
import { postJSON } from './http'
import * as intl from './locales'

export const IGNORE = 0
export const DATA = 1
export const POKE = 2
export const SUCCESS = 3
export const WARNING = 4
export const ERROR = 5

/*
 * make constructs a new notification. The notification structure is a mirror of
 * the structure of notifications sent from the web server.
 * NOTE: I'm hoping to make this function obsolete, since errors generated in
 * javascript should usually be displayed/cached somewhere better. For example,
 * if the error is generated during submission of a form, the error should be
 * displayed on or near the form itself, not in the notifications.
 */
export function make (subject: string, details: string, severity: number): CoreNote {
  return {
    subject: subject,
    details: details,
    severity: severity,
    stamp: new Date().getTime(),
    acked: false,
    type: 'internal',
    topic: 'internal',
    id: ''
  }
}

const NoteTypeOrder = 'order'
const NoteTypeMatch = 'match'
const NoteTypeBondPost = 'bondpost'
const NoteTypeConnEvent = 'conn'

type BrowserNtfnSettingLabel = {
  [x: string]: string
}

type BrowserNtfnSetting = {
  [x: string]: boolean
}

export const browserNtfnLabels: BrowserNtfnSettingLabel = {
  [NoteTypeOrder]: intl.ID_BROWSER_NTFN_ORDERS,
  [NoteTypeMatch]: intl.ID_BROWSER_NTFN_MATCHES,
  [NoteTypeBondPost]: intl.ID_BROWSER_NTFN_BONDS,
  [NoteTypeConnEvent]: intl.ID_BROWSER_NTFN_CONNECTIONS
}

export const defaultBrowserNtfnSettings: BrowserNtfnSetting = {
  [NoteTypeOrder]: true,
  [NoteTypeMatch]: true,
  [NoteTypeBondPost]: true,
  [NoteTypeConnEvent]: true
}

let browserNtfnSettings: BrowserNtfnSetting

export function ntfnPermissionGranted () {
  return window.Notification.permission === 'granted'
}

export function ntfnPermissionDenied () {
  return window.Notification.permission === 'denied'
}

export async function requestNtfnPermission () {
  if (!('Notification' in window)) {
    return
  }
  if (Notification.permission === 'granted') {
    showBrowserNtfn(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
  } else if (Notification.permission !== 'denied') {
    await Notification.requestPermission()
    showBrowserNtfn(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
  }
}

export function showBrowserNtfn (title: string, body?: string) {
  if (window.Notification.permission !== 'granted') return
  const ntfn = new window.Notification(title, {
    body: body,
    icon: '/img/softened-icon.png'
  })
  return ntfn
}

export function browserNotify (note: CoreNote) {
  if (!browserNtfnSettings[note.type]) return
  showBrowserNtfn(note.subject, note.details)
}

export async function fetchBrowserNtfnSettings () {
  if (browserNtfnSettings !== undefined) {
    return browserNtfnSettings
  }
  const res = await postJSON('/api/getntfnsettings', {})
  if (!app().checkResponse(res)) {
    return defaultBrowserNtfnSettings
  }
  browserNtfnSettings = res.notetypes
  return browserNtfnSettings
}

export async function updateNtfnSetting (noteType: string, enabled: boolean) {
  const request: {
    notetype: string,
    enabled: boolean
  } = {
    notetype: noteType,
    enabled: enabled
  }
  browserNtfnSettings[noteType] = enabled
  return await postJSON('/api/updatentfnsetting', request)
}
