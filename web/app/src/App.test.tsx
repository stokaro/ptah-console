import { render, screen } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { App } from "./App"

describe("App", () => {
  it("names itself and has no accessibility violations", async () => {
    const { container } = render(<App />)

    expect(screen.getByRole("heading", { level: 1, name: "Ptah Console" })).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })
})
