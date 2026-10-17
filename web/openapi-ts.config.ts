import { defineConfig } from '@hey-api/openapi-ts'

export default defineConfig({
  input: './src/api/openapi.json',
  output: { path: './src/api/gen' },
  plugins: [
    { name: '@hey-api/client-fetch', runtimeConfigPath: './src/api/runtime' },
    '@hey-api/typescript',
    '@hey-api/sdk',
    '@tanstack/react-query',
  ],
})
