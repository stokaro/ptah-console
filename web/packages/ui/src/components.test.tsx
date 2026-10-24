import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it, vi } from "vitest"

import { CommandBlock } from "./CommandBlock"
import { Fingerprint } from "./Fingerprint"
import { Freshness } from "./Freshness"
import { ScrollRegion } from "./ScrollRegion"
import { Segmented } from "./Segmented"
import { Select } from "./Select"
import { SequenceTable } from "./SequenceTable"
import { StepDots } from "./StepDots"
import { Tabs } from "./Tabs"
import { Terminal } from "./Terminal"

const fingerprint = "sha256:2069e386bdca701da7782dd0b2b29bd5c79151d90bcfc52f30a9678f97ac2aac"

describe("Fingerprint", () => {
  it("shows the value shortened and copies all of it", async () => {
    const user = userEvent.setup()
    const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue()
    render(<Fingerprint value={fingerprint} />)

    expect(screen.getByText("fingerprint sha256:2069e386…97ac2aac")).toHaveAttribute(
      "title",
      fingerprint
    )
    await user.click(screen.getByRole("button", { name: `Copy fingerprint ${fingerprint}` }))

    expect(write).toHaveBeenCalledWith(fingerprint)
    expect(screen.getByRole("status")).toHaveTextContent("Copied.")
  })

  it("says so when the browser refuses to copy", async () => {
    const user = userEvent.setup()
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error("denied"))
    render(<Fingerprint value={fingerprint} />)

    await user.click(screen.getByRole("button"))

    expect(screen.getByRole("status")).toHaveTextContent("The browser did not allow copying.")
  })
})

describe("CommandBlock", () => {
  it("copies the command as written", async () => {
    const user = userEvent.setup()
    const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue()
    render(<CommandBlock command="kubectl ptah plan storefront -n application" />)

    await user.click(screen.getByRole("button", { name: "Copy" }))

    expect(write).toHaveBeenCalledWith("kubectl ptah plan storefront -n application")
  })
})

describe("Terminal", () => {
  it("shows markup and terminal escapes as text", () => {
    const { container } = render(
      <Terminal label="Log" text={"<b>bold</b> \u001b[31mred\u001b[0m"} />
    )

    expect(screen.getByLabelText("Log").textContent).toBe("<b>bold</b> \u001b[31mred\u001b[0m")
    expect(container.querySelector("b")).toBeNull()
  })
})

describe("Freshness", () => {
  it("says how long ago the page was current", () => {
    render(
      <Freshness
        state="live"
        at={new Date("2026-10-20T10:00:18Z")}
        now={new Date("2026-10-20T10:00:30Z")}
      />
    )

    expect(screen.getByRole("status")).toHaveTextContent("Live · current 12 s ago")
  })
})

describe("SequenceTable", () => {
  it("names its columns for a screen reader", () => {
    render(
      <SequenceTable
        caption="Steps in order"
        rows={[{ version: "014", name: "a.sql", note: "3e1a9c2" }]}
      />
    )

    expect(screen.getByRole("table", { name: "Steps in order" })).toBeInTheDocument()
    expect(screen.getByRole("columnheader", { name: "Checksum" })).toBeInTheDocument()
  })
})

describe("Select", () => {
  it("reports the value chosen, not the event", async () => {
    const chosen: string[] = []
    render(
      <Select
        aria-label="Engine"
        value="PostgreSQL"
        onChange={(value) => chosen.push(value)}
        options={[
          { value: "PostgreSQL", label: "PostgreSQL" },
          { value: "MySQL", label: "MySQL" },
        ]}
      />
    )

    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Engine" }), "MySQL")

    expect(chosen).toEqual(["MySQL"])
  })
})

describe("ScrollRegion", () => {
  it("is a named stop in the tab order, so a keyboard can scroll a table with nothing to focus", async () => {
    const user = userEvent.setup()
    render(
      <ScrollRegion label="Phases of storefront">
        <table>
          <caption>Phases of storefront</caption>
          <tbody>
            <tr>
              <td>Approve: waiting</td>
            </tr>
          </tbody>
        </table>
      </ScrollRegion>
    )

    await user.tab()

    expect(screen.getByRole("group", { name: "Phases of storefront" })).toHaveFocus()
  })
})

describe("Segmented", () => {
  it("presses the current choice and reports another", async () => {
    const user = userEvent.setup()
    const change = vi.fn()
    render(
      <Segmented
        label="Kind"
        value="all"
        onChange={change}
        options={[
          { value: "all", label: "All", count: 3 },
          { value: "schema", label: "Schemas", count: 2 },
        ]}
      />
    )

    const group = screen.getByRole("group", { name: "Kind" })
    expect(group).toContainElement(screen.getByRole("button", { name: "All 3", pressed: true }))
    expect(screen.getByRole("button", { name: "Schemas 2", pressed: false })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Schemas 2" }))

    expect(change).toHaveBeenCalledWith("schema")
  })
})

describe("StepDots", () => {
  it("reads the whole lifecycle as one sentence", () => {
    render(
      <StepDots
        label="Lifecycle of billing"
        steps={[
          { label: "Plan", tone: "ok" },
          { label: "Apply", tone: "bad" },
        ]}
      />
    )

    expect(
      screen.getByRole("img", { name: "Lifecycle of billing. Plan: done, Apply: failed" })
    ).toBeInTheDocument()
  })
})

describe("Tabs", () => {
  function Panels() {
    const [value, setValue] = useState<"history" | "events" | "configuration">("history")
    return (
      <Tabs
        label="Sections of billing"
        value={value}
        onChange={setValue}
        tabs={[
          { value: "history", label: "History" },
          { value: "events", label: "Events", count: 3 },
          { value: "configuration", label: "Configuration" },
        ]}
      >
        <p>The {value} panel.</p>
      </Tabs>
    )
  }

  it("opens one panel at a time, by click and by the keyboard", async () => {
    const user = userEvent.setup()
    render(<Panels />)

    const list = screen.getByRole("tablist", { name: "Sections of billing" })
    expect(screen.getByRole("tab", { name: "History" })).toHaveAttribute("aria-selected", "true")
    expect(screen.getByRole("tabpanel")).toHaveTextContent("The history panel.")
    expect(list).toContainElement(screen.getByRole("tab", { name: "Events 3" }))

    await user.click(screen.getByRole("tab", { name: "Events 3" }))
    expect(screen.getByRole("tabpanel")).toHaveTextContent("The events panel.")

    await user.keyboard("{ArrowRight}{Enter}")
    expect(screen.getByRole("tab", { name: "Configuration" })).toHaveAttribute(
      "aria-selected",
      "true"
    )
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1)
    expect(screen.getByRole("tabpanel")).toHaveTextContent("The configuration panel.")
  })
})
