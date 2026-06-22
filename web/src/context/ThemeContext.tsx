import { createContext, useContext, useState, useEffect, useCallback, useMemo, type ReactNode } from 'react'
import { theme, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'

type ThemeMode = 'light' | 'dark'

interface ThemeContextValue {
  mode: ThemeMode
  toggle: () => void
  setMode: (m: ThemeMode) => void
}

const ThemeContext = createContext<ThemeContextValue>({
  mode: 'light',
  toggle: () => {},
  setMode: () => {},
})

export function useTheme() {
  return useContext(ThemeContext)
}

const THEME_KEY = 'fileupload_theme'

function getInitialTheme(): ThemeMode {
  const stored = localStorage.getItem(THEME_KEY)
  if (stored === 'dark' || stored === 'light') return stored
  if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    return 'dark'
  }
  return 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(getInitialTheme)

  const setMode = useCallback((m: ThemeMode) => {
    setModeState(m)
    localStorage.setItem(THEME_KEY, m)
    // 更新 html class，方便 Tailwind 暗色类
    document.documentElement.classList.toggle('dark', m === 'dark')
  }, [])

  const toggle = useCallback(() => {
    setMode(mode === 'light' ? 'dark' : 'light')
  }, [mode, setMode])

  useEffect(() => {
    // 初始化同步
    document.documentElement.classList.toggle('dark', mode === 'dark')

    // 监听系统主题变化
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = (e: MediaQueryListEvent) => {
      if (!localStorage.getItem(THEME_KEY)) {
        setMode(e.matches ? 'dark' : 'light')
      }
    }
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const value = useMemo(() => ({ mode, toggle, setMode }), [mode, toggle, setMode])

  return (
    <ThemeContext.Provider value={value}>
      <ConfigProvider
        locale={zhCN}
        theme={{
          algorithm: mode === 'dark' ? theme.darkAlgorithm : theme.defaultAlgorithm,
        }}
      >
        {children}
      </ConfigProvider>
    </ThemeContext.Provider>
  )
}
