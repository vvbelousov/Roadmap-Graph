import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";
export default defineConfig({
  plugins: [svelte()],
  resolve: { conditions: ["browser"] },
  test: { exclude: ["tests/e2e/**", "node_modules/**"] },
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
});
