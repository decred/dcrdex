import { useState, useEffect } from 'react'

/**
 * Enum representing Bootstrap breakpoint sizes with their min-width values.
 * - XS: 0 (default/smallest)
 * - SM: 576px
 * - MD: 768px
 * - LG: 992px
 * - XL: 1200px
 * - XXL: 1400px
 */
export enum BootstrapBreakpoint {
  XS = 0,
  SM = 576,
  MD = 768,
  LG = 992,
  XL = 1200,
  XXL = 1400,
}

/**
 * Map of breakpoint labels to their enum values for easy lookup.
 */
const BREAKPOINT_VALUES: Record<keyof typeof BootstrapBreakpoint, number> = {
  XS: BootstrapBreakpoint.XS,
  SM: BootstrapBreakpoint.SM,
  MD: BootstrapBreakpoint.MD,
  LG: BootstrapBreakpoint.LG,
  XL: BootstrapBreakpoint.XL,
  XXL: BootstrapBreakpoint.XXL
}

/**
 * A hook to get the current window width.
 * @returns The current window width.
 */
const useWindowWidth = () => {
  const [width, setWidth] = useState<number>(typeof window !== 'undefined' ? window.innerWidth : 0)

  useEffect(() => {
    const handleResize = () => {
      setWidth(window.innerWidth)
    }

    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  return width
}

/**
 * A React hook that determines the current Bootstrap-like breakpoint based on a list of passed breakpoints.
 * - Passed breakpoints (e.g., ['md', 'xl']) define the lower bounds for ranges (min-width).
 * - Returns 'xs' if below the first passed breakpoint (or if 'xs' is passed/derived).
 * - Returns the label of the range the current width falls into.
 * - Assumes passed breakpoints are valid (e.g., 'xs', 'md') and in ascending order; sorts if needed.
 * - If 'xs' is passed, it's treated as the base (0px).
 * @param passedBreakpoints Array of breakpoint labels (e.g., ['md', 'xl'])
 * @returns The current breakpoint label as a string (e.g., 'xs', 'md', 'xl')
 */
export const useBootstrapBreakpoints = (passedBreakpoints: Array<'xs' | 'sm' | 'md' | 'lg' | 'xl' | 'xxl'>): string => {
  const width = useWindowWidth()

  // Validate and map passed breakpoints to {label, value} array, sorted by value
  const breakpointMap = passedBreakpoints
    .map(bp => {
      const key = bp.toUpperCase() as keyof typeof BootstrapBreakpoint
      if (!(key in BREAKPOINT_VALUES)) {
        console.warn(`Invalid breakpoint: ${bp}. Ignoring.`)
        return null
      }
      return { label: bp.toLowerCase(), value: BREAKPOINT_VALUES[key] }
    })
    .filter((bp): bp is { label: string; value: number } => bp !== null)
    .sort((a, b) => a.value - b.value)

  if (breakpointMap.length === 0) {
    return 'xs' // Default if no breakpoints passed
  }

  // Find the range
  let currentBp = 'xs'
  for (let i = 0; i < breakpointMap.length; i++) {
    const current = breakpointMap[i]
    const next = breakpointMap[i + 1]

    if (width >= current.value && (!next || width < next.value)) {
      currentBp = current.label
      break
    }
  }

  return currentBp
}
