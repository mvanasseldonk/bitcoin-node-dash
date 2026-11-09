import { defineConfig } from 'vite'
import preact from '@preact/preset-vite'

// In development, run the Go server on :8080 and `npm run dev` here;
// API calls are proxied to the Go server.
export default defineConfig({
  plugins: [preact()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
