import { CoreNote } from './registry'
import * as intl from './locales'
import State from './state'

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

type DesktopNtfnSettingLabel = {
  [x: string]: string
}

export type DesktopNtfnSetting = {
  [x: string]: boolean
}

function desktopNtfnSettingsKey (): string {
  return `desktop_notifications-${window.location.host}`
}

export const desktopNtfnLabels: DesktopNtfnSettingLabel = {
  [NoteTypeOrder]: intl.ID_BROWSER_NTFN_ORDERS,
  [NoteTypeMatch]: intl.ID_BROWSER_NTFN_MATCHES,
  [NoteTypeBondPost]: intl.ID_BROWSER_NTFN_BONDS,
  [NoteTypeConnEvent]: intl.ID_BROWSER_NTFN_CONNECTIONS
}

export const defaultDesktopNtfnSettings: DesktopNtfnSetting = {
  [NoteTypeOrder]: true,
  [NoteTypeMatch]: true,
  [NoteTypeBondPost]: true,
  [NoteTypeConnEvent]: true
}

let desktopNtfnSettings: DesktopNtfnSetting

// BrowserNotifier is a wrapper around the browser's notification API.
class BrowserNotifier {
  static ntfnPermissionGranted (): boolean {
    return window.Notification.permission === 'granted'
  }

  static ntfnPermissionDenied (): boolean {
    return window.Notification.permission === 'denied'
  }

  static async requestNtfnPermission (): Promise<void> {
    if (!('Notification' in window)) {
      return
    }
    if (BrowserNotifier.ntfnPermissionGranted()) {
      BrowserNotifier.sendDesktopNotification(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
    } else if (!BrowserNotifier.ntfnPermissionDenied()) {
      await Notification.requestPermission()
      BrowserNotifier.sendDesktopNotification(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
    }
  }

  static async sendDesktopNotification (title: string, body?: string) {
    if (!BrowserNotifier.ntfnPermissionGranted()) return
    const ntfn = new window.Notification(title, {
      body: body,
      icon: '/img/softened-icon.png'
    })
    return ntfn
  }
}

// OSDesktopNotifier manages OS desktop notifications via the same interface
// as BrowserNotifier, but sends notifications using an underlying Go
// notification library exposed to the webview.
class OSDesktopNotifier {
  static ntfnPermissionGranted (): boolean {
    return true
  }

  static ntfnPermissionDenied (): boolean {
    return false
  }

  static async requestNtfnPermission (): Promise<void> {
    await OSDesktopNotifier.sendDesktopNotification(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
    return Promise.resolve()
  }

  static async sendDesktopNotification (title: string, body?: string): Promise<void> {
    // webview/linux or webview/windows
    if (isDesktopWebview()) await window.sendOSNotification(title, body)
    // webkit/darwin
    // See: client/cmd/dexc-desktop/app_darwin.go#L673-#L697
    else if (isDesktopWebkit()) await window.webkit.messageHandlers.dexcHandler.postMessage(['sendOSNotification', title, body])
    else console.error('sendDesktopNotification: unknown environment')
  }
}

// isDesktopWebview checks if we are running in webview
function isDesktopWebview (): boolean {
  return window.isWebview !== undefined
}

// isDesktopDarwin returns true if we are running in a webview on darwin
// It tests for the existence of the dexcHandler webkit message handler.
function isDesktopWebkit (): boolean {
  return window.webkit?.messageHandlers?.dexcHandler !== undefined
}

// determine whether we're running in a webview or in browser, and export
// the appropriate notifier accordingly.
export const Notifier = isDesktopWebview() || isDesktopWebkit() ? OSDesktopNotifier : BrowserNotifier

export async function desktopNotify (note: CoreNote) {
  if (!desktopNtfnSettings.browserNtfnEnabled || !desktopNtfnSettings[note.type]) return
  await Notifier.sendDesktopNotification(note.subject, note.details)
}

export function fetchDesktopNtfnSettings (): DesktopNtfnSetting {
  if (desktopNtfnSettings !== undefined) {
    return desktopNtfnSettings
  }
  const k = desktopNtfnSettingsKey()
  desktopNtfnSettings = (State.fetchLocal(k) ?? {}) as DesktopNtfnSetting
  return desktopNtfnSettings
}

export function updateNtfnSetting (noteType: string, enabled: boolean) {
  fetchDesktopNtfnSettings()
  desktopNtfnSettings[noteType] = enabled
  State.storeLocal(desktopNtfnSettingsKey(), desktopNtfnSettings)
}
