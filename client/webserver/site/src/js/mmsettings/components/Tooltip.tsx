import React from 'react'
import { createPortal } from 'react-dom'

interface TooltipProps {
  content: string
  children: React.ReactElement
}

const Tooltip: React.FC<TooltipProps> = ({ content, children }) => {
  const [isVisible, setIsVisible] = React.useState(false)
  const [position, setPosition] = React.useState({ top: 0, left: 0 })
  const triggerRef = React.useRef<HTMLElement>(null)

  const handleMouseEnter = () => {
    if (triggerRef.current) {
      const rect = triggerRef.current.getBoundingClientRect()
      const tooltipWidth = 200 // Approximate tooltip width
      let left = rect.left + rect.width / 2 - tooltipWidth / 2

      // Ensure tooltip doesn't go off screen
      if (left < 5) left = 5
      if (left + tooltipWidth > window.innerWidth) {
        left = window.innerWidth - tooltipWidth - 5
      }

      setPosition({
        top: rect.top - 35, // Position above the element
        left: left
      })
      setIsVisible(true)
    }
  }

  const handleMouseLeave = () => {
    setIsVisible(false)
  }

  // Clone the child element and add event handlers and ref
  const childWithHandlers = React.cloneElement(children as React.ReactElement<any>, {
    ref: triggerRef,
    onMouseEnter: handleMouseEnter,
    onMouseLeave: handleMouseLeave,
    style: { cursor: 'help', ...(children.props?.style || {}) }
  })

  return (
    <>
      {childWithHandlers}
      {isVisible && createPortal(
        <div
          className="tooltip"
          style={{
            position: 'fixed',
            top: `${position.top}px`,
            left: `${position.left}px`,
            backgroundColor: 'rgba(0, 0, 0, 0.8)',
            color: 'white',
            padding: '5px 8px',
            borderRadius: '4px',
            fontSize: '14px',
            whiteSpace: 'normal',
            zIndex: 9999,
            pointerEvents: 'none',
            maxWidth: '300px',
            wordWrap: 'break-word',
            overflowWrap: 'break-word'
          }}
        >
          {content}
        </div>,
        document.body
      )}
    </>
  )
}

export default Tooltip
