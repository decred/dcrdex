import React from 'react'
import { prep, ID_MM_LOADING } from '../../locales'

interface PanelHeaderProps {
  title: string
  description?: string
  buttonText?: string
  onClick?: () => void
}

export const PanelHeader: React.FC<PanelHeaderProps> = ({ title, description, buttonText, onClick }) => {
  return (
    <div className="pb-2 my-2 border-bottom">
      <div className="d-flex justify-content-start lh1 align-items-center">
        <span className="fs20 pt-pt5 me-4">{title}</span>
        {buttonText && onClick && (
          <button className="small" onClick={onClick}>
            <span className="ico-settings fs14 me-1"></span>
            {buttonText}
          </button>
        )}
      </div>
      {description && (
        <div className="mt-2">
          <span className="fs14 text-muted">{description}</span>
        </div>
      )}
    </div>
  )
}

interface FormLabelProps {
  text: string
  description?: string
  className?: string
  isBold?: boolean
}

export const FormLabel: React.FC<FormLabelProps> = ({ text, description, className = '', isBold = true }) => {
  return (
    <div className={`pb-1 ${className}`}>
      <span className={`fs18 ${isBold ? 'demi' : ''}`}>{text}</span>
      {description && <span className="fs14 mt-1 d-block">{description}</span>}
    </div>
  )
}

interface NumberInputProps {
  value?: number;
  onChange: (num: number) => void;
  min?: number;
  max?: number;
  precision?: number;
  className?: string;
  // If onIncrement and onDecrement are provided, the component will show up
  // and down arrows
  onIncrement?: () => void;
  onDecrement?: () => void;
  header?: React.ReactNode;
  bottomContent?: React.ReactNode;
  suffix?: string;
  disabled?: boolean;
  withSlider?: boolean;
}

export const NumberInput: React.FC<NumberInputProps> = ({
  value,
  onChange,
  min,
  max,
  precision = 0,
  className = 'p-2 text-center fs20',
  suffix,
  onIncrement,
  onDecrement,
  header,
  bottomContent,
  withSlider = false,
  disabled = false
}) => {
  const [inputValue, setInputValue] = React.useState<string>(value !== undefined ? value.toFixed(precision) : '')
  const [isDragging, setIsDragging] = React.useState(false)
  const sliderRef = React.useRef<HTMLDivElement>(null)

  React.useEffect(() => {
    const formattedValue = value !== undefined ? value.toFixed(precision) : ''
    if (inputValue !== formattedValue) {
      setInputValue(formattedValue)
    }
  }, [value, precision])

  if (withSlider && (min === undefined || max === undefined)) {
    console.error('The props `min` and `max` must be provided when `withSlider` is true.')
    return null
  }

  const commitInputValue = React.useCallback(() => {
    if (inputValue === '') {
      if (value !== undefined) {
        setInputValue(value.toFixed(precision))
      }
      return
    }

    const numericValue = parseFloat(inputValue)
    if (isNaN(numericValue)) {
      const formattedValue = value !== undefined ? value.toFixed(precision) : ''
      setInputValue(formattedValue)
      return
    }
    let clampedValue = numericValue
    if (min !== undefined) clampedValue = Math.max(min, clampedValue)
    if (max !== undefined) clampedValue = Math.min(max, clampedValue)
    const roundedValue = parseFloat(clampedValue.toFixed(precision))
    setInputValue(roundedValue.toFixed(precision))
    if (roundedValue !== value) {
      onChange(roundedValue)
    }
  }, [inputValue, min, max, precision, value, onChange])

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value)
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      commitInputValue();
      (e.target as HTMLInputElement).blur()
    }
  }

  const getPercentage = React.useCallback(() => {
    if (value === undefined || min === undefined || max === undefined || max === min) return 0
    return ((value - min) / (max - min)) * 100
  }, [value, min, max])

  const getValueFromPercentage = React.useCallback((percentage: number) => {
    if (min === undefined || max === undefined) return 0
    const clampedPercentage = Math.max(0, Math.min(100, percentage))
    const rawValue = min + (clampedPercentage / 100) * (max - min)
    return parseFloat(rawValue.toFixed(precision))
  }, [min, max, precision])

  const handleSliderInteraction = React.useCallback((clientX: number) => {
    if (disabled || !sliderRef.current) return
    const rect = sliderRef.current.getBoundingClientRect()
    const percentage = ((clientX - rect.left) / rect.width) * 100
    const newValue = getValueFromPercentage(percentage)
    setInputValue(newValue.toFixed(precision))
    onChange(newValue)
  }, [disabled, getValueFromPercentage, onChange, precision])

  const handleSliderClick = (e: React.MouseEvent) => {
    handleSliderInteraction(e.clientX)
  }

  const handleMouseDown = (e: React.MouseEvent) => {
    if (disabled) return
    setIsDragging(true)
    e.preventDefault()
  }

  const handleMouseMove = React.useCallback((e: MouseEvent) => {
    if (!isDragging) return
    handleSliderInteraction(e.clientX)
  }, [isDragging, handleSliderInteraction])

  const handleMouseUp = React.useCallback(() => {
    setIsDragging(false)
  }, [])

  React.useEffect(() => {
    if (isDragging) {
      document.addEventListener('mousemove', handleMouseMove)
      document.addEventListener('mouseup', handleMouseUp)
      return () => {
        document.removeEventListener('mousemove', handleMouseMove)
        document.removeEventListener('mouseup', handleMouseUp)
      }
    }
  }, [isDragging, handleMouseMove, handleMouseUp])

  const hasArrows = onIncrement && onDecrement

  return (
    <div className="d-flex flex-column align-items-stretch">
      {header}
      <div className="d-flex align-items-center">
        <div className="flex-grow-1">
          <input
            type="text"
            inputMode="decimal"
            className={`${className} w-100`}
            value={inputValue}
            disabled={disabled}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            onBlur={commitInputValue}
          />
          {withSlider && (
            <div className="position-relative mt-2">
              <div
                ref={sliderRef}
                className="w-100"
                style={{
                  height: '2px',
                  cursor: disabled ? 'not-allowed' : 'pointer',
                  backgroundColor: '#6c757d'
                }}
                onClick={handleSliderClick}
              />
              <div
                className="position-absolute"
                style={{
                  width: '12px',
                  height: '12px',
                  borderRadius: '50%',
                  top: '50%',
                  left: `${getPercentage()}%`,
                  transform: 'translate(-50%, -50%)',
                  cursor: disabled ? 'not-allowed' : isDragging ? 'grabbing' : 'grab',
                  userSelect: 'none',
                  backgroundColor: '#6c757d',
                  opacity: disabled ? 0.5 : 1
                }}
                onMouseDown={handleMouseDown}
              />
            </div>
          )}
        </div>

        {hasArrows && (
          <div className="d-flex flex-column align-items-stretch ms-2">
            <div
              className="flex-grow-1 flex-center px-2 hoverbg pointer user-select-none lh1 ico-arrowup"
              onClick={disabled ? undefined : onIncrement}
            />
            <div
              className="flex-grow-1 flex-center px-2 hoverbg pointer user-select-none lh1 ico-arrowdown"
              onClick={disabled ? undefined : onDecrement}
            />
          </div>
        )}
        {suffix && <span className="fs24 ms-2">{suffix}</span>}
      </div>

      {bottomContent}
    </div>
  )
}

interface IconButtonProps {
  iconClass: string
  onClick: () => void
  size?: string
  ariaLabel: string
  className?: string
}

export const IconButton: React.FC<IconButtonProps> = ({ iconClass, onClick, size = 'fs15', ariaLabel, className = 'pointer px-2' }) => {
  return (
    <span
      className={`${iconClass} ${size} ${className}`}
      onClick={onClick}
      role="button"
      aria-label={ariaLabel}
    ></span>
  )
}

interface ErrorMessageProps {
  message: string
  onClear?: () => void
  timeout?: number
}

export const ErrorMessage: React.FC<ErrorMessageProps> = ({ message, onClear, timeout = 5000 }) => {
  React.useEffect(() => {
    if (message && onClear) {
      const timer = setTimeout(onClear, timeout)
      return () => clearTimeout(timer)
    }
  }, [message, onClear, timeout])

  if (!message) return null
  return <div className="text-danger fs14 text-center py-2">{message}</div>
}

interface LoadingSpinnerProps {
  isLoading: boolean
}

export const LoadingSpinner: React.FC<LoadingSpinnerProps> = ({ isLoading }) => {
  if (!isLoading) return null

  return (
    <div
      className="loading-overlay"
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.5)',
        backdropFilter: 'blur(5px)',
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        zIndex: 9999
      }}
    >
      <div className="spinner-border text-primary" role="status" style={{ width: '3rem', height: '3rem' }}>
        <span className="visually-hidden">{prep(ID_MM_LOADING)}</span>
      </div>
    </div>
  )
}
