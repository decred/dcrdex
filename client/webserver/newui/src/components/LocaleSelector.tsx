import { useState } from 'react'
import app from '../js/application'

export default function LocaleSelector () {
  const [lang, setLang] = useState<string>(app.lang)

  const changeLanguage = async (newLang: string) => {
    await app.changeLocale(newLang)
    app.reRender()
    setLang(newLang)
  }

  return (
    <div className="form-input-bg flex-center form-input-bg p-2 pointer hoverbg"
      onClick={(e) => {
        e.currentTarget.querySelector('select').click()
      }}>
      <select className="pointer" onChange={(e) => changeLanguage(e.target.value)} value={lang}
        onClick={(e) => e.stopPropagation()}>
        {Object.keys(localeData).map((key) => (
          <option key={key} value={key}>
            {localeData[key].flag} {localeData[key].name}
          </option>
        ))}
      </select>
      <span className="ico-arrowdown fs8" />
    </div>

  )
}

interface LangData {
  name: string
  flag: string
}

const localeData: Record<string, LangData> = {
  'en-US': {
    name: 'English',
    flag: '🇺🇸' // Not 🇬🇧. MURICA!
  },
  'pt-BR': {
    name: 'Português',
    flag: '🇧🇷'
  },
  'zh-CN': {
    name: '中文',
    flag: '🇨🇳'
  },
  'pl-PL': {
    name: 'Polski',
    flag: '🇵🇱'
  },
  'de-DE': {
    name: 'Deutsch',
    flag: '🇩🇪'
  },
  'ar': {
    name: 'العربية',
    flag: '🇪🇬' // Egypt I guess
  }
}
