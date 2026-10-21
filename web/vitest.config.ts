import path from "node:path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./app/src"),
    },
  },
  test: {
    environment: "jsdom",
    // Above the five seconds a findBy* waits (test/setup.ts), so a missing
    // element fails with its own error rather than as a timed-out test.
    testTimeout: 15_000,
    setupFiles: ["./test/setup.ts"],
    include: ["app/src/**/*.test.{ts,tsx}", "packages/*/src/**/*.test.{ts,tsx}"],
    coverage: {
      provider: "v8",
      reporter: ["text", "json-summary"],
      include: ["app/src/**/*.{ts,tsx}", "packages/*/src/**/*.{ts,tsx}"],
      exclude: ["**/*.test.{ts,tsx}", "app/src/gen/**", "app/src/main.tsx"],
    },
  },
})
