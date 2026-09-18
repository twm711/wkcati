import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// NK3C React 前端：dev 端口 5173，/api 反代到 Go 后端 :8080
export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
    allowedHosts: true, // 预览代理域名放行
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
  build: { chunkSizeWarningLimit: 1600 },
})
