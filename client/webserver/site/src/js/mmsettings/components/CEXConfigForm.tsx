import React, { useState } from 'react'
import { CEXDisplayInfos, MM } from '../../mmutil'
import { MMCEXStatus, app } from '../../registry'

interface CEXConfigFormProps {
  cexName: string;
  cexStatus: MMCEXStatus | null;
  onClose: () => void;
  onCEXUpdated: () => void;
}

const CEXConfigForm: React.FC<CEXConfigFormProps> = ({
  cexName,
  onClose,
  cexStatus,
  onCEXUpdated
}) => {
  const [apiKey, setApiKey] = useState(cexStatus && cexStatus.connectErr ? cexStatus.config.apiKey : '')
  const [apiSecret, setApiSecret] = useState(cexStatus && cexStatus.connectErr ? cexStatus.config.apiSecret : '')
  const [error, setError] = useState('')

  const cexInfo = CEXDisplayInfos[cexName]

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!apiKey.trim() || !apiSecret.trim()) {
      setError('API Key and Secret are required')
      return
    }

    try {
      const res = await MM.updateCEXConfig({ name: cexName, apiKey: apiKey.trim(), apiSecret: apiSecret.trim() })
      await app().fetchMMStatus()
      // Update the CEX statuses even if we fail, because if the CEX has a
      // connection error, we still want the pre-populated incorrect credentials
      // to be updated.
      onCEXUpdated?.()

      if (!app().checkResponse(res)) {
        setError(`Failed to update CEX configuration: ${res.msg}`)
      } else {
        onClose()
      }
    } catch (error) {
      setError(`Failed to update CEX configuration: ${error}`)
    }
  }

  return (
    <div id="forms" className="stylish-overflow flex-center">
      <form className="position-relative mw-425" autoComplete="off" onSubmit={handleSubmit}>
        {/* Form Closer */}
        <div className="form-closer">
          <span className="ico-cross pointer" onClick={onClose}></span>
        </div>

        {/* Configure CEX Prompt */}
        <div className="pt-4 fs18">
          Configure your {cexName} account to enable arbitrage trading.
        </div>

        {/* CEX Info */}
        <div className="flex-center flex-column mt-3 border-top">
          <img
            className="xclogo enourmous-icon"
            src={cexInfo?.logo || '/img/coins/question.png'}
            alt={cexName}
          />
          <div className="mt-2 fs20">{cexName}</div>
        </div>

        {/* Error Box (for connection errors) */}
        {cexStatus && cexStatus.connectErr && (
          <div className="flex-center flex-column text-danger px-3">
            <span className="ico-disconnected fs24"></span>
            <span>Error with CEX credentials</span>
            <span className="fs14 mt-2 text-break" style={{ maxWidth: '100%', wordBreak: 'break-word' }}>{cexStatus.connectErr}</span>
          </div>
        )}

        {/* API Key Input */}
        <div className="d-flex flex-column">
          <label htmlFor="cexApiKeyInput">API Key</label>
          <input
            id="cexApiKeyInput"
            type="text"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            autoComplete="off"
          />
        </div>

        {/* API Secret Input */}
        <div className="d-flex flex-column">
          <label htmlFor="cexSecretInput">API Secret</label>
          <input
            id="cexSecretInput"
            type="password"
            value={apiSecret}
            onChange={(e) => setApiSecret(e.target.value)}
            autoComplete="off"
          />
        </div>

        {/* Form Error */}
        {error && (
          <div className="flex-center text-danger text-break" style={{ maxWidth: '100%', wordBreak: 'break-word' }}>
            {error}
          </div>
        )}

        {/* Submit Button */}
        <div className="flex-stretch-column">
          <button
            type="submit"
            className="feature"
            disabled={!apiKey.trim() || !apiSecret.trim()}
          >
            Submit
          </button>
        </div>
      </form>
    </div>
  )
}

export default CEXConfigForm
