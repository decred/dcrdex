import React, { useContext } from 'react'
import { MMSettingsSetErrorContext } from './MMSettings'

interface ErrorPopupProps {
  error: { message: string; onClose?: () => void } | null
}

const ErrorPopup: React.FC<ErrorPopupProps> = ({ error }) => {
  const setError = useContext(MMSettingsSetErrorContext)

  if (!error || !setError) {
    return null
  }

  const handleClose = () => {
    setError(null)
    if (error.onClose) {
      error.onClose()
    }
  }

  return (
    <div id="forms" className="stylish-overflow flex-center">
      <form className="position-relative mw-425 stylish-overflow" autoComplete="off">
          {/* Form Closer */}
          <div className="form-closer">
            <span className="ico-cross pointer" onClick={handleClose}></span>
          </div>

          {/* Header */}
          <header className="d-flex align-items-center mb-3">
            <div className="fs24">Error</div>
          </header>

          {/* Error Message */}
          <div className="mb-4">
            <div className="fs18 text-danger">
              {error.message}
            </div>
          </div>

          {/* Close Button */}
          <div className="flex-stretch-column">
            <button
              type="button"
              className="feature"
              onClick={handleClose}
            >
              Close
            </button>
          </div>
      </form>
    </div>
  )
}

export default ErrorPopup
