import { useState } from 'react'
import t from '../js/intl'
import PasswordInput from './PasswordInput'
import { postJSON } from '../js/http'

/*
 * SeedPromptFormParams is the interface for the SeedPromptForm component.
 */
interface SeedPromptFormParams { setUsingSeed: (y: boolean) => void }

const SeedPromptForm = ({ setUsingSeed }: SeedPromptFormParams) => {
  const onUsingSeedClicked = () => setUsingSeed(true)
  const onNoSeedClicked = () => setUsingSeed(false)

  return (
    <form className="modal">
      <header>
        <img className="logo-full small me-2" />
        <span>{t('Welcome to Bison Wallet!')}</span>
      </header>
      <div className="px-3">{t('prompt_for_seed')}</div>
      <div className="d-flex align-items-center p-3">
        <button className="fs16 me-1 flex-grow-1" onClick={onUsingSeedClicked}>Yes</button>
        <button className="feature fs16 ms-1 flex-grow-1" onClick={onNoSeedClicked}>No</button>
      </div>
    </form>
  )
}

interface PasswordFormParams {
  setRegistered: (y: boolean) => void
  setSeed: (s: string) => void
  setUsingSeed: (y: boolean) => void
  seed: string
}

const PasswordForm = ({ setRegistered, setUsingSeed, setSeed, seed }: PasswordFormParams) => {
  const [pw, setPW] = useState('')
  const [pw2, setPW2] = useState('')
  const [err, setErr] = useState<string | undefined>(undefined)

  const onGoBackClicked = () => {
    setPW('')
    setPW2('')
    setUsingSeed(undefined)
  }

  const submitPW = async () => {
    const r = await postJSON('/api/init', { pass: pw, seed })
    if (r.ok) {
      setRegistered(true)
      if (r.mnemonic) setSeed(r.mnemonic)
    }
    else setErr(r.errMsg)
  }

  return (
    <form className="modal form-width-300">
      <div className="fs18 p-3 text-center">{t('Set a password for your wallet')}</div>
      <div className="flex-stretch-column px-3">
        <div className="mb-2"><PasswordInput onChange={(e) => setPW(e.target.value)} value={pw} /></div>
        <div><PasswordInput placeholder={t('password again')} onChange={(e) => setPW2(e.target.value)} value={pw2} /></div>
      </div>
      <div className={`flex-center errcolor p-3 ${err ? '' : 'd-none'}`}>{err}</div>
      <div className="flex-stretch-column p-3">
        <button className="feature w-100" disabled={pw !== pw2 || pw.trim() === ''} onClick={submitPW}>{t('Submit')}</button>
        <button className="w-100 mt-2" onClick={onGoBackClicked}>{t('Go Back')}</button>
      </div>
    </form>
  )
}

interface SeedInputParams {
  setSeed: (s: string) => void
  setUsingSeed: (y: boolean) => void
}

export const SeedInput = ({ setSeed, setUsingSeed }: SeedInputParams) => {
  const [err, setErr] = useState<string | undefined>(undefined)
  const [workingSeed, setWorkingSeed] = useState<string>('')

  const onChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => setWorkingSeed(e.target.value)
  const onGoBackClicked = () => setUsingSeed(undefined)
  const submitSeed = async () => {
    const r = await postJSON('/api/validateSeed', { seed: workingSeed })
    if (r.ok) setSeed(workingSeed)
    else setErr(r.errMsg)
  }

  return (
    <form className="modal">
      <div className="fs18 p-3 text-center">{t('Input your recovery seed')}</div>
      <div className="d-flex px-3">
        <textarea className="fs14" onChange={onChange}></textarea>
      </div>
      <div className={`flex-center errcolor p-3 ${err ? '' : 'd-none'}`}>{err}</div>
      <div className="d-flex flex-column p-3">
        <button
          className="feature w-100"
          disabled={workingSeed.trim() === ''}
          onClick={submitSeed}>Recover</button>
        <button
          className="w-100 mt-2"
          onClick={onGoBackClicked}>Go Back</button>
      </div>
    </form>
  )
}

interface SeedDisplayParams {
  acked: () => void
  seed: string
}

export const SeedDisplay = ({ acked, seed }: SeedDisplayParams) => {
  return (
    <form className="modal form-width-300">
      <div className="pt-3 px-3">{t('seed_display_warning')}</div>
      <div className="mt-3 mx-3 p-2 fs14 form-input-bg mono">{seed}</div>
      <div className="flex-stretch-column p-3">
        <button className="feature w-100" onClick={acked}>{t('I have written down my seed')}</button>
      </div>
    </form>
  )
}

export interface InitWizardParams {
  setInited: () => void
}

export default function InitWizard ({ setInited }: InitWizardParams) {
  const [usingSeed, setUsingSeed] = useState<boolean | undefined>(undefined)
  const [seed, setSeed] = useState<string | undefined>(undefined)
  const [registered, setRegistered] = useState<boolean>(false)

  const done = () => {
    setSeed('')
    setInited()
  }

  const registerFormFeedback = () => {
    setRegistered(true)
    if (usingSeed) done()
  }

  return (
    <div className="fill-abs flex-center init-bg">
      {
        usingSeed === undefined ? <SeedPromptForm setUsingSeed={setUsingSeed} /> :
          usingSeed && !seed ? (
            <SeedInput setSeed={setSeed} setUsingSeed={setUsingSeed} />) :
            !registered ? (
              <PasswordForm
                setRegistered={registerFormFeedback}
                setSeed={setSeed}
                setUsingSeed={setUsingSeed}
                seed={seed || ''} />
            ) : <SeedDisplay acked={() => { done() }} seed={seed} />
      }
    </div>
  )
}