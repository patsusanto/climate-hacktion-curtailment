import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const backend =
  (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env
    ?.BACKEND_PROXY ?? 'http://localhost:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    // Forward /api/* to the Go backend during development
    proxy: {
      '/api': {
        target: backend,
        rewrite: (path) => path.replace(/^\/api/, ''),
        timeout: 0,
        proxyTimeout: 0,
      },
    },
  },
})
