import State from "./state"
import { postJSON } from "./http"
import { PageElement } from "./registry"

let translations: Record<string, string> = {}

// translate
export default function t (slug: string): string {
  const s = translations[slug]
  if (!s && recordMissingTranslations && missingTranslations.indexOf(slug) === -1) {
    console.log(`missing translation: '${slug}'\n`)
    missingTranslations.push(slug)
  }
  return s || slug
}

export function tvar (slug: string, args: Record<string, string>): string {
  return stringTemplateParser(t(slug), args)
}

export function tdom (slug: string, args: Record<string, string>): PageElement {
  return textToDOM(tvar(slug, args))
}

export async function loadLocale (lang: string, commitHash: string, skipCache: boolean) {
  if (!skipCache) {
    const specs = State.fetchLocal(State.localeSpecsKey)
    if (specs && specs.lang === lang && specs.commitHash === commitHash) {
      translations = State.fetchLocal(State.localeKey)
      return
    }
  }

  translations = await postJSON('/api/locale', lang)
  State.storeLocal(State.localeSpecsKey, { lang, commitHash })
  State.storeLocal(State.localeKey, translations)
}

window.clearLocale = () => {
  State.removeLocal(State.localeSpecsKey)
  State.removeLocal(State.localeKey)
}

/* prep will format the message to the current locale. */
export function prep (k: string, args?: Record<string, string>) {
  const text = translations[k]
  if (!text) return ''
  return stringTemplateParser(text, args || {})
}

/*
 * stringTemplateParser is a template string matcher, where expression is any
 * text. It switches what is inside double brackets (e.g. 'buy {{ asset }}')
 * for the value described into args. args is an object with keys
 * equal to the placeholder keys. (e.g. {"asset": "dcr"}).
 * So that will be switched for: 'asset dcr'.
 */
function stringTemplateParser (expression: string, args: Record<string, string>) {
  // templateMatcher matches any text which:
  // is some {{ text }} between two brackets, and a space between them.
  // It is global, therefore it will change all occurrences found.
  // text can be anything, but brackets '{}' and space '\s'
  const templateMatcher = /{{\s?([^{}\s]*)\s?}}/g
  return expression.replace(templateMatcher, (_, value) => args[value])
}

const domParser = new DOMParser()

function textToDOM (s: string) {
  const doc = domParser.parseFromString(s, 'text/html')
  return doc.body
}

const missingTranslations: string[] = []
let recordMissingTranslations: boolean = State.fetchLocal(State.recordMissingTranslationsLK) || false

window.recordMissingTranslations = (enable?: boolean) => {
  enable = enable === undefined ? true : enable
  recordMissingTranslations = enable
  State.storeLocal(State.recordMissingTranslationsLK, enable)
}
window.dumpMissingTranslations = () => {
  const formattedData = JSON.stringify(missingTranslations, null, 2)
  const blob = new Blob([formattedData], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'missing-translations.json'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
