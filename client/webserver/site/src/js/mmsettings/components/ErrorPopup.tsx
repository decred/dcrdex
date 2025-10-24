import React, { useContext } from 'react'
import { MMSettingsSetErrorContext } from './MMSettings'
import Popup from './Popup'
import { prep, ID_MM_ERROR, ID_MM_CLOSE } from '../../locales'

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
    <Popup
      title={prep(ID_MM_ERROR)}
      message={error.message}
      messageClass="text-danger"
      buttons={[
        { text: prep(ID_MM_CLOSE), onClick: handleClose }
      ]}
      onClose={handleClose}
    />
  )
}

export default ErrorPopup
