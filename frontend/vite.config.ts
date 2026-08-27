import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [tailwindcss(), react()],
  build: {
    rollupOptions: {
      input: {
        // Desktop Wails window.
        main: resolve(__dirname, 'index.html'),
        // Mobile kiosk-management UI, served by the Go backend at /kiosk.
        // Vite writes both entries' hashed assets into the same dist/
        // directory, which the Fiber static handler serves as-is.
        kiosk: resolve(__dirname, 'kiosk.html'),
      },
    },
  },
})
