import { defineConfig } from '@playwright/test';

// The app is driven through the browser endpoint of `wails dev`; scripts/e2e.sh starts it.
export default defineConfig({
  testDir: 'e2e',
  timeout: 60_000,
  workers: 1,
  fullyParallel: false,
  reporter: 'list',
  outputDir: 'test-results',
  use: { baseURL: 'http://localhost:34115', viewport: { width: 1400, height: 900 } },
});
