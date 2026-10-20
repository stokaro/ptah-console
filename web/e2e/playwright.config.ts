import { defineConfig } from "@playwright/test"

// The acceptance rows run against the environment `make e2e-up` built, each
// with a `ptah-console local` of its own. They run one at a time, in the order
// the file declares them, because each starts from the state the one before
// left. Every row is recorded; hack/e2e/film.sh cuts the waits and joins the
// recordings into one film.
export default defineConfig({
  testDir: ".",
  timeout: 25 * 60_000,
  retries: 0,
  workers: 1,
  fullyParallel: false,
  reporter: [["list"], ["html", { open: "never", outputFolder: "../playwright-report" }]],
  outputDir: "../test-results",
  use: {
    viewport: { width: 1280, height: 800 },
    video: { mode: "on", size: { width: 1280, height: 800 } },
    trace: "on",
    colorScheme: "light",
  },
})
