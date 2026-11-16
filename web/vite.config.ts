import { defineConfig } from 'vitest/config'

export default defineConfig({
  oxc: { jsx: { runtime: 'automatic' } },
  server: { proxy: { '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false } } },
  test: {
    environment: 'jsdom',
    coverage: {
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['**/*.test.*'],
      reporter: ['text', 'json-summary', 'lcov'],
      thresholds: { lines: 81 },
    },
  },
})
