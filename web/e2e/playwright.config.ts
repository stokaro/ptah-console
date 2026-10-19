import { defineConfig } from "@playwright/test"

// The demo runs against a live `ptah-console local` on the operator's kind
// lab; the workflow passes the sign-in link in DEMO_LINK. It records a video
// of the whole run.
export default defineConfig({
  testDir: ".",
  timeout: 25 * 60_000,
  retries: 0,
  workers: 1,
  // Each run keeps its own results, so a later run does not clear an
  // earlier one's video.
  reporter: [
    ["list"],
    [
      "html",
      { open: "never", outputFolder: process.env.DEMO_REPORT_DIR ?? "../playwright-report" },
    ],
  ],
  outputDir: process.env.DEMO_RESULTS_DIR ?? "../test-results",
  use: {
    viewport: { width: 1280, height: 800 },
    video: { mode: "on", size: { width: 1280, height: 800 } },
    trace: "on",
    colorScheme: "light",
  },
})
