import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { ThemeProvider, useTheme } from './ThemeContext'

function ThemeProbe() {
  const { mode, resolvedMode, setMode } = useTheme()
  return (
    <div>
      <output data-testid="theme-state">{`${mode}:${resolvedMode}`}</output>
      <button type="button" onClick={() => setMode('dark')}>dark</button>
      <button type="button" onClick={() => setMode('system')}>system</button>
    </div>
  )
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('uses the system color scheme until the user selects a manual theme', () => {
    const listeners: Array<(event: MediaQueryListEvent) => void> = []
    vi.stubGlobal('matchMedia', vi.fn(() => ({
      matches: true,
      media: '(prefers-color-scheme: dark)',
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.push(listener),
      removeEventListener: () => {},
      dispatchEvent: () => false,
    })))

    render(<ThemeProvider><ThemeProbe /></ThemeProvider>)
    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:dark')
    expect(document.documentElement).toHaveClass('dark')

    act(() => screen.getByRole('button', { name: 'dark' }).click())
    expect(screen.getByTestId('theme-state')).toHaveTextContent('dark:dark')
    expect(localStorage.getItem('fileupload_theme')).toBe('dark')

    act(() => screen.getByRole('button', { name: 'system' }).click())
    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:dark')
    expect(localStorage.getItem('fileupload_theme')).toBe('system')
    expect(listeners).not.toHaveLength(0)

    const latestListener = listeners[listeners.length - 1]
    act(() => latestListener?.({ matches: false } as MediaQueryListEvent))
    expect(screen.getByTestId('theme-state')).toHaveTextContent('system:light')
    expect(document.documentElement).not.toHaveClass('dark')
  })
})
