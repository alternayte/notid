import { defineConfig } from 'vitest/config'

// Vitest runs without the Start and Nitro plugins: they evaluate server bundles that expect Node.
export default defineConfig({
  resolve: { tsconfigPaths: true },
  test: { environment: 'node' },
})
