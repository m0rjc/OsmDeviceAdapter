import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// Set this to your production server URL
const PROXY_TARGET = process.env.VITE_PROXY_TARGET || 'https://your-production-domain.com';

// https://vite.dev/config/
export default defineConfig({
  define: {
    __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
  },
  plugins: [
    react(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      registerType: 'prompt',
      includeAssets: ['favicon.svg', 'icons/*.png'],
      manifest: false, // Use the manifest.json in public/
      injectManifest: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff,woff2}'],
      },
      devOptions: {
        enabled: true,
        type: 'module',
      },
    }),
  ],
  base: '/admin/',  // Matches server path for SPA
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,  // Enable source maps for debugging
  },
  server: {
    proxy: {
      // Proxy API requests to production server
      '/api': {
        target: PROXY_TARGET,
        changeOrigin: true,
        secure: true,
        cookieDomainRewrite: 'localhost',
      },
      // Proxy admin endpoints (login, callback, logout)
      '/admin/login': {
        target: PROXY_TARGET,
        changeOrigin: true,
        secure: true,
      },
      '/admin/callback': {
        target: PROXY_TARGET,
        changeOrigin: true,
        secure: true,
      },
      '/admin/logout': {
        target: PROXY_TARGET,
        changeOrigin: true,
        secure: true,
      },
    },
  },
})
