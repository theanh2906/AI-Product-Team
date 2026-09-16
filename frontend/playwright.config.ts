import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  reporter: 'list',
  use: {
    ...devices['Desktop Edge'],
    launchOptions: process.platform === 'win32' ? { channel: 'msedge' } : undefined,
  },
});
