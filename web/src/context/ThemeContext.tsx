import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'

type ThemeMode = 'light' | 'dark' | 'system'
type ResolvedThemeMode = Exclude<ThemeMode, 'system'>

interface ThemeContextValue {
  mode: ThemeMode
  resolvedMode: ResolvedThemeMode
  toggle: () => void
  setMode: (mode: ThemeMode) => void
}

const ThemeContext = createContext<ThemeContextValue>({
  mode: 'system',
  resolvedMode: 'light',
  toggle: () => {},
  setMode: () => {},
})

export function useTheme() {
  return useContext(ThemeContext)
}

const THEME_KEY = 'fileupload_theme'

function systemTheme(): ResolvedThemeMode {
  const mediaQuery = window.matchMedia?.('(prefers-color-scheme: dark)')
  return mediaQuery?.matches ? 'dark' : 'light'
}

function resolveTheme(mode: ThemeMode): ResolvedThemeMode {
  return mode === 'system' ? systemTheme() : mode
}

function getInitialTheme(): ThemeMode {
  const stored = localStorage.getItem(THEME_KEY)
  if (stored === 'dark' || stored === 'light' || stored === 'system') return stored
  return 'system'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(getInitialTheme)
  const [resolvedMode, setResolvedMode] = useState<ResolvedThemeMode>(() => resolveTheme(getInitialTheme()))

  const setMode = useCallback((nextMode: ThemeMode) => {
    setModeState(nextMode)
    setResolvedMode(resolveTheme(nextMode))
    localStorage.setItem(THEME_KEY, nextMode)
  }, [])

  const toggle = useCallback(() => {
    setMode(resolvedMode === 'light' ? 'dark' : 'light')
  }, [resolvedMode, setMode])

  useEffect(() => {
    document.documentElement.classList.toggle('dark', resolvedMode === 'dark')
  }, [resolvedMode])

  useEffect(() => {
    const mediaQuery = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!mediaQuery) return undefined

    const handleChange = (event: MediaQueryListEvent) => {
      if (mode === 'system') {
        setResolvedMode(event.matches ? 'dark' : 'light')
      }
    }
    mediaQuery.addEventListener('change', handleChange)
    return () => mediaQuery.removeEventListener('change', handleChange)
  }, [mode])

  const value = useMemo(
    () => ({ mode, resolvedMode, toggle, setMode }),
    [mode, resolvedMode, toggle, setMode],
  )

  return (
    <ThemeContext.Provider value={value}>
      <ConfigProvider
        locale={zhCN}
        theme={{
          algorithm: resolvedMode === 'dark' ? theme.darkAlgorithm : theme.defaultAlgorithm,
        }}
      >
        {children}
      </ConfigProvider>
    </ThemeContext.Provider>
  )
}
