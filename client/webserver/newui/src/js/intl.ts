const defaultTranslations = {
  'Initialize Wallet': 'Initialize Wallet',
  'prompt_for_seed': 'Are you restoring Bison Wallet from a seed?',
  'Unlock': 'Unlock',
  'seed_display_warning': 'Your seed is shown below. Write it down and keep it safe. You will need it to restore your wallet if you lose your device.'
}

let translations: Record<string, string> = Object.assign({}, defaultTranslations)

export const setTranslations = (ts: Record<string, string>) => {
  translations = Object.assign({}, defaultTranslations, ts)
}

// translate
export default function t (slug: string): string {
  return translations[slug] || slug
}
