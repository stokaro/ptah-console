/// <reference types="vite/client" />
import { composeStories } from "@storybook/react-vite"
import { cleanup, render } from "@testing-library/react"
import { axe } from "jest-axe"
import type { ComponentType } from "react"
import { describe, expect, it } from "vitest"

type StoryModule = Parameters<typeof composeStories>[0]

// The app's own stories, rendered and checked for accessibility the way
// packages/ui/src/stories.test.tsx checks the components': a story is a test
// of the screen rather than a picture of it.
const modules = import.meta.glob<StoryModule>("./**/*.stories.tsx", { eager: true })

const cases = Object.entries(modules).flatMap(([file, module]) =>
  Object.entries(composeStories(module)).map(([name, Story]) => ({
    title: `${file} ${name}`,
    Story: Story as unknown as ComponentType,
  }))
)

describe("app stories", () => {
  it("exist", () => {
    expect(cases.length).toBeGreaterThanOrEqual(3)
  })

  it.each(cases)("$title renders with no accessibility violations", async ({ Story }) => {
    render(
      <main>
        <Story />
      </main>
    )
    expect(await axe(document.body)).toHaveNoViolations()
    expect(document.querySelector("script")).toBeNull()
    cleanup()
  })
})
