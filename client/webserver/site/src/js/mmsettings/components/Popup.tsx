import React, { useRef } from 'react'

interface PopupButton {
  text: string
  onClick: () => void
  className?: string
}

interface PopupProps {
  title?: string
  message: string
  messageClass?: string
  buttons: PopupButton[]
  onClose: () => void
  closeOnBackdropClick?: boolean
}

const Popup: React.FC<PopupProps> = ({
  title,
  message,
  messageClass,
  buttons,
  onClose,
  closeOnBackdropClick = true
}) => {
  const formRef = useRef<HTMLFormElement>(null)

  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (closeOnBackdropClick && formRef.current && !formRef.current.contains(e.target as Node)) {
      onClose()
    }
  }

  return (
    <div id="forms" className="stylish-overflow flex-center" onMouseDown={handleBackdropClick}>
      <form ref={formRef} className="position-relative mw-425 stylish-overflow" autoComplete="off">
        <div className="form-closer">
          <span className="ico-cross pointer" onClick={onClose}></span>
        </div>

        {title && (
          <header className="d-flex align-items-center mb-3">
            <div className="fs24">{title}</div>
          </header>
        )}

        <div className={`mb-4 ${!title ? 'mt-3' : ''}`}>
          <div className={`fs18 ${messageClass || ''}`}>
            {message}
          </div>
        </div>

        <div className={buttons.length === 1 ? 'flex-stretch-column' : 'd-flex'}>
          {buttons.map((button, index) => (
            <button
              key={index}
              type="button"
              className={`feature ${button.className || ''} ${buttons.length > 1 ? 'flex-fill' : ''} ${index < buttons.length - 1 ? 'me-2' : ''}`}
              onClick={button.onClick}
            >
              {button.text}
            </button>
          ))}
        </div>
      </form>
    </div>
  )
}

export default Popup
