import React, { useState } from 'react';
import { CEXDisplayInfos } from '../../mmutil';

interface CEXConfigFormProps {
  cexName: string;
  onClose: () => void;
  onSubmit: (cexName: string, apiKey: string, apiSecret: string) => void;
}

const CEXConfigForm: React.FC<CEXConfigFormProps> = ({
  cexName,
  onClose,
  onSubmit
}) => {
  const [apiKey, setApiKey] = useState('');
  const [apiSecret, setApiSecret] = useState('');
  const [error, setError] = useState('');

  const cexInfo = CEXDisplayInfos[cexName];

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!apiKey.trim() || !apiSecret.trim()) {
      setError('API Key and Secret are required');
      return;
    }

    onSubmit(cexName, apiKey.trim(), apiSecret.trim());
  };

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
        <div className="flex-center flex-column text-danger d-none">
          <span className="ico-disconnected fs24"></span>
          <span>Error with CEX credentials</span>
          <span className="fs14 mt-2 text-break"></span>
        </div>

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
            type="text"
            value={apiSecret}
            onChange={(e) => setApiSecret(e.target.value)}
            autoComplete="off"
          />
        </div>

        {/* Form Error */}
        {error && (
          <div className="flex-center text-danger text-break">
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
  );
};

export default CEXConfigForm;
