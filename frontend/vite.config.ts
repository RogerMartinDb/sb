import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        rewrite: path => path.replace(/^\/api/, ''),
      },
      '/identity': {
        target: 'http://localhost:8084',
        rewrite: path => path.replace(/^\/identity/, ''),
      },
      '/catalog-api': {
        target: 'http://localhost:8086',
        rewrite: path => path.replace(/^\/catalog-api/, ''),
        ws: true,
      },
      '/history-api': {
        target: 'http://localhost:8082',
        rewrite: path => path.replace(/^\/history-api/, ''),
      },
    },
  },
})
