import { useState, ChangeEvent } from 'react'
import t from '../js/intl'
import PasswordInput from './PasswordInput'
import { postJSON } from '../js/http'

interface UnlockedFormParams {
  setUnlocked: () => void
}

export default function UnlockForm ({ setUnlocked }: UnlockedFormParams) {
  const [pw, setPW] = useState('')
  const [err, setErr] = useState('')

  const pwChanged = (e: ChangeEvent<HTMLInputElement>) => {
    setPW(e.target.value)
  }

  const submitPW = async () => {
    const r = await postJSON('/api/login', { pass: pw })
    if (r.ok) setUnlocked()
    else setErr(r.errMsg)
  }

  return (
    <div className="forms">
      <form className="modal form-width-250">
        <header>
          <img className="logo-full small me-2" />
          <span>{t('Welcome Back!')}</span>
        </header>
        <div className="flex-center px-3">
          <PasswordInput value={pw} onChange={pwChanged} />
        </div>
        <div className="flex-stretch-column p-3">
          <button onClick={submitPW} disabled={pw === ''} className="feature">
            {t('Unlock')}
          </button>
        </div>
        <div className={`flex-center errcolor py-3 ${err ? '' : 'd-none'}`}>{err}</div>
      </form>
    </div>
  )
}