import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Vite config for the Cloud Sandbox Management UI.
// The dev server proxies /api and /health to the Go backend so the browser can
// use same-origin cookies/headers while the API runs on :8080.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
})
