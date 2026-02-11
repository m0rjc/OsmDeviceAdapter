import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// https://vite.dev/config/
export default defineConfig({
  define: {
    __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
  },
  plugins: [
    react(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src/worker',
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
    sourcemap: true,  // Enable source maps for production builds
  },
  // Explicitly enable source maps in dev mode (default, but explicit)
  server: {
    sourcemapIgnoreList: () => false,  // Don't ignore any files in source maps
    proxy: {
      // Proxy API requests to the mock server (or real server)
      '/api': {
        target: process.env.VITE_API_URL || 'http://localhost:8081',
        changeOrigin: true,
      },
    },
  },
})
