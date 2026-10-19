import path from "node:path"
import babel from "@rolldown/plugin-babel"
import tailwindcss from "@tailwindcss/vite"
import react, { reactCompilerPreset } from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// The Go server embeds dist/ and serves it with a strict Content Security
// Policy: no inline script, no third-party origin. Nothing here may emit one.
export default defineConfig({
  plugins: [react(), babel({ presets: [reactCompilerPreset()] }), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  build: {
    // Inlining small assets as data: URIs would need `data:` in the CSP.
    assetsInlineLimit: 0,
    sourcemap: false,
  },
})
