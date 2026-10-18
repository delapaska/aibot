import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The built panel is copied into backend/internal/infra/httpapi/dist/app
// and embedded into the Go binary. Base './' keeps asset URLs relative so
// the panel works mounted at '/'.
export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: 'dist',
  },
  server: {
    proxy: {
      // Local dev: the Go API runs on :8080 (make run / docker compose).
      '/api': 'http://127.0.0.1:8080',
    },
  },
})
