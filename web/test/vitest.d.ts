// The matchers test/setup.ts registers at runtime, typed for Vitest 5.
//
// Vitest 5's Assertion takes two type parameters and extends Matchers<R, T>.
// jest-dom 7 still augments Assertion with one, which no longer merges, and
// jest-axe types itself against jest. Matchers is where Vitest 5 expects a
// matcher to be declared.
/* eslint-disable @typescript-eslint/no-unused-vars -- Matchers repeats Vitest's T to merge */
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers"
import "vitest"

declare module "vitest" {
  // A merged declaration repeats Vitest's type parameters exactly, used or not.
  interface Matchers<
    R extends void | Promise<void> = void | Promise<void>,
    T = unknown,
  > extends TestingLibraryMatchers<unknown, R> {
    toHaveNoViolations(): R
  }
}
