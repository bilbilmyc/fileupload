import React, { createContext, useContext, useState, useCallback, useMemo } from 'react'
import axios from 'axios'

interface AuthContextValue {
  token: string | null
  namespace: string
  userId: string | null
  isAuthenticated: boolean
  login: (token: string) => void
  loginWithCredentials: (username: string, password: string) => Promise<void>
  logout: () => void
  setNamespace: (ns: string) => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

const TOKEN_KEY = 'fileupload_token'
const NS_KEY = 'fileupload_namespace'
const USER_KEY = 'fileupload_user'

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(TOKEN_KEY))
  const [namespace, setNamespaceState] = useState(() => localStorage.getItem(NS_KEY) || 'default')
  const [userId, setUserId] = useState<string | null>(() => localStorage.getItem(USER_KEY))

  const login = useCallback((newToken: string) => {
    localStorage.setItem(TOKEN_KEY, newToken)
    setToken(newToken)
  }, [])

  const loginWithCredentials = useCallback(async (username: string, password: string) => {
    const response = await axios.post('/v1/auth/login', { username, password })
    const data = response.data
    localStorage.setItem(TOKEN_KEY, data.access_token)
    localStorage.setItem(NS_KEY, data.namespace || 'default')
    localStorage.setItem(USER_KEY, data.user_id || '')
    setToken(data.access_token)
    setNamespaceState(data.namespace || 'default')
    setUserId(data.user_id || null)
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
    setToken(null)
    setUserId(null)
  }, [])

  const setNamespace = useCallback((ns: string) => {
    localStorage.setItem(NS_KEY, ns)
    setNamespaceState(ns)
  }, [])

  const value = useMemo(
    () => ({
      token,
      namespace,
      userId,
      isAuthenticated: !!token,
      login,
      loginWithCredentials,
      logout,
      setNamespace,
    }),
    [token, namespace, userId, login, loginWithCredentials, logout, setNamespace]
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
