/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // The lazy DiffView chunk bundles Monaco (~3 MB, loaded on the first diff); the main chunk stays small.
  build: { chunkSizeWarningLimit: 3500 },
  test: { environment: 'node', include: ['src/**/*.test.{ts,tsx}'] },
});
