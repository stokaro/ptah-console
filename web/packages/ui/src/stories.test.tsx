/// <reference types="vite/client" />
import { composeStories } from "@storybook/react-vite"
import { cleanup, render } from "@testing-library/react"
import { axe } from "jest-axe"
import type { ComponentType } from "react"
import { describe, expect, it } from "vitest"

type StoryModule = Parameters<typeof composeStories>[0]

// Every story is rendered and checked for accessibility, so a story is a
// test of the component rather than a picture of it.
const modules = import.meta.glob<StoryModule>("./*.stories.tsx", { eager: true })

const cases = Object.entries(modules).flatMap(([file, module]) =>
  Object.entries(composeStories(module)).map(([name, Story]) => ({
    title: `${file} ${name}`,
    Story: Story as unknown as ComponentType,
  }))
)

describe("stories", () => {
  it("exist for every component", () => {
    expect(cases.length).toBeGreaterThanOrEqual(20)
  })

  it.each(cases)("$title renders with no accessibility violations", async ({ Story }) => {
    // A component sits in a page's main region; the story gives it one.
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
