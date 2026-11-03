import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// ALEXA_API points the dev server's /api proxy at another backend, e.g.
// ALEXA_API=http://127.0.0.1:8091 npx vite --port 5181.
const api = process.env.ALEXA_API || 'http://127.0.0.1:8090'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: api, ws: true },
    },
  },
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 900 },
})
