export const IGNORE = 0
export const DATA = 1
export const POKE = 2
export const SUCCESS = 3
export const WARNING = 4
export const ERROR = 5

export function make (subject, details, severity) {
  return {
    subject: subject,
    details: details,
    severity: severity,
    stamp: new Date().getTime()
  }
}
