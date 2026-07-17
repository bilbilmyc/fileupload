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
    rollupOptions: {
      output: {
        // 仅固定稳定的跨页基础依赖；Ant Design 组件按路由依赖图自动拆分，避免人为分块造成循环依赖。
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          'antd-icons': ['@ant-design/icons'],
          axios: ['axios'],
          'fileupload-sdk': ['@fileupload/sdk'],
        },
      },
    },
  },
})
