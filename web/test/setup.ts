import "@testing-library/jest-dom/vitest"
import { cleanup, configure } from "@testing-library/react"
import { toHaveNoViolations } from "jest-axe"
import { afterEach, expect } from "vitest"

import { settleRouters } from "./routers"

expect.extend(toHaveNoViolations)

// A screen is a lazily imported chunk. The first test in a worker to open one
// waits for Vite to transform it, which under a full parallel run takes more
// than the default one second, and a findBy* then failed whichever test came
// first. The waits poll for their condition, so a higher ceiling costs a
// passing test nothing.
configure({ asyncUtilTimeout: 5_000 })

// A router still loading when its tree unmounts commits later, possibly into
// a torn-down jsdom, so the routers settle first (test/routers.ts).
afterEach(async () => {
  try {
    await settleRouters()
  } finally {
    cleanup()
  }
})
