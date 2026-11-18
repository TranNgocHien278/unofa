import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// One project, two SPAs: `vite` builds the game (uno.*), `vite --mode admin` the admin site (admin.*).
export default defineConfig(({ mode }) => {
  const admin = mode === 'admin'
  return {
    plugins: [react()],
    root: admin ? 'admin' : '.',
    publicDir: admin ? '../public' : 'public',
    build: {
      outDir: admin ? '../dist/admin' : 'dist/game',
      emptyOutDir: true,
      target: 'es2020',
    },
    server: {
      proxy: {
        '/api': { target: admin ? 'http://localhost:8081' : 'http://localhost:8080', ws: true },
      },
    },
    test: { environment: 'jsdom', root: '.', include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'admin/**/*.test.ts'] },
  }
})
