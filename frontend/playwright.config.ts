import { defineConfig } from '@playwright/test';
import { existsSync } from 'node:fs';
import path from 'node:path';

const localChrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || (existsSync(localChrome) ? localChrome : undefined);

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:15173',
    viewport: { width: 1512, height: 982 },
    launchOptions: { executablePath },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: 'go run ./cmd/server -addr 127.0.0.1:18081 -static ""',
      cwd: '../backend',
      env: { GOCACHE: path.resolve('../.cache/go-build'), GOTOOLCHAIN: 'local', GOWORK: 'off', GOPROXY: 'off', GOSUMDB: 'off' },
      url: 'http://127.0.0.1:18081/api/health',
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: 'npm run dev -- --port 15173 --strictPort',
      env: { GOVIZ_API_TARGET: 'http://127.0.0.1:18081' },
      url: 'http://127.0.0.1:15173',
      reuseExistingServer: false,
      timeout: 60_000,
    },
  ],
});
