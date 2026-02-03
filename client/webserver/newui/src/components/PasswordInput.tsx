
import { useState, ChangeEvent } from "react"

interface PasswordInputParams {
  onChange: (e: ChangeEvent<HTMLInputElement>) => void
  value?: string
  placeholder?: string
}

export default function PasswordInput ({ onChange, placeholder, value }: PasswordInputParams) {
  const [showPW, setShowPW] = useState(false)

  const pwChanged = (e: ChangeEvent<HTMLInputElement>) => {
    onChange(e)
  }

  const showPWClicked = () => setShowPW(!showPW)

  return (
    <div className="d-flex align-items-center form-input-bg br5">
      <span className="ico-key fs16 p-2"></span>
      <input
        type={showPW ? 'text' : 'password'}
        onChange={pwChanged} placeholder={placeholder || 'password'}
        value={value}
      ></input>
      <div className={`p-2 hoverbg pointer ${showPW ? 'ico-eyeopen' : 'ico-eyeclosed'}`} onClick={showPWClicked}></div>
    </div>
  )
}