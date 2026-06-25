import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/v1': 'http://localhost:8080',
      '/uploads': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // v0.9.0：精细化 vendor 分块 — 把 antd 核心与 icons 分离
    // 主包仅含应用代码 + 路由；react / antd-core / antd-icons / axios 各独立 chunk
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          'antd-core': ['antd'],
          'antd-icons': ['@ant-design/icons'],
          axios: ['axios'],
        },
      },
    },
  },
})
