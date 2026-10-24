import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Icon, iconNames } from "./Icon"

describe("Icon", () => {
  it("is decoration that assistive technology skips", () => {
    const { container } = render(<Icon name="overview" />)
    const svg = container.querySelector("svg")

    expect(svg).toHaveAttribute("aria-hidden", "true")
    expect(svg).toHaveAttribute("focusable", "false")
  })

  it("draws every name it offers", () => {
    expect(iconNames.length).toBeGreaterThan(0)
    for (const name of iconNames) {
      const { container, unmount } = render(<Icon name={name} />)
      expect(container.querySelector("path")?.getAttribute("d")).toMatch(/^M/)
      unmount()
    }
  })
})
