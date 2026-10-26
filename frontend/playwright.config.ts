import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir:'./tests/e2e',
  use:{baseURL:process.env.ROADMAP_TEST_URL || 'http://127.0.0.1:5173',trace:'retain-on-failure'},
  reporter:[['list'],['html',{open:'never'}]],
  workers:1,
});
