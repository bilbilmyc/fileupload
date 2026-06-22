import React, { createContext, useContext, useState, useCallback, useMemo } from 'react'

interface AuthContextValue {
  token: string | null
  namespace: string
  isAuthenticated: boolean
  login: (token: string) => void
  logout: () => void
  setNamespace: (ns: string) => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

const TOKEN_KEY = 'fileupload_token'
const NS_KEY = 'fileupload_namespace'

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(TOKEN_KEY))
  const [namespace, setNamespaceState] = useState(() => localStorage.getItem(NS_KEY) || 'default')

  const login = useCallback((newToken: string) => {
    localStorage.setItem(TOKEN_KEY, newToken)
    setToken(newToken)
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem(TOKEN_KEY)
    setToken(null)
  }, [])

  const setNamespace = useCallback((ns: string) => {
    localStorage.setItem(NS_KEY, ns)
    setNamespaceState(ns)
  }, [])

  const value = useMemo(
    () => ({
      token,
      namespace,
      isAuthenticated: !!token,
      login,
      logout,
      setNamespace,
    }),
    [token, namespace, login, logout, setNamespace]
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
