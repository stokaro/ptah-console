import { render, screen } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { Wordmark } from "./Wordmark"

describe("Wordmark", () => {
  it("shows the name and the caption", async () => {
    const { container } = render(<Wordmark caption="local" />)

    expect(screen.getByText("ptah")).toBeInTheDocument()
    expect(screen.getByText("local")).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("renders a caption as text, never as markup", () => {
    const { container } = render(<Wordmark caption="<script>alert(1)</script>" />)

    expect(screen.getByText("<script>alert(1)</script>")).toBeInTheDocument()
    expect(container.querySelector("script")).toBeNull()
  })
})
